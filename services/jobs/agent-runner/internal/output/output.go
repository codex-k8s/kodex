// Package output формирует bounded owner-deliverable artifacts из outbox.
package output

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/matter-codex/libs/go/runtimecontract"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	maximumStagedOutputBytes = int64(256 << 20)
	maximumAggregateBytes    = int64(256 << 20)
	maximumMarkdownBytes     = 60 << 10
	maximumSummaryRunes      = 12000
	maximumResponseBytes     = 8 << 10
	maximumFailureDetails    = 8
)

type scheduledResultDocument struct {
	Schema       string
	Outcome      string
	Summary      string
	ArtifactRefs []string
}

// ScheduledOutcome читает обязательный закрытый бизнес-исход scheduled
// execution. Идентификаторы маршрута и policy из документа не принимаются:
// authority разрешает только control-plane из server-owned occurrence.
func ScheduledOutcome(input model.Input, runtimeOutcome string) (string, error) {
	if input.ScheduleOccurrenceID == "" {
		return "", nil
	}
	if input.ScheduledResultContract == nil || input.ScheduledResultContract.Validate() != nil {
		return "", errors.New("scheduled runtime result contract is invalid")
	}
	name := filepath.Base(input.ScheduledResultContract.Path)
	path := filepath.Join(input.OutboxRoot, name)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return "", errors.New("scheduled runtime result is missing")
	} else if err != nil {
		return "", errors.New("inspect scheduled runtime result")
	}
	directory, err := unix.Open(input.OutboxRoot,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", errors.New("open scheduled runtime result")
	}
	defer unix.Close(directory)
	raw, err := readOutput(directory, name, int64(input.ScheduledResultContract.MaximumBytes))
	if err != nil {
		return "", errors.New("read scheduled runtime result")
	}
	document, err := decodeScheduledResult(raw)
	if err != nil || document.Schema != input.ScheduledResultContract.Schema ||
		(runtimeOutcome != "SUCCEEDED" && document.Outcome != "failed") {
		return "", errors.New("scheduled runtime result is invalid")
	}
	return document.Outcome, nil
}

func decodeScheduledResult(raw []byte) (scheduledResultDocument, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return scheduledResultDocument{}, errors.New("scheduled result root is invalid")
	}
	seen := make(map[string]struct{}, 4)
	var document scheduledResultDocument
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok {
			return scheduledResultDocument{}, errors.New("scheduled result field is invalid")
		}
		if _, duplicate := seen[key]; duplicate {
			return scheduledResultDocument{}, errors.New("scheduled result field is duplicated")
		}
		seen[key] = struct{}{}
		switch key {
		case "schema":
			err = decoder.Decode(&document.Schema)
		case "outcome":
			err = decoder.Decode(&document.Outcome)
		case "summary":
			err = decoder.Decode(&document.Summary)
		case "artifact_refs":
			err = decoder.Decode(&document.ArtifactRefs)
		default:
			return scheduledResultDocument{}, errors.New("scheduled result field is unknown")
		}
		if err != nil {
			return scheduledResultDocument{}, errors.New("scheduled result field value is invalid")
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') ||
		decoder.Decode(&struct{}{}) != io.EOF || len(seen) != 4 ||
		(document.Outcome != "no_action" && document.Outcome != "action_taken" &&
			document.Outcome != "requires_human" && document.Outcome != "failed") ||
		!utf8.ValidString(document.Summary) || len([]rune(document.Summary)) > 2000 ||
		strings.ContainsRune(document.Summary, '\x00') || len(document.ArtifactRefs) > 32 {
		return scheduledResultDocument{}, errors.New("scheduled result document is invalid")
	}
	refs := make(map[string]struct{}, len(document.ArtifactRefs))
	for _, reference := range document.ArtifactRefs {
		if len(reference) == 0 || len(reference) > 255 || !safeScheduledArtifactRef(reference) {
			return scheduledResultDocument{}, errors.New("scheduled result artifact reference is invalid")
		}
		if _, duplicate := refs[reference]; duplicate {
			return scheduledResultDocument{}, errors.New("scheduled result artifact reference is duplicated")
		}
		refs[reference] = struct{}{}
	}
	return document, nil
}

func safeScheduledArtifactRef(value string) bool {
	for index, symbol := range value {
		if (symbol >= 'A' && symbol <= 'Z') || (symbol >= 'a' && symbol <= 'z') ||
			(symbol >= '0' && symbol <= '9') || index > 0 && (symbol == '.' || symbol == '_' || symbol == '-') {
			continue
		}
		return false
	}
	return true
}

type stagedReference struct {
	ArtifactID      string `json:"artifact_id"`
	ArtifactVersion uint64 `json:"artifact_version"`
	SHA256          string `json:"sha256"`
	SizeBytes       uint64 `json:"size_bytes"`
	Name            string `json:"name"`
	MediaType       string `json:"media_type"`
	StorageRef      string `json:"storage_ref"`
}

