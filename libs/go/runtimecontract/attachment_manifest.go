package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	AttachmentManifestSchema  = "kodex.workspace-input-manifest"
	AttachmentManifestVersion = 1

	AttachmentScopeInput     = "INPUT"
	AttachmentScopeSession   = "SESSION"
	AttachmentScopeKnowledge = "KNOWLEDGE"
)

var attachmentContexts = []string{
	"ASSISTANT_MESSAGE",
	"SESSION_TURN",
	"RUN_INPUT",
	"WORKFLOW_INPUT",
	"OWNER_GATE_MESSAGE",
}

// AttachmentManifestFile описывает один неизменяемый файл ровно в том виде,
// в котором он будет доступен runtime внутри read-only workspace.
type AttachmentManifestFile struct {
	ArtifactRef string `json:"artifactRef"`
	Revision    int64  `json:"revision"`
	Version     int64  `json:"version"`
	FileName    string `json:"fileName"`
	MediaType   string `json:"mediaType"`
	SizeBytes   int64  `json:"sizeBytes"`
	SHA256      string `json:"sha256"`
	Position    int64  `json:"position"`
	Scope       string `json:"scope"`
	Source      string `json:"source"`
	Purpose     string `json:"purpose"`
	Path        string `json:"path"`
}

// AttachmentManifest является единым каталогом входов для control plane и
// runtime. Digest связывает канонический payload без самого поля digest.
type AttachmentManifest struct {
	Schema            string                   `json:"schema"`
	Version           int                      `json:"version"`
	AttachmentSetRef  string                   `json:"attachmentSetRef"`
	AttachmentContext string                   `json:"attachmentContext"`
	Files             []AttachmentManifestFile `json:"files"`
	Digest            string                   `json:"digest"`
}

// CanonicalAttachmentManifest содержит детерминированный JSON и его digest.
type CanonicalAttachmentManifest struct {
	Manifest AttachmentManifest
	Bytes    []byte
	Digest   string
}

type attachmentManifestPayload struct {
	Schema            string                   `json:"schema"`
	Version           int                      `json:"version"`
	AttachmentSetRef  string                   `json:"attachmentSetRef"`
	AttachmentContext string                   `json:"attachmentContext"`
	Files             []AttachmentManifestFile `json:"files"`
}

// BuildAttachmentManifest строит единственное каноническое представление.
// Caller передаёт только server-owned metadata; путь и purpose вычисляются здесь.
func BuildAttachmentManifest(attachmentSetRef, attachmentContext string, artifacts []RunnerInputArtifact) (CanonicalAttachmentManifest, error) {
	hasInput, err := validateRunnerInputArtifacts(artifacts)
	if err != nil {
		return CanonicalAttachmentManifest{}, err
	}
	if hasInput {
		if !opaqueReferencePattern.MatchString(attachmentSetRef) || !containsString(attachmentContexts, attachmentContext) {
			return CanonicalAttachmentManifest{}, errors.New("attachment manifest binding is invalid")
		}
	} else if attachmentSetRef != "" || attachmentContext != "" {
		return CanonicalAttachmentManifest{}, errors.New("attachment manifest binding is invalid")
	}

	ordered := append([]RunnerInputArtifact(nil), artifacts...)
	sort.Slice(ordered, func(left, right int) bool {
		leftRank, rightRank := attachmentScopeRank(ordered[left].Scope), attachmentScopeRank(ordered[right].Scope)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if ordered[left].Position != ordered[right].Position {
			return ordered[left].Position < ordered[right].Position
		}
		return ordered[left].Ref < ordered[right].Ref
	})

	files := make([]AttachmentManifestFile, 0, len(ordered))
	paths := make(map[string]struct{}, len(ordered))
	for _, artifact := range ordered {
		path, pathErr := ArtifactWorkspacePath(attachmentSetRef, artifact)
		if pathErr != nil {
			return CanonicalAttachmentManifest{}, pathErr
		}
		if _, duplicate := paths[path]; duplicate {
			return CanonicalAttachmentManifest{}, errors.New("attachment manifest path is duplicated")
		}
		paths[path] = struct{}{}
		files = append(files, AttachmentManifestFile{
			ArtifactRef: artifact.Ref,
			Revision:    artifact.Revision,
			Version:     artifact.Version,
			FileName:    artifact.FileName,
			MediaType:   artifact.MediaType,
			SizeBytes:   artifact.SizeBytes,
			SHA256:      artifact.Digest,
			Position:    artifact.Position,
			Scope:       artifact.Scope,
			Source:      artifact.Source,
			Purpose:     attachmentPurpose(attachmentContext, artifact.Scope),
			Path:        path,
		})
	}

	payload := attachmentManifestPayload{
		Schema:            AttachmentManifestSchema,
		Version:           AttachmentManifestVersion,
		AttachmentSetRef:  attachmentSetRef,
		AttachmentContext: attachmentContext,
		Files:             files,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return CanonicalAttachmentManifest{}, errors.New("encode attachment manifest payload")
	}
	digestBytes := sha256.Sum256(rawPayload)
	digest := hex.EncodeToString(digestBytes[:])
	manifest := AttachmentManifest{
		Schema:            payload.Schema,
		Version:           payload.Version,
		AttachmentSetRef:  payload.AttachmentSetRef,
		AttachmentContext: payload.AttachmentContext,
		Files:             payload.Files,
		Digest:            digest,
	}
	rawManifest, err := json.Marshal(manifest)
	if err != nil || len(rawManifest) > 1<<20 {
		return CanonicalAttachmentManifest{}, errors.New("encode attachment manifest")
	}
	return CanonicalAttachmentManifest{Manifest: manifest, Bytes: rawManifest, Digest: digest}, nil
}

