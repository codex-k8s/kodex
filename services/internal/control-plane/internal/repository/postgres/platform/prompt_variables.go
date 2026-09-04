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
	return map[string]any{
		"input":    promptFileScope(inputFiles, inputFilesDir, inputManifestPath),
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
	return artifact, map[string]any{
		"artifact_ref": artifact.Ref, "revision_ref": fmt.Sprintf("%s@%d", artifact.Ref, artifact.Revision),
		"name": artifact.FileName, "media_type": artifact.MediaType, "size": artifact.SizeBytes,
		"sha256": artifact.Digest, "path": path, "source": artifact.Source,
		"version": artifact.Version, "purpose": artifact.AttachmentPurpose,
	}, nil
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
