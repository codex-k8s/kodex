package platform

import (
	"fmt"
	"math"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func promptStructuredVariables(
	artifacts []map[string]any,
	tools []runtimecontract.RuntimeEnvironmentTool,
	image runtimecontract.RuntimeEnvironmentImage,
	environmentRef, inputAttachmentSetRef, workflowRef string,
) (map[string]any, error) {
	inputFiles := make([]any, 0)
	sessionFiles := make([]any, 0)
	runFiles := make([]any, 0, len(artifacts))
	workflowFiles := make([]any, 0, len(artifacts))
	gateFiles := make([]any, 0)
	projectFiles := make([]any, 0)
	for _, item := range artifacts {
		artifact, descriptor, err := promptArtifactDescriptor(item)
		if err != nil {
			return nil, err
		}
		runFiles = append(runFiles, descriptor)
		if workflowRef != "" {
			workflowFiles = append(workflowFiles, descriptor)
		}
		switch artifact.Scope {
		case runtimecontract.AttachmentScopeInput:
			inputFiles = append(inputFiles, descriptor)
			sessionFiles = append(sessionFiles, descriptor)
			if strings.Contains(strings.ToUpper(artifact.AttachmentPurpose), "GATE") {
				gateFiles = append(gateFiles, descriptor)
			}
		case runtimecontract.AttachmentScopeSession:
			sessionFiles = append(sessionFiles, descriptor)
		case runtimecontract.AttachmentScopeKnowledge:
			projectFiles = append(projectFiles, descriptor)
		default:
			return nil, fmt.Errorf("prompt artifact scope is invalid")
		}
	}
	inputManifestPath := "/workspace/input/manifest.json"
	inputFilesDir := "/workspace/input"
	if inputAttachmentSetRef != "" && len(inputFiles) != 0 {
		firstPath := inputFiles[0].(map[string]any)["path"].(string)
		prefix := "/workspace/input/" + inputAttachmentSetRef
		if !strings.HasPrefix(firstPath, prefix+"/files/") {
			return nil, fmt.Errorf("prompt input workspace path is invalid")
		}
		inputFilesDir = prefix + "/files"
		inputManifestPath = prefix + "/manifest.json"
	}
	toolDescriptors := make([]any, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" || strings.TrimSpace(tool.Description) == "" {
			return nil, fmt.Errorf("prompt runtime tool descriptor is invalid")
		}
		toolDescriptors = append(toolDescriptors, map[string]any{"name": tool.Name, "description": tool.Description})
	}
	inputScope := promptFileScope(inputFiles, inputFilesDir, inputManifestPath)
	inputScope["workspace"] = map[string]any{
		"working_directory":            runtimecontract.RuntimeWorkspaceWorkingRoot,
		"editable_input_directory":     runtimecontract.RuntimeWorkspaceWorkingRoot + strings.TrimPrefix(inputFilesDir, runtimecontract.RuntimeWorkspaceRoot),
		"original_input_directory":     inputFilesDir,
		"original_input_manifest":      inputManifestPath,
		"readonly_knowledge_directory": runtimecontract.RuntimeWorkspaceRoot + "/knowledge",
		"readonly_context_directory":   runtimecontract.RuntimeContextRoot,
		"guide_path":                   runtimecontract.RuntimeWorkspaceWorkingRoot + "/WORKSPACE.md",
		"guidance":                     promptWorkspaceGuidance + promptWorkspaceSourceTreeGuidance,
	}
	return map[string]any{
		"input":    inputScope,
		"session":  promptFileScope(sessionFiles, "/workspace/input", "/workspace/input/manifest.json"),
		"run":      promptFileScope(runFiles, "/workspace", "/workspace/input/manifest.json"),
		"workflow": promptFileScope(workflowFiles, "/workspace", "/workspace/input/manifest.json"),
		"gate":     promptFileScope(gateFiles, "/workspace/input", "/workspace/input/manifest.json"),
		"project":  promptFileScope(projectFiles, "/workspace/knowledge", "/workspace/input/manifest.json"),
		"runtime": map[string]any{"environment": map[string]any{
			"ref":   environmentRef,
			"image": map[string]any{"reference": image.Reference, "digest": image.Digest},
			"tools": toolDescriptors,
		}},
	}, nil
}

