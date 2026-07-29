package uitest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	ErrorApplicationStart = "application_start"
	ErrorHealthTimeout    = "health_timeout"
	ErrorSetup            = "setup"
	ErrorBrowser          = "browser"
	ErrorPolicy           = "policy"
	ErrorCancelled        = "cancelled"

	defaultStartupLimit = 120 * time.Second
	defaultSetupLimit   = 60 * time.Second
	defaultSessionLimit = 20 * time.Minute
)

type LifecycleError struct {
	Class string
	Op    string
	Err   error
}

func (e *LifecycleError) Error() string {
	if e.Op == "" {
		return fmt.Sprintf("%s: %v", e.Class, e.Err)
	}
	return fmt.Sprintf("%s: %s: %v", e.Class, e.Op, e.Err)
}

func (e *LifecycleError) Unwrap() error { return e.Err }

type ReadyRuntime struct {
	Directory string
}

type SessionOptions struct {
	WorkDir      string
	TaskID       string
	Runtime      ReadyRuntime
	Config       ResolvedConfig
	ArtifactDir  string
	StartupLimit time.Duration
	SetupLimit   time.Duration
	SessionLimit time.Duration
}

type Session struct {
	opts     SessionOptions
	registry *processRegistry

	context context.Context
	cancel  context.CancelCauseFunc
	timerMu sync.Mutex
	timer   *time.Timer

	initOnce    sync.Once
	initErr     error
	lifecycleMu sync.Mutex
	closeOnce   sync.Once
	closeErr    error
}

