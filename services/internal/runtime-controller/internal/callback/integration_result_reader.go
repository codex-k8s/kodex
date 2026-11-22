package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"unicode/utf8"
)

// readInvocationResult selects a private purpose reader from the pinned grant, never from model arguments.
// Missing reader or invalid owner data fails closed without falling back to generic organization-wide reads.
// readInvocationResult выбирает приватный purpose reader по закреплённому grant, а не аргументам модели.
// Отсутствующий reader или повреждённый ответ закрыто отклоняется без общего чтения по организации.
func (s *Server) readInvocationResult(ctx context.Context, input runtimecontract.RunnerInput, grant runtimecontract.RunnerIntegrationGrant, inv string) (*cp.GetIntegrationInvocationResponse, error) {
	if grant.DefinitionKey == integrationpackage.SSHManagedDefinitionKey || grant.DefinitionKey == integrationpackage.SSHExecuteDefinitionKey {
		return s.readSSHInvocationResult(ctx, input, grant, inv)
	}
	if grant.DefinitionKey != integrationpackage.KubernetesDefinitionKey {
		return s.control.Runtime.GetIntegrationInvocation(ctx, &cp.GetIntegrationInvocationRequest{InvocationRef: inv})
	}
	if _, ok := integrationpackage.KubernetesOperationByKey(grant.Operation); !ok || s.control.KubernetesOperations == nil {
		return nil, status.Error(codes.Unavailable, "kubernetes result reader unavailable")
	}
	result, err := s.control.KubernetesOperations.ReadManagedKubernetesInvocation(ctx, &cp.ReadManagedKubernetesInvocationRequest{InvocationRef: inv, LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, RuntimeInputDigest: input.InputDigest})
	if err != nil {
		return nil, err
	}
	return kubernetesInvocationResult(inv, result)
}

// kubernetesInvocationResult verifies exact origin and closed state before translating to the existing tool lifecycle.
// An open child Gate yields pending to the agent without changing the authoritative RUNNING invocation state.
// kubernetesInvocationResult проверяет исходный вызов и закрытый state перед прежним жизненным циклом инструмента.
// Открытый дочерний Gate возвращает pending сотруднику без изменения авторитетного RUNNING состояния вызова.
func kubernetesInvocationResult(inv string, v *cp.ReadManagedKubernetesInvocationResponse) (*cp.GetIntegrationInvocationResponse, error) {
	if v == nil || len(v.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.Unavailable, runtimeResultProjectionInvalid)
	}
	return runtimeInvocationResult(inv, v.InvocationRef, strings.TrimPrefix(v.State.String(), "MANAGED_KUBERNETES_TOOL_STATE_"), v.ResultSummary, v.SafeErrorCode, v.EffectReceiptRef, v.ResultDigest, v.WaitingForGate)
}

// runtimeResultProjectionInvalid conceals private output and malformed owner response details from callback diagnostics.
// runtimeResultProjectionInvalid скрывает private output и malformed owner response details в callback diagnostics.
const runtimeResultProjectionInvalid = "runtime result projection invalid"

// runtimeInvocationResult shares only closed result-envelope validation across purpose readers; it grants no lookup, actor authority or execution credit.
// runtimeInvocationResult разделяет лишь closed result-envelope validation между purpose readers; lookup, actor authority и execution credit не выдаются.
func runtimeInvocationResult(inv, ref, state, summary, safeError, receipt, digest string, waiting bool) (*cp.GetIntegrationInvocationResponse, error) {
	invalid := func() (*cp.GetIntegrationInvocationResponse, error) {
		return nil, status.Error(codes.Unavailable, runtimeResultProjectionInvalid)
	}
	if ref != inv || len(summary) > 64<<10 || !utf8.ValidString(summary) || len(safeError) > 96 {
		return invalid()
	}
	for _, c := range safeError {
		if c != '_' && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return invalid()
		}
	}
	terminal := false
	switch state {
	case "READY", "RUNNING", "WAITING_APPROVAL":
	case "SUCCEEDED", "FAILED", "REJECTED", "CANCELLED", "UNKNOWN_OUTCOME":
		terminal = true
	default:
		return invalid()
	}
	if terminal && waiting {
		return invalid()
	}
	if state == "SUCCEEDED" {
		sum := sha256.Sum256([]byte(summary))
		if safeError != "" || !safeEffectReceiptRef(receipt) || digest != hex.EncodeToString(sum[:]) {
			return invalid()
		}
	} else if summary != "" || receipt != "" || digest != "" {
		return invalid()
	}
	if waiting {
		state = "WAITING_APPROVAL"
	}
	return &cp.GetIntegrationInvocationResponse{State: state, ResultSummary: summary, SafeErrorCode: safeError, EffectReceiptRef: receipt}, nil
}
