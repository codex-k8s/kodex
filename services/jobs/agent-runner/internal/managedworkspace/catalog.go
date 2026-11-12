package managedworkspace

import "github.com/codex-k8s/kodex/libs/go/runtimecontract"

// objectSchema closes local tool objects; bounds and authority remain enforced by handlers.
// objectSchema закрывает local tool objects; handlers дополнительно проверяют пределы и authority.
func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

// textSchema describes bounded UTF-8 JSON strings, never an executable shell command line.
// textSchema описывает bounded UTF-8 JSON strings, не исполняемую shell command line.
func textSchema(maximum int) map[string]any {
	return map[string]any{"type": "string", "maxLength": maximum}
}

// workspacePathSchema describes the existing lexical boundary without normalizing caller paths.
// workspacePathSchema описывает действующую границу путей без нормализации ввода вызывающего.
func workspacePathSchema() map[string]any {
	value := textSchema(1024)
	value["description"] = "Canonical relative path in this session's workspace: work, work/subdirectory/file, outbox, or outbox/file. Outbox is flat. Absolute paths (including /workspace/work/file), trailing slashes, dot or parent components and backslashes are rejected. Another session's workspace is not accessible through this tool."
	return value
}

// integerSchema encodes inclusive local tool bounds without client-side coercion or defaults.
// integerSchema кодирует включительные границы local tool без client-side coercion либо defaults.
func integerSchema(minimum, maximum int64) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
}

// Tools owns the separate job catalog. These descriptors never modify the controller's primary
// binding digest; its explicit get_workspace_policy marker fences legacy runner compatibility.
// Tools владеет отдельным job catalog. Descriptors не меняют primary binding digest controller;
// явный marker get_workspace_policy закрыто проверяет совместимость со старыми runner.
func Tools(input runtimecontract.RunnerInput) []map[string]any {
	result := make([]map[string]any, 0, 5)
	for _, name := range runtimecontract.RuntimeWorkspaceToolNames(input) {
		var description string
		var arguments, output map[string]any
		switch name {
		case runtimecontract.WorkspaceToolList:
			description = "List one work or flat outbox directory; follow next_after only when has_more is true. No private state or platform input paths."
			arguments = objectSchema([]string{"path"}, map[string]any{"path": workspacePathSchema(), "after": textSchema(255)})
			entry := objectSchema([]string{"path", "kind", "size_bytes"}, map[string]any{"path": textSchema(1280), "kind": map[string]any{"enum": []string{"FILE", "DIRECTORY"}}, "size_bytes": integerSchema(0, 1<<40)})
			output = objectSchema([]string{"entries", "has_more", "next_after"}, map[string]any{"entries": map[string]any{"type": "array", "maxItems": 200, "items": entry}, "has_more": map[string]any{"type": "boolean"}, "next_after": textSchema(255)})
		case runtimecontract.WorkspaceToolRead:
			description = "Read a UTF-8 source chunk from this session's workspace, with complete file SHA-256 for compare-and-swap edits. To read another employee's published result, use authorized platform artifact tools get_file_manifest and read_file when available; its workspace path does not identify a local file. maximum_bytes is 4..32768 per call (default 32768), even when the file is larger. offset_bytes and next_offset_bytes are byte offsets; follow next_offset_bytes for further chunks. The complete file ceiling is one MiB."
			arguments = objectSchema([]string{"path"}, map[string]any{"path": workspacePathSchema(), "offset_bytes": integerSchema(0, maximumFileBytes), "maximum_bytes": integerSchema(4, 32768)})
			output = objectSchema([]string{"text", "sha256", "size_bytes", "offset_bytes", "next_offset_bytes"}, map[string]any{"text": textSchema(32768), "sha256": textSchema(64), "size_bytes": integerSchema(0, maximumFileBytes), "offset_bytes": integerSchema(0, maximumFileBytes), "next_offset_bytes": integerSchema(0, maximumFileBytes)})
		case runtimecontract.WorkspaceToolWrite:
			description = "Atomically write local UTF-8 bytes only. expected_sha256 must be the complete previous hash from read_workspace_file, or ABSENT to create a new file. Does not publish an artifact, commit or MR."
			arguments = objectSchema([]string{"path", "text", "expected_sha256"}, map[string]any{"path": workspacePathSchema(), "text": textSchema(maximumFileBytes), "expected_sha256": textSchema(64)})
			output = objectSchema([]string{"sha256"}, map[string]any{"sha256": textSchema(64)})
		case runtimecontract.WorkspaceToolExecute:
			description = "Run exactly one environment-selected command with separate arguments in work, inside a secret-free container. No personal Git/API/model tokens are supplied. Timeout or unknown outcome must not be automatically retried. This result is local execution, not external CI evidence."
			directory := textSchema(1024)
			directory["description"] = "Canonical relative directory: work for the work root, or work/subdirectory. Absolute paths, trailing slashes, dot and parent components are rejected. Command arguments use paths relative to this directory."
			commands := make([]string, 0, len(input.EnvironmentTools))
			for _, tool := range input.EnvironmentTools {
				commands = append(commands, tool.Command)
			}
			arguments = objectSchema([]string{"command", "arguments", "directory", "timeout_seconds"}, map[string]any{"command": map[string]any{"type": "string", "enum": commands}, "arguments": map[string]any{"type": "array", "maxItems": 64, "items": textSchema(8192)}, "directory": directory, "timeout_seconds": integerSchema(1, maximumCommandSeconds)})
			output = objectSchema([]string{"status", "exit_code", "output", "truncated"}, map[string]any{"status": map[string]any{"type": "string", "enum": []string{"SUCCEEDED", "FAILED", "TIMED_OUT", "CANCELLED", "UNKNOWN"}}, "exit_code": integerSchema(-1, 255), "output": textSchema(maximumCommandOutput), "truncated": map[string]any{"type": "boolean"}})
		case runtimecontract.WorkspaceToolCheckout:
			description = "Materialize one complete authorized GitLab source snapshot at an exact commit SHA into a NEW work directory. Uses managed commit/tree/file read grants, never PAT or git transport. Returns API_SNAPSHOT provenance, not a full git clone or fabricated local HEAD. Symlinks, submodules, unsafe paths, partial pages and oversized files are rejected."
			destination := textSchema(1024)
			destination["description"] = "Canonical relative NEW directory in this session's workspace, such as work/source. Absolute paths, trailing slashes, dot and parent components are rejected. Another session's workspace is not accessible through this tool."
			arguments = objectSchema([]string{"connection_ref", "project_path", "project_id", "commit_sha", "destination"}, map[string]any{"connection_ref": textSchema(96), "project_path": textSchema(255), "project_id": integerSchema(1, 9007199254740991), "commit_sha": textSchema(64), "destination": destination})
			output = objectSchema([]string{"representation", "source_commit_sha", "destination", "files", "bytes", "manifest_sha256"}, map[string]any{"representation": map[string]any{"const": "API_SNAPSHOT"}, "source_commit_sha": textSchema(64), "destination": textSchema(1024), "files": integerSchema(0, 128), "bytes": integerSchema(0, 8<<20), "manifest_sha256": textSchema(64)})
		}
		result = append(result, map[string]any{"name": name, "description": description, "inputSchema": arguments, "outputSchema": output})
	}
	return result
}
