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
	// AttachmentManifestSchema identifies the read-only workspace input catalog format.
	// AttachmentManifestSchema обозначает формат каталога входов рабочего пространства только для чтения.
	AttachmentManifestSchema = "kodex.workspace-input-manifest"
	// AttachmentManifestVersion pins the manifest layout used for canonical serialization.
	// AttachmentManifestVersion закрепляет структуру манифеста для канонической сериализации.
	AttachmentManifestVersion = 1

	// AttachmentScopeInput denotes attachments supplied to the current input.
	// AttachmentScopeInput обозначает вложения текущего входного сообщения.
	AttachmentScopeInput = "INPUT"
	// AttachmentScopeSession denotes attachments retained from earlier session turns.
	// AttachmentScopeSession обозначает вложения предыдущих ходов сессии.
	AttachmentScopeSession = "SESSION"
	// AttachmentScopeKnowledge denotes project-bound reference material.
	// AttachmentScopeKnowledge обозначает справочные материалы, привязанные к проекту.
	AttachmentScopeKnowledge = "KNOWLEDGE"
)

// attachmentContexts closes the allowed owner-defined attachment purposes for input sets.
// attachmentContexts задаёт закрытый набор назначений входных вложений, определяемых владельцем.
var attachmentContexts = []string{
	"ASSISTANT_MESSAGE",
	"SESSION_TURN",
	"RUN_INPUT",
	"WORKFLOW_INPUT",
	"OWNER_GATE_MESSAGE",
}

// AttachmentManifestFile describes one immutable file exactly as exposed in the read-only workspace,
// including its artifact revision, content digest, provenance, and computed absolute path.
// AttachmentManifestFile описывает неизменяемый файл в том виде, как он доступен в workspace только для чтения,
// включая версию артефакта, дайджест содержимого, происхождение и вычисленный абсолютный путь.
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

// AttachmentManifest is the shared control-plane/runtime input catalog;
// its digest covers the canonical payload with the digest field omitted.
// AttachmentManifest — общий каталог входов control plane и runtime;
// его дайджест охватывает канонический payload без самого поля digest.
type AttachmentManifest struct {
	Schema            string                   `json:"schema"`
	Version           int                      `json:"version"`
	AttachmentSetRef  string                   `json:"attachmentSetRef"`
	AttachmentContext string                   `json:"attachmentContext"`
	Files             []AttachmentManifestFile `json:"files"`
	Digest            string                   `json:"digest"`
}

// CanonicalAttachmentManifest carries the typed catalog, deterministic JSON bytes, and payload digest.
// CanonicalAttachmentManifest хранит типизированный каталог, детерминированные байты JSON и дайджест payload.
type CanonicalAttachmentManifest struct {
	Manifest AttachmentManifest
	Bytes    []byte
	Digest   string
}

// WorkspaceAttachmentSet pins one admitted input set, its provenance, and an optional session turn.
// WorkspaceAttachmentSet закрепляет один допущенный набор входов, его происхождение и необязательный ход сессии.
type WorkspaceAttachmentSet struct {
	Ref            string `json:"attachmentSetRef"`
	ManifestDigest string `json:"manifestDigest"`
	Purpose        string `json:"purpose"`
	Scope          string `json:"scope"`
	Provenance     string `json:"provenance"`
	TurnRef        string `json:"turnRef,omitempty"`
}

// WorkspaceAttachmentManifest combines current, historical, and knowledge files for one runtime revision.
// WorkspaceAttachmentManifest объединяет текущие, исторические и справочные файлы одной версии runtime.
type WorkspaceAttachmentManifest struct {
	Schema         string                   `json:"schema"`
	Version        int                      `json:"version"`
	AttachmentSets []WorkspaceAttachmentSet `json:"attachmentSets"`
	Files          []AttachmentManifestFile `json:"files"`
}

// CanonicalWorkspaceAttachmentManifest pairs the combined typed catalog with its deterministic JSON bytes.
// CanonicalWorkspaceAttachmentManifest связывает общий типизированный каталог с детерминированными байтами JSON.
type CanonicalWorkspaceAttachmentManifest struct {
	Manifest WorkspaceAttachmentManifest
	Bytes    []byte
}