type pendingOutput struct {
	kind, name, mediaType string
	payload               []byte
}

type artifactFailure struct {
	name, reason string
}

type RecoveryItem struct {
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	MediaType     string `json:"media_type"`
	SHA256        string `json:"sha256"`
	SizeBytes     uint64 `json:"size_bytes"`
	Sequence      uint32 `json:"sequence"`
	Total         uint32 `json:"total"`
	InlinePayload []byte `json:"inline_payload,omitempty"`
}

type BuildResult struct {
	Outputs []runtimecontract.OutputV2
	Failed  []RecoveryItem
}

// TerminalOnly создаёт bounded owner-visible terminal без чтения outbox и
// staging effects. Он используется, когда server-authorized delivery journal
// утрачен и повтор provider запрещён.
func TerminalOnly(input model.Input, markdown string) (BuildResult, error) {
	if markdown == "" || !utf8.ValidString(markdown) {
		return BuildResult{}, errors.New("runtime terminal Markdown is invalid")
	}
	summary, _ := markdownSummary(markdown, input.MattermostPostMaximumRunes)
	primary := makeInlineOutput(input.ExecutionID, "FINAL_MARKDOWN", "result.md", "text/markdown", []byte(summary), 1, 1)
	return BuildResult{Outputs: []runtimecontract.OutputV2{primary}}, nil
}

