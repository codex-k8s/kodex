package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// personalEmailDelegatedReadFixture creates a child only through the exact parent runtime lease
// and server-owned edge, rejects a foreign-project target and wrong fence, and follows the child's
// READ through actual claim/Proto/gateway/authorization/completion. The credential actor must
// remain the original root USER rather than the worker, child, or approver. The caller retains
// the child runtime lease until its later authoritative parent cancellation; no mailbox is read.
// personalEmailDelegatedReadFixture создаёт child только по точной parent runtime lease и
// server-owned edge, отклоняет foreign-project target и неверный fence и проводит дочерний READ
// через настоящие claim/Proto/gateway/authorization/completion. Credential actor остаётся исходным
// root USER, а не worker, child либо approver. Caller сохраняет runtime lease child до последующей
// авторитетной parent cancellation; remote mailbox не читается.
func personalEmailDelegatedReadFixture(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, runtime, gateway value.Principal, parent map[string]any, rootRunRef, connectionRef, childAgentRef, foreignAgentRef, rootActor string) map[string]any {
	t.Helper()
	input := command.DelegateInput{LeaseRef: stringMap(parent, "leaseRef"), Fence: stringMap(parent, "fence"), Generation: runtimeRevisionMapInt64(parent, "generation"), TargetAgentRef: foreignAgentRef, Task: "Read only the original initiator's selected synthetic mailbox"}
	if _, err := service.Execute(ctx, command.Command{Kind: command.DelegateExecution, Principal: runtime, Mutation: value.Mutation{IdempotencyKey: "email-delegate-foreign-project"}, Payload: input}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("mailbox delegation crossed its original project boundary", err)
	}
	input.TargetAgentRef = childAgentRef
	wrong := input
	wrong.Fence += "-foreign"
	if _, err := service.Execute(ctx, command.Command{Kind: command.DelegateExecution, Principal: runtime, Mutation: value.Mutation{IdempotencyKey: "email-delegate-wrong-parent-fence"}, Payload: wrong}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("caller-supplied parent coordinates established mailbox lineage", err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.DelegateExecution, Principal: runtime, Mutation: value.Mutation{IdempotencyKey: "email-delegate-own-root"}, Payload: input})
	if err != nil || created.Run == nil || created.Run.Ref == rootRunRef || created.Run.RootRunRef != rootRunRef || created.Run.ParentRunRef != rootRunRef {
		t.Fatal("create exact server-owned mailbox child lineage", err)
	}
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: runtime,
		Mutation: value.Mutation{IdempotencyKey: "email-delegated-runtime-claim"}, Payload: command.LeaseInput{WorkloadInstance: "personal-email-child-runtime", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 || stringMap(claimed.RuntimeItems[0], "runRef") != created.Run.Ref {
		t.Fatal("claim exact delegated mailbox child", err)
	}
	lease := claimed.RuntimeItems[0]
	invocation, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{"run_ref": created.Run.Ref, "node_ref": stringMap(lease, "nodeRef"), "connection_ref": connectionRef, "capability_key": "email.message.list", "idempotency_key": "email-delegated-read"}, map[string]any{})
	if err != nil || stringMap(invocation, "state") != "READY" || stringMap(invocation, "gateRef") != "" {
		t.Fatal("delegated root USER lost its own mailbox NONE authority", err)
	}
	items, err := service.ClaimIntegrationInvocations(ctx, gateway, "email-delegated-gateway-claim", 1)
	if err != nil || len(items) != 1 || stringMap(items[0], "invocationRef") != stringMap(invocation, "invocationRef") || items[0]["credential"] != nil {
		t.Fatal("delegated mailbox claim used another credential lane", err)
	}
	work := items[0]
	testEmailGatewayClaim(t, work, "invocation")
	configuration, err := r.EmailConfiguration(ctx)
	if err != nil || len(configuration.Mailboxes) != 1 {
		t.Fatal("read accepted delegated mailbox configuration", err)
	}
	mailbox := configuration.Mailboxes[0]
	semantic, err := api.CommandForIntegration("email.message.list", mailbox.Id, mailbox.Sender, stringMap(work, "effectKey"), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	bridge := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "email-bridge", Operation: "platform.email.authorization.resolve"}, "email-bridge")
	request := query.EmailAuthorization{Binding: entity.EmailExecutionBinding{InvocationRef: stringMap(work, "invocationRef"), LeaseRef: stringMap(work, "leaseRef"), Fence: stringMap(work, "fence"), Generation: work["generation"].(int64), ExpiresAt: work["expiresAt"].(time.Time)},
		MailboxRef: mailbox.Id, ConfigurationRevision: mailbox.Revision, Operation: "list", Sender: mailbox.Sender, Folder: mailbox.Folder, SemanticInputDigest: api.Digest(semantic), EffectKey: semantic.EffectKey}
	decision, err := service.ResolveEmailAuthorization(ctx, bridge, request)
	if err != nil || !decision.Allowed || decision.GateApproved || decision.ActorRef != rootActor || decision.AgentRef != childAgentRef {
		t.Fatal("delegated mailbox operation replaced its persisted root credential actor", err)
	}
	if _, err := personalEmailCompleteReadFixture(ctx, service, gateway, "email-delegated-read-complete", work, "successful delegated synthetic read"); err != nil {
		t.Fatal("complete current delegated mailbox READ", err)
	}
	return lease
}
