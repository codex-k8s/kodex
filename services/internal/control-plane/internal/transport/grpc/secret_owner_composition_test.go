package grpc

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	service "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	postgres "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	grpcgo "google.golang.org/grpc"
)

// Harness доступен лишь отдельному тестовому бинарю с isolated template DB.
// Он использует настоящие owner-транзакции и production casters, но намеренно
// не заявляет проверку bearer/mTLS: principal назначает закрытая test fixture.
func TestSecretOwnerLoopbackHarness(t *testing.T) {
	ready := os.Getenv("KODEX_SECRET_COMPOSITION_READY_FILE")
	if ready == "" {
		t.Skip("disposable owner composition is not configured")
	}
	dsn := os.Getenv("KODEX_SECRET_COMPOSITION_OWNER_DSN")
	configuration, err := pgx.ParseConfig(dsn)
	if err != nil || os.Getenv("KODEX_SECRET_COMPOSITION_SYNTHETIC") != "1" || configuration.Host != "127.0.0.1" || configuration.Port < 1024 || configuration.User != "control_plane_runtime_g1" || !strings.HasPrefix(configuration.Database, "kodex_assistant_test_") || os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN") != "" {
		t.Fatal("owner composition requires a dedicated isolated database")
	}
	directory, err := os.Lstat(filepath.Dir(ready))
	if err != nil || !directory.IsDir() || directory.Mode().Perm() != 0o700 {
		t.Fatal("composition rendezvous directory must be private")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("open isolated owner database")
	}
	defer pool.Close()
	repository, err := postgres.New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProviderCredential(postgres.ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(postgres.RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles", DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal("bootstrap isolated owner", err)
	}
	ownerService, err := service.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	harness := &secretCompositionOwner{repository: repository, service: ownerService}
	principal, err := harness.principal(ctx, "platform.command.projects.create", false)
	if err != nil {
		t.Fatal(err)
	}
	project, err := ownerService.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: principal, Mutation: value.Mutation{IdempotencyKey: "composition-owner-project"}, Payload: command.ProjectInput{Name: "Synthetic secret owner composition", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatal("create isolated composition project", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("allocate composition loopback listener")
	}
	server := grpcgo.NewServer()
	cp.RegisterRuntimeSecretDraftWorkServiceServer(server, harness)
	cp.RegisterPlatformCommandServiceServer(server, harness)
	cp.RegisterRuntimeSecretWorkServiceServer(server, harness)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() { server.Stop(); <-served }()
	raw, err := json.Marshal(struct{ Address, ProjectRef string }{listener.Addr().String(), project.Project.Ref})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(ready, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal("create private composition receipt")
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		t.Fatal("write composition receipt")
	}
	if err := file.Close(); err != nil {
		t.Fatal("close composition receipt")
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("composition owner exceeded bounded lifetime")
		case <-ticker.C:
			if info, err := os.Lstat(ready + ".stop"); err == nil {
				if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
					t.Fatal("invalid composition stop marker")
				}
				return
			}
		}
	}
}

type secretCompositionOwner struct {
	cp.UnimplementedRuntimeSecretDraftWorkServiceServer
	cp.UnimplementedPlatformCommandServiceServer
	cp.UnimplementedRuntimeSecretWorkServiceServer
	repository *postgres.Repository
	service    *service.Service
}

func (h *secretCompositionOwner) principal(ctx context.Context, permission string, worker bool) (value.Principal, error) {
	input := port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", OwnerClaim: true, Operation: permission}
	if worker {
		input.ExternalActorID = "kodex-system-subject"
		input.ExternalTenantID = "kodex-installation"
		input.CallerWorkload = "secret-broker"
		input.OwnerClaim = false
	}
	authority, err := h.repository.ResolveProofAuthority(ctx, input)
	if err != nil {
		return value.Principal{}, transportError(err)
	}
	principal := value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID, Permission: permission, CorrelationRef: "composition-" + permission, CallerWorkload: input.CallerWorkload, CredentialRevision: 1}
	if !worker {
		principal.CredentialAuthenticatedAt = time.Now().UTC()
		principal.CredentialACR = "urn:kodex:acr:interactive"
		principal.CredentialAMR = []string{"pwd"}
	}
	return principal, nil
}

