package dettools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	uiTestPublicationJournalName    = ".ui-test-report.transaction.json"
	uiTestPublicationPendingName    = ".ui-test-report.transaction.pending"
	uiTestPublicationCommitName     = ".ui-test-report.transaction.commit"
	uiTestPublicationSchema         = "multica.ui-test-report.publication"
	uiTestPublicationJournalVersion = 1
	uiTestPublicationMaxStateBytes  = 16 << 10
	uiTestPublicationMaxTreeEntries = 10_000
)

var (
	errUITestPublicationInterrupted = errors.New("UI test report publication interrupted")
	errUITestPublicationRecovery    = errors.New("UI test report publication recovery failed")
	uiTestPublicationTokenPattern   = regexp.MustCompile(`^[0-9a-f]{16}$`)
	uiTestPublicationDigestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type uiTestPublicationFailpoint func(point string) error

type uiTestPublicationFailpointContextKey struct{}

type uiTestPublicationJournal struct {
	Schema  string                         `json:"schema"`
	Version int                            `json:"version"`
	Token   string                         `json:"token"`
	Items   []uiTestPublicationJournalItem `json:"items"`
}

type uiTestPublicationJournalItem struct {
	Name        string `json:"name"`
	Directory   bool   `json:"directory"`
	HadPrior    bool   `json:"had_prior"`
	OldDigest   string `json:"old_digest"`
	NewDigest   string `json:"new_digest"`
	hadPriorSet bool
}

type uiTestPublicationCommit struct {
	Schema     string   `json:"schema"`
	Version    int      `json:"version"`
	Token      string   `json:"token"`
	NewDigests []string `json:"new_digests"`
}

func (item *uiTestPublicationJournalItem) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Name      *string `json:"name"`
		Directory *bool   `json:"directory"`
		HadPrior  *bool   `json:"had_prior"`
		OldDigest *string `json:"old_digest"`
		NewDigest *string `json:"new_digest"`
	}
	if err := strictUIReportUnmarshal(raw, &wire); err != nil {
		return err
	}
	if wire.Name == nil || wire.Directory == nil || wire.HadPrior == nil ||
		wire.OldDigest == nil || wire.NewDigest == nil {
		return fmt.Errorf("publication journal item lacks required field")
	}
	item.Name = *wire.Name
	item.Directory = *wire.Directory
	item.HadPrior = *wire.HadPrior
	item.OldDigest = *wire.OldDigest
	item.NewDigest = *wire.NewDigest
	item.hadPriorSet = true
	return nil
}

