package dettools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const (
	uiTestPublicationJournalName    = ".ui-test-report.transaction.json"
	uiTestPublicationPendingName    = ".ui-test-report.transaction.pending"
	uiTestPublicationCommitName     = ".ui-test-report.transaction.commit"
	uiTestPublicationJournalVersion = 1
	uiTestPublicationMaxStateBytes  = 16 << 10
)

var (
	errUITestPublicationInterrupted = errors.New("UI test report publication interrupted")
	errUITestPublicationRecovery    = errors.New("UI test report publication recovery failed")
	uiTestPublicationTokenPattern   = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

type uiTestPublicationFailpoint func(point string) error

type uiTestPublicationFailpointContextKey struct{}

type uiTestPublicationJournal struct {
	Version int                            `json:"version"`
	Token   string                         `json:"token"`
	Items   []uiTestPublicationJournalItem `json:"items"`
}

type uiTestPublicationJournalItem struct {
	Name      string `json:"name"`
	Directory bool   `json:"directory"`
	HadPrior  bool   `json:"had_prior"`
}

type uiTestPublicationCommit struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
}

func publishUITestOutputsJournaled(
	ctx context.Context,
	root *os.Root,
	evidence []uiSealedEvidence,
	outputs []uiReportOutput,
) (cleanupPending bool, err error) {
	states, err := prepareUITestPublicationStates(root, outputs)
	if err != nil {
		return false, err
	}
	token, err := uiTestTempSuffix()
	if err != nil {
		return false, err
	}
	for i := range states {
		states[i].temp = "." + states[i].name + ".tmp-" + token
		states[i].backup = "." + states[i].name + ".bak-" + token
	}
	journal := uiTestPublicationJournal{
		Version: uiTestPublicationJournalVersion,
		Token:   token,
		Items:   make([]uiTestPublicationJournalItem, len(states)),
	}
	for i, item := range states {
		journal.Items[i] = uiTestPublicationJournalItem{
			Name: item.name, Directory: item.directory, HadPrior: item.hadPrior,
		}
	}
	if err := createUITestPublicationJournal(ctx, root, journal); err != nil {
		return false, err
	}
	abandonOrRollback := func(cause error) (bool, error) {
		if errors.Is(cause, errUITestPublicationInterrupted) {
			return false, cause
		}
		recoveryErr := recoverUITestPublication(context.Background(), root)
		return false, errors.Join(cause, recoveryErr)
	}

	if err := runUITestPublicationFailpoint(ctx, "before staging"); err != nil {
		return false, err
	}
	if err := stageUITestEvidence(root, states[0].temp, evidence); err != nil {
		return abandonOrRollback(err)
	}
	for i, output := range outputs {
		if err := writeUITestStagedFile(root, states[i+1].temp, output.Content, 0o644); err != nil {
			return abandonOrRollback(fmt.Errorf("stage %s: %w", states[i+1].name, err))
		}
	}
	if err := syncUITestStagedPublication(root, states); err != nil {
		return abandonOrRollback(err)
	}
	if err := runUITestPublicationFailpoint(ctx, "after staging"); err != nil {
		return false, err
	}

	for i := range states {
		item := &states[i]
		if !item.hadPrior {
			continue
		}
		if err := renameUITestPublicationBoundary(
			ctx, root, item.name, item.backup, "backup "+item.name,
		); err != nil {
			return abandonOrRollback(fmt.Errorf("backup %s: %w", item.name, err))
		}
	}
	for i := range states {
		item := &states[i]
		if err := renameUITestPublicationBoundary(
			ctx, root, item.temp, item.name, "install "+item.name,
		); err != nil {
			return abandonOrRollback(fmt.Errorf("install %s: %w", item.name, err))
		}
	}
	if err := createUITestPublicationCommit(ctx, root, token); err != nil {
		committed, validationErr := isCommittedUITestPublication(root, token, states)
		if validationErr != nil {
			return false, errors.Join(err, validationErr)
		}
		if committed {
			return true, nil
		}
		if errors.Is(err, errUITestPublicationInterrupted) {
			return false, err
		}
		return abandonOrRollback(err)
	}
	if err := cleanupCommittedUITestPublication(ctx, root, journal); err != nil {
		if validationErr := validateCommittedUITestPublication(root, states); validationErr != nil {
			return false, errors.Join(err, validationErr)
		}
		return true, nil
	}
	return false, nil
}

