package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func TestAssistantAgentConfigurationJSONFullPreservation(t *testing.T) {
	content := strings.Repeat("Полный шаблон {{ .organization.name }} {{ .agent.name }}\n", 1800)
	digest := sha256.Sum256([]byte(content))
	native := entity.InstructionVersion{Ref: "ins_native123", VersionNumber: 1, State: "PUBLISHED", Content: "Preserve native published instructions.", Digest: ""}
	nativeDigest := sha256.Sum256([]byte(native.Content))
	native.Digest = hex.EncodeToString(nativeDigest[:])
	effective := entity.InstructionVersion{Ref: "mcr_template123", VersionNumber: 9, State: "PUBLISHED", Content: content, Digest: hex.EncodeToString(digest[:])}
	agent := entity.Agent{Ref: "agt_recipient123", ProjectRef: "prj_fixture123", Name: "Manager", PublishedInstructions: &native, InstructionBinding: &entity.AgentInstructionsBinding{Ref: "ibd_binding123", Version: 2, RevisionRef: native.Ref, Effective: false}}
	raw, err := assistantAgentConfigurationJSON(agent, effective)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	_ = json.Unmarshal(raw, &snapshot)
	if len(snapshot) != 14 || snapshot["publishedInstructions"].(map[string]any)["content"] != native.Content || snapshot["effectiveInstructions"].(map[string]any)["content"] != content || snapshot["effectiveInstructions"].(map[string]any)["ref"] != effective.Ref {
		t.Fatal("full publication selection or template lost")
	}
	for _, mutate := range []func(*entity.Agent, *entity.InstructionVersion){
		func(a *entity.Agent, _ *entity.InstructionVersion) { a.InstructionBinding = nil },
		func(_ *entity.Agent, e *entity.InstructionVersion) { e.Digest = strings.Repeat("0", 64) },
		func(_ *entity.Agent, e *entity.InstructionVersion) { e.State = "DRAFT" },
		func(_ *entity.Agent, e *entity.InstructionVersion) { e.Content = strings.Repeat("x", (256<<10)+1) },
		func(a *entity.Agent, _ *entity.InstructionVersion) {
			binding := *a.InstructionBinding
			binding.Effective = true
			a.InstructionBinding = &binding
		},
	} {
		a, e := agent, effective
		mutate(&a, &e)
		if _, err := assistantAgentConfigurationJSON(a, e); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("corrupt publication accepted")
		}
	}
}

