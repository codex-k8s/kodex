package output

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"unicode/utf8"

	"github.com/codex-k8s/matter-codex/libs/go/runtimecontract"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	recoverySchema       = "mattercodex.output-recovery.v2"
	recoveryRelativePath = ".matter-codex/recovery/output.json"
	recoveryMarkdownPath = ".matter-codex/recovery/full-result.md"
	maximumRecoveryBytes = 1 << 20
)

// ErrRecoveryJournalUnavailable означает, что owner разрешил delivery-only
// attempt, но retained journal отсутствует. Это не разрешает повтор provider.
var ErrRecoveryJournalUnavailable = errors.New("runtime output recovery journal is unavailable")

// RecoveryJournal сохраняется trusted runner после завершения provider. UID
// provider-runtime не имеет доступа к каталогу recovery; checksum блокирует
// частично записанный либо повреждённый журнал до семантического использования.
type RecoveryJournal struct {
	Schema              string                     `json:"schema"`
	ChecksumSHA256      string                     `json:"checksum_sha256"`
	TurnID              string                     `json:"turn_id"`
	SourceExecutionID   string                     `json:"source_execution_id"`
	ArchiveExecutionID  string                     `json:"archive_execution_id"`
	SourceAttempt       uint32                     `json:"source_attempt"`
	OriginalOutcome     string                     `json:"original_outcome"`
	ScheduledOutcome    string                     `json:"scheduled_outcome,omitempty"`
	TerminalMarkdown    string                     `json:"terminal_markdown"`
	CodexSessionID      string                     `json:"codex_session_id"`
	ArchiveRelativePath string                     `json:"archive_relative_path"`
	ArchiveSHA256       string                     `json:"archive_sha256"`
	Existing            []runtimecontract.OutputV2 `json:"existing"`
	Failed              []RecoveryItem             `json:"failed"`
}

// AuthorizeRecovery связывает локальный protected journal с server-owned
// execution marker. Ни отсутствие journal, ни локально подложенный journal не
// могут превратить обычный Retry в повтор только доставки или наоборот.
func AuthorizeRecovery(input model.Input, journal RecoveryJournal, found bool) error {
	if input.CodexDeliveryRecoverySourceExecutionID == "" {
		if found {
			return errors.New("runtime output recovery is not authorized")
		}
		return nil
	}
	if !found || journal.SourceExecutionID != input.CodexDeliveryRecoverySourceExecutionID ||
		journal.TurnID != input.TurnID || journal.SourceAttempt >= input.Attempt ||
		(input.ScheduleOccurrenceID == "") != (journal.ScheduledOutcome == "") {
		if !found {
			return ErrRecoveryJournalUnavailable
		}
		return errors.New("runtime output recovery is not authorized")
	}
	return nil
}

func SaveRecovery(input model.Input, journal RecoveryJournal) error {
	journal.Schema = recoverySchema
	for index := range journal.Failed {
		if len(journal.Failed[index].InlinePayload) == 0 {
			continue
		}
		if journal.Failed[index].Name != "full-result.md" ||
			writeRecoveryPayload(input, journal.Failed[index].InlinePayload) != nil {
			return errors.New("persist full runtime result for recovery")
		}
		journal.Failed[index].InlinePayload = nil
	}
	journal.ChecksumSHA256 = ""
	checksum, err := recoveryChecksum(journal)
	if err != nil {
		return err
	}
	journal.ChecksumSHA256 = checksum
	if err := validateRecovery(journal); err != nil {
		return err
	}
	raw, err := json.Marshal(journal)
	if err != nil || len(raw) == 0 || len(raw) > maximumRecoveryBytes {
		return errors.New("encode runtime output recovery journal")
	}
	directory := filepath.Join(input.WorkspaceRoot, ".matter-codex/recovery")
	temporary := filepath.Join(directory, ".output-"+input.ExecutionID+".tmp")
	target := filepath.Join(input.WorkspaceRoot, recoveryRelativePath)
	descriptor, err := unix.Open(temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return errors.New("create runtime output recovery journal")
	}
	file := os.NewFile(uintptr(descriptor), temporary)
	if file == nil {
		_ = unix.Close(descriptor)
		return errors.New("open runtime output recovery journal")
	}
	writeErr := func() error {
		defer file.Close()
		var stat unix.Stat_t
		if unix.Fstat(descriptor, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
			stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
			return errors.New("protect runtime output recovery journal")
		}
		if _, err := file.Write(raw); err != nil || file.Sync() != nil {
			return errors.New("write runtime output recovery journal")
		}
		return nil
	}()
	if writeErr != nil {
		_ = os.Remove(temporary)
		return writeErr
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return errors.New("publish runtime output recovery journal")
	}
	return nil
}