func isCommittedUITestPublication(
	root *os.Root,
	token string,
	states []uiReportPublishState,
) (bool, error) {
	exists, err := uiTestPublicationPathExists(root, uiTestPublicationCommitName)
	if err != nil || !exists {
		return false, err
	}
	var marker uiTestPublicationCommit
	if err := readUITestPublicationJSON(root, uiTestPublicationCommitName, &marker); err != nil {
		return false, err
	}
	if err := validateUITestPublicationCommit(marker); err != nil {
		return false, err
	}
	if marker.Token != token {
		return false, fmt.Errorf("publication commit marker token differs")
	}
	if err := validateCommittedUITestPublication(root, states); err != nil {
		return false, err
	}
	return true, nil
}

func prepareUITestPublicationStates(root *os.Root, outputs []uiReportOutput) ([]uiReportPublishState, error) {
	expectedOutputs := []string{
		uiTestReportJSONName,
		uiTestReportMarkdownName,
		uiTestManifestName,
		uiTestCommentName,
	}
	if len(outputs) != len(expectedOutputs) {
		return nil, fmt.Errorf("UI test publication requires four generated outputs")
	}
	states := []uiReportPublishState{{
		name: uiTestPublishedDir, directory: true,
	}}
	for i, expected := range expectedOutputs {
		if outputs[i].Name != expected {
			return nil, fmt.Errorf("unexpected UI test output %q at index %d", outputs[i].Name, i)
		}
		states = append(states, uiReportPublishState{name: expected})
	}
	for i := range states {
		exists, err := validateUITestPublicationPathType(root, states[i].name, states[i].directory)
		if err != nil {
			return nil, fmt.Errorf("inspect existing output %s: %w", states[i].name, err)
		}
		states[i].hadPrior = exists
	}
	return states, nil
}

func createUITestPublicationJournal(
	ctx context.Context,
	root *os.Root,
	journal uiTestPublicationJournal,
) error {
	if err := validateUITestPublicationJournal(journal); err != nil {
		return err
	}
	for _, name := range []string{
		uiTestPublicationJournalName,
		uiTestPublicationPendingName,
		uiTestPublicationCommitName,
	} {
		if exists, err := uiTestPublicationPathExists(root, name); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("publication state %s already exists", name)
		}
	}
	if err := writeDurableUITestPublicationJSON(root, uiTestPublicationPendingName, journal); err != nil {
		return err
	}
	if err := runUITestPublicationFailpoint(ctx, "before journal"); err != nil {
		return err
	}
	if err := root.Rename(uiTestPublicationPendingName, uiTestPublicationJournalName); err != nil {
		return fmt.Errorf("install publication journal: %w", err)
	}
	if err := syncUITestDirectory(root, "."); err != nil {
		return fmt.Errorf("sync publication journal rename: %w", err)
	}
	return runUITestPublicationFailpoint(ctx, "after journal")
}

func createUITestPublicationCommit(ctx context.Context, root *os.Root, token string) error {
	state := uiTestPublicationCommit{Version: uiTestPublicationJournalVersion, Token: token}
	temp := uiTestPublicationCommitTempName(token)
	if err := writeDurableUITestPublicationJSON(root, temp, state); err != nil {
		return err
	}
	if err := runUITestPublicationFailpoint(ctx, "before commit marker"); err != nil {
		return err
	}
	if err := root.Rename(temp, uiTestPublicationCommitName); err != nil {
		return fmt.Errorf("install publication commit marker: %w", err)
	}
	if err := syncUITestDirectory(root, "."); err != nil {
		return fmt.Errorf("sync publication commit marker: %w", err)
	}
	return runUITestPublicationFailpoint(ctx, "after commit marker")
}