// Вызывается публичной TestProjectAssistantIntegrationGrantsComponent в SYSTEM
// и PROJECT fixture. Все эффекты принадлежат только disposable PostgreSQL.
func testAssistantAgentConfigurationUnderLease(t *testing.T, ctx context.Context, repository *Repository, service *serviceplatform.Service, owner, reader value.Principal, lease map[string]any, agentRef, otherRef, foreignHelperRef string) {
	t.Helper()
	input := entity.AssistantConfigurationCatalogRequest{Kind: "AGENT_CONFIGURATION", AssistantRef: stringMap(lease, "agentRef"), EntityKind: "AGENT", EntityRef: agentRef}
	read := func(request entity.AssistantConfigurationCatalogRequest, fence string, generation int64) (entity.AssistantConfigurationCatalogResponse, error) {
		return service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), fence, generation, request)
	}
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	var beforeEffects, afterEffects string
	if err := repository.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, actor.organizationID).Scan(&beforeEffects); err != nil {
		t.Fatal(err)
	}
	result, err := read(input, stringMap(lease, "fence"), lease["generation"].(int64))
	if err != nil || result.AgentConfiguration == nil {
		t.Fatal("exact agent configuration owner read failed", err)
	}
	if err := repository.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, actor.organizationID).Scan(&afterEffects); err != nil || beforeEffects != afterEffects {
		t.Fatal("agent configuration read mutated receipt/audit/outbox state")
	}
	current, err := service.GetAgent(ctx, owner, agentRef)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := repository.GetEffectivePromptTemplate(ctx, resolved, agentRef)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if json.Unmarshal(result.AgentConfiguration.ConfigurationJSON, &snapshot) != nil || result.AgentConfiguration.Version != current.Version || snapshot["name"] != current.Name || snapshot["purpose"] != current.Purpose || snapshot["roleDescription"] != current.RoleDescription || snapshot["publishedInstructions"].(map[string]any)["content"] != current.PublishedInstructions.Content || snapshot["effectiveInstructions"].(map[string]any)["content"] != effective.Content || snapshot["effectiveInstructions"].(map[string]any)["digest"] != effective.Digest {
		t.Fatal("agent owner snapshot lost full published instructions/version")
	}
	digest := sha256.Sum256(result.AgentConfiguration.ConfigurationJSON)
	if hex.EncodeToString(digest[:]) != result.AgentConfiguration.ConfigurationSHA256 {
		t.Fatal("agent snapshot hash mismatch")
	}
	for name, mutate := range map[string]func(*entity.AssistantConfigurationCatalogRequest){
		"foreign recipient":   func(i *entity.AssistantConfigurationCatalogRequest) { i.EntityRef = otherRef },
		"foreign helper":      func(i *entity.AssistantConfigurationCatalogRequest) { i.AssistantRef = foreignHelperRef },
		"recipient as source": func(i *entity.AssistantConfigurationCatalogRequest) { i.AssistantRef = agentRef },
		"workflow":            func(i *entity.AssistantConfigurationCatalogRequest) { i.EntityKind = "WORKFLOW" },
		"no recipient":        func(i *entity.AssistantConfigurationCatalogRequest) { i.EntityRef = "" },
		"query":               func(i *entity.AssistantConfigurationCatalogRequest) { i.Query = "Manager" },
		"pagination":          func(i *entity.AssistantConfigurationCatalogRequest) { i.Offset = 40 },
		"account":             func(i *entity.AssistantConfigurationCatalogRequest) { i.AccountRef = "acc_foreign123" },
	} {
		t.Run("agent configuration "+name, func(t *testing.T) {
			bad := input
			mutate(&bad)
			if _, err := read(bad, stringMap(lease, "fence"), lease["generation"].(int64)); err == nil {
				t.Fatal("closed agent selector accepted")
			}
		})
	}
	if _, err := service.ListAssistantConfigurationCatalog(ctx, owner, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("ordinary caller read instructions")
	}
	for _, pins := range []struct {
		fence      string
		generation int64
	}{{"wrong-fence", lease["generation"].(int64)}, {stringMap(lease, "fence"), lease["generation"].(int64) + 1}} {
		if _, err := read(input, pins.fence, pins.generation); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("stale lease pins read instructions")
		}
	}
	var expiry time.Time
	if err := repository.pool.QueryRow(ctx, queryAssistantCurrentConfigurationExpire, stringMap(lease, "leaseRef")).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	_, expiredErr := read(input, stringMap(lease, "fence"), lease["generation"].(int64))
	if _, err := repository.pool.Exec(ctx, queryAssistantCurrentConfigurationRestoreExpiry, stringMap(lease, "leaseRef"), expiry); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(expiredErr, errs.ErrNotFound) {
		t.Fatal("expired lease read instructions")
	}
	var revokedBindings []string
	if err := repository.pool.QueryRow(ctx, queryAssistantConfigurationComponentMembership, pgx.StrictNamedArgs{"organization_id": actor.organizationID, "actor_id": actor.actorID}).Scan(&revokedBindings); err != nil {
		t.Fatal(err)
	}
	_, revokedErr := read(input, stringMap(lease, "fence"), lease["generation"].(int64))
	if _, err := repository.pool.Exec(ctx, queryAssistantConfigurationComponentMembershipRestore, pgx.StrictNamedArgs{"organization_id": actor.organizationID, "actor_id": actor.actorID, "refs": revokedBindings}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(revokedErr, errs.ErrForbidden) && !errors.Is(revokedErr, errs.ErrNotFound) {
		t.Fatal("revoked view/manage authority read instructions")
	}
}