func LoadRecovery(input model.Input) (RecoveryJournal, bool, error) {
	path := filepath.Join(input.WorkspaceRoot, recoveryRelativePath)
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ENOENT) {
		return RecoveryJournal{}, false, nil
	}
	if err != nil {
		return RecoveryJournal{}, false, errors.New("open runtime output recovery journal")
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return RecoveryJournal{}, false, errors.New("read runtime output recovery journal")
	}
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(descriptor, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || stat.Mode&0o077 != 0 || stat.Size <= 0 || stat.Size > maximumRecoveryBytes {
		return RecoveryJournal{}, false, errors.New("runtime output recovery journal is unsafe")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumRecoveryBytes+1))
	if err != nil || int64(len(raw)) != stat.Size {
		return RecoveryJournal{}, false, errors.New("read runtime output recovery journal")
	}
	var journal RecoveryJournal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&journal) != nil || decoder.Decode(&struct{}{}) != io.EOF || validateRecovery(journal) != nil {
		return RecoveryJournal{}, false, errors.New("runtime output recovery journal is invalid")
	}
	want := journal.ChecksumSHA256
	journal.ChecksumSHA256 = ""
	actual, err := recoveryChecksum(journal)
	journal.ChecksumSHA256 = want
	if err != nil || actual != want {
		return RecoveryJournal{}, false, errors.New("runtime output recovery checksum mismatch")
	}
	return journal, true, nil
}

func Resume(ctx context.Context, input model.Input, journal RecoveryJournal) (BuildResult, error) {
	if err := validateRecovery(journal); err != nil || journal.TurnID != input.TurnID ||
		journal.SourceAttempt >= input.Attempt {
		return BuildResult{}, errors.New("runtime output recovery scope is invalid")
	}
	client, token, stagingErr := stagingClient(input)
	directory, err := unix.Open(input.OutboxRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return BuildResult{}, errors.New("open runtime recovery outbox")
	}
	defer unix.Close(directory)
	references := slices.Clone(journal.Existing)
	remaining := make([]RecoveryItem, 0, len(journal.Failed))
	for _, item := range journal.Failed {
		payload := slices.Clone(item.InlinePayload)
		var readErr error
		if len(payload) == 0 && item.Name == "full-result.md" {
			payload, readErr = readRecoveryPayload(input, item.SizeBytes)
		} else if len(payload) == 0 {
			payload, readErr = readOutput(directory, item.Name, int64(item.SizeBytes))
		}
		digest := sha256.Sum256(payload)
		if readErr != nil || uint64(len(payload)) != item.SizeBytes || hex.EncodeToString(digest[:]) != item.SHA256 || stagingErr != nil {
			remaining = append(remaining, item)
			continue
		}
		staged, stageErr := stage(ctx, client, token, input, pendingOutput{kind: item.Kind, name: item.Name,
			mediaType: item.MediaType, payload: payload}, item.Sequence, item.Total)
		if stageErr != nil {
			remaining = append(remaining, item)
			continue
		}
		references = append(references, staged)
	}
	message := journal.TerminalMarkdown
	if len(remaining) == 0 {
		message = "Доставка выходных артефактов восстановлена без повторного запуска модели.\n\n" + message
	} else {
		message = fmt.Sprintf("%d выходных артефактов всё ещё ожидают восстановления; следующий Retry снова выполнит только доставку.\n\n%s", len(remaining), message)
	}
	message, _ = markdownSummary(message, input.MattermostPostMaximumRunes)
	primary := makeInlineOutput(input.ExecutionID, "FINAL_MARKDOWN", "result.md", "text/markdown", []byte(message), 1, 1)
	outputs := append([]runtimecontract.OutputV2{primary}, references...)
	if len(outputs) > runtimecontract.MaximumOutputs {
		return BuildResult{}, errors.New("runtime output recovery reference limit exceeded")
	}
	normalizeSequences(outputs)
	return BuildResult{Outputs: outputs, Failed: remaining}, nil
}

