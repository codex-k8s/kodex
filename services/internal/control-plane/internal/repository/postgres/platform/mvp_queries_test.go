package platform

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderAccountActionsUseCanonicalNextActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		item entity.ProviderAccount
		want []string
	}{
		{name: "pending without attempt", item: entity.ProviderAccount{State: "PENDING_AUTHORIZATION"}, want: []string{"OPEN", "CONFIGURE_CREDENTIAL", "REVOKE"}},
		{name: "pending device authorization", item: entity.ProviderAccount{
			State:         "PENDING_AUTHORIZATION",
			Authorization: &entity.ProviderAuthorization{State: "PENDING"},
		}, want: []string{"OPEN", "REFRESH_AUTHORIZATION", "REVOKE"}},
		{name: "active", item: entity.ProviderAccount{State: "AUTHORIZED", Enabled: true}, want: []string{"OPEN", "TEST", "REVOKE", "DISABLE"}},
		{name: "disabled", item: entity.ProviderAccount{State: "DISABLED"}, want: []string{"OPEN", "REVOKE", "ENABLE"}},
		{name: "configure", item: entity.ProviderAccount{State: "REAUTHORIZATION_REQUIRED"}, want: []string{"OPEN", "CONFIGURE_CREDENTIAL", "REVOKE"}},
		{name: "revoked", item: entity.ProviderAccount{State: "REVOKED"}, want: []string{"OPEN"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := providerAccountActions(test.item, true, true, true); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("provider account actions = %v, want %v", got, test.want)
			}
		})
	}
}

func TestProviderAccountLifecycleStateMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state   string
		enabled bool
		valid   bool
	}{
		{state: "PENDING_AUTHORIZATION", valid: true},
		{state: "AUTHORIZED", enabled: true, valid: true},
		{state: "DISABLED", valid: true},
		{state: "REAUTHORIZATION_REQUIRED", valid: true},
		{state: "REAUTHORIZATION_REQUIRED", enabled: true, valid: true},
		{state: "REVOKED", valid: true},
		{state: "AUTHORIZED", valid: false},
		{state: "DISABLED", enabled: true, valid: false},
		{state: "REVOKED", enabled: true, valid: false},
		{state: "UNKNOWN", valid: false},
	}
	for _, test := range tests {
		if got := validProviderAccountLifecycle(test.state, test.enabled); got != test.valid {
			t.Fatalf("lifecycle %s/enabled=%v = %v, want %v", test.state, test.enabled, got, test.valid)
		}
	}
}

func TestProviderAccountEnabledTransitionMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                              string
		state                             string
		enabled, hasCredential, requested bool
		wantState                         string
		wantOK                            bool
	}{
		{name: "disable", state: "AUTHORIZED", enabled: true, hasCredential: true, wantState: "DISABLED", wantOK: true},
		{name: "enable", state: "DISABLED", hasCredential: true, requested: true, wantState: "AUTHORIZED", wantOK: true},
		{name: "revoke terminal", state: "REVOKED", hasCredential: true, requested: true},
		{name: "reauthorization cannot enable", state: "REAUTHORIZATION_REQUIRED", hasCredential: true, requested: true},
		{name: "missing credential", state: "DISABLED", requested: true},
		{name: "disable idempotent conflict", state: "DISABLED", hasCredential: true},
		{name: "enable idempotent conflict", state: "AUTHORIZED", enabled: true, hasCredential: true, requested: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state, ok := providerAccountEnabledTransition(test.state, test.enabled, test.hasCredential, test.requested)
			if state != test.wantState || ok != test.wantOK {
				t.Fatalf("transition = %q/%v, want %q/%v", state, ok, test.wantState, test.wantOK)
			}
		})
	}
}