// ArtifactWorkspacePath вычисляет абсолютный путь без доверия имени файла.
func ArtifactWorkspacePath(attachmentSetRef string, artifact RunnerInputArtifact) (string, error) {
	if !validArtifactFileName(artifact.FileName) || artifact.Position < 1 {
		return "", errors.New("workspace artifact path is invalid")
	}
	name := fmt.Sprintf("%04d-%s", artifact.Position, normalizedWorkspaceBaseName(artifact.FileName))
	switch artifact.Scope {
	case AttachmentScopeInput:
		if !opaqueReferencePattern.MatchString(attachmentSetRef) {
			return "", errors.New("workspace attachment set path is invalid")
		}
		return filepath.ToSlash(filepath.Join("/workspace/input", attachmentSetRef, "files", name)), nil
	case AttachmentScopeSession:
		return filepath.ToSlash(filepath.Join("/workspace/session", name)), nil
	case AttachmentScopeKnowledge:
		return filepath.ToSlash(filepath.Join("/workspace/knowledge", name)), nil
	default:
		return "", errors.New("workspace artifact scope is invalid")
	}
}

func validateRunnerInputArtifacts(artifacts []RunnerInputArtifact) (bool, error) {
	artifactRefs := make(map[string]struct{}, len(artifacts))
	artifactPositions := make(map[string]struct{}, len(artifacts))
	var artifactBytes int64
	hasInput := false
	for _, artifact := range artifacts {
		if !opaqueReferencePattern.MatchString(artifact.Ref) || !validArtifactFileName(artifact.FileName) ||
			strings.TrimSpace(artifact.MediaType) == "" || len(artifact.MediaType) > 255 ||
			!imageDigestPattern.MatchString(artifact.Digest) || artifact.SizeBytes < 0 ||
			artifact.SizeBytes > MaximumInputArtifactBytes || artifact.Revision < 1 || artifact.Version < 1 ||
			!containsString([]string{AttachmentScopeInput, AttachmentScopeSession, AttachmentScopeKnowledge}, artifact.Scope) || artifact.Position < 1 ||
			!containsString([]string{"CONTROL_CENTER", "AGENT_RESULT", "INTEGRATION_RESULT", "KNOWLEDGE_SOURCE", "INTERACTION_ATTACHMENT"}, artifact.Source) {
			return false, errors.New("runner artifact catalog is invalid")
		}
		if _, exists := artifactRefs[artifact.Ref]; exists {
			return false, errors.New("runner artifact catalog is invalid")
		}
		artifactRefs[artifact.Ref] = struct{}{}
		positionKey := artifact.Scope + ":" + strconv.FormatInt(artifact.Position, 10)
		if _, exists := artifactPositions[positionKey]; exists {
			return false, errors.New("runner artifact catalog is invalid")
		}
		artifactPositions[positionKey] = struct{}{}
		hasInput = hasInput || artifact.Scope == AttachmentScopeInput
		if artifactBytes > MaximumInputArtifactTotal-artifact.SizeBytes {
			return false, errors.New("runner artifact catalog is invalid")
		}
		artifactBytes += artifact.SizeBytes
	}
	return hasInput, nil
}

func attachmentPurpose(attachmentContext, scope string) string {
	switch scope {
	case AttachmentScopeSession:
		return "SESSION_INPUT"
	case AttachmentScopeKnowledge:
		return "PROJECT_KNOWLEDGE"
	default:
		return attachmentContext
	}
}

func attachmentScopeRank(scope string) int {
	switch scope {
	case AttachmentScopeInput:
		return 0
	case AttachmentScopeSession:
		return 1
	default:
		return 2
	}
}

func normalizedWorkspaceBaseName(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '.' || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
		if builder.Len() >= 160 {
			break
		}
	}
	result := strings.Trim(builder.String(), ".")
	if result == "" {
		return "file.bin"
	}
	return result
}