func NewSession(opts SessionOptions, logger *slog.Logger) (*Session, error) {
	normalized, err := normalizeSessionOptions(opts)
	if err != nil {
		return nil, lifecycleError(ErrorPolicy, "validate session", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	registry, err := newProcessRegistry(normalized.WorkDir, normalized.TaskID, logger)
	if err != nil {
		return nil, lifecycleError(ErrorPolicy, "create session", err)
	}
	sessionContext, cancel := context.WithCancelCause(context.Background())
	session := &Session{
		opts: normalized, registry: registry,
		context: sessionContext, cancel: cancel,
	}
	session.timerMu.Lock()
	session.timer = time.AfterFunc(normalized.SessionLimit, func() {
		session.shutdown(context.DeadlineExceeded)
	})
	session.timerMu.Unlock()
	return session, nil
}

func (s *Session) EnsureReady(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if cause := context.Cause(s.context); cause != nil {
		return sessionEndedError(cause)
	}
	s.initOnce.Do(func() {
		s.lifecycleMu.Lock()
		linkedContext, cancel := context.WithCancelCause(ctx)
		stop := context.AfterFunc(s.context, func() {
			cancel(context.Cause(s.context))
		})
		s.initErr = s.initialize(linkedContext)
		stop()
		cancel(nil)
		s.lifecycleMu.Unlock()
		if s.initErr != nil {
			s.shutdown(s.initErr)
		}
	})
	if s.initErr != nil {
		return s.initErr
	}
	if cause := context.Cause(s.context); cause != nil {
		return sessionEndedError(cause)
	}
	return nil
}

func (s *Session) Close() error {
	s.shutdown(context.Canceled)
	return s.closeErr
}

func (s *Session) shutdown(cause error) {
	s.closeOnce.Do(func() {
		s.timerMu.Lock()
		if s.timer != nil {
			s.timer.Stop()
		}
		s.timerMu.Unlock()
		s.cancel(cause)
		s.lifecycleMu.Lock()
		s.closeErr = s.registry.cleanup()
		s.lifecycleMu.Unlock()
	})
}

func (s *Session) initialize(ctx context.Context) error {
	if err := firstCancellation(ctx, s.context); err != nil {
		return lifecycleError(ErrorCancelled, "initialize", err)
	}
	appLog := filepath.Join(s.opts.ArtifactDir, "app.log")
	app, err := startManagedProcess(
		s.registry, "app", s.opts.Config.StartCommand,
		s.opts.WorkDir, os.Environ(), appLog,
	)
	if err != nil {
		if cancellation := firstCancellation(ctx, s.context); cancellation != nil {
			return lifecycleError(ErrorCancelled, "start application", cancellation)
		}
		return lifecycleError(ErrorApplicationStart, "start application", err)
	}
	if err := s.pollHealth(ctx, app); err != nil {
		return err
	}
	if cancellation := firstCancellation(ctx, s.context); cancellation != nil {
		return lifecycleError(ErrorCancelled, "initialize storage state", cancellation)
	}

	statePath := filepath.Join(s.opts.WorkDir, ".multica", "ui-test", s.opts.TaskID, "storage-state.json")
	if err := writeEmptyStorageState(statePath); err != nil {
		return lifecycleError(ErrorSetup, "write empty storage state", err)
	}
	if cancellation := firstCancellation(ctx, s.context); cancellation != nil {
		return lifecycleError(ErrorCancelled, "initialize storage state", cancellation)
	}
	if s.opts.Config.SetupCommand != "" {
		if err := s.runSetup(ctx, app, statePath); err != nil {
			return err
		}
	}
	if cancellation := firstCancellation(ctx, s.context); cancellation != nil {
		return lifecycleError(ErrorCancelled, "validate storage state", cancellation)
	}
	if err := validateStorageState(statePath); err != nil {
		return lifecycleError(ErrorPolicy, "validate storage state", err)
	}
	select {
	case <-app.done:
		return lifecycleError(ErrorApplicationStart, "application exited", app.result())
	default:
	}
	go func() {
		<-app.done
		s.shutdown(lifecycleError(ErrorApplicationStart, "application exited", app.result()))
	}()
	return nil
}

func (s *Session) pollHealth(ctx context.Context, app *managedProcess) error {
	healthContext, cancel := context.WithTimeout(ctx, s.opts.StartupLimit)
	defer cancel()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return &redirectPolicyError{err: errors.New("too many health redirects")}
			}
			if _, err := ValidateLoopbackURL(request.URL.String()); err != nil {
				return &redirectPolicyError{err: err}
			}
			if !sameOrigin(s.opts.Config.BaseURL, request.URL) {
				return &redirectPolicyError{err: fmt.Errorf("health redirect changed configured origin")}
			}
			return nil
		},
	}
	defer transport.CloseIdleConnections()

	started := time.Now()
	for {
		if cancellation := firstCancellation(ctx, s.context); cancellation != nil {
			return lifecycleError(ErrorCancelled, "health check", cancellation)
		}
		select {
		case <-app.done:
			if cancellation := firstCancellation(ctx, s.context); cancellation != nil {
				return lifecycleError(ErrorCancelled, "health check", cancellation)
			}
			return lifecycleError(ErrorApplicationStart, "application exited before health", app.result())
		case <-healthContext.Done():
			if ctx.Err() != nil {
				return lifecycleError(ErrorCancelled, "health check", context.Cause(ctx))
			}
			return lifecycleError(ErrorHealthTimeout, "health check", context.DeadlineExceeded)
		default:
		}

		request, err := http.NewRequestWithContext(healthContext, http.MethodGet, s.opts.Config.HealthURL.String(), nil)
		if err != nil {
			return lifecycleError(ErrorPolicy, "build health request", err)
		}
		response, requestErr := client.Do(request)
		if response != nil {
			drainAndClose(response.Body)
		}
		var redirectErr *redirectPolicyError
		if errors.As(requestErr, &redirectErr) {
			return lifecycleError(ErrorPolicy, "health redirect", redirectErr.err)
		}
		if requestErr == nil && response.StatusCode >= 200 && response.StatusCode <= 399 {
			return nil
		}

		delay := 250 * time.Millisecond
		if time.Since(started) >= 2*time.Second {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-app.done:
			timer.Stop()
			return lifecycleError(ErrorApplicationStart, "application exited before health", app.result())
		case <-healthContext.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return lifecycleError(ErrorCancelled, "health check", context.Cause(ctx))
			}
			return lifecycleError(ErrorHealthTimeout, "health check", context.DeadlineExceeded)
		case <-timer.C:
		}
	}
}

func (s *Session) runSetup(ctx context.Context, app *managedProcess, statePath string) error {
	setupContext, cancel := context.WithTimeout(ctx, s.opts.SetupLimit)
	defer cancel()
	setupEnvironment := replaceEnvironment(os.Environ(), map[string]string{
		"MULTICA_UI_TEST_BASE_URL":      s.opts.Config.BaseURL.String(),
		"MULTICA_UI_TEST_STORAGE_STATE": statePath,
		"MULTICA_UI_TEST_ARTIFACT_DIR":  s.opts.ArtifactDir,
		"MULTICA_UI_TEST_TASK_ID":       s.opts.TaskID,
	})
	setup, err := startManagedProcess(
		s.registry, "setup", s.opts.Config.SetupCommand,
		s.opts.WorkDir, setupEnvironment, filepath.Join(s.opts.ArtifactDir, "setup.log"),
	)
	if err != nil {
		return lifecycleError(ErrorSetup, "start setup", err)
	}
	select {
	case <-app.done:
		_ = setup.stop()
		if cancellation := firstCancellation(ctx, s.context); cancellation != nil {
			return lifecycleError(ErrorCancelled, "setup", cancellation)
		}
		return lifecycleError(ErrorApplicationStart, "application exited during setup", app.result())
	case <-setup.done:
		if err := setup.result(); err != nil {
			return lifecycleError(ErrorSetup, "run setup", err)
		}
		return nil
	case <-setupContext.Done():
		_ = setup.stop()
		if ctx.Err() != nil {
			return lifecycleError(ErrorCancelled, "setup", context.Cause(ctx))
		}
		return lifecycleError(ErrorSetup, "setup timeout", context.DeadlineExceeded)
	}
}