// attachmentManifestPayload excludes the digest field when hashing one attachment-set manifest.
// attachmentManifestPayload исключает поле дайджеста при хешировании манифеста одного набора вложений.
type attachmentManifestPayload struct {
	Schema            string                   `json:"schema"`
	Version           int                      `json:"version"`
	AttachmentSetRef  string                   `json:"attachmentSetRef"`
	AttachmentContext string                   `json:"attachmentContext"`
	Files             []AttachmentManifestFile `json:"files"`
}

// BuildAttachmentManifest validates server-owned metadata, sorts files, computes paths and purposes,
// and builds one canonical representation with a payload digest; it does not grant artifact access.
// BuildAttachmentManifest проверяет серверные метаданные, сортирует файлы, вычисляет пути и назначения,
// создаёт каноническое представление с дайджестом payload; права на артефакты он не выдаёт.
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

// BuildAttachmentSetManifest selects one exact set from an already validated runtime catalog,
// copies its files into the original INPUT representation and delegates canonical encoding.
// It does not validate runtime provenance, compare the expected digest or grant artifact access;
// callers retain those checks before materialization, and the supplied catalog is not mutated.
// BuildAttachmentSetManifest выбирает точный набор из уже проверенного каталога runtime,
// копирует его файлы в исходное представление INPUT и делегирует каноническое кодирование.
// Происхождение runtime, ожидаемый хеш и права на артефакты здесь не проверяются;
// эти проверки до материализации остаются у вызывающего кода, исходный каталог не изменяется.
func BuildAttachmentSetManifest(set RunnerAttachmentSet, artifacts []RunnerInputArtifact) (CanonicalAttachmentManifest, error) {
	selected := make([]RunnerInputArtifact, 0)
	for _, artifact := range artifacts {
		if artifact.AttachmentSetRef != set.Ref {
			continue
		}
		canonical := artifact
		canonical.Scope = AttachmentScopeInput
		canonical.AttachmentSetRef = ""
		canonical.AttachmentPurpose = ""
		canonical.Provenance = ""
		selected = append(selected, canonical)
	}
	return BuildAttachmentManifest(set.Ref, set.Purpose, selected)
}

// ArtifactWorkspacePath builds an absolute input or knowledge path from a validated filename,
// position, and set reference, replacing unsafe basename characters before joining.
// ArtifactWorkspacePath строит абсолютный путь входа или знаний по проверенным имени, позиции
// и ссылке набора, заменяя недопустимые символы базового имени до объединения пути.
func ArtifactWorkspacePath(attachmentSetRef string, artifact RunnerInputArtifact) (string, error) {
	if !validArtifactFileName(artifact.FileName) || artifact.Position < 1 {
		return "", errors.New("workspace artifact path is invalid")
	}
	name := fmt.Sprintf("%04d-%s", artifact.Position, normalizedWorkspaceBaseName(artifact.FileName))
	switch artifact.Scope {
	case AttachmentScopeInput, AttachmentScopeSession:
		setRef := artifact.AttachmentSetRef
		if setRef == "" {
			setRef = attachmentSetRef
		}
		if !opaqueReferencePattern.MatchString(setRef) {
			return "", errors.New("workspace attachment set path is invalid")
		}
		return filepath.ToSlash(filepath.Join("/workspace/input", setRef, "files", name)), nil
	case AttachmentScopeKnowledge:
		return filepath.ToSlash(filepath.Join("/workspace/knowledge", name)), nil
	default:
		return "", errors.New("workspace artifact scope is invalid")
	}
}