func TestProviderCompensationQueryWaitsForOwnerTransactionAndMatchesExactDescriptor(t *testing.T) {
	t.Parallel()
	if !strings.Contains(queryProviderAccountsMaterializationGuard, "FOR UPDATE") {
		t.Fatal("provider compensation guard does not wait for the owner transaction")
	}
	for _, field := range []string{
		"auth_attempt.ref = @authorization_ref",
		"auth_attempt.materializer_attempt_ref = @materializer_attempt_ref",
		"credential.secret_name = @secret_name",
		"credential.secret_uid::text = @secret_uid",
		"credential.secret_resource_version = @secret_resource_version",
		"credential.content_sha256 = @content_sha256",
	} {
		if !strings.Contains(queryProviderAccountsMaterializationReferenced, field) {
			t.Fatalf("provider compensation query does not match %q", field)
		}
	}
}

func TestBootstrapComponentProviderAccountLifecycle(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatalf("construct repository: %v", err)
	}
	credentialConfig := ProviderCredentialConfig{
		SecretName:            "runtime-provider-openai-default-r1",
		SecretUID:             "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1",
		ContentSHA256:         "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}
	if err := repository.ConfigureProviderCredential(credentialConfig); err != nil {
		t.Fatalf("configure provider credential: %v", err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles",
		DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64),
		LeaseSigningKey:       []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatalf("configure role images: %v", err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap provider account: %v", err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Provider lifecycle owner", CallerWorkload: "control-api-gateway",
		Operation: "platform.command.provider-accounts.set-enabled",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct provider lifecycle service: %v", err)
	}
	accounts, _, _, err := service.ListProviderAccounts(ctx, owner, query.Filter{Page: query.Page{Size: 20}})
	if err != nil || len(accounts) == 0 {
		t.Fatalf("list provider accounts: accounts=%#v err=%v", accounts, err)
	}
	account := accounts[0]
	materializerPrincipal, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve provider materializer principal: %v", err)
	}
	credential := entity.ProviderCredentialDescriptor{
		SecretName:            credentialConfig.SecretName,
		SecretUID:             credentialConfig.SecretUID,
		SecretResourceVersion: credentialConfig.SecretResourceVersion,
		ContentSHA256:         credentialConfig.ContentSHA256,
	}
	referenced, err := repository.ProviderMaterializationReferenced(
		ctx, materializerPrincipal, account.Ref, "pauth_component_exact", "", &credential,
	)
	if err != nil || !referenced {
		t.Fatalf("exact provider materialization reference = %v, err=%v", referenced, err)
	}
	credential.ContentSHA256 = strings.Repeat("f", 64)
	referenced, err = repository.ProviderMaterializationReferenced(
		ctx, materializerPrincipal, account.Ref, "pauth_component_mismatch", "", &credential,
	)
	if err != nil || referenced {
		t.Fatalf("mismatched provider materialization reference = %v, err=%v", referenced, err)
	}

	account = executeProviderEnabledTransition(t, ctx, service, owner, account, false, "provider-disable-component")
	if account.State != "DISABLED" || account.Enabled || account.Ready ||
		!reflect.DeepEqual(account.NextActions, []string{"OPEN", "REVOKE", "ENABLE"}) {
		t.Fatalf("disabled provider account = %#v", account)
	}
	account = executeProviderEnabledTransition(t, ctx, service, owner, account, true, "provider-enable-component")
	if account.State != "AUTHORIZED" || !account.Enabled || !account.Ready ||
		!reflect.DeepEqual(account.NextActions, []string{"OPEN", "TEST", "REVOKE", "DISABLE"}) {
		t.Fatalf("re-enabled provider account = %#v", account)
	}
	version := account.Version
	revoked, err := service.Execute(ctx, command.Command{
		Kind: command.RevokeProviderAccount, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-revoke-component", ExpectedVersion: &version},
		Payload:  command.ProviderAccountInput{AccountRef: account.Ref},
	})
	if err != nil || revoked.ProviderAccount == nil {
		t.Fatalf("revoke provider account: account=%#v err=%v", revoked.ProviderAccount, err)
	}
	account = *revoked.ProviderAccount
	if account.State != "REVOKED" || account.Enabled || account.Ready ||
		!reflect.DeepEqual(account.NextActions, []string{"OPEN"}) {
		t.Fatalf("revoked provider account = %#v", account)
	}
	version = account.Version
	_, err = service.Execute(ctx, command.Command{
		Kind: command.SetProviderAccountEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-revoked-enable-component", ExpectedVersion: &version},
		Payload:  command.ProviderAccountInput{AccountRef: account.Ref, Enabled: true},
	})
	if !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("terminal revoked account enable error = %v", err)
	}
}

