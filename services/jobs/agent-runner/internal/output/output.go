// Package output формирует bounded owner-deliverable artifacts из outbox.
package output

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/matter-codex/libs/go/runtimecontract"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

func Build(input model.Input, markdown, archivePath string) ([]runtimecontract.OutputV2, error) {
	chunks := chunkMarkdown(markdown, input.MattermostPostMaximumRunes)
	outputs := make([]runtimecontract.OutputV2, 0, len(chunks)+8)
	for index, chunk := range chunks {
		outputs = append(outputs, makeOutput(input.ExecutionID, "FINAL_MARKDOWN",
			fmt.Sprintf("result-%03d.md", index+1), "text/markdown", []byte(chunk), uint32(index+1), uint32(len(chunks))))
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
	entries, err := outbox.ReadDir(-1)
	if err != nil {
		return nil, errors.New("read runtime outbox")
	}
	slices.SortFunc(entries, func(left, right os.DirEntry) int {
		return strings.Compare(left.Name(), right.Name())
	})
	type pendingOutput struct {
		kind, name, mediaType string
		payload               []byte
	}
	pending := make([]pendingOutput, 0, len(entries))
	counts := map[string]uint32{}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(input.OutboxRoot, name)
		if path == archivePath {
			continue
		}
		if strings.ContainsAny(name, "/\\\x00\r\n") {
			return nil, errors.New("runtime outbox artifact is unsafe")
		}
		raw, err := readOutput(outboxDescriptor, name)
		if err != nil {
			return nil, errors.New("read runtime outbox artifact")
		}
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
		counts[kind]++
	}
	sequences := map[string]uint32{}
	for _, item := range pending {
		sequences[item.kind]++
		outputs = append(outputs, makeOutput(input.ExecutionID, item.kind, item.name, item.mediaType,
			item.payload, sequences[item.kind], counts[item.kind]))
	}
	if len(outputs) > runtimecontract.MaximumOutputs {
		return nil, errors.New("runtime output count exceeds the limit")
	}
	total := 0
	for _, item := range outputs {
		total += len(item.Payload)
	}
	if total > runtimecontract.MaximumOutputBytes {
		return nil, errors.New("runtime output payload exceeds the limit")
	}
	return outputs, nil
}

func readOutput(directory int, name string) ([]byte, error) {
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
		stat.Size <= 0 || stat.Size > runtimecontract.MaximumOutputBytes {
		return nil, errors.New("runtime outbox artifact is unsafe")
	}
	raw, err := io.ReadAll(io.LimitReader(file, runtimecontract.MaximumOutputBytes+1))
	if err != nil || int64(len(raw)) != stat.Size {
		return nil, errors.New("runtime outbox artifact content is invalid")
	}
	return raw, nil
}

func makeOutput(executionID, kind, name, mediaType string, payload []byte, sequence, total uint32) runtimecontract.OutputV2 {
	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	identifier := uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:runtime-output:"+executionID+":"+kind+":"+
		fmt.Sprint(sequence)+":"+digestText)).String()
	return runtimecontract.OutputV2{Kind: kind, ID: identifier, Version: 1, SHA256: digestText,
		Name: name, MediaType: mediaType, Payload: payload, Sequence: sequence, Total: total}
}

func chunkMarkdown(value string, maximumRunes int) []string {
	if value == "" || !utf8.ValidString(value) || maximumRunes <= 0 || maximumRunes > 16383 {
		return nil
	}
	runes := []rune(value)
	result := make([]string, 0, (len(runes)+maximumRunes-1)/maximumRunes)
	for len(runes) > 0 {
		count := maximumRunes
		if len(runes) < count {
			count = len(runes)
		}
		result = append(result, string(runes[:count]))
		runes = runes[count:]
	}
	return result
}