type redirectPolicyError struct {
	err error
}

func (e *redirectPolicyError) Error() string { return e.err.Error() }

func normalizeSessionOptions(opts SessionOptions) (SessionOptions, error) {
	workDir, taskID, err := validateTaskLocation(opts.WorkDir, opts.TaskID)
	if err != nil {
		return SessionOptions{}, err
	}
	opts.WorkDir = workDir
	opts.TaskID = taskID
	opts.Config.StartCommand = strings.TrimSpace(opts.Config.StartCommand)
	opts.Config.SetupCommand = strings.TrimSpace(opts.Config.SetupCommand)
	if opts.Config.StartCommand == "" || opts.Config.BaseURL == nil || opts.Config.HealthURL == nil {
		return SessionOptions{}, fmt.Errorf("session requires start, base URL, and health URL")
	}
	baseURL, err := ValidateLoopbackURL(opts.Config.BaseURL.String())
	if err != nil {
		return SessionOptions{}, err
	}
	healthURL, err := ValidateLoopbackURL(opts.Config.HealthURL.String())
	if err != nil {
		return SessionOptions{}, err
	}
	if !sameOrigin(baseURL, healthURL) {
		return SessionOptions{}, fmt.Errorf("health URL must use configured application origin")
	}
	opts.Config.BaseURL = baseURL
	opts.Config.HealthURL = healthURL

	artifactRoot := filepath.Join(workDir, ".multica", "artifacts", "ui-test", taskID)
	if opts.ArtifactDir == "" {
		opts.ArtifactDir = artifactRoot
	} else {
		opts.ArtifactDir, err = filepath.Abs(opts.ArtifactDir)
		if err != nil {
			return SessionOptions{}, fmt.Errorf("resolve artifact directory: %w", err)
		}
		relative, err := filepath.Rel(artifactRoot, opts.ArtifactDir)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return SessionOptions{}, fmt.Errorf("artifact directory must stay inside task artifact root")
		}
	}
	if opts.StartupLimit < 0 || opts.SetupLimit < 0 || opts.SessionLimit < 0 {
		return SessionOptions{}, fmt.Errorf("session time limits must not be negative")
	}
	if opts.StartupLimit == 0 {
		opts.StartupLimit = defaultStartupLimit
	}
	if opts.SetupLimit == 0 {
		opts.SetupLimit = defaultSetupLimit
	}
	if opts.SessionLimit == 0 {
		opts.SessionLimit = defaultSessionLimit
	}
	return opts, nil
}

func sameOrigin(first, second *url.URL) bool {
	if first == nil || second == nil ||
		!strings.EqualFold(first.Scheme, second.Scheme) ||
		!strings.EqualFold(first.Hostname(), second.Hostname()) {
		return false
	}
	return originPort(first) == originPort(second)
}

func originPort(value *url.URL) string {
	if value.Port() != "" {
		return value.Port()
	}
	if strings.EqualFold(value.Scheme, "http") {
		return "80"
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return ""
}

func replaceEnvironment(environment []string, replacements map[string]string) []string {
	result := make([]string, 0, len(environment)+len(replacements))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, replace := replacements[key]; replace {
				continue
			}
		}
		result = append(result, entry)
	}
	for key, value := range replacements {
		result = append(result, key+"="+value)
	}
	return result
}

func lifecycleError(class, operation string, err error) error {
	if err == nil {
		err = errors.New("unknown lifecycle failure")
	}
	return &LifecycleError{Class: class, Op: operation, Err: err}
}

func firstCancellation(contexts ...context.Context) error {
	for _, candidate := range contexts {
		if candidate != nil {
			if cause := context.Cause(candidate); cause != nil {
				return cause
			}
		}
	}
	return nil
}

func sessionEndedError(cause error) error {
	var lifecycleErr *LifecycleError
	if errors.As(cause, &lifecycleErr) {
		return cause
	}
	return lifecycleError(ErrorCancelled, "session ended", cause)
}