func recoverUITestPublication(ctx context.Context, root *os.Root) error {
	journalExists, err := uiTestPublicationPathExists(root, uiTestPublicationJournalName)
	if err != nil {
		return err
	}
	pendingExists, err := uiTestPublicationPathExists(root, uiTestPublicationPendingName)
	if err != nil {
		return err
	}
	markerExists, err := uiTestPublicationPathExists(root, uiTestPublicationCommitName)
	if err != nil {
		return err
	}
	if pendingExists {
		if journalExists || markerExists {
			return fmt.Errorf("conflicting UI test publication state")
		}
		var pending uiTestPublicationJournal
		if err := readUITestPublicationJSON(root, uiTestPublicationPendingName, &pending); err != nil {
			return fmt.Errorf("read pending publication journal: %w", err)
		}
		if err := validateUITestPublicationJournal(pending); err != nil {
			return err
		}
		return removeUITestPublicationBoundary(
			ctx, root, uiTestPublicationPendingName, false, "cleanup pending journal",
		)
	}
	if !journalExists {
		if !markerExists {
			return nil
		}
		var marker uiTestPublicationCommit
		if err := readUITestPublicationJSON(root, uiTestPublicationCommitName, &marker); err != nil {
			return fmt.Errorf("read orphan publication commit marker: %w", err)
		}
		if err := validateUITestPublicationCommit(marker); err != nil {
			return err
		}
		if err := validateCommittedUITestPublication(root, nil); err != nil {
			return err
		}
		return removeUITestPublicationBoundary(
			ctx, root, uiTestPublicationCommitName, false, "cleanup marker",
		)
	}

	var journal uiTestPublicationJournal
	if err := readUITestPublicationJSON(root, uiTestPublicationJournalName, &journal); err != nil {
		return fmt.Errorf("read publication journal: %w", err)
	}
	if err := validateUITestPublicationJournal(journal); err != nil {
		return err
	}
	states := uiTestPublicationStatesFromJournal(journal)
	if err := validateUITestPublicationResidue(root, states); err != nil {
		return err
	}
	if !markerExists {
		return rollbackUITestPublicationJournal(ctx, root, journal, states)
	}
	var marker uiTestPublicationCommit
	if err := readUITestPublicationJSON(root, uiTestPublicationCommitName, &marker); err != nil {
		return fmt.Errorf("read publication commit marker: %w", err)
	}
	if err := validateUITestPublicationCommit(marker); err != nil {
		return err
	}
	if marker.Token != journal.Token {
		return fmt.Errorf("publication journal and commit marker tokens differ")
	}
	if err := validateCommittedUITestPublication(root, states); err != nil {
		return err
	}
	return cleanupCommittedUITestPublication(ctx, root, journal)
}

func rollbackUITestPublicationJournal(
	ctx context.Context,
	root *os.Root,
	journal uiTestPublicationJournal,
	states []uiReportPublishState,
) error {
	for i := len(states) - 1; i >= 0; i-- {
		item := states[i]
		backupExists, err := uiTestPublicationPathExists(root, item.backup)
		if err != nil {
			return err
		}
		canonicalExists, err := uiTestPublicationPathExists(root, item.name)
		if err != nil {
			return err
		}
		if item.hadPrior && backupExists {
			if canonicalExists {
				if err := removeUITestPublicationBoundary(
					ctx, root, item.name, item.directory, "recovery canonical "+item.name,
				); err != nil {
					return err
				}
			}
			if err := renameUITestPublicationBoundary(
				ctx, root, item.backup, item.name, "recovery restore "+item.name,
			); err != nil {
				return err
			}
		} else if !item.hadPrior && canonicalExists {
			if err := removeUITestPublicationBoundary(
				ctx, root, item.name, item.directory, "recovery canonical "+item.name,
			); err != nil {
				return err
			}
		}
		if err := removeUITestPublicationBoundary(
			ctx, root, item.temp, item.directory, "recovery temp "+item.name,
		); err != nil {
			return err
		}
	}
	if err := removeUITestPublicationBoundary(
		ctx,
		root,
		uiTestPublicationCommitTempName(journal.Token),
		false,
		"recovery commit temp",
	); err != nil {
		return err
	}
	return removeUITestPublicationBoundary(
		ctx, root, uiTestPublicationJournalName, false, "recovery journal",
	)
}