// Build всегда сохраняет bounded terminal Markdown. Ошибка отдельного staged
// artifact добавляется в итог, но не уничтожает owner terminal transaction.
func Build(ctx context.Context, input model.Input, markdown, archivePath string) (BuildResult, error) {
	if markdown == "" || !utf8.ValidString(markdown) {
		return BuildResult{}, errors.New("runtime terminal Markdown is invalid")
	}
	fullMarkdown := []byte(markdown)
	summary, truncated := markdownSummary(markdown, input.MattermostPostMaximumRunes)
	maximumOutboxReferences := runtimecontract.MaximumOutputs - 1
	if truncated {
		maximumOutboxReferences--
	}
	outboxDescriptor, err := unix.Open(input.OutboxRoot,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return BuildResult{}, errors.New("read runtime outbox")
	}
	outbox := os.NewFile(uintptr(outboxDescriptor), input.OutboxRoot)
	if outbox == nil {
		_ = unix.Close(outboxDescriptor)
		return BuildResult{}, errors.New("open runtime outbox")
	}
	defer outbox.Close()
	names := make([]string, 0, maximumOutboxReferences)
	totalSafeNames := 0
	unsafeNames := 0
	for {
		entries, readErr := outbox.ReadDir(64)
		for _, entry := range entries {
			name := entry.Name()
			path := filepath.Join(input.OutboxRoot, name)
			if path == archivePath || input.ScheduledResultContract != nil &&
				name == filepath.Base(input.ScheduledResultContract.Path) {
				continue
			}
			if strings.ContainsAny(name, "/\\\x00\r\n") {
				unsafeNames++
				continue
			}
			totalSafeNames++
			names = append(names, name)
			slices.Sort(names)
			if len(names) > maximumOutboxReferences {
				names = names[:maximumOutboxReferences]
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return BuildResult{}, errors.New("read runtime outbox")
		}
	}
	pending := make([]pendingOutput, 0, len(names)+1)
	failures := make([]artifactFailure, 0, maximumFailureDetails)
	notices := make([]string, 0, 4+maximumFailureDetails)
	omitted := unsafeNames + max(0, totalSafeNames-maximumOutboxReferences)
	var aggregateBytes int64
	for _, name := range names {
		path := filepath.Join(input.OutboxRoot, name)
		if path == archivePath {
			continue
		}
		raw, readErr := readOutput(outboxDescriptor, name, maximumAggregateBytes-aggregateBytes)
		if readErr != nil {
			omitted++
			failures = appendArtifactFailure(failures, name, "защищённая проверка отклонена")
			continue
		}
		aggregateBytes += int64(len(raw))
		mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if mediaType == "" {
			mediaType = http.DetectContentType(raw)
		}
		mediaType = strings.Split(mediaType, ";")[0]
		kind := "FILE"
		if strings.HasPrefix(mediaType, "image/") {
			kind = "IMAGE"
		}
		pending = append(pending, pendingOutput{kind: kind, name: name, mediaType: mediaType, payload: raw})
	}
	if truncated {
		pending = append([]pendingOutput{{kind: "FILE", name: "full-result.md", mediaType: "text/markdown",
			payload: fullMarkdown}}, pending...)
	}
	if omitted != 0 {
		notices = append(notices, fmt.Sprintf("%d выходных артефактов пропущено: файл не прошёл защищённую проверку размера, типа или общего лимита.", omitted))
	}

	client, token, stagingErr := stagingClient(input)
	counts := map[string]uint32{}
	for _, item := range pending {
		counts[item.kind]++
	}
	sequences := map[string]uint32{}
	references := make([]runtimecontract.OutputV2, 0, min(len(pending), runtimecontract.MaximumOutputs-1))
	failedItems := make([]RecoveryItem, 0, min(len(pending), runtimecontract.MaximumOutputs-1))
	failed := 0
	fullResultStaged := false
	for _, item := range pending {
		if len(references) >= runtimecontract.MaximumOutputs-1 {
			failed++
			continue
		}
		sequences[item.kind]++
		if stagingErr != nil {
			failed++
			failedItems = append(failedItems, makeRecoveryItem(item, sequences[item.kind], counts[item.kind]))
			failures = appendArtifactFailure(failures, item.name, "staging credential или TLS binding недоступен")
			continue
		}
		output, stageErr := stage(ctx, client, token, input, item, sequences[item.kind], counts[item.kind])
		if stageErr != nil {
			failed++
			failedItems = append(failedItems, makeRecoveryItem(item, sequences[item.kind], counts[item.kind]))
			failures = appendArtifactFailure(failures, item.name, "owner staging/readback не завершён")
			continue
		}
		references = append(references, output)
		if item.kind == "FILE" && item.name == "full-result.md" {
			fullResultStaged = true
		}
	}
	if fullResultStaged {
		notices = append(notices, "Полный итог приложен отдельным защищённым артефактом.")
	}
	if failed != 0 {
		notices = append(notices, fmt.Sprintf("%d выходных артефактов не удалось сохранить. Retry повторит только доставку и не запустит модель повторно.", failed))
	}
	for _, failure := range failures {
		notices = append(notices, fmt.Sprintf("- %q: %s.", failure.name, failure.reason))
	}
	if len(failures) == maximumFailureDetails && failed+omitted > len(failures) {
		notices = append(notices, "- Остальные ошибки артефактов сведены в счётчик выше.")
	}
	if len(notices) != 0 {
		summary = strings.Join(notices, "\n") + "\n\n" + summary
	}
	summary, _ = markdownSummary(summary, input.MattermostPostMaximumRunes)
	primary := makeInlineOutput(input.ExecutionID, "FINAL_MARKDOWN", "result.md", "text/markdown", []byte(summary), 1, 1)
	outputs := append([]runtimecontract.OutputV2{primary}, references...)
	normalizeSequences(outputs)
	return BuildResult{Outputs: outputs, Failed: failedItems}, nil
}

func makeRecoveryItem(item pendingOutput, sequence, total uint32) RecoveryItem {
	digest := sha256.Sum256(item.payload)
	recovery := RecoveryItem{Kind: item.kind, Name: item.name, MediaType: item.mediaType,
		SHA256: hex.EncodeToString(digest[:]), SizeBytes: uint64(len(item.payload)), Sequence: sequence, Total: total}
	if item.name == "full-result.md" {
		recovery.InlinePayload = slices.Clone(item.payload)
	}
	return recovery
}

func stagingClient(input model.Input) (*http.Client, string, error) {
	tokenRaw, err := os.ReadFile(input.CredentialFiles.MaterializationToken)
	if err != nil || len(tokenRaw) == 0 || len(tokenRaw) > 16<<10 {
		return nil, "", errors.New("read runtime output credential")
	}
	token := strings.TrimSpace(string(tokenRaw))
	if token == "" || token != string(bytes.TrimSpace(tokenRaw)) || strings.ContainsAny(token, "\x00\r\n") {
		return nil, "", errors.New("runtime output credential is invalid")
	}
	caRaw, err := os.ReadFile(input.InteractionGateway.TLS.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, "", errors.New("read runtime output CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, "", errors.New("parse runtime output CA")
	}
	certificate, err := tls.LoadX509KeyPair(input.InteractionGateway.TLS.CertificateFile,
		input.InteractionGateway.TLS.PrivateKeyFile)
	if err != nil {
		return nil, "", errors.New("load runtime output client identity")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13,
		ServerName: input.InteractionGateway.TLS.ServerName, RootCAs: roots,
		Certificates: []tls.Certificate{certificate}}, DisableCompression: true,
		MaxIdleConns: 2, MaxIdleConnsPerHost: 2, ResponseHeaderTimeout: 10 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second}
	return &http.Client{Transport: transport, Timeout: 5 * time.Minute}, token, nil
}

func stage(ctx context.Context, client *http.Client, token string, input model.Input, item pendingOutput,
	sequence, total uint32) (runtimecontract.OutputV2, error) {
	if len(item.payload) == 0 || int64(len(item.payload)) > maximumStagedOutputBytes {
		return runtimecontract.OutputV2{}, errors.New("runtime output size is invalid")
	}
	digest := sha256.Sum256(item.payload)
	digestText := hex.EncodeToString(digest[:])
	endpoint, err := url.Parse(strings.TrimRight(input.InteractionGateway.URL, "/"))
	if err != nil {
		return runtimecontract.OutputV2{}, errors.New("runtime output endpoint is invalid")
	}
	endpoint.Path = "/internal/v1/runtime-outputs/" + url.PathEscape(input.ExecutionID)
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), bytes.NewReader(item.payload))
	if err != nil {
		return runtimecontract.OutputV2{}, errors.New("create runtime output request")
	}
	request.ContentLength = int64(len(item.payload))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-MatterCodex-Output-Kind", item.kind)
	request.Header.Set("X-MatterCodex-Output-Name", item.name)
	request.Header.Set("X-MatterCodex-Output-SHA256", digestText)
	request.Header.Set("X-MatterCodex-Output-Media-Type", item.mediaType)
	request.Header.Set("X-MatterCodex-Output-Sequence", strconv.FormatUint(uint64(sequence), 10))
	request.Header.Set("X-MatterCodex-Output-Total", strconv.FormatUint(uint64(total), 10))
	response, err := client.Do(request)
	if err != nil {
		return runtimecontract.OutputV2{}, errors.New("runtime output staging is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated || response.ContentLength > maximumResponseBytes {
		return runtimecontract.OutputV2{}, errors.New("runtime output staging was rejected")
	}
	var reference stagedReference
	decoder := json.NewDecoder(io.LimitReader(response.Body, maximumResponseBytes+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&reference) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		uuid.Validate(reference.ArtifactID) != nil || reference.ArtifactVersion == 0 ||
		reference.SHA256 != digestText || reference.SizeBytes != uint64(len(item.payload)) ||
		reference.Name != item.name || reference.MediaType != item.mediaType ||
		!strings.HasPrefix(reference.StorageRef, "s3://") || len(reference.StorageRef) > 2048 ||
		strings.ContainsAny(reference.StorageRef, "\x00\r\n") {
		return runtimecontract.OutputV2{}, errors.New("runtime output staging readback is invalid")
	}
	return runtimecontract.OutputV2{Kind: item.kind, ID: reference.ArtifactID,
		Version: reference.ArtifactVersion, SHA256: reference.SHA256, Name: reference.Name,
		MediaType: reference.MediaType, StorageRef: reference.StorageRef, SizeBytes: reference.SizeBytes,
		Sequence: sequence, Total: total}, nil
}

func readOutput(directory int, name string, remaining int64) ([]byte, error) {
	descriptor, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), name)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, errors.New("wrap runtime outbox artifact")
	}
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(descriptor, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 ||
		stat.Size <= 0 || stat.Size > maximumStagedOutputBytes || stat.Size > remaining {
		return nil, errors.New("runtime outbox artifact is unsafe")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumStagedOutputBytes+1))
	if err != nil || int64(len(raw)) != stat.Size {
		return nil, errors.New("runtime outbox artifact content is invalid")
	}
	return raw, nil
}

