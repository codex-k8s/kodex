package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
)

// MaximumNativeSessionBindingBytes bounds owner metadata independently of the private transcript.
// MaximumNativeSessionBindingBytes ограничивает owner metadata независимо от private transcript.
const MaximumNativeSessionBindingBytes = 4096

// ErrNativeSession rejects invalid or mismatched recovery metadata without exposing private locators.
// ErrNativeSession отклоняет неверные или чужие recovery metadata без раскрытия private locators.
var ErrNativeSession = errors.New("native session checkpoint binding is invalid")

// nativeSessionIDPattern validates the qualified upstream identifier without accepting caller paths.
// nativeSessionIDPattern проверяет ID закреплённого upstream без допуска caller paths.
var nativeSessionIDPattern = regexp.MustCompile(`^ses_[A-Za-z0-9]{1,100}$`)

// NativeSessionCheckpoint is the control-plane-selected prior export, not a resume permission.
// Ownership, current grants and provider compatibility must be resolved before materialization.
// NativeSessionCheckpoint — выбранный control plane прежний export, а не разрешение resume.
// Владение, current grants и provider compatibility проверяются до materialization.
type NativeSessionCheckpoint struct {
	SessionID           string `json:"session_id"`
	Version             string `json:"version"`
	Generation          int64  `json:"generation"`
	RelativePath        string `json:"relative_path"`
	SHA256              string `json:"sha256"`
	SizeBytes           int64  `json:"size_bytes"`
	WorkspaceRef        string `json:"workspace_ref,omitempty"`
	WorkspaceGeneration int64  `json:"workspace_generation,omitempty"`
}

// DecodeNativeSessionCheckpoint rejects ambiguous/unknown metadata before any native state is opened.
// DecodeNativeSessionCheckpoint отклоняет ambiguous/unknown metadata до открытия native state.
func DecodeNativeSessionCheckpoint(raw []byte) (NativeSessionCheckpoint, error) {
	var value NativeSessionCheckpoint
	if len(raw) == 0 || len(raw) > MaximumNativeSessionBindingBytes || boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 2) != nil {
		return value, ErrNativeSession
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.Validate() != nil {
		return NativeSessionCheckpoint{}, ErrNativeSession
	}
	return value, nil
}

// NativeSessionExportPath derives immutable source paths from exact native identity and generation.
// It never accepts a directory, URL or arbitrary archive name from a prompt.
// NativeSessionExportPath выводит immutable source paths из exact native identity и generation.
// Directory, URL или произвольное имя архива из prompt не принимаются.
func NativeSessionExportPath(sessionID string, generation int64) (string, error) {
	if !nativeSessionIDPattern.MatchString(sessionID) || generation < 1 || generation >= 9007199254740991 {
		return "", ErrNativeSession
	}
	return ".kodex/state/opencode/checkpoints/g" + strconv.FormatInt(generation, 10) + "/" + sessionID + ".json", nil
}

// Validate checks the closed current native format and known export quota, not the archived bytes.
// Validate проверяет закрытый текущий native-формат и известную export-квоту, но не archived bytes.
func (checkpoint NativeSessionCheckpoint) Validate() error {
	path, err := NativeSessionExportPath(checkpoint.SessionID, checkpoint.Generation)
	if err != nil || checkpoint.Version != OpenCodeVersion || checkpoint.RelativePath != path || !sha256Pattern.MatchString(checkpoint.SHA256) || checkpoint.SizeBytes < 1 || checkpoint.SizeBytes > MaximumSessionSourceBytes {
		return ErrNativeSession
	}
	if (checkpoint.WorkspaceRef == "") != (checkpoint.WorkspaceGeneration == 0) || checkpoint.WorkspaceRef != "" && (!workspaceStorageRefPattern.MatchString(checkpoint.WorkspaceRef) || checkpoint.WorkspaceGeneration < 1) {
		return ErrNativeSession
	}
	return nil
}

// ValidateWorkspace requires native history and files to identify the same retained or restored tree.
// Current owner/fencing and S3 receipts remain mandatory; equal generation numbers alone grant nothing.
// ValidateWorkspace требует одинакового retained/restored дерева для native history и файлов.
// Current owner/fencing и S3 receipts обязательны; равенство generations не выдаёт полномочий.
func (checkpoint NativeSessionCheckpoint) ValidateWorkspace(workspace *WorkspaceStorageBinding) error {
	if checkpoint.Validate() != nil {
		return ErrNativeSession
	}
	if workspace == nil {
		if checkpoint.WorkspaceRef != "" {
			return ErrNativeSession
		}
		return nil
	}
	if checkpoint.WorkspaceRef != workspace.WorkspaceRef {
		return ErrNativeSession
	}
	if workspace.Source != nil {
		if checkpoint.WorkspaceGeneration != workspace.Source.Generation || checkpoint.WorkspaceRef != workspace.Source.WorkspaceRef {
			return ErrNativeSession
		}
	} else if checkpoint.WorkspaceGeneration != workspace.Generation {
		return ErrNativeSession
	}
	return nil
}

// ValidateCompletion binds a new native export to the current execution's source and file generation.
// It checks metadata only; the isolated broker separately proves actual bytes and writer termination.
// ValidateCompletion связывает новый native export с source и file generation текущего исполнения.
// Проверяет лишь metadata; isolated broker отдельно доказывает bytes и writer termination.
func (checkpoint NativeSessionCheckpoint) ValidateCompletion(input RunnerInput) error {
	if checkpoint.Validate() != nil || input.Provider != "opencode" || input.Mode != RunnerModeTurn {
		return ErrNativeSession
	}
	wantGeneration := int64(1)
	if input.NativeSession != nil {
		if input.NativeSession.Validate() != nil || input.NativeSession.SessionID != checkpoint.SessionID {
			return ErrNativeSession
		}
		wantGeneration = input.NativeSession.Generation + 1
	}
	if checkpoint.Generation != wantGeneration {
		return ErrNativeSession
	}
	if input.WorkspaceStorage == nil {
		if checkpoint.WorkspaceRef != "" {
			return ErrNativeSession
		}
		return nil
	}
	if checkpoint.WorkspaceRef != input.WorkspaceStorage.WorkspaceRef || checkpoint.WorkspaceGeneration != input.WorkspaceStorage.Generation {
		return ErrNativeSession
	}
	return nil
}