func cleanupCommittedUITestPublication(
	ctx context.Context,
	root *os.Root,
	journal uiTestPublicationJournal,
) error {
	states := uiTestPublicationStatesFromJournal(journal)
	if err := validateCommittedUITestPublication(root, states); err != nil {
		return err
	}
	if err := validateUITestPublicationResidue(root, states); err != nil {
		return err
	}
	for _, item := range states {
		if err := removeUITestPublicationBoundary(
			ctx, root, item.backup, item.directory, "cleanup backup "+item.name,
		); err != nil {
			return err
		}
		if err := removeUITestPublicationBoundary(
			ctx, root, item.temp, item.directory, "cleanup temp "+item.name,
		); err != nil {
			return err
		}
	}
	if err := removeUITestPublicationBoundary(
		ctx,
		root,
		uiTestPublicationCommitTempName(journal.Token),
		false,
		"cleanup commit temp",
	); err != nil {
		return err
	}
	if err := removeUITestPublicationBoundary(
		ctx, root, uiTestPublicationJournalName, false, "cleanup journal",
	); err != nil {
		return err
	}
	return removeUITestPublicationBoundary(
		ctx, root, uiTestPublicationCommitName, false, "cleanup marker",
	)
}

func validateUITestPublicationJournal(journal uiTestPublicationJournal) error {
	if journal.Version != uiTestPublicationJournalVersion {
		return fmt.Errorf("unsupported publication journal version %d", journal.Version)
	}
	if !uiTestPublicationTokenPattern.MatchString(journal.Token) {
		return fmt.Errorf("invalid publication journal token")
	}
	expected := []struct {
		name      string
		directory bool
	}{
		{name: uiTestPublishedDir, directory: true},
		{name: uiTestReportJSONName},
		{name: uiTestReportMarkdownName},
		{name: uiTestManifestName},
		{name: uiTestCommentName},
	}
	if len(journal.Items) != len(expected) {
		return fmt.Errorf("publication journal must contain five canonical items")
	}
	for i, item := range journal.Items {
		if item.Name != expected[i].name || item.Directory != expected[i].directory {
			return fmt.Errorf("invalid publication journal item %d", i)
		}
	}
	return nil
}

func validateUITestPublicationCommit(marker uiTestPublicationCommit) error {
	if marker.Version != uiTestPublicationJournalVersion {
		return fmt.Errorf("unsupported publication commit version %d", marker.Version)
	}
	if !uiTestPublicationTokenPattern.MatchString(marker.Token) {
		return fmt.Errorf("invalid publication commit token")
	}
	return nil
}

func uiTestPublicationStatesFromJournal(journal uiTestPublicationJournal) []uiReportPublishState {
	states := make([]uiReportPublishState, len(journal.Items))
	for i, item := range journal.Items {
		states[i] = uiReportPublishState{
			name:      item.Name,
			temp:      "." + item.Name + ".tmp-" + journal.Token,
			backup:    "." + item.Name + ".bak-" + journal.Token,
			directory: item.Directory,
			hadPrior:  item.HadPrior,
		}
	}
	return states
}

func validateUITestPublicationResidue(root *os.Root, states []uiReportPublishState) error {
	for _, item := range states {
		canonical, err := validateUITestPublicationPathType(root, item.name, item.directory)
		if err != nil {
			return err
		}
		backup, err := validateUITestPublicationPathType(root, item.backup, item.directory)
		if err != nil {
			return err
		}
		if _, err := validateUITestPublicationPathType(root, item.temp, item.directory); err != nil {
			return err
		}
		if item.hadPrior && !canonical && !backup {
			return fmt.Errorf("prior publication item %s has no canonical or backup", item.name)
		}
		if !item.hadPrior && backup {
			return fmt.Errorf("publication item %s has unexpected backup", item.name)
		}
	}
	return nil
}