func appendArtifactFailure(failures []artifactFailure, name, reason string) []artifactFailure {
	if len(failures) >= maximumFailureDetails {
		return failures
	}
	return append(failures, artifactFailure{name: name, reason: reason})
}

func makeInlineOutput(executionID, kind, name, mediaType string, payload []byte, sequence, total uint32) runtimecontract.OutputV2 {
	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	identifier := uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:runtime-output:"+executionID+":"+kind+":"+
		fmt.Sprint(sequence)+":"+digestText)).String()
	return runtimecontract.OutputV2{Kind: kind, ID: identifier, Version: 1, SHA256: digestText,
		Name: name, MediaType: mediaType, Payload: payload, SizeBytes: uint64(len(payload)), Sequence: sequence, Total: total}
}

func markdownSummary(value string, maximumRunes int) (string, bool) {
	if value == "" || !utf8.ValidString(value) || maximumRunes <= 0 || maximumRunes > 16383 {
		return "", true
	}
	runes := []rune(value)
	limit := min(maximumRunes, maximumSummaryRunes)
	count := min(len(runes), limit)
	for count > 0 && len([]byte(string(runes[:count]))) > maximumMarkdownBytes {
		count--
	}
	return string(runes[:count]), count != len(runes)
}

func normalizeSequences(outputs []runtimecontract.OutputV2) {
	totals := make(map[string]uint32, 3)
	for _, output := range outputs {
		totals[output.Kind]++
	}
	sequences := make(map[string]uint32, 3)
	for index := range outputs {
		sequences[outputs[index].Kind]++
		outputs[index].Sequence = sequences[outputs[index].Kind]
		outputs[index].Total = totals[outputs[index].Kind]
	}
}