// BuildWorkspaceAttachmentManifest validates and deterministically orders all sets and knowledge files
// already authorized by one RuntimeRevision, rejecting colliding paths before serialization.
// BuildWorkspaceAttachmentManifest проверяет и детерминированно упорядочивает наборы и справочные файлы,
// уже разрешённые одной RuntimeRevision, отклоняя совпадающие пути до сериализации.
func BuildWorkspaceAttachmentManifest(sets []RunnerAttachmentSet, artifacts []RunnerInputArtifact) (CanonicalWorkspaceAttachmentManifest, error) {
	if _, err := validateRunnerAttachmentSets(sets, artifacts); err != nil {
		return CanonicalWorkspaceAttachmentManifest{}, err
	}
	orderedSets := append([]RunnerAttachmentSet(nil), sets...)
	sort.Slice(orderedSets, func(left, right int) bool {
		if orderedSets[left].Scope != orderedSets[right].Scope {
			return attachmentScopeRank(orderedSets[left].Scope) < attachmentScopeRank(orderedSets[right].Scope)
		}
		if orderedSets[left].TurnRef != orderedSets[right].TurnRef {
			return orderedSets[left].TurnRef < orderedSets[right].TurnRef
		}
		return orderedSets[left].Ref < orderedSets[right].Ref
	})
	manifestSets := make([]WorkspaceAttachmentSet, 0, len(orderedSets))
	for _, set := range orderedSets {
		manifestSets = append(manifestSets, WorkspaceAttachmentSet{
			Ref: set.Ref, ManifestDigest: set.ManifestDigest, Purpose: set.Purpose,
			Scope: set.Scope, Provenance: set.Provenance, TurnRef: set.TurnRef,
		})
	}
	orderedArtifacts := append([]RunnerInputArtifact(nil), artifacts...)
	sort.Slice(orderedArtifacts, func(left, right int) bool {
		if orderedArtifacts[left].Scope != orderedArtifacts[right].Scope {
			return attachmentScopeRank(orderedArtifacts[left].Scope) < attachmentScopeRank(orderedArtifacts[right].Scope)
		}
		if orderedArtifacts[left].AttachmentSetRef != orderedArtifacts[right].AttachmentSetRef {
			return orderedArtifacts[left].AttachmentSetRef < orderedArtifacts[right].AttachmentSetRef
		}
		if orderedArtifacts[left].Position != orderedArtifacts[right].Position {
			return orderedArtifacts[left].Position < orderedArtifacts[right].Position
		}
		return orderedArtifacts[left].Ref < orderedArtifacts[right].Ref
	})
	files := make([]AttachmentManifestFile, 0, len(orderedArtifacts))
	paths := make(map[string]struct{}, len(orderedArtifacts))
	for _, artifact := range orderedArtifacts {
		path, err := ArtifactWorkspacePath("", artifact)
		if err != nil {
			return CanonicalWorkspaceAttachmentManifest{}, err
		}
		if _, duplicate := paths[path]; duplicate {
			return CanonicalWorkspaceAttachmentManifest{}, errors.New("workspace attachment path is duplicated")
		}
		paths[path] = struct{}{}
		files = append(files, AttachmentManifestFile{
			ArtifactRef: artifact.Ref, Revision: artifact.Revision, Version: artifact.Version,
			FileName: artifact.FileName, MediaType: artifact.MediaType, SizeBytes: artifact.SizeBytes,
			SHA256: artifact.Digest, Position: artifact.Position, Scope: artifact.Scope,
			Source: artifact.Source, Purpose: artifact.AttachmentPurpose, Path: path,
		})
	}
	manifest := WorkspaceAttachmentManifest{
		Schema: AttachmentManifestSchema, Version: AttachmentManifestVersion,
		AttachmentSets: manifestSets, Files: files,
	}
	raw, err := json.Marshal(manifest)
	if err != nil || len(raw) > 1<<20 {
		return CanonicalWorkspaceAttachmentManifest{}, errors.New("encode workspace attachment manifest")
	}
	return CanonicalWorkspaceAttachmentManifest{Manifest: manifest, Bytes: raw}, nil
}

// validateRunnerInputArtifacts enforces metadata and byte bounds plus unique references and
// scope/position pairs, and reports whether a current-input artifact requires a set binding.
// validateRunnerInputArtifacts проверяет метаданные, объём, уникальные ссылки и пары область/позиция,
// а также сообщает, требуется ли привязка набора для артефакта текущего входа.
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

