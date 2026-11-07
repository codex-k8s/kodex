package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertPersonalServerReadRecovery qualifies native owner UNKNOWN/claim/Begin/complete for an
// already admitted Server write. Provider observations are synthetic; no HTTP/TLS or live effect
// is claimed. Exactly one Execute and two read-only admissions must survive immutable receipts.
// assertPersonalServerReadRecovery проверяет native owner UNKNOWN/claim/Begin/complete для уже
// допущенной Server записи. Provider observations синтетические; HTTP/TLS либо live effect не
// заявляются. Immutable receipts сохраняют ровно один Execute и два read-only admission.
func assertPersonalServerReadRecovery(t *testing.T, ctx context.Context, repository *Repository, service, workers *platformservice.Service, gateway value.Principal, original map[string]any, admission *cp.BeginPersonalIntegrationInvocationRequest, credentialRef, providerRef string, output map[string]any) {
	t.Helper()
	_, err := service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "personal-server-unknown-" + admission.InvocationRef}, Payload: command.IntegrationInvocationInput{InvocationRef: admission.InvocationRef, LeaseRef: admission.LeaseRef, Fence: admission.Fence, Generation: admission.Generation, UnknownOutcome: true, SafeErrorCode: "INTEGRATION_OUTCOME_UNKNOWN"}})
	if err != nil {
		t.Fatal("record synthetic unknown Server effect", err)
	}
	rpc := personalRuntimeRPCClient(t, workers)
	for step := 0; step < 2; step++ {
		claims, err := service.ClaimIntegrationInvocations(ctx, gateway, admission.WorkloadInstance, 1)
		if err != nil || len(claims) != 1 || stringMap(claims[0], "invocationRef") != admission.InvocationRef || stringMap(claims[0], "workMode") != "RECOVER_READ_ONLY" || stringMap(claims[0], "effectKey") != stringMap(original, "effectKey") || stringMap(claims[0], "inputDigest") != stringMap(original, "inputDigest") {
			t.Fatal("Server recovery lost immutable effect or became Execute", err)
		}
		work := claims[0]
		next := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: admission.InvocationRef, LeaseRef: stringMap(work, "leaseRef"), Fence: stringMap(work, "fence"), Generation: runtimeRevisionMapInt64(work, "generation"), WorkloadInstance: admission.WorkloadInstance}
		response, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), next)
		if err != nil || !response.GetAdmitted() || response.GetWorkMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY || response.GetCredentialRevision().GetRef() != credentialRef || response.GetResourcePermissionProfile().String() != "INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_"+stringMap(original, "resourcePermissionProfile") {
			t.Fatal("Server recovery Begin lost exact current root key/mode", err)
		}
		payload := command.IntegrationInvocationInput{InvocationRef: next.InvocationRef, LeaseRef: next.LeaseRef, Fence: next.Fence, Generation: next.Generation, SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"}
		expected, key := "UNKNOWN_OUTCOME", "personal-server-read-error-"+admission.InvocationRef
		if step == 1 {
			raw, e := json.Marshal(output)
			if e != nil {
				t.Fatal(e)
			}
			digest := sha256.Sum256(raw)
			payload.Success, payload.SafeErrorCode, payload.ResultSummary = true, "", string(raw)
			payload.EffectKey, payload.InputDigest, payload.ProviderEffectRef, payload.ResponseDigest = stringMap(work, "effectKey"), stringMap(work, "inputDigest"), providerRef, hex.EncodeToString(digest[:])
			expected, key = "SUCCEEDED", "personal-server-read-confirmed-"+admission.InvocationRef
		}
		_, err = service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: key}, Payload: payload})
		if err != nil {
			t.Fatal("complete Server read-only observation", err)
		}
		stored, err := service.GetIntegrationInvocation(ctx, gateway, next.InvocationRef)
		if err != nil || stringMap(stored, "state") != expected || (stringMap(stored, "effectReceiptRef") != "") != (step == 1) {
			t.Fatal("Server read-only result fabricated failure/receipt or lost success", err)
		}
	}
	var executions, recoveries int
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE NOT admission.recovery_read_only),count(*) FILTER (WHERE admission.recovery_read_only)
FROM control_plane.integration_personal_invocation_admissions admission JOIN control_plane.integration_invocations invocation ON invocation.id=admission.invocation_id WHERE invocation.ref=$1`, admission.InvocationRef).Scan(&executions, &recoveries); err != nil || executions != 1 || recoveries != 2 {
		t.Fatal("Server recovery repeated Execute or lost admission history", err)
	}
}