func publishUITestOutputsJournaled(
	ctx context.Context,
	root *os.Root,
	evidence []uiSealedEvidence,
	outputs []uiReportOutput,
) (cleanupPending bool, err error) {
	states, err := prepareUITestPublicationStates(root, evidence, outputs)
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
		Schema:  uiTestPublicationSchema,
		Version: uiTestPublicationJournalVersion,
		Token:   token,
		Items:   make([]uiTestPublicationJournalItem, len(states)),
	}
	for i, item := range states {
		journal.Items[i] = uiTestPublicationJournalItem{
			Name: item.name, Directory: item.directory, HadPrior: item.hadPrior,
			OldDigest: item.oldDigest, NewDigest: item.newDigest, hadPriorSet: true,
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
	if err := validateStagedUITestPublication(root, states); err != nil {
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
	if err := createUITestPublicationCommit(ctx, root, journal); err != nil {
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
	if err := validateUITestPublicationCommitMatches(marker, states); err != nil {
		return false, err
	}
	if err := validateCommittedUITestPublication(root, states); err != nil {
		return false, err
	}
	return true, nil
}

func prepareUITestPublicationStates(
	root *os.Root,
	evidence []uiSealedEvidence,
	outputs []uiReportOutput,
) ([]uiReportPublishState, error) {
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
		if exists {
			states[i].oldDigest, err = digestUITestPublicationPath(
				root, states[i].name, states[i].directory,
			)
			if err != nil {
				return nil, fmt.Errorf("digest existing output %s: %w", states[i].name, err)
			}
		}
	}
	evidenceDigest, err := digestSealedUITestEvidence(evidence)
	if err != nil {
		return nil, err
	}
	states[0].newDigest = evidenceDigest
	for i, output := range outputs {
		states[i+1].newDigest = digestUITestBytes(output.Content)
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

func createUITestPublicationCommit(
	ctx context.Context,
	root *os.Root,
	journal uiTestPublicationJournal,
) error {
	state := uiTestPublicationCommit{
		Schema:     uiTestPublicationSchema,
		Version:    uiTestPublicationJournalVersion,
		Token:      journal.Token,
		NewDigests: make([]string, len(journal.Items)),
	}
	for i, item := range journal.Items {
		state.NewDigests[i] = item.NewDigest
	}
	temp := uiTestPublicationCommitTempName(journal.Token)
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
		states := uiTestPublicationStatesFromCommit(marker)
		if err := validateCommittedUITestPublication(root, states); err != nil {
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
	if err := validateUITestPublicationCommitMatches(marker, states); err != nil {
		return err
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
	if err := validateRollbackUITestPublication(root, states); err != nil {
		return err
	}
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
	if journal.Schema != uiTestPublicationSchema {
		return fmt.Errorf("invalid publication journal schema")
	}
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
		if !item.hadPriorSet {
			return fmt.Errorf("publication journal item %d lacks had_prior", i)
		}
		if item.HadPrior {
			if !uiTestPublicationDigestPattern.MatchString(item.OldDigest) {
				return fmt.Errorf("invalid prior digest for publication journal item %d", i)
			}
		} else if item.OldDigest != "" {
			return fmt.Errorf("unexpected prior digest for publication journal item %d", i)
		}
		if !uiTestPublicationDigestPattern.MatchString(item.NewDigest) {
			return fmt.Errorf("invalid new digest for publication journal item %d", i)
		}
	}
	return nil
}

func validateUITestPublicationCommit(marker uiTestPublicationCommit) error {
	if marker.Schema != uiTestPublicationSchema {
		return fmt.Errorf("invalid publication commit schema")
	}
	if marker.Version != uiTestPublicationJournalVersion {
		return fmt.Errorf("unsupported publication commit version %d", marker.Version)
	}
	if !uiTestPublicationTokenPattern.MatchString(marker.Token) {
		return fmt.Errorf("invalid publication commit token")
	}
	if len(marker.NewDigests) != 5 {
		return fmt.Errorf("publication commit must contain five new digests")
	}
	for i, digest := range marker.NewDigests {
		if !uiTestPublicationDigestPattern.MatchString(digest) {
			return fmt.Errorf("invalid publication commit digest %d", i)
		}
	}
	return nil
}

func validateUITestPublicationCommitMatches(
	marker uiTestPublicationCommit,
	states []uiReportPublishState,
) error {
	if len(marker.NewDigests) != len(states) {
		return fmt.Errorf("publication commit digest count differs")
	}
	for i, state := range states {
		if marker.NewDigests[i] != state.newDigest {
			return fmt.Errorf("publication commit digest %d differs", i)
		}
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
			oldDigest: item.OldDigest,
			newDigest: item.NewDigest,
		}
	}
	return states
}

func uiTestPublicationStatesFromCommit(marker uiTestPublicationCommit) []uiReportPublishState {
	names := []struct {
		name      string
		directory bool
	}{
		{name: uiTestPublishedDir, directory: true},
		{name: uiTestReportJSONName},
		{name: uiTestReportMarkdownName},
		{name: uiTestManifestName},
		{name: uiTestCommentName},
	}
	states := make([]uiReportPublishState, len(names))
	for i, item := range names {
		states[i] = uiReportPublishState{
			name: item.name, directory: item.directory, newDigest: marker.NewDigests[i],
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

func validateStagedUITestPublication(root *os.Root, states []uiReportPublishState) error {
	for _, item := range states {
		digest, err := digestUITestPublicationPath(root, item.temp, item.directory)
		if err != nil {
			return fmt.Errorf("digest staged publication item %s: %w", item.name, err)
		}
		if digest != item.newDigest {
			return fmt.Errorf("staged publication item %s digest differs", item.name)
		}
	}
	return nil
}

func validateRollbackUITestPublication(root *os.Root, states []uiReportPublishState) error {
	if err := validateUITestPublicationResidue(root, states); err != nil {
		return err
	}
	for _, item := range states {
		canonicalExists, err := uiTestPublicationPathExists(root, item.name)
		if err != nil {
			return err
		}
		backupExists, err := uiTestPublicationPathExists(root, item.backup)
		if err != nil {
			return err
		}
		if item.hadPrior {
			if backupExists {
				backupDigest, err := digestUITestPublicationPath(root, item.backup, item.directory)
				if err != nil {
					return fmt.Errorf("digest rollback backup %s: %w", item.name, err)
				}
				if backupDigest != item.oldDigest {
					return fmt.Errorf("rollback backup %s digest differs", item.name)
				}
				if canonicalExists {
					canonicalDigest, err := digestUITestPublicationPath(root, item.name, item.directory)
					if err != nil {
						return fmt.Errorf("digest rollback canonical %s: %w", item.name, err)
					}
					if canonicalDigest != item.oldDigest && canonicalDigest != item.newDigest {
						return fmt.Errorf("rollback canonical %s digest differs", item.name)
					}
				}
				continue
			}
			if !canonicalExists {
				return fmt.Errorf("rollback prior item %s is missing", item.name)
			}
			canonicalDigest, err := digestUITestPublicationPath(root, item.name, item.directory)
			if err != nil {
				return fmt.Errorf("digest rollback canonical %s: %w", item.name, err)
			}
			if canonicalDigest != item.oldDigest {
				return fmt.Errorf("rollback prior canonical %s digest differs", item.name)
			}
			continue
		}
		if backupExists {
			return fmt.Errorf("rollback new item %s has unexpected backup", item.name)
		}
		if canonicalExists {
			canonicalDigest, err := digestUITestPublicationPath(root, item.name, item.directory)
			if err != nil {
				return fmt.Errorf("digest rollback canonical %s: %w", item.name, err)
			}
			if canonicalDigest != item.newDigest {
				return fmt.Errorf("rollback new canonical %s digest differs", item.name)
			}
		}
	}
	return nil
}

func validateCommittedUITestPublication(root *os.Root, states []uiReportPublishState) error {
	for _, item := range states {
		exists, err := validateUITestPublicationPathType(root, item.name, item.directory)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("committed publication item %s is missing", item.name)
		}
		digest, err := digestUITestPublicationPath(root, item.name, item.directory)
		if err != nil {
			return fmt.Errorf("digest committed publication item %s: %w", item.name, err)
		}
		if digest != item.newDigest {
			return fmt.Errorf("committed publication item %s digest differs", item.name)
		}
	}
	return nil
}

type uiTestTreeDigestEntry struct {
	path      string
	directory bool
	content   []byte
}

func digestUITestBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func digestSealedUITestEvidence(evidence []uiSealedEvidence) (string, error) {
	entries := make(map[string]uiTestTreeDigestEntry)
	prefix := uiTestPublishedDir + "/"
	var total int64
	for _, artifact := range evidence {
		if !strings.HasPrefix(artifact.RelativePath, prefix) {
			return "", fmt.Errorf("invalid sealed evidence path %q", artifact.PublishedPath)
		}
		relative := filepath.ToSlash(filepath.Clean(filepath.FromSlash(
			strings.TrimPrefix(artifact.RelativePath, prefix),
		)))
		if relative == "." || pathEscapesRoot(filepath.FromSlash(relative)) {
			return "", fmt.Errorf("invalid sealed evidence path %q", artifact.PublishedPath)
		}
		if _, duplicate := entries[relative]; duplicate {
			return "", fmt.Errorf("duplicate sealed evidence path %q", artifact.PublishedPath)
		}
		total += int64(len(artifact.Content))
		if total > uiTestMaxPublishedBytes {
			return "", fmt.Errorf("sealed evidence exceeds publication size limit")
		}
		entries[relative] = uiTestTreeDigestEntry{path: relative, content: artifact.Content}
		for parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(relative))); parent != "."; parent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(parent))) {
			if existing, ok := entries[parent]; ok && !existing.directory {
				return "", fmt.Errorf("sealed evidence path conflicts with file %q", parent)
			}
			entries[parent] = uiTestTreeDigestEntry{path: parent, directory: true}
		}
	}
	if len(entries) > uiTestPublicationMaxTreeEntries {
		return "", fmt.Errorf("sealed evidence has too many tree entries")
	}
	return digestUITestTreeEntries(entries), nil
}

func digestUITestPublicationPath(root *os.Root, name string, directory bool) (string, error) {
	if !directory {
		content, err := readUITestPublicationFile(root, name, uiTestMaxPublishedBytes)
		if err != nil {
			return "", err
		}
		return digestUITestBytes(content), nil
	}
	entries := make(map[string]uiTestTreeDigestEntry)
	var total int64
	if err := collectUITestPublicationTree(root, name, "", entries, &total); err != nil {
		return "", err
	}
	if len(entries) > uiTestPublicationMaxTreeEntries {
		return "", fmt.Errorf("%s has too many tree entries", name)
	}
	return digestUITestTreeEntries(entries), nil
}

func collectUITestPublicationTree(
	root *os.Root,
	base string,
	relative string,
	entries map[string]uiTestTreeDigestEntry,
	total *int64,
) error {
	current := base
	if relative != "" {
		current = filepath.Join(base, filepath.FromSlash(relative))
	}
	before, err := root.Lstat(current)
	if err != nil {
		return err
	}
	if !before.IsDir() {
		return fmt.Errorf("%s is not a directory", current)
	}
	dir, err := root.Open(current)
	if err != nil {
		return err
	}
	opened, statErr := dir.Stat()
	if statErr != nil || !opened.IsDir() || !os.SameFile(before, opened) {
		_ = dir.Close()
		return errors.Join(fmt.Errorf("%s identity changed during open", current), statErr)
	}
	children, readErr := dir.ReadDir(-1)
	closeErr := dir.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
	for _, child := range children {
		childRelative := child.Name()
		if relative != "" {
			childRelative = relative + "/" + child.Name()
		}
		childPath := filepath.Join(base, filepath.FromSlash(childRelative))
		info, err := root.Lstat(childPath)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			entries[childRelative] = uiTestTreeDigestEntry{
				path: childRelative, directory: true,
			}
			if err := collectUITestPublicationTree(
				root, base, childRelative, entries, total,
			); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			content, err := readUITestPublicationFile(root, childPath, uiTestMaxPublishedBytes-*total)
			if err != nil {
				return err
			}
			*total += int64(len(content))
			if *total > uiTestMaxPublishedBytes {
				return fmt.Errorf("%s exceeds publication size limit", base)
			}
			entries[childRelative] = uiTestTreeDigestEntry{
				path: childRelative, content: content,
			}
		default:
			return fmt.Errorf("%s contains unsupported file type", childPath)
		}
		if len(entries) > uiTestPublicationMaxTreeEntries {
			return fmt.Errorf("%s has too many tree entries", base)
		}
	}
	return nil
}

func readUITestPublicationFile(root *os.Root, name string, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("%s exceeds publication size limit", name)
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > limit {
		return nil, fmt.Errorf("%s is not a bounded regular file", name)
	}
	file, err := openUITestEvidence(root, name)
	if err != nil {
		return nil, err
	}
	opened, statErr := file.Stat()
	if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, errors.Join(fmt.Errorf("%s identity changed during open", name), statErr)
	}
	links, linkErr := uiTestEvidenceLinkCount(file, opened)
	if linkErr != nil || links != 1 {
		_ = file.Close()
		return nil, errors.Join(fmt.Errorf("%s must have one hard link", name), linkErr)
	}
	content, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	after, afterErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(readErr, afterErr, closeErr); err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("%s exceeds publication size limit", name)
	}
	if !os.SameFile(opened, after) || opened.Size() != after.Size() ||
		!opened.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("%s changed while hashing", name)
	}
	return content, nil
}

