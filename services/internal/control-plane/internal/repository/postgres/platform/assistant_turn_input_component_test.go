package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAssistantTurnInputBoundsComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("e", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add"}, "control-api-gateway")
	service, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	warm := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report"}, "runtime-controller")
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, key string, payload any) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "turn-input-" + key}, Payload: payload})
	}
	project, err := execute(command.CreateProject, "project", command.ProjectInput{Name: "Проверка длинного задания", Language: "ru"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execute(command.CreateProjectAssistant, "profile", command.ProjectAssistantInput{ProjectRef: project.Project.Ref, Name: "Помощник", Purpose: "Проверка задания", Instructions: "Работай только в этом проекте."}); err != nil {
		t.Fatal(err)
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	ownerScope, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	effects := func() [5]int {
		var result [5]int
		if err := pool.QueryRow(ctx, queryAssistantRunScopeEffects, ownerScope.organizationID).Scan(&result[0], &result[1], &result[2], &result[3], &result[4]); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, scope := range []string{"SYSTEM", "PROJECT"} {
		t.Run(scope, func(t *testing.T) {
			input := command.AssistantConversationInput{AssistantScope: scope}
			if scope == "PROJECT" {
				input.ProjectRef = project.Project.Ref
			}
			created, err := execute(command.CreateAssistantConversation, scope+"-create", input)
			if err != nil {
				t.Fatal(err)
			}
			ref := created.Conversation.Ref
			for index, content := range []string{strings.Repeat("я", 24995), strings.Repeat("я", 32768), strings.Repeat("😀", 32768), "я" + strings.Repeat("\x01", 32767)} {
				key := fmt.Sprintf("%s-%d", scope, index)
				payload := command.AssistantTurnInput{ConversationRef: ref, Content: content, DeliveryMode: "QUEUE"}
				result, err := execute(command.AddAssistantTurn, key, payload)
				if err != nil || result.Conversation == nil || len(result.Conversation.Turns) != 1 {
					t.Fatalf("valid input case %d rejected: %v", index, err)
				}
				turn := result.Conversation.Turns[0]
				run, err := service.GetRun(ctx, owner, turn.RunRef)
				if err != nil || run.Task != content || turn.Content != content {
					t.Fatal("persisted task or turn did not roundtrip exactly")
				}
				events, _, _, err := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: turn.RunRef, Page: query.Page{Size: 20}})
				if err != nil {
					t.Fatal(err)
				}
				found := 0
				for _, event := range events {
					if event.Delta.Message != nil && event.Delta.Message.Phase == "USER" {
						if event.Delta.Message.Text != content || event.Delta.Message.Ref != turn.Ref || event.Delta.Execution == nil || event.Delta.Execution.RunRef != turn.RunRef {
							t.Fatal("authoritative event lost exact USER text or execution binding")
						}
						found++
					}
				}
				if found != 1 {
					t.Fatal("authoritative USER event missing or duplicated")
				}
				var outbox []byte
				if err := pool.QueryRow(ctx, `SELECT payload FROM control_plane.outbox_events WHERE ordering_key=$1 ORDER BY sequence LIMIT 1`, "run:"+turn.RunRef).Scan(&outbox); err != nil {
					t.Fatal(err)
				}
				var envelope struct {
					RootRunRef string
					Data       struct{ Message struct{ Text, Phase string } }
				}
				if json.Unmarshal(outbox, &envelope) != nil || envelope.RootRunRef != turn.RunRef || envelope.Data.Message.Phase != "USER" || envelope.Data.Message.Text != content || len(outbox) > runtimecontract.MaximumControlPlaneRunEventPayloadBytes {
					t.Fatal("outbox lost exact bounded USER envelope")
				}
				beforeReplay := effects()
				replay, err := execute(command.AddAssistantTurn, key, payload)
				if err != nil || replay.Conversation.Turns[0].RunRef != turn.RunRef || replay.Conversation.Version != result.Conversation.Version {
					t.Fatal("replay created another turn or run")
				}
				if effects() != beforeReplay {
					t.Fatal("replay changed durable state")
				}
			}
			before := effects()
			for index, content := range []string{strings.Repeat("я", 32769), "bad\xff", "bad\x00", " \n"} {
				if _, err := execute(command.AddAssistantTurn, fmt.Sprintf("%s-invalid-%d", scope, index), command.AssistantTurnInput{ConversationRef: ref, Content: content, DeliveryMode: "QUEUE"}); !errors.Is(err, errs.ErrInvalid) {
					t.Fatal("invalid input did not fail closed")
				}
				if effects() != before {
					t.Fatal("invalid input changed durable state")
				}
			}
		})
	}
}