func validateCommittedUITestPublication(root *os.Root, states []uiReportPublishState) error {
	if states == nil {
		states = []uiReportPublishState{
			{name: uiTestPublishedDir, directory: true},
			{name: uiTestReportJSONName},
			{name: uiTestReportMarkdownName},
			{name: uiTestManifestName},
			{name: uiTestCommentName},
		}
	}
	for _, item := range states {
		exists, err := validateUITestPublicationPathType(root, item.name, item.directory)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("committed publication item %s is missing", item.name)
		}
	}
	return nil
}

func validateUITestPublicationPathType(root *os.Root, name string, directory bool) (bool, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if directory && !info.IsDir() {
		return false, fmt.Errorf("%s is not a directory", name)
	}
	if !directory && !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s is not a regular file", name)
	}
	return true, nil
}

func readUITestPublicationJSON(root *os.Root, name string, value any) error {
	before, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", name)
	}
	file, err := openUITestEvidence(root, name)
	if err != nil {
		return err
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		_ = file.Close()
		return fmt.Errorf("%s identity changed during open", name)
	}
	links, err := uiTestEvidenceLinkCount(file, opened)
	if err != nil || links != 1 {
		_ = file.Close()
		return errors.Join(fmt.Errorf("%s must have one hard link", name), err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, uiTestPublicationMaxStateBytes+1))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	if len(raw) > uiTestPublicationMaxStateBytes {
		return fmt.Errorf("%s is too large", name)
	}
	return strictUIReportUnmarshal(raw, value)
}

func writeDurableUITestPublicationJSON(root *os.Root, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(raw)
	if writeErr == nil && written != len(raw) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return syncUITestDirectory(root, ".")
}

func syncUITestStagedPublication(root *os.Root, states []uiReportPublishState) error {
	for _, item := range states {
		if !item.directory {
			continue
		}
		if err := syncUITestDirectoryTree(root, item.temp); err != nil {
			return err
		}
	}
	return syncUITestDirectory(root, ".")
}

func syncUITestDirectoryTree(root *os.Root, name string) error {
	entries, err := root.Open(name)
	if err != nil {
		return err
	}
	children, readErr := entries.ReadDir(-1)
	closeErr := entries.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	for _, child := range children {
		if child.IsDir() {
			if err := syncUITestDirectoryTree(root, filepath.Join(name, child.Name())); err != nil {
				return err
			}
		}
	}
	return syncUITestDirectory(root, name)
}

func renameUITestPublicationBoundary(
	ctx context.Context,
	root *os.Root,
	oldName, newName, operation string,
) error {
	if err := runUITestPublicationFailpoint(ctx, "before "+operation); err != nil {
		return err
	}
	if err := root.Rename(oldName, newName); err != nil {
		return err
	}
	if err := syncUITestDirectory(root, "."); err != nil {
		return err
	}
	return runUITestPublicationFailpoint(ctx, "after "+operation)
}

func removeUITestPublicationBoundary(
	ctx context.Context,
	root *os.Root,
	name string,
	directory bool,
	operation string,
) error {
	if err := runUITestPublicationFailpoint(ctx, "before "+operation); err != nil {
		return err
	}
	if err := removeUITestPublicationPath(root, name, directory); err != nil {
		return err
	}
	if err := syncUITestDirectory(root, "."); err != nil {
		return err
	}
	return runUITestPublicationFailpoint(ctx, "after "+operation)
}

func runUITestPublicationFailpoint(ctx context.Context, point string) error {
	failpoint, ok := ctx.Value(uiTestPublicationFailpointContextKey{}).(uiTestPublicationFailpoint)
	if !ok {
		return nil
	}
	return failpoint(point)
}

func uiTestPublicationPathExists(root *os.Root, name string) (bool, error) {
	_, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func uiTestPublicationCommitTempName(token string) string {
	return uiTestPublicationCommitName + ".tmp-" + token
}