// validateRunnerAttachmentSets checks set provenance, exact artifact membership, per-catalog uniqueness,
// and aggregate byte limits, returning at most one current-turn set and rejecting empty sets.
// validateRunnerAttachmentSets проверяет происхождение наборов, принадлежность файлов, уникальность
// внутри каталога и общий объём, возвращая не более одного текущего набора и отклоняя пустые наборы.
func validateRunnerAttachmentSets(sets []RunnerAttachmentSet, artifacts []RunnerInputArtifact) (*RunnerAttachmentSet, error) {
	byRef := make(map[string]RunnerAttachmentSet, len(sets))
	itemCounts := make(map[string]int, len(sets))
	var current *RunnerAttachmentSet
	for _, set := range sets {
		if !opaqueReferencePattern.MatchString(set.Ref) || !sha256Pattern.MatchString(set.ManifestDigest) ||
			!containsString(attachmentContexts, set.Purpose) ||
			!containsString([]string{AttachmentScopeInput, AttachmentScopeSession}, set.Scope) ||
			!containsString([]string{"CURRENT_TURN", "SESSION_HISTORY"}, set.Provenance) ||
			(set.Scope == AttachmentScopeInput) != (set.Provenance == "CURRENT_TURN") {
			return nil, errors.New("runner attachment set catalog is invalid")
		}
		if set.TurnRef != "" && !opaqueReferencePattern.MatchString(set.TurnRef) {
			return nil, errors.New("runner attachment set catalog is invalid")
		}
		if _, duplicate := byRef[set.Ref]; duplicate {
			return nil, errors.New("runner attachment set catalog is invalid")
		}
		byRef[set.Ref] = set
		if set.Provenance == "CURRENT_TURN" {
			if current != nil {
				return nil, errors.New("runner attachment set catalog is invalid")
			}
			copy := set
			current = &copy
		}
	}

	artifactKeys := make(map[string]struct{}, len(artifacts))
	positions := make(map[string]struct{}, len(artifacts))
	var totalBytes int64
	for _, artifact := range artifacts {
		if !opaqueReferencePattern.MatchString(artifact.Ref) || !validArtifactFileName(artifact.FileName) ||
			strings.TrimSpace(artifact.MediaType) == "" || len(artifact.MediaType) > 255 ||
			!imageDigestPattern.MatchString(artifact.Digest) || artifact.SizeBytes < 0 ||
			artifact.SizeBytes > MaximumInputArtifactBytes || artifact.Revision < 1 || artifact.Version < 1 ||
			!containsString([]string{AttachmentScopeInput, AttachmentScopeSession, AttachmentScopeKnowledge}, artifact.Scope) || artifact.Position < 1 ||
			!containsString([]string{"CONTROL_CENTER", "AGENT_RESULT", "INTEGRATION_RESULT", "KNOWLEDGE_SOURCE", "INTERACTION_ATTACHMENT"}, artifact.Source) {
			return nil, errors.New("runner artifact catalog is invalid")
		}
		catalogKey := artifact.AttachmentSetRef
		if artifact.Scope == AttachmentScopeKnowledge {
			if artifact.AttachmentSetRef != "" || artifact.AttachmentPurpose != "PROJECT_KNOWLEDGE" || artifact.Provenance != "PROJECT_BINDING" {
				return nil, errors.New("runner artifact provenance is invalid")
			}
			catalogKey = AttachmentScopeKnowledge
		} else {
			set, exists := byRef[artifact.AttachmentSetRef]
			if !exists || artifact.Scope != set.Scope || artifact.AttachmentPurpose != set.Purpose || artifact.Provenance != set.Provenance {
				return nil, errors.New("runner artifact provenance is invalid")
			}
			itemCounts[set.Ref]++
		}
		artifactKey := catalogKey + ":" + artifact.Ref
		if _, duplicate := artifactKeys[artifactKey]; duplicate {
			return nil, errors.New("runner artifact catalog is invalid")
		}
		artifactKeys[artifactKey] = struct{}{}
		positionKey := catalogKey + ":" + strconv.FormatInt(artifact.Position, 10)
		if _, duplicate := positions[positionKey]; duplicate {
			return nil, errors.New("runner artifact catalog is invalid")
		}
		positions[positionKey] = struct{}{}
		if totalBytes > MaximumInputArtifactTotal-artifact.SizeBytes {
			return nil, errors.New("runner artifact catalog is invalid")
		}
		totalBytes += artifact.SizeBytes
	}
	for ref := range byRef {
		if itemCounts[ref] == 0 {
			return nil, errors.New("runner attachment set catalog is invalid")
		}
	}
	return current, nil
}

// attachmentPurpose derives stable purposes for historical and knowledge files, preserving current input context.
// attachmentPurpose назначает постоянные назначения историческим и справочным файлам, сохраняя контекст текущего входа.
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

// attachmentScopeRank orders current input before session history and project knowledge.
// attachmentScopeRank располагает текущие входы перед историей сессии и знаниями проекта.
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

// normalizedWorkspaceBaseName limits names to 160 ASCII bytes, replaces unsupported characters,
// trims edge dots, and supplies a fallback when no usable basename remains.
// normalizedWorkspaceBaseName ограничивает имя 160 ASCII-байтами, заменяет неподдерживаемые символы,
// удаляет точки по краям и возвращает запасное имя, если пригодного имени не осталось.
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
