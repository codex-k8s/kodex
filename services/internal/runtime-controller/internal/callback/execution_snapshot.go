package callback

import (
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func executionSnapshotTool() map[string]any {
	properties := map[string]any{
		"run_ref": opaqueRefSchema(), "node_ref": opaqueRefSchema(), "session_ref": opaqueRefSchema(), "turn_ref": opaqueRefSchema(),
		"attempt":                  map[string]any{"type": "integer", "minimum": 1},
		"runtime_revision_ref":     stringSchema(8, 128),
		"runtime_revision_version": map[string]any{"type": "integer", "minimum": 1},
		"runtime_revision_digest":  stringSchema(64, 64),
		"image_reference":          stringSchema(1, 4096), "image_manifest_digest": stringSchema(71, 71),
		"model": stringSchema(1, 128), "reasoning_effort": stringSchema(0, 32),
		"provider_process": map[string]any{"anyOf": []any{
			objectSchema([]string{"status", "provider"}, map[string]any{
				"status": map[string]any{"const": "UNKNOWN"}, "provider": map[string]any{"const": "CODEX"},
			}),
			objectSchema([]string{"status", "provider", "version", "observation_source"}, map[string]any{
				"status": map[string]any{"const": "OBSERVED"}, "provider": map[string]any{"const": "CODEX"},
				"version": stringSchema(1, 64), "observation_source": map[string]any{"const": "INITIALIZE_USER_AGENT"},
			}),
		}},
	}
	return map[string]any{
		"name":        runtimecontract.ExecutionSnapshotTool,
		"description": "Read only this current execution's immutable runtime/image/model pins and actual serving provider-process observation. No arguments or foreign/historical selectors. UNKNOWN means no bound initialize observation; image inventory and client version are not process evidence.",
		"inputSchema": objectSchema([]string{}, map[string]any{}),
		"outputSchema": objectSchema([]string{"run_ref", "node_ref", "session_ref", "turn_ref", "attempt",
			"runtime_revision_ref", "runtime_revision_version", "runtime_revision_digest", "image_reference", "image_manifest_digest",
			"model", "reasoning_effort", "provider_process"}, properties),
	}
}

// Вызывается только между двумя существующими fresh owner RecordRunToolCall
// проверками. Immutable input назначен сервером; caller не выбирает координаты.
func (server *Server) executionSnapshot(input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	if len(arguments) != 0 || !runtimecontract.RuntimeExecutionSnapshotAvailable(input) ||
		!assistantCatalogModelPattern.MatchString(input.Model) || len(input.EffectiveReasoningEffort) > 32 {
		return nil, errors.New("execution snapshot is not available")
	}
	return map[string]any{
		"run_ref": input.RunRef, "node_ref": input.NodeRef, "session_ref": input.SessionRef, "turn_ref": input.TurnRef, "attempt": input.Attempt,
		"runtime_revision_ref": input.RuntimeRevisionRef, "runtime_revision_version": input.RuntimeRevisionVersion, "runtime_revision_digest": input.RuntimeRevisionDigest,
		"image_reference": input.ImageReference, "image_manifest_digest": input.ImageManifestDigest,
		"model": input.Model, "reasoning_effort": input.EffectiveReasoningEffort,
		"provider_process": server.coordinator.providerProcessSnapshot(input),
	}, nil
}
