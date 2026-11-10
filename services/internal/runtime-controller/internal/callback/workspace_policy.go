package callback

import (
	"errors"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// workspacePolicyTool advertises only the opt-in profile's own policy read. Local file and
// command tools belong to the job catalog; this marker makes legacy runners reject readiness.
// workspacePolicyTool публикует чтение собственной opt-in policy. Файлы и команды принадлежат
// каталогу job; этот marker заставляет старые runner закрыто отклонять новый профиль.
func workspacePolicyTool() map[string]any {
	return map[string]any{
		"name":        runtimecontract.WorkspacePolicyTool,
		"description": "Read this execution's managed workspace policy. Does not execute commands or publish repository changes.",
		"inputSchema": objectSchema([]string{}, map[string]any{}),
		"outputSchema": objectSchema([]string{"profile", "input_digest", "tools", "commands", "maximum_bytes", "maximum_files"}, map[string]any{
			"profile":       map[string]any{"const": "managed-workspace-v1"},
			"input_digest":  stringSchema(64, 64),
			"tools":         map[string]any{"type": "array", "maxItems": 5, "items": stringSchema(1, 64)},
			"commands":      map[string]any{"type": "array", "maxItems": 128, "items": stringSchema(1, 160)},
			"maximum_bytes": map[string]any{"type": "integer", "minimum": 1},
			"maximum_files": map[string]any{"type": "integer", "minimum": 1},
		}),
	}
}

// workspacePolicy projects only the current immutable profile between fresh owner activity
// checks. It grants no external write, accepts no caller selectors and reveals no credentials.
// workspacePolicy проецирует текущий immutable профиль между свежими owner-проверками.
// Внешней записи не разрешает, caller selectors не принимает и credentials не раскрывает.
func workspacePolicy(input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	if len(arguments) != 0 || !runtimecontract.RuntimeWorkspaceToolsAvailable(input) {
		return nil, errors.New("managed workspace policy is unavailable")
	}
	commands := make([]string, 0, len(input.EnvironmentTools))
	for _, tool := range input.EnvironmentTools {
		commands = append(commands, tool.Command)
	}
	return map[string]any{"profile": "managed-workspace-v1", "input_digest": input.InputDigest,
		"tools": runtimecontract.RuntimeWorkspaceToolNames(input), "commands": commands,
		"maximum_bytes": input.WorkspacePolicy.MaximumWritableBytes,
		"maximum_files": input.WorkspacePolicy.MaximumFileCount}, nil
}
