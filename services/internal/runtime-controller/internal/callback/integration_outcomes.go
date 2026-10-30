package callback

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// errIntegrationOutcomes is fixed diagnostic text, excluding source and request data.
// errIntegrationOutcomes — фиксированная диагностика без source и request data.
var errIntegrationOutcomes = errors.New("integration outcome page invalid")

// integrationOutcomesAvailable enables an additional closed read selector on an
// existing tool for ordinary project turns. The owner still checks exact lineage.
// integrationOutcomesAvailable включает дополнительный closed read selector прежнего
// инструмента обычным project turns. Owner отдельно проверяет точную lineage.
func integrationOutcomesAvailable(input runtimecontract.RunnerInput) bool {
	return input.Mode == runtimecontract.RunnerModeTurn && input.AssistantScope == runtimecontract.AssistantScopeNone && input.ProjectRef != "" && len(input.IntegrationGrants) > 0
}

// extendIntegrationOutcomesSchema adds one mutually exclusive selector without
// changing the runner's closed tool-name set or existing catalog request modes.
// extendIntegrationOutcomesSchema добавляет взаимоисключающий selector без изменения
// закрытого набора имён runner либо прежних catalog request modes.
func extendIntegrationOutcomesSchema(tool map[string]any) {
	schema := tool["inputSchema"].(map[string]any)
	schema["properties"].(map[string]any)["outcomes_read"] = objectSchema([]string{"source_invocation_ref", "source_sha256", "offset_bytes", "maximum_bytes"}, map[string]any{
		"source_invocation_ref": opaqueRefSchema(), "source_sha256": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"},
		"offset_bytes": map[string]any{"type": "integer", "minimum": 0, "maximum": 64 << 20}, "maximum_bytes": map[string]any{"type": "integer", "minimum": 4, "maximum": 4096},
	})
	branches := schema["oneOf"].([]map[string]any)
	for _, branch := range branches {
		branch["allOf"] = []map[string]any{{"not": map[string]any{"required": []string{"outcomes_read"}}}}
	}
	schema["oneOf"] = append(branches, map[string]any{"required": []string{"outcomes_read"}, "not": map[string]any{"anyOf": []map[string]any{{"required": []string{"query"}}, {"required": []string{"offset"}}, {"required": []string{"grant_ref"}}, {"required": []string{"connection_ref"}}, {"required": []string{"capability_key"}}}}})
	tool["outputSchema"] = map[string]any{"type": "object", "oneOf": []any{tool["outputSchema"], objectSchema([]string{"outcomes_page"}, map[string]any{"outcomes_page": objectSchema([]string{"source_invocation_ref", "source_sha256", "size_bytes", "outcomes_count", "offset_bytes", "next_offset_bytes", "eof", "page_sha256", "text"}, map[string]any{
		"source_invocation_ref": opaqueRefSchema(), "source_sha256": stringSchema(64, 64), "page_sha256": stringSchema(64, 64), "size_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": 64 << 20}, "outcomes_count": map[string]any{"type": "integer", "minimum": 1, "maximum": runtimecontract.MaximumNativeToolCalls}, "offset_bytes": map[string]any{"type": "integer", "minimum": 0}, "next_offset_bytes": map[string]any{"type": "integer", "minimum": 0}, "eof": map[string]any{"type": "boolean"}, "text": stringSchema(0, 4096),
	})})}}
	tool["description"] = tool["description"].(string) + " When the server task supplies a workflow evidence manifest, use only outcomes_read to read every byte page of its exact predecessor snapshot through eof. Never repeat completed effects to reconstruct history."
}

// integrationOutcomesRequest validates exclusive selector and bounded page pins;
// the request itself cannot choose another actor, project or producing node.
// integrationOutcomesRequest проверяет exclusive selector и bounded page pins;
// запрос не назначает другой actor, project либо producing node.
func integrationOutcomesRequest(input runtimecontract.RunnerInput, args map[string]any) (*cp.GetExecutionIntegrationOutcomesRequest, bool) {
	if !integrationOutcomesAvailable(input) || !onlyKeys(args, "outcomes_read") {
		return nil, false
	}
	selected, ok := args["outcomes_read"].(map[string]any)
	if !ok || len(selected) != 4 || !onlyKeys(selected, "source_invocation_ref", "source_sha256", "offset_bytes", "maximum_bytes") {
		return nil, false
	}
	ref, ok := selected["source_invocation_ref"].(string)
	if !ok || !runtimeFileRefPattern.MatchString(ref) {
		return nil, false
	}
	digest, ok := selected["source_sha256"].(string)
	if !ok || !workflowCatalogDigestPattern.MatchString(digest) {
		return nil, false
	}
	offset, ok := assistantConfigurationPageInteger(selected, "offset_bytes", 0, 64<<20)
	if !ok {
		return nil, false
	}
	maximum, ok := assistantConfigurationPageInteger(selected, "maximum_bytes", 4096, 4096)
	if !ok || maximum < 4 {
		return nil, false
	}
	return &cp.GetExecutionIntegrationOutcomesRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, SourceInvocationRef: ref, SourceSha256: digest, OffsetBytes: offset, MaximumBytes: int32(maximum)}, true
}

