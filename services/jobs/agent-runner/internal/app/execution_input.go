package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxCodexInstructionBytes  = 128 * 1024
	maxCodexResultSchemaBytes = 64 * 1024
	workspaceRefPrefix        = "workspace://"
	executionInputRoot        = ".kodex/execution"
)

type CodexExecutionInput struct {
	Instruction        []byte
	InstructionPath    string
	ResultSchemaPath   string
	ResultSchemaRef    string
	ResultSchemaDigest string
}

func LoadCodexExecutionInput(cfg Config, spec CodexSessionExecutionSpec) (CodexExecutionInput, Diagnostic) {
	instructionPath, diagnostic := workspaceExecutionPath(cfg, spec.InstructionObjectRef)
	if !diagnostic.OK() {
		return CodexExecutionInput{}, diagnostic
	}
	schemaPath, diagnostic := workspaceExecutionPath(cfg, spec.ResultSchemaRef)
	if !diagnostic.OK() {
		return CodexExecutionInput{}, diagnostic
	}
	instruction, diagnostic := readCheckedWorkspaceFile(instructionPath, spec.InstructionObjectDigest, maxCodexInstructionBytes, true)
	if !diagnostic.OK() {
		return CodexExecutionInput{}, diagnostic
	}
	if _, diagnostic := readCheckedWorkspaceFile(schemaPath, spec.ResultSchemaDigest, maxCodexResultSchemaBytes, true); !diagnostic.OK() {
		return CodexExecutionInput{}, diagnostic
	}
	if diagnostic := validateJSONFile(schemaPath); !diagnostic.OK() {
		return CodexExecutionInput{}, diagnostic
	}
	return CodexExecutionInput{
		Instruction:        instruction,
		InstructionPath:    instructionPath,
		ResultSchemaPath:   schemaPath,
		ResultSchemaRef:    spec.ResultSchemaRef,
		ResultSchemaDigest: spec.ResultSchemaDigest,
	}, OKDiagnostic()
}

func workspaceExecutionPath(cfg Config, ref string) (string, Diagnostic) {
	value := strings.TrimSpace(ref)
	if !strings.HasPrefix(value, workspaceRefPrefix) {
		return "", executionContractUnavailable("codex execution input ref is not a supported workspace ref")
	}
	relative := strings.TrimPrefix(value, workspaceRefPrefix)
	relative = strings.TrimLeft(relative, "/")
	cleanRelative := filepath.Clean(relative)
	if cleanRelative == "." || filepath.IsAbs(cleanRelative) || strings.HasPrefix(cleanRelative, "..") {
		return "", executionContractUnavailable("codex execution input ref is invalid")
	}
	cleanSlash := filepath.ToSlash(cleanRelative)
	if cleanSlash != executionInputRoot && !strings.HasPrefix(cleanSlash, executionInputRoot+"/") {
		return "", executionContractUnavailable("codex execution input ref is outside the execution contract")
	}
	workspace := filepath.Clean(cfg.WorkspaceMountPath)
	fullPath := filepath.Join(workspace, cleanRelative)
	relToWorkspace, err := filepath.Rel(workspace, fullPath)
	if err != nil || relToWorkspace == "." || strings.HasPrefix(relToWorkspace, "..") || filepath.IsAbs(relToWorkspace) {
		return "", executionContractUnavailable("codex execution input ref is outside the workspace")
	}
	return fullPath, OKDiagnostic()
}

func readCheckedWorkspaceFile(path string, digest string, maxBytes int64, required bool) ([]byte, Diagnostic) {
	info, err := os.Stat(path)
	if err != nil {
		if required {
			return nil, executionContractUnavailable("codex execution input file is unavailable")
		}
		return nil, OKDiagnostic()
	}
	if !info.Mode().IsRegular() {
		return nil, executionContractUnavailable("codex execution input ref is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > maxBytes {
		return nil, executionContractUnavailable("codex execution input file has invalid size")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, executionContractUnavailable("codex execution input file is unavailable")
	}
	if !digestMatches(raw, digest) {
		return nil, executionContractUnavailable("codex execution input digest does not match")
	}
	return raw, OKDiagnostic()
}

func validateJSONFile(path string) Diagnostic {
	raw, err := os.ReadFile(path)
	if err != nil {
		return executionContractUnavailable("codex result schema file is unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || len(value) == 0 {
		return executionContractUnavailable("codex result schema JSON is invalid")
	}
	var extra json.RawMessage
	err = decoder.Decode(&extra)
	if err == nil || !errors.Is(err, io.EOF) {
		return executionContractUnavailable("codex result schema JSON is invalid")
	}
	return OKDiagnostic()
}
