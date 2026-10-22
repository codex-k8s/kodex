package callback

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// workflowCatalogDigestPattern accepts exactly sixty-four lowercase hexadecimal
// characters for catalog commitments; matching text alone supplies no owner authority.
// workflowCatalogDigestPattern допускает ровно шестьдесят четыре lowercase hex characters
// catalog commitments; совпадение text само не даёт owner authority.
var workflowCatalogDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// workflowCatalogTool exposes the single discovery/publication/active-run tool schema
// rather than maintaining a second independent workflow discovery contract.
// workflowCatalogTool выдаёт единую discovery/publication/active-run tool schema
// вместо второго независимого workflow discovery contract.
func workflowCatalogTool() map[string]any {
	return workflowCatalogReadTool()
}

// workflowCatalog routes read selectors to the specialized handler; otherwise it
// checks ordinary launch eligibility and bounded discovery query/token before a
// lease-fenced owner RPC. It validates distinct result refs, input types and readiness
// then seals a bounded public projection. Readiness remains owner-observed/advisory;
// the catalog neither reserves a launch nor rechecks every domain permission locally.
// workflowCatalog направляет read selectors специализированному handler; иначе проверяет
// ordinary launch eligibility и bounded discovery query/token до lease-fenced owner RPC.
// После проверки refs,input types/readiness формирует bounded public projection
// с локальным evidence. Readiness — owner observation/advisory, не резервирование
// запуска и не повтор всех domain permissions локально.
func (server *Server) workflowCatalog(ctx context.Context, input runtimecontract.RunnerInput, args map[string]any) (any, error) {
	if _, ok := args["publication_read"]; ok {
		return server.workflowCatalogRead(ctx, input, args)
	}
	if _, ok := args["active_runs_read"]; ok {
		return server.workflowCatalogRead(ctx, input, args)
	}
	if !workflowLaunchAvailable(input) || !onlyKeys(args, "query", "page_token") {
		return nil, errors.New("workflow catalog input is invalid")
	}
	search, token := "", ""
	for key, dest := range map[string]*string{"query": &search, "page_token": &token} {
		if raw, exists := args[key]; exists {
			value, ok := raw.(string)
			if !ok || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
				return nil, errors.New("workflow catalog input is invalid")
			}
			*dest = value
		}
	}
	if utf8.RuneCountInString(search) > 200 || len(token) > 512 {
		return nil, errors.New("workflow catalog input is invalid")
	}
	requestCtx, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	result, err := server.control.Runtime.GetExecutionWorkflowCatalog(requestCtx, &cp.GetExecutionWorkflowCatalogRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Query: search, PageToken: token})
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.ProtoReflect().GetUnknown()) != 0 || result.Read != nil || len(result.Items) > 10 || len(result.NextPageToken) > 512 {
		return nil, errors.New("workflow catalog response is invalid")
	}
	items := make([]map[string]any, 0, len(result.Items))
	seen := map[string]bool{}
	for _, item := range result.Items {
		if item == nil || !runtimeFileRefPattern.MatchString(item.WorkflowRef) || !runtimeFileRefPattern.MatchString(item.PublishedRef) || !workflowCatalogDigestPattern.MatchString(item.SpecDigest) || item.WorkflowVersion < 1 || item.Readiness == nil || seen[item.WorkflowRef] || item.Readiness.RevisionRef != item.PublishedRef || item.Readiness.WorkflowVersion != item.WorkflowVersion {
			return nil, errors.New("workflow catalog response is invalid")
		}
		seen[item.WorkflowRef] = true
		readiness := item.Readiness
		if !onlyWorkflowReadiness(readiness) {
			return nil, errors.New("workflow catalog response is invalid")
		}
		fields := make([]map[string]any, 0, len(item.InputFields))
		keys := map[string]bool{}
		for _, field := range item.InputFields {
			if field == nil || field.Key == "" || keys[field.Key] || !workflowCatalogInputType(field.ValueType) {
				return nil, errors.New("workflow catalog response is invalid")
			}
			keys[field.Key] = true
			fields = append(fields, map[string]any{"key": field.Key, "label": field.Label, "description": field.Description, "value_type": field.ValueType, "required": field.Required, "options": field.Options})
		}
		items = append(items, map[string]any{"workflow_ref": item.WorkflowRef, "name": item.Name, "purpose": item.Purpose, "workflow_version": item.WorkflowVersion, "published_ref": item.PublishedRef, "spec_digest": item.SpecDigest, "input_fields": fields, "readiness": map[string]any{"allowed_to_submit": readiness.AllowedToSubmit, "reason": readiness.Reason, "operational_state": readiness.OperationalState, "context_digest": readiness.ContextDigest}})
	}
	projection := map[string]any{"items": items, "next_page_token": result.NextPageToken}
	raw, err := json.Marshal(projection)
	if err != nil || len(raw) > 32768 {
		return nil, errors.New("workflow catalog response exceeds boundary")
	}
	return newWorkflowCatalogResult(ctx, input, args, projection, workflowCatalogReceipt{Version: 1, Kind: workflowReceiptKind, Mode: workflowModeDiscovery, List: &workflowListReceipt{ItemsCount: len(items), HasNext: result.NextPageToken != ""}})
}

// workflowCatalogInputType accepts the six published workflow input value types;
// it validates the type label only, not a supplied value or field options.
// workflowCatalogInputType допускает шесть published workflow input value types,
// проверяя лишь type label, не конкретное value и field options.
func workflowCatalogInputType(value string) bool {
	switch value {
	case "TEXT", "LONG_TEXT", "NUMBER", "BOOLEAN", "DATE", "SELECT":
		return true
	}
	return false
}

// onlyWorkflowReadiness checks closed reason/operational-state labels and context
// digest syntax, requiring READY reason for allowed submission. UNKNOWN operational
// state is preserved; this local shape check is not a live readiness probe.
// onlyWorkflowReadiness проверяет closed reason/operational-state labels и context digest,
// требуя READY reason при allowed submission. UNKNOWN operational state сохраняется;
// local shape check не является live readiness probe.
func onlyWorkflowReadiness(value *cp.WorkflowLaunchReadiness) bool {
	switch value.Reason {
	case "READY", "PERMISSION_REQUIRED", "UNPUBLISHED", "DEPENDENCY_UNAVAILABLE":
	default:
		return false
	}
	switch value.OperationalState {
	case "READY", "BLOCKED", "UNKNOWN":
	default:
		return false
	}
	return workflowCatalogDigestPattern.MatchString(value.ContextDigest) && (!value.AllowedToSubmit || value.Reason == "READY")
}