func ClearRecovery(input model.Input) error {
	for _, relative := range []string{recoveryRelativePath, recoveryMarkdownPath} {
		err := os.Remove(filepath.Join(input.WorkspaceRoot, relative))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("remove runtime output recovery journal")
		}
	}
	return nil
}

func writeRecoveryPayload(input model.Input, raw []byte) error {
	if len(raw) == 0 || int64(len(raw)) > maximumStagedOutputBytes {
		return errors.New("runtime recovery payload size is invalid")
	}
	directory := filepath.Join(input.WorkspaceRoot, ".matter-codex/recovery")
	temporary := filepath.Join(directory, ".full-result-"+input.ExecutionID+".tmp")
	target := filepath.Join(input.WorkspaceRoot, recoveryMarkdownPath)
	descriptor, err := unix.Open(temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(descriptor), temporary)
	if file == nil {
		_ = unix.Close(descriptor)
		return errors.New("open runtime recovery payload")
	}
	writeErr := func() error {
		defer file.Close()
		var stat unix.Stat_t
		if unix.Fstat(descriptor, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
			stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
			return errors.New("protect runtime recovery payload")
		}
		if _, err := file.Write(raw); err != nil || file.Sync() != nil {
			return errors.New("write runtime recovery payload")
		}
		return nil
	}()
	if writeErr != nil {
		_ = os.Remove(temporary)
		return writeErr
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func readRecoveryPayload(input model.Input, size uint64) ([]byte, error) {
	path := filepath.Join(input.WorkspaceRoot, recoveryMarkdownPath)
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, errors.New("open runtime recovery payload")
	}
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(descriptor, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || stat.Mode&0o077 != 0 ||
		stat.Size <= 0 || uint64(stat.Size) != size || stat.Size > maximumStagedOutputBytes {
		return nil, errors.New("runtime recovery payload is unsafe")
	}
	return io.ReadAll(io.LimitReader(file, maximumStagedOutputBytes+1))
}

func recoveryChecksum(journal RecoveryJournal) (string, error) {
	raw, err := json.Marshal(journal)
	if err != nil {
		return "", errors.New("hash runtime output recovery journal")
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func validateRecovery(journal RecoveryJournal) error {
	if journal.Schema != recoverySchema || uuid.Validate(journal.TurnID) != nil ||
		uuid.Validate(journal.SourceExecutionID) != nil || uuid.Validate(journal.ArchiveExecutionID) != nil || journal.SourceAttempt == 0 ||
		(journal.OriginalOutcome != "SUCCEEDED" && journal.OriginalOutcome != "FAILED" && journal.OriginalOutcome != "BLOCKED") ||
		(journal.ScheduledOutcome != "" && journal.ScheduledOutcome != "no_action" &&
			journal.ScheduledOutcome != "action_taken" && journal.ScheduledOutcome != "requires_human" &&
			journal.ScheduledOutcome != "failed") ||
		journal.TerminalMarkdown == "" || len(journal.TerminalMarkdown) > maximumMarkdownBytes ||
		!utf8.ValidString(journal.TerminalMarkdown) || len(journal.Failed) == 0 ||
		len(journal.Failed)+len(journal.Existing) > runtimecontract.MaximumOutputs-1 ||
		len(journal.ChecksumSHA256) != sha256.Size*2 {
		return errors.New("runtime output recovery journal fields are invalid")
	}
	if journal.CodexSessionID != "" && uuid.Validate(journal.CodexSessionID) != nil {
		return errors.New("runtime output recovery Codex session is invalid")
	}
	for _, item := range journal.Failed {
		if item.Kind != "FILE" && item.Kind != "IMAGE" {
			return errors.New("runtime output recovery kind is invalid")
		}
		if item.Name == "" || filepath.Base(item.Name) != item.Name || len(item.Name) > 255 ||
			item.MediaType == "" || item.SizeBytes == 0 || item.SizeBytes > uint64(maximumStagedOutputBytes) ||
			len(item.SHA256) != sha256.Size*2 || item.Sequence == 0 || item.Total == 0 || item.Sequence > item.Total {
			return errors.New("runtime output recovery item is invalid")
		}
		if len(item.InlinePayload) != 0 && (uint64(len(item.InlinePayload)) != item.SizeBytes || item.Name != "full-result.md") {
			return errors.New("runtime output recovery inline item is invalid")
		}
	}
	return nil
}