// integrationOutcomesResult seals public wire to the current input/request and
// safe page metadata. JSON serialization cannot reconstruct its private evidence.
// integrationOutcomesResult связывает public wire с current input/request и safe
// page metadata. JSON serialization не восстанавливает private evidence.
type integrationOutcomesResult struct {
	wire                                         map[string]any
	inputDigest, requestDigest, wireDigest, safe string
}

// MarshalJSON exports only validated public MCP data, never private evidence.
// MarshalJSON выдаёт лишь проверенные public MCP данные без private evidence.
func (result integrationOutcomesResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(result.wire)
}

// integrationOutcomes calls only the specialized owner RPC, validates all page
// coordinates and commitments and seals metadata without persisting the source text.
// integrationOutcomes вызывает лишь специализированный owner RPC, проверяет все page
// coordinates и commitments, связывает metadata без сохранения source text в activity.
func (server *Server) integrationOutcomes(ctx context.Context, input runtimecontract.RunnerInput, args map[string]any) (any, error) {
	request, ok := integrationOutcomesRequest(input, args)
	if !ok {
		return nil, errIntegrationOutcomes
	}
	bounded, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.GetExecutionIntegrationOutcomes(bounded, request)
	if err != nil {
		return nil, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.SourceInvocationRef != request.SourceInvocationRef || response.SourceSha256 != request.SourceSha256 || response.SizeBytes < 1 || response.SizeBytes > 64<<20 || response.OutcomesCount < 1 || response.OutcomesCount > runtimecontract.MaximumNativeToolCalls || response.OffsetBytes != request.OffsetBytes || response.OffsetBytes > response.SizeBytes || !utf8.ValidString(response.Text) || len(response.Text) > int(request.MaximumBytes) || response.NextOffsetBytes != response.OffsetBytes+int64(len(response.Text)) || response.NextOffsetBytes > response.SizeBytes || response.Eof != (response.NextOffsetBytes == response.SizeBytes) || !response.Eof && len(response.Text) == 0 || response.PageSha256 != workflowReceiptDigest([]byte(response.Text)) {
		return nil, errIntegrationOutcomes
	}
	page := map[string]any{"source_invocation_ref": response.SourceInvocationRef, "source_sha256": response.SourceSha256, "size_bytes": response.SizeBytes, "outcomes_count": response.OutcomesCount, "offset_bytes": response.OffsetBytes, "next_offset_bytes": response.NextOffsetBytes, "eof": response.Eof, "page_sha256": response.PageSha256, "text": response.Text}
	wire := map[string]any{"outcomes_page": page}
	raw, err := json.Marshal(wire)
	if err != nil || len(raw) > 32768 || ctx.Err() != nil {
		return nil, errIntegrationOutcomes
	}
	safe := map[string]any{"kind": "integration_outcomes_page", "version": 1, "source_sha256": response.SourceSha256, "page_sha256": response.PageSha256, "size_bytes": response.SizeBytes, "outcomes_count": response.OutcomesCount, "offset_bytes": response.OffsetBytes, "next_offset_bytes": response.NextOffsetBytes, "eof": response.Eof}
	safeRaw, _ := json.Marshal(safe)
	requestRaw, _ := json.Marshal(args)
	return integrationOutcomesResult{wire: wire, inputDigest: workflowReceiptInputDigest(input), requestDigest: workflowReceiptDigest(requestRaw), wireDigest: workflowReceiptDigest(raw), safe: string(safeRaw)}, nil
}

// safeIntegrationOutcomesReceipt rechecks process-local commitments; callers cannot
// turn a reconstructed map or mutated wire into a successful audit receipt.
// safeIntegrationOutcomesReceipt перепроверяет process-local commitments; caller не
// превращает восстановленную map либо изменённый wire в успешный audit receipt.
func safeIntegrationOutcomesReceipt(input runtimecontract.RunnerInput, args map[string]any, result any) (string, error) {
	value, ok := result.(integrationOutcomesResult)
	if !ok {
		return "", errIntegrationOutcomes
	}
	if _, valid := integrationOutcomesRequest(input, args); !valid {
		return "", errIntegrationOutcomes
	}
	request, err := json.Marshal(args)
	wire, wireErr := json.Marshal(value.wire)
	if err != nil || wireErr != nil || len(wire) > 32768 || value.inputDigest != workflowReceiptInputDigest(input) || value.requestDigest != workflowReceiptDigest(request) || value.wireDigest != workflowReceiptDigest(wire) || len(value.safe) == 0 || len(value.safe) > 2000 {
		return "", errIntegrationOutcomes
	}
	return value.safe, nil
}
