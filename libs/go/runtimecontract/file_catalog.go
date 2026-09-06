package runtimecontract

import (
	"errors"
	"slices"
	"strings"
)

const (
	FilePurposeProject        = "PROJECT"
	FilePurposeWorkspaceInput = "WORKSPACE_INPUT"
	FilePurposeRunResult      = "RUN_RESULT"
	FilePurposeSkill          = "SKILL"
	FileToolSearch            = "search_files"
	FileToolMetadata          = "get_file_metadata"
	FileToolPreview           = "preview_file"
	FileToolManifest          = "get_file_manifest"
)

func IsRuntimeFileTool(tool string) bool {
	return tool == FileToolSearch || tool == FileToolMetadata || tool == FileToolPreview || tool == FileToolManifest
}

// RuntimeFileCatalog закрепляет private owner catalog, но не передаёт его
// содержимое или object-store authority в процесс агента.
type RuntimeFileCatalog struct {
	Ref      string   `json:"ref"`
	Digest   string   `json:"digest"`
	Total    int64    `json:"total"`
	Purposes []string `json:"purposes"`
}

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
