package runtimecontract

import (
	"errors"
	"slices"
	"strings"
)

const (
	// FilePurposeProject identifies files scoped to the current project.
	// FilePurposeProject обозначает файлы текущего проекта.
	FilePurposeProject = "PROJECT"
	// FilePurposeWorkspaceInput identifies files admitted as workspace inputs.
	// FilePurposeWorkspaceInput обозначает файлы, допущенные как входные данные рабочего пространства.
	FilePurposeWorkspaceInput = "WORKSPACE_INPUT"
	// FilePurposeRunResult identifies files produced by a completed run.
	// FilePurposeRunResult обозначает файлы, созданные завершённым запуском.
	FilePurposeRunResult = "RUN_RESULT"
	// FilePurposeSkill identifies files belonging to an available skill.
	// FilePurposeSkill обозначает файлы доступного навыка.
	FilePurposeSkill = "SKILL"
	// FileToolSearch names the capability for bounded file discovery.
	// FileToolSearch называет возможность ограниченного поиска файлов.
	FileToolSearch = "search_files"
	// FileToolMetadata names the capability for file metadata lookup.
	// FileToolMetadata называет возможность чтения метаданных файла.
	FileToolMetadata = "get_file_metadata"
	// FileToolPreview names the capability for a bounded file preview.
	// FileToolPreview называет возможность ограниченного просмотра файла.
	FileToolPreview = "preview_file"
	// FileToolManifest names the capability for a file manifest lookup.
	// FileToolManifest называет возможность чтения манифеста файла.
	FileToolManifest = "get_file_manifest"
	// FileToolRead names the capability for an authorized file read.
	// FileToolRead называет возможность разрешённого чтения файла.
	FileToolRead = "read_file"
)

// IsRuntimeFileTool limits file operations to the closed runtime tool set.
// IsRuntimeFileTool ограничивает операции с файлами закрытым набором инструментов runtime.
func IsRuntimeFileTool(tool string) bool {
	return tool == FileToolSearch || tool == FileToolMetadata || tool == FileToolPreview || tool == FileToolManifest || tool == FileToolRead
}

// RuntimeFileCatalog binds a private owner catalog by reference and digest without granting
// its contents or object-store authority to the agent process.
// RuntimeFileCatalog связывает закрытый каталог владельца по ссылке и дайджесту, не передавая
// его содержимое или полномочия object store процессу агента.
type RuntimeFileCatalog struct {
	Ref      string   `json:"ref"`
	Digest   string   `json:"digest"`
	Total    int64    `json:"total"`
	Purposes []string `json:"purposes"`
}

// Validate rejects malformed bindings and requires a unique sorted subset of allowed purposes.
// Validate отклоняет неверную привязку и требует уникальное отсортированное подмножество назначений.
func (catalog RuntimeFileCatalog) Validate() error {
	if !strings.HasPrefix(catalog.Ref, "vfc_") || !opaqueReferencePattern.MatchString(catalog.Ref) || !sha256Pattern.MatchString(catalog.Digest) || catalog.Total < 0 || len(catalog.Purposes) < 1 || len(catalog.Purposes) > 4 {
		return errors.New("runtime file catalog binding is invalid")
	}
	for index, purpose := range catalog.Purposes {
		if !slices.Contains([]string{FilePurposeProject, FilePurposeWorkspaceInput, FilePurposeRunResult, FilePurposeSkill}, purpose) || index > 0 && catalog.Purposes[index-1] >= purpose {
			return errors.New("runtime file catalog purpose is invalid")
		}
	}
	return nil
}
