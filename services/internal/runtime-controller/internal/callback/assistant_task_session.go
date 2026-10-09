package callback

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

var errAssistantTaskSessionRead = errors.New("assistant task session read unavailable")

type taskSessionToolResult struct {
	controlplaneapi.TaskSessionProjection
}

func assistantTaskSessionTool() map[string]any {
	decimal := map[string]any{"type": "string", "pattern": "^[1-9][0-9]*$"}
	text := map[string]any{"type": "string"}
	message := objectSchema([]string{"event_ref", "message_ref", "phase", "text", "origin", "source_run_ref", "source_run_version", "session_ref", "node_ref", "turn_ref", "turn_number", "attempt", "event_sequence", "message_revision"}, map[string]any{
		"event_ref": opaqueRefSchema(), "message_ref": opaqueRefSchema(), "phase": enumSchema("USER", "COMMENTARY", "FINAL"), "text": text, "origin": enumSchema("ORDINARY", "CALLBACK_CONTINUATION"),
		"source_run_ref": opaqueRefSchema(), "source_run_version": decimal, "session_ref": opaqueRefSchema(), "node_ref": opaqueRefSchema(), "turn_ref": opaqueRefSchema(), "turn_number": decimal,
		"attempt": map[string]any{"type": "integer", "minimum": 1}, "event_sequence": decimal, "message_revision": enumSchema("1"),
	})
	return map[string]any{"name": "read_task_session", "description": "Read an exact run_ref discovered by find_platform_resources: authoritative public result and published messages of its server-resolved Session. Read is not resume or launch authority. At most 10 messages/512 KiB per page, newest first; follow next_cursor unchanged. SELECTED_SESSION_PUBLIC_MESSAGES excludes child Sessions, provider archives, reasoning and tool payloads. On source version conflict start again without cursor; never invent results.",
		"inputSchema": objectSchema([]string{"run_ref"}, map[string]any{"run_ref": opaqueRefSchema(), "cursor": stringSchema(1, 1024)}),
		"outputSchema": objectSchema([]string{"version", "coverage", "order", "run_ref", "project_ref", "session_ref", "title", "state", "run_version", "result_summary", "safe_error_code", "safe_error_message", "session_storage_state", "messages", "source_sha256", "next_cursor", "truncated", "projection_sha256"}, map[string]any{
			"version": map[string]any{"type": "integer", "const": 1}, "coverage": enumSchema("SELECTED_SESSION_PUBLIC_MESSAGES"), "order": enumSchema("NEWEST_FIRST"), "run_ref": opaqueRefSchema(), "project_ref": stringSchema(0, 96), "session_ref": opaqueRefSchema(), "title": stringSchema(1, 300),
			"state": enumSchema("QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING", "SUCCEEDED", "FAILED", "CANCELLED"), "run_version": decimal, "result_summary": text, "safe_error_code": text, "safe_error_message": text,
			"session_storage_state": enumSchema("UNTRACKED", "LIVE", "SNAPSHOT_READY", "SNAPSHOTTING", "DELETE_PVC_READY", "ARCHIVED", "RESTORE_READY", "RESTORING", "ERROR", "PURGED"),
			"messages":              map[string]any{"type": "array", "maxItems": 10, "items": message}, "source_sha256": stringSchema(64, 64), "next_cursor": stringSchema(0, 1024), "truncated": map[string]any{"type": "boolean"}, "projection_sha256": stringSchema(64, 64),
		})}
}

func (server *Server) readTaskSession(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	if !input.IsAssistant() || input.LeaseRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 || !onlyKeys(arguments, "run_ref", "cursor") {
		return nil, errAssistantTaskSessionRead
	}
	runRef, ok := arguments["run_ref"].(string)
	if !ok || !validAssistantResourceRef(runRef) {
		return nil, errAssistantTaskSessionRead
	}
	cursor := ""
	if value, present := arguments["cursor"]; present {
		var valid bool
		cursor, valid = value.(string)
		if !valid || cursor == "" || len(cursor) > 1024 {
			return nil, errAssistantTaskSessionRead
		}
	}
	bounded, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.SearchAssistantResources(bounded, &controlplanev1.SearchAssistantResourcesRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, AssistantTaskSessionRead: &controlplanev1.AssistantTaskSessionReadRequest{RunRef: runRef, Cursor: cursor}})
	if err != nil {
		return nil, errAssistantTaskSessionRead
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || len(response.Results) != 0 || response.Truncated || len(response.Definitions) != 0 || response.NextDefinitionOffset != 0 || response.AssistantConfigurationCatalog != nil {
		return nil, errAssistantTaskSessionRead
	}
	projection, err := controlplaneapi.ProjectTaskSessionPage(response.AssistantTaskSession)
	if err != nil || projection.RunRef != runRef || (input.AssistantScope == runtimecontract.AssistantScopeProject && projection.ProjectRef != input.ProjectRef) {
		return nil, errAssistantTaskSessionRead
	}
	previous, cursorErr := controlplaneapi.ReadTaskSessionCursor(cursor)
	if cursorErr != nil || (cursor != "" && previous.Source != projection.SourceSHA256) {
		return nil, errAssistantTaskSessionRead
	}
	if projection.Truncated {
		next, nextErr := controlplaneapi.ReadTaskSessionCursor(projection.NextCursor)
		if nextErr != nil || next.Offset != previous.Offset+len(projection.Messages) || (cursor != "" && next.Binding != previous.Binding) {
			return nil, errAssistantTaskSessionRead
		}
	}
	return taskSessionToolResult{projection}, nil
}
