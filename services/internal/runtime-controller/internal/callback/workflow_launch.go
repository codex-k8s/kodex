package callback

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/types/known/structpb"
)

// workflowLaunchAvailable reuses the runtime contract's ordinary turn/project/
// capability predicate so tool discovery and local launch rejection share one rule.
// Current owner authorization is still checked by LaunchWorkflowExecution.
// workflowLaunchAvailable переиспользует runtime contract ordinary turn/project/
// capability predicate для общего правила discovery/local rejection. Текущую owner
// authorization всё равно проверяет LaunchWorkflowExecution.
func workflowLaunchAvailable(input runtimecontract.RunnerInput) bool {
	return runtimecontract.RuntimeWorkflowLaunchAvailable(input)
}

// workflowLaunchTool describes exact discovered workflow/publication/version pins
// and explicit keyed input, instructing the caller to end the turn after acceptance.
// The schema neither resolves required values nor launches or authorizes a workflow.
// workflowLaunchTool описывает exact discovered workflow/publication/version pins
// и explicit keyed input, требуя завершить turn после acceptance. Схема не разрешает
// required values,не запускает и не авторизует workflow.
func workflowLaunchTool() map[string]any {
	return map[string]any{"name": "launch_workflow", "description": "Discover one eligible published project Workflow with get_workflow_catalog, then launch it using exact workflow_ref, published_ref spec_digest and workflow_version pins. Pass workflow_version as expected_workflow_version and published_ref as expected_published_ref and spec_digest as expected_spec_digest. Before calling, obtain its published input schema and supply input as a map keyed by exact WorkflowInputField.Key with every required field. Do not guess keys or use labels as aliases. If the schema or required values are unavailable, obtain them before launching. The server creates a required child workflow and revalidates current authority. End this turn after acceptance; the final Workflow result arrives in a fresh callback turn. Do not repeat an accepted launch.", "inputSchema": objectSchema([]string{"workflow_ref", "expected_published_ref", "expected_spec_digest", "expected_workflow_version", "task"}, map[string]any{"workflow_ref": opaqueRefSchema(), "expected_published_ref": opaqueRefSchema(), "expected_spec_digest": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}, "expected_workflow_version": map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991}, "task": map[string]any{"type": "string", "minLength": 1, "maxLength": runtimecontract.MaximumAssistantTurnCodepoints}, "title": map[string]any{"type": "string", "maxLength": 240}, "input": map[string]any{"type": "object", "description": "Map each published WorkflowInputField.Key to its value. Include every required field using its declared type and options. Use exact keys, not labels or guessed aliases. If the schema or required values are unavailable, obtain them before launching."}})}
}

// launchWorkflow checks ordinary launch eligibility, closed keys, required pins,
// bounded task/title and protobuf-compatible input, then submits one owner command
// with lease fence/generation and a stable lease/call idempotency key. It returns
// only the accepted run/launch/callback locators; actual child creation, current authority,
// deduplication and terminal callback lifecycle remain owner responsibilities.
// launchWorkflow проверяет ordinary launch eligibility,closed keys,required pins,
// bounded task/title и protobuf-compatible input, передавая один owner command
// с lease fence/generation и stable lease/call idempotency key. Возвращаются лишь
// accepted run/launch/callback locators; child creation,current authority,deduplication
// и terminal callback lifecycle принадлежат owner.
func (server *Server) launchWorkflow(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	if !workflowLaunchAvailable(input) || !onlyKeys(arguments, "workflow_ref", "task", "title", "input", "expected_published_ref", "expected_spec_digest", "expected_workflow_version") {
		return nil, errors.New("workflow launch input is invalid")
	}
	workflow, ok := arguments["workflow_ref"].(string)
	if !ok || !runtimeFileRefPattern.MatchString(workflow) {
		return nil, errors.New("workflow launch input is invalid")
	}
	publishedRef, ok := arguments["expected_published_ref"].(string)
	digest, digestOK := arguments["expected_spec_digest"].(string)
	workflowVersion, versionOK := exactJSONInt64(arguments["expected_workflow_version"])
	if !ok || !runtimeFileRefPattern.MatchString(publishedRef) || !digestOK || !workflowCatalogDigestPattern.MatchString(digest) || !versionOK || workflowVersion < 1 {
		return nil, errors.New("workflow launch input is invalid")
	}
	task, ok := arguments["task"].(string)
	if !ok || !runtimecontract.ValidAssistantTurnContent(task) {
		return nil, errors.New("workflow launch input is invalid")
	}
	title := ""
	if value, exists := arguments["title"]; exists {
		var ok bool
		title, ok = value.(string)
		if !ok || !utf8.ValidString(title) || utf8.RuneCountInString(title) > 240 {
			return nil, errors.New("workflow launch input is invalid")
		}
	}
	values := map[string]any{}
	if value, exists := arguments["input"]; exists {
		var ok bool
		values, ok = value.(map[string]any)
		if !ok {
			return nil, errors.New("workflow launch input is invalid")
		}
	}
	structure, err := structpb.NewStruct(values)
	if err != nil {
		return nil, errors.New("workflow launch input is invalid")
	}
	requestCtx, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.LaunchWorkflowExecution(requestCtx, &controlplanev1.LaunchWorkflowExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, string(callID))}, LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, WorkflowRef: workflow, Task: task, Input: structure, Title: title, ExpectedPublishedRef: publishedRef, ExpectedSpecDigest: digest, ExpectedWorkflowVersion: workflowVersion})
	if err != nil {
		return nil, err
	}
	if response.GetRun().GetRef() == "" || response.GetLaunchRef() == "" || response.GetCallbackEdgeRef() == "" {
		return nil, errors.New("workflow launch response is invalid")
	}
	return map[string]any{"ok": true, "workflow_run_ref": response.GetRun().GetRef(), "launch_ref": response.GetLaunchRef(), "callback_edge_ref": response.GetCallbackEdgeRef()}, nil
}
