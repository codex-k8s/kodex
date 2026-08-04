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

// Build всегда сохраняет bounded terminal Markdown. Ошибка отдельного staged
// artifact добавляется в итог, но не уничтожает owner terminal transaction.
func Build(ctx context.Context, input model.Input, markdown, archivePath string) ([]runtimecontract.OutputV2, error) {
	if markdown == "" || !utf8.ValidString(markdown) {
		return nil, errors.New("runtime terminal Markdown is invalid")
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
		return nil, errors.New("read runtime outbox")
	}
	outbox := os.NewFile(uintptr(outboxDescriptor), input.OutboxRoot)
	if outbox == nil {
		_ = unix.Close(outboxDescriptor)
		return nil, errors.New("open runtime outbox")
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
			if path == archivePath {
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
			return nil, errors.New("read runtime outbox")
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
			failures = appendArtifactFailure(failures, item.name, "staging credential или TLS binding недоступен")
			continue
		}
		output, stageErr := stage(ctx, client, token, input, item, sequences[item.kind], counts[item.kind])
		if stageErr != nil {
			failed++
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
		notices = append(notices, fmt.Sprintf("%d выходных артефактов не удалось сохранить. Повторная доставка потребует Retry.", failed))
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
	return outputs, nil
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