func (h *secretCompositionOwner) prepare(ctx context.Context, input port.RuntimeSecretDraftPrepareInput, organization bool) (*cp.RuntimeSecretDraftOperationReceipt, error) {
	permission := "secret.rotate"
	if input.Kind == "SAVE" {
		permission = "secret.create"
	}
	p, err := h.principal(ctx, permission, false)
	if err != nil {
		return nil, err
	}
	var result entity.RuntimeSecretDraftOperationReceipt
	if organization {
		result, err = h.service.PrepareOrganizationRuntimeSecretDraft(ctx, p, input)
	} else {
		result, err = h.service.PrepareRuntimeSecretDraft(ctx, p, input)
	}
	if err != nil {
		return nil, transportError(err)
	}
	return castSecretDraftReceipt(result), nil
}
func (h *secretCompositionOwner) PrepareOrganizationRuntimeSecretDraft(ctx context.Context, r *cp.PrepareOrganizationRuntimeSecretDraftRequest) (*cp.PrepareOrganizationRuntimeSecretDraftResponse, error) {
	o, e := h.prepare(ctx, port.RuntimeSecretDraftPrepareInput{Kind: "SAVE", Mutation: mutation(r.Mutation), Name: r.Name, Description: r.Description, ValueType: runtimeSecretValueTypeName(r.ValueType), ExpectedContentSHA256: r.ExpectedContentSha256}, true)
	return &cp.PrepareOrganizationRuntimeSecretDraftResponse{Operation: o}, e
}
func (h *secretCompositionOwner) PrepareSaveRuntimeSecretDraft(ctx context.Context, r *cp.PrepareSaveRuntimeSecretDraftRequest) (*cp.PrepareSaveRuntimeSecretDraftResponse, error) {
	o, e := h.prepare(ctx, port.RuntimeSecretDraftPrepareInput{Kind: "SAVE", Mutation: mutation(r.Mutation), Name: r.Name, ProjectRef: r.ProjectRef, SecretRef: r.SecretRef, ValueType: runtimeSecretValueTypeName(r.ValueType), ExpectedContentSHA256: r.ExpectedContentSha256}, false)
	return &cp.PrepareSaveRuntimeSecretDraftResponse{Operation: o}, e
}
func (h *secretCompositionOwner) PrepareValidateRuntimeSecretDraft(ctx context.Context, r *cp.PrepareValidateRuntimeSecretDraftRequest) (*cp.PrepareValidateRuntimeSecretDraftResponse, error) {
	o, e := h.prepare(ctx, port.RuntimeSecretDraftPrepareInput{Kind: "VALIDATE", Mutation: mutation(r.Mutation), DraftRef: r.DraftRef}, false)
	return &cp.PrepareValidateRuntimeSecretDraftResponse{Operation: o}, e
}
func (h *secretCompositionOwner) PreparePublishRuntimeSecretDraft(ctx context.Context, r *cp.PreparePublishRuntimeSecretDraftRequest) (*cp.PreparePublishRuntimeSecretDraftResponse, error) {
	o, e := h.prepare(ctx, port.RuntimeSecretDraftPrepareInput{Kind: "PUBLISH", Mutation: mutation(r.Mutation), DraftRef: r.DraftRef, ExpectedSecretVersion: r.ExpectedSecretVersion, ImpactPlanRef: r.ImpactPlanRef, SelectedItemRefs: r.SelectedItemRefs}, false)
	return &cp.PreparePublishRuntimeSecretDraftResponse{Operation: o}, e
}
func (h *secretCompositionOwner) PrepareRuntimeSecretDraftImpact(ctx context.Context, r *cp.PrepareRuntimeSecretDraftImpactRequest) (*cp.PrepareRuntimeSecretDraftImpactResponse, error) {
	p, e := h.principal(ctx, "secret.rotate", false)
	if e != nil {
		return nil, e
	}
	result, e := h.service.PrepareRuntimeSecretDraftImpact(ctx, p, r.DraftRef, mutation(r.Mutation))
	if e != nil {
		return nil, transportError(e)
	}
	return &cp.PrepareRuntimeSecretDraftImpactResponse{Plan: castDraftImpactPlan(result)}, nil
}
func (h *secretCompositionOwner) CheckRuntimeSecretDraftWorkReadiness(ctx context.Context, _ *cp.CheckRuntimeSecretDraftWorkReadinessRequest) (*cp.CheckRuntimeSecretDraftWorkReadinessResponse, error) {
	p, e := h.principal(ctx, "platform.runtime-secret-drafts.readiness.check", true)
	if e != nil {
		return nil, e
	}
	if e = h.service.CheckRuntimeSecretDraftWork(ctx, p); e != nil {
		return nil, transportError(e)
	}
	return &cp.CheckRuntimeSecretDraftWorkReadinessResponse{Ready: true}, nil
}
func (h *secretCompositionOwner) ConsumeRuntimeSecretDraftOperation(ctx context.Context, r *cp.ConsumeRuntimeSecretDraftOperationRequest) (*cp.ConsumeRuntimeSecretDraftOperationResponse, error) {
	p, e := h.principal(ctx, "platform.runtime-secret-drafts.operations.consume", true)
	if e != nil {
		return nil, e
	}
	w, e := h.service.ConsumeRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{OperationGrant: r.OperationGrant, ClaimantID: r.ClaimantId})
	if e != nil {
		return nil, transportError(e)
	}
	return &cp.ConsumeRuntimeSecretDraftOperationResponse{Work: castDraftWork(w)}, nil
}
func (h *secretCompositionOwner) ListRuntimeSecretDraftRecoveryWork(ctx context.Context, r *cp.ListRuntimeSecretDraftRecoveryWorkRequest) (*cp.ListRuntimeSecretDraftRecoveryWorkResponse, error) {
	p, e := h.principal(ctx, "platform.runtime-secret-drafts.operations.recover", true)
	if e != nil {
		return nil, e
	}
	works, next, e := h.service.ListRuntimeSecretDraftRecovery(ctx, p, page(r.Page))
	if e != nil {
		return nil, transportError(e)
	}
	out := &cp.ListRuntimeSecretDraftRecoveryWorkResponse{Page: &cp.PageInfo{NextPageToken: next}}
	for _, w := range works {
		out.Operations = append(out.Operations, castDraftWork(w))
	}
	return out, nil
}
func (h *secretCompositionOwner) finish(ctx context.Context, action, ref, claimant string, generation int64, encrypted *cp.RuntimeSecretDraftEncryptedDescriptor, materialized *cp.RuntimeSecretMaterialization) (entity.RuntimeSecretDraftResult, error) {
	permission := map[string]string{"COMPLETE": "operations.complete", "RECOVER": "materialization.recover", "CLEANUP": "cleanup.complete"}[action]
	p, e := h.principal(ctx, "platform.runtime-secret-drafts."+permission, true)
	if e != nil {
		return entity.RuntimeSecretDraftResult{}, e
	}
	result, e := h.service.FinishRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{Action: action, OperationRef: ref, ClaimantID: claimant, ClaimGeneration: generation, Encrypted: draftEncrypted(encrypted), Materialization: runtimeSecretMaterialization(materialized)})
	return result, transportError(e)
}
func (h *secretCompositionOwner) CompleteRuntimeSecretDraftOperation(ctx context.Context, r *cp.CompleteRuntimeSecretDraftOperationRequest) (*cp.CompleteRuntimeSecretDraftOperationResponse, error) {
	result, e := h.finish(ctx, "COMPLETE", r.OperationRef, r.ClaimantId, r.ClaimGeneration, r.Encrypted, r.Materialization)
	if e != nil {
		return nil, e
	}
	out := &cp.CompleteRuntimeSecretDraftOperationResponse{Draft: castSecretDraft(result.Draft)}
	if result.Secret != nil {
		out.Secret = castRuntimeSecret(*result.Secret)
	}
	return out, nil
}
func (h *secretCompositionOwner) RecoverRuntimeSecretDraftMaterialization(ctx context.Context, r *cp.RecoverRuntimeSecretDraftMaterializationRequest) (*cp.RecoverRuntimeSecretDraftMaterializationResponse, error) {
	result, e := h.finish(ctx, "RECOVER", r.OperationRef, r.ClaimantId, r.ClaimGeneration, r.Encrypted, r.Materialization)
	if e != nil {
		return nil, e
	}
	return &cp.RecoverRuntimeSecretDraftMaterializationResponse{Draft: castSecretDraft(result.Draft), OperationState: runtimeSecretOperationState(result.State), EncryptedAction: cp.RuntimeSecretRecoveryAction(cp.RuntimeSecretRecoveryAction_value["RUNTIME_SECRET_RECOVERY_ACTION_"+result.EncryptedAction]), MaterializationAction: cp.RuntimeSecretRecoveryAction(cp.RuntimeSecretRecoveryAction_value["RUNTIME_SECRET_RECOVERY_ACTION_"+result.MaterializationAction])}, nil
}
func (h *secretCompositionOwner) CompleteRuntimeSecretDraftCleanup(ctx context.Context, r *cp.CompleteRuntimeSecretDraftCleanupRequest) (*cp.CompleteRuntimeSecretDraftCleanupResponse, error) {
	result, e := h.finish(ctx, "CLEANUP", r.OperationRef, r.ClaimantId, r.ClaimGeneration, r.Encrypted, r.Materialization)
	if e != nil {
		return nil, e
	}
	return &cp.CompleteRuntimeSecretDraftCleanupResponse{Completed: result.Completed}, nil
}
func (h *secretCompositionOwner) PrepareRevokeRuntimeSecret(ctx context.Context, r *cp.PrepareRevokeRuntimeSecretRequest) (*cp.PrepareRevokeRuntimeSecretResponse, error) {
	p, e := h.principal(ctx, "secret.revoke", false)
	if e != nil {
		return nil, e
	}
	result, e := h.service.PrepareRuntimeSecretOperation(ctx, p, port.RuntimeSecretPrepareInput{Kind: "REVOKE", SecretRef: r.SecretRef, Mutation: mutation(r.Mutation)})
	if e != nil {
		return nil, transportError(e)
	}
	return &cp.PrepareRevokeRuntimeSecretResponse{Operation: &cp.RuntimeSecretOperationReceipt{OperationRef: result.OperationRef, OperationGrant: result.OperationGrant, ExpiresAt: timestamp(result.ExpiresAt), State: runtimeSecretOperationState(result.State)}}, nil
}
func (h *secretCompositionOwner) ConsumeRuntimeSecretOperation(ctx context.Context, r *cp.ConsumeRuntimeSecretOperationRequest) (*cp.ConsumeRuntimeSecretOperationResponse, error) {
	p, e := h.principal(ctx, "platform.runtime-secrets.operations.consume", true)
	if e != nil {
		return nil, e
	}
	o, e := h.service.ConsumeRuntimeSecretOperation(ctx, p, port.RuntimeSecretConsumeInput{OperationGrant: r.OperationGrant, ClaimantID: r.ClaimantId})
	if e != nil {
		return nil, transportError(e)
	}
	return &cp.ConsumeRuntimeSecretOperationResponse{ScopeKind: runtimeSecretScopeKind(o.ScopeKind), OrganizationRef: o.OrganizationRef, ProjectRef: o.ProjectRef, OperationRef: o.Ref, Kind: runtimeSecretOperationKind(o.Kind), SecretRef: o.SecretRef, ClaimGeneration: o.ClaimGeneration}, nil
}
func (h *secretCompositionOwner) CompleteRuntimeSecretOperation(ctx context.Context, r *cp.CompleteRuntimeSecretOperationRequest) (*cp.CompleteRuntimeSecretOperationResponse, error) {
	p, e := h.principal(ctx, "platform.runtime-secrets.operations.complete", true)
	if e != nil {
		return nil, e
	}
	s, e := h.service.CompleteRuntimeSecretOperation(ctx, p, port.RuntimeSecretCompleteInput{OperationRef: r.OperationRef, ClaimantID: r.ClaimantId, ClaimGeneration: r.ClaimGeneration, Materialization: runtimeSecretMaterialization(r.Materialization)})
	if e != nil {
		return nil, transportError(e)
	}
	return &cp.CompleteRuntimeSecretOperationResponse{Secret: castRuntimeSecret(s)}, nil
}
func (h *secretCompositionOwner) RecoverRuntimeSecretMaterialization(ctx context.Context, r *cp.RecoverRuntimeSecretMaterializationRequest) (*cp.RecoverRuntimeSecretMaterializationResponse, error) {
	p, e := h.principal(ctx, "platform.runtime-secrets.operations.recover", true)
	if e != nil {
		return nil, e
	}
	m := runtimeSecretMaterialization(r.Materialization)
	if m == nil {
		return nil, transportError(errs.ErrInvalid)
	}
	result, e := h.service.RecoverRuntimeSecretMaterialization(ctx, p, port.RuntimeSecretRecoveryInput{OperationRef: r.OperationRef, Materialization: *m})
	if e != nil {
		return nil, transportError(e)
	}
	return &cp.RecoverRuntimeSecretMaterializationResponse{Action: runtimeSecretRecoveryAction(result.Action), OperationState: runtimeSecretOperationState(result.OperationState)}, nil
}