func digestUITestTreeEntries(entries map[string]uiTestTreeDigestEntry) string {
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	hasher := sha256.New()
	for _, path := range paths {
		entry := entries[path]
		kind := byte('f')
		if entry.directory {
			kind = 'd'
		}
		writeUITestTreeDigestRecord(hasher, kind, path, entry.content)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// These hashes detect torn or unintended publication state. They are not
// authentication against an actor with write access in the same trust boundary.
func writeUITestTreeDigestRecord(hasher hash.Hash, kind byte, path string, content []byte) {
	_, _ = hasher.Write([]byte{kind})
	_ = binary.Write(hasher, binary.BigEndian, uint32(len(path)))
	_ = binary.Write(hasher, binary.BigEndian, uint64(len(content)))
	_, _ = hasher.Write([]byte(path))
	_, _ = hasher.Write(content)
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
	if err := rejectDuplicateUITestPublicationJSONKeys(raw); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if err := validateCanonicalUITestPublicationJSONKeys(raw, value); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return strictUIReportUnmarshal(raw, value)
}

func validateCanonicalUITestPublicationJSONKeys(raw []byte, value any) error {
	switch value.(type) {
	case *uiTestPublicationJournal:
		root, err := decodeCanonicalUITestPublicationObject(
			raw, "schema", "version", "token", "items",
		)
		if err != nil {
			return err
		}
		var items []json.RawMessage
		if err := json.Unmarshal(root["items"], &items); err != nil {
			return fmt.Errorf("publication journal items must be an array: %w", err)
		}
		for index, item := range items {
			if _, err := decodeCanonicalUITestPublicationObject(
				item, "name", "directory", "had_prior", "old_digest", "new_digest",
			); err != nil {
				return fmt.Errorf("publication journal item %d: %w", index, err)
			}
		}
		return nil
	case *uiTestPublicationCommit:
		_, err := decodeCanonicalUITestPublicationObject(
			raw, "schema", "version", "token", "new_digests",
		)
		return err
	default:
		return fmt.Errorf("unsupported publication state type")
	}
}

func decodeCanonicalUITestPublicationObject(
	raw []byte,
	required ...string,
) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("publication state must be an object: %w", err)
	}
	if object == nil {
		return nil, fmt.Errorf("publication state must be an object")
	}
	allowed := make(map[string]struct{}, len(required))
	for _, key := range required {
		allowed[key] = struct{}{}
		if _, exists := object[key]; !exists {
			return nil, fmt.Errorf("publication state lacks canonical key %q", key)
		}
	}
	unknown := make([]string, 0, len(object))
	for key := range object {
		if _, exists := allowed[key]; !exists {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("publication state contains noncanonical key %q", unknown[0])
	}
	return object, nil
}

func rejectDuplicateUITestPublicationJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := consumeUITestPublicationJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("publication state contains trailing JSON")
		}
		return err
	}
	return nil
}

func consumeUITestPublicationJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("publication state object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("publication state contains duplicate key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeUITestPublicationJSONValue(decoder); err != nil {
				return err
			}
		}
		closeToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if closeToken != json.Delim('}') {
			return fmt.Errorf("publication state object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := consumeUITestPublicationJSONValue(decoder); err != nil {
				return err
			}
		}
		closeToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if closeToken != json.Delim(']') {
			return fmt.Errorf("publication state array is not closed")
		}
	default:
		return fmt.Errorf("unexpected publication state delimiter %q", delim)
	}
	return nil
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