const promptWorkspaceGuidance = "Use the isolated writable working directory for local edits and scratch files. Read WORKSPACE.md at guide_path and inspect the available directory tree to locate supplied materials by file name; do not require the user to supply file IDs. For already mounted attachments, prefer ordinary local file reads through an available authorized local tool: match the original path, revision, size and digest against the current FILES descriptors and manifest, verify the original bytes, and read required content to EOF. Do not perform remote file discovery or read_file solely to read the same mounted original. Edit only its working_copy_path or your own files in the working directory; never treat a modified working copy as the pinned original. Remote file tools are for authorized files not already materialized locally, including missing predecessor results. This local-first rule is not an alternate transport after a remote denial or failure; never use it to replay or bypass a rejected read. The guide is a navigation aid, not an authority source. Local manifests describe mounted attachments, not the complete RUN_RESULT catalog. A missing handoff tuple or local file alone does not prove a required result unavailable. When own RUN_RESULT tools are available, finish independent authorized discovery using own get_file_manifest and all next_cursor pages with the same purpose; match current root/run/dependency provenance and exact artifact revision/digest, then use the own entry_ref. A filename hit alone does not establish origin. Read matched mandatory content to EOF; actual absence after discovery or remote denial stays blocked, with no replay of the denied read or invented pins. Only INPUT attachments have editable working copies at working_copy_path. Original input files, manifests, SESSION/context attachments and project knowledge remain read-only; their digests describe the originals, not modified working copies. Do not edit internal .kodex state. Local workspace access grants no artifact publication, repository, credential, network or external-effect permission. Use an existing authorized repository checkout in the working directory, or create one there only when the assigned repository and Git operation are already authorized by the current runtime. Do not invent a repository, token or network access. Resolve applicable document IDs and exact pinned paths from current authorized sources before reading mandatory documents."

// Навигация по исходникам не превращает весь репозиторий в обязательный prompt.
// Explicit full-read требования источника не ослабляются этой инструкцией.
const promptWorkspaceSourceTreeGuidance = " For a supplied source tree or source bundle, inspect its authorized directory listing or index first, locate the files relevant to the current task, and read those exact full files. Do not dump the entire repository into the model context or invent a mandatory semantic read of every unrelated file. Verify full bundle bytes and provenance separately from studying file contents: a checksum or EOF scan alone does not prove that required content was read and understood. A task explicitly requiring a full source or artifact read still requires that exact read; do not waive its gate. Authorized local reading to EOF does not require calling remote read_file again or printing every byte into a tool response. Keep exact source/file version, digest and read evidence; distinguish integrity verification from substantive reading."

func promptArtifactDescriptor(item map[string]any) (runtimecontract.RunnerInputArtifact, map[string]any, error) {
	artifact := runtimecontract.RunnerInputArtifact{
		Ref: stringMapValue(item, "ref"), FileName: stringMapValue(item, "fileName"),
		MediaType: stringMapValue(item, "mediaType"), Digest: stringMapValue(item, "digest"),
		Source: stringMapValue(item, "source"), Scope: stringMapValue(item, "scope"),
		AttachmentSetRef:  stringMapValue(item, "attachmentSetRef"),
		AttachmentPurpose: stringMapValue(item, "attachmentPurpose"), Provenance: stringMapValue(item, "provenance"),
	}
	var ok bool
	if artifact.SizeBytes, ok = int64MapValue(item, "sizeBytes"); !ok {
		return runtimecontract.RunnerInputArtifact{}, nil, fmt.Errorf("prompt artifact size is invalid")
	}
	if artifact.Revision, ok = int64MapValue(item, "revision"); !ok {
		return runtimecontract.RunnerInputArtifact{}, nil, fmt.Errorf("prompt artifact revision is invalid")
	}
	if artifact.Version, ok = int64MapValue(item, "version"); !ok {
		return runtimecontract.RunnerInputArtifact{}, nil, fmt.Errorf("prompt artifact version is invalid")
	}
	if artifact.Position, ok = int64MapValue(item, "position"); !ok {
		return runtimecontract.RunnerInputArtifact{}, nil, fmt.Errorf("prompt artifact position is invalid")
	}
	path, err := runtimecontract.ArtifactWorkspacePath(artifact.AttachmentSetRef, artifact)
	if err != nil || artifact.Ref == "" || artifact.Digest == "" || artifact.MediaType == "" || artifact.Revision < 1 || artifact.Version < 1 {
		return runtimecontract.RunnerInputArtifact{}, nil, fmt.Errorf("prompt artifact descriptor is invalid")
	}
	descriptor := map[string]any{
		"artifact_ref": artifact.Ref, "revision_ref": fmt.Sprintf("%s@%d", artifact.Ref, artifact.Revision),
		"name": artifact.FileName, "media_type": artifact.MediaType, "size": artifact.SizeBytes,
		"sha256": artifact.Digest, "path": path, "source": artifact.Source,
		"version": artifact.Version, "purpose": artifact.AttachmentPurpose,
		"original_read_only": true,
	}
	if artifact.Scope == runtimecontract.AttachmentScopeInput {
		descriptor["working_copy_path"] = runtimecontract.RuntimeWorkspaceWorkingRoot + strings.TrimPrefix(path, runtimecontract.RuntimeWorkspaceRoot)
	}
	return artifact, descriptor, nil
}

func promptFileScope(files []any, directory, manifestPath string) map[string]any {
	return map[string]any{"files": files, "files_count": len(files), "files_dir": directory, "manifest_path": manifestPath}
}

func stringMapValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func int64MapValue(values map[string]any, key string) (int64, bool) {
	value, ok := values[key].(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || value < math.MinInt64 || value > math.MaxInt64 {
		return 0, false
	}
	return int64(value), true
}