func executeProviderEnabledTransition(
	t *testing.T,
	ctx context.Context,
	service *platformservice.Service,
	principal value.Principal,
	account entity.ProviderAccount,
	enabled bool,
	idempotencyKey string,
) entity.ProviderAccount {
	t.Helper()
	version := account.Version
	result, err := service.Execute(ctx, command.Command{
		Kind: command.SetProviderAccountEnabled, Principal: principal,
		Mutation: value.Mutation{IdempotencyKey: idempotencyKey, ExpectedVersion: &version},
		Payload:  command.ProviderAccountInput{AccountRef: account.Ref, Enabled: enabled},
	})
	if err != nil || result.ProviderAccount == nil {
		t.Fatalf("set provider account enabled=%v: account=%#v err=%v", enabled, result.ProviderAccount, err)
	}
	return *result.ProviderAccount
}

func TestSynchronousPurgeKeepsTombstoneNameUnique(t *testing.T) {
	if !strings.Contains(queryArtifactsPurgeFinalize, "file_name = 'purged-' || ref") {
		t.Fatal("synchronous artifact purge reuses a shared tombstone file name")
	}
}

func TestAttachmentSnapshotQueriesKeepImmutableRuntimeBoundary(t *testing.T) {
	for _, fragment := range []string{
		"artifact.lifecycle_state = 'ACTIVE'",
		"artifact.revision = item.artifact_revision",
		"content.digest = item.digest",
		"content.size_bytes = item.size_bytes",
		"FOR SHARE OF artifact",
	} {
		if !strings.Contains(queryAttachmentSetsLockMaterializableItems, fragment) {
			t.Fatalf("attachment set materialization lock lacks %q", fragment)
		}
	}
	for _, fragment := range []string{
		"artifact.lifecycle_state IN ('ACTIVE', 'DELETED')",
		"artifact.revision = item.artifact_revision",
		"content.digest = item.digest",
		"input_artifact.lifecycle_state IN ('ACTIVE', 'DELETED')",
		"runtime_revision.safe_snapshot -> 'artifacts'",
	} {
		if !strings.Contains(queryRuntimeClaimExecutionSelectClaimableAgentExecutions, fragment) &&
			!strings.Contains(queryArtifactsImpact, fragment) {
			t.Fatalf("runtime snapshot queries lack %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"artifact.version = item.artifact_version",
		"input_artifact.version = input_item.artifact_version",
	} {
		if strings.Contains(queryRuntimeClaimExecutionSelectClaimableAgentExecutions, forbidden) {
			t.Fatalf("runtime claim depends on mutable artifact version through %q", forbidden)
		}
	}
	for _, fragment := range []string{
		"artifact.lifecycle_state IN ('ACTIVE', 'DELETED')",
		"exact.item -> 'revision' = to_jsonb(artifact.revision)",
		"exact.item ->> 'digest' = content.digest",
	} {
		if !strings.Contains(queryRuntimeReadexecutionartifactSelectArtifactContent, fragment) {
			t.Fatalf("runtime artifact read lacks %q", fragment)
		}
	}
	if !strings.Contains(queryArtifactsDownloadartifactSelectArtifactForGrant, "ar.lifecycle_state = 'ACTIVE'") {
		t.Fatal("ordinary artifact download no longer rejects soft-deleted artifacts")
	}
	for _, fragment := range []string{
		"item.artifact_revision = artifact.revision",
		"runtime_revision.safe_snapshot -> 'artifacts'",
		"run.id = runtime_revision.root_run_id",
	} {
		if !strings.Contains(queryArtifactsImpact, fragment) {
			t.Fatalf("artifact purge guard lacks %q", fragment)
		}
	}
}

func TestProviderAccountActionsForViewOnlyMember(t *testing.T) {
	t.Parallel()
	item := entity.ProviderAccount{State: "AUTHORIZED", Enabled: true}
	if got := providerAccountActions(item, false, false, false); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("view-only provider account actions = %v, want [OPEN]", got)
	}
}
