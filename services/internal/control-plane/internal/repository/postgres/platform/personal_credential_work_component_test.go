package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// personalTestCompletionFromClaim copies only the exact owner-issued lease coordinates. The
// fixture's declared observation is not evidence of a live provider call or JWT transport.
// personalTestCompletionFromClaim копирует только exact owner-issued lease coordinates.
// Объявленное fixture наблюдение не доказывает live provider call либо JWT transport.
func personalTestCompletionFromClaim(claim map[string]any) command.PersonalIntegrationCredentialTestCompletion {
	return command.PersonalIntegrationCredentialTestCompletion{TestRef: stringMap(claim, "testRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64)}
}

// assertPersonalCredentialWork uses real owner transactions to qualify queue/claim/one-time
// admission/receipt, changed-configuration/revoke fences, expiry and concurrent claim isolation.
// External health observations remain synthetic fixture inputs and do not establish provider rights.
// assertPersonalCredentialWork проверяет реальными owner-транзакциями queue/claim/одноразовый
// admission/receipt, configuration/revoke fences, expiry и concurrent claim isolation.
// Внешние health observations остаются synthetic fixture inputs и не подтверждают provider rights.
func assertPersonalCredentialWork(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner, user value.Principal,
	connection entity.IntegrationConnection, binding entity.PersonalIntegrationCredential) (entity.IntegrationConnection, entity.PersonalIntegrationCredential) {
	t.Helper()
	workerService, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "integration-gateway", Operation: "platform.personal-integration-tests.claim"}, "integration-gateway")
	read := func() entity.PersonalIntegrationCredential {
		t.Helper()
		p := user
		p.Permission = "platform.personal-integrations.get"
		item, err := service.GetPersonalIntegrationCredential(ctx, p, connection.Ref)
		if err != nil {
			t.Fatal("read work binding", err)
		}
		return item
	}
	queue := func(key string) entity.PersonalIntegrationCredential {
		t.Helper()
		current := read()
		p := user
		p.Permission = "platform.personal-integrations.test"
		result, err := service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: p,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &current.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}})
		if err != nil || result.PersonalIntegrationCredential == nil || result.PersonalIntegrationCredential.State != "CHECKING" {
			t.Fatal("queue current personal test", err)
		}
		return *result.PersonalIntegrationCredential
	}
	claim := func() command.PersonalIntegrationCredentialTestCompletion {
		t.Helper()
		p := worker
		p.Permission = "platform.personal-integration-tests.claim"
		items, err := workerService.ClaimPersonalIntegrationCredentialTests(ctx, p, "personal-native-worker", 1)
		if err != nil || len(items) != 1 {
			t.Fatal("claim personal test", err)
		}
		credential, ok := items[0]["credential"].(entity.IntegrationCredentialRevision)
		if !ok || credential.Ref != read().Credential.Ref {
			t.Fatal("claim selected a shared or foreign credential")
		}
		keys, pinned := items[0]["personalReadProbeCapabilityKeys"].([]string)
		if !pinned || !slices.Contains(keys, "gitlab.project.metadata.read") || !slices.Contains(keys, "gitlab.branch.list") || slices.Contains(keys, "gitlab.issue.create") {
			t.Fatal("owner claim did not pin registered READ probes")
		}
		return personalTestCompletionFromClaim(items[0])
	}
	begin := func(input command.PersonalIntegrationCredentialTestCompletion) error {
		p := worker
		p.Permission = "platform.personal-integration-tests.begin"
		return workerService.BeginPersonalIntegrationCredentialProbe(ctx, p, input)
	}
	complete := func(key string, input command.PersonalIntegrationCredentialTestCompletion) (command.Result, error) {
		p := worker
		p.Permission = "platform.personal-integration-tests.complete"
		return workerService.Execute(ctx,
			command.Command{Kind: command.CompletePersonalIntegrationCredentialTest, Principal: p, Mutation: value.Mutation{IdempotencyKey: key}, Payload: input})
	}
	user.Permission = "platform.personal-integrations.test"
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: user,
		Mutation: value.Mutation{IdempotencyKey: "personal-work-disabled", ExpectedVersion: &binding.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("disabled Connection admitted personal verification", err)
	}
	owner.Permission = "platform.command.integrations.enable"
	enabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-work-enable", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref, Enabled: true}})
	if err != nil || enabled.Connection == nil {
		t.Fatal("enable work fixture", err)
	}
	connection = *enabled.Connection
	checking := queue("personal-work-first-test")
	user.Permission = "platform.personal-integrations.test"
	replayed, err := service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: user,
		Mutation: value.Mutation{IdempotencyKey: "personal-work-first-test", ExpectedVersion: &binding.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}})
	if err != nil || replayed.PersonalIntegrationCredential == nil || replayed.PersonalIntegrationCredential.Version != checking.Version {
		t.Fatal("test request receipt did not replay", err)
	}
	probe := claim()
	report := probe
	report.Outcome = entity.PersonalIntegrationCredentialAuthenticated
	if _, err := complete("personal-work-before-begin", report); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("completion bypassed probe admission", err)
	}
	if err := begin(probe); err != nil {
		t.Fatal("consume current probe lease", err)
	}
	if err := begin(probe); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("admitted probe was consumable twice", err)
	}
	result, err := complete("personal-work-complete", report)
	if err != nil || result.PersonalIntegrationCredentialTest == nil || !result.PersonalIntegrationCredentialTest.Applied || result.PersonalIntegrationCredentialTest.State != "SUCCEEDED" {
		t.Fatal("complete personal probe", err)
	}
	ready := read()
	if ready.State != "READY" || len(ready.VerifiedCapabilities) != 1 || ready.ExpiresAt != nil {
		t.Fatal("health completion fabricated permission coverage or expiry")
	}
	if _, err := complete("personal-work-complete", report); err != nil || read().Version != ready.Version {
		t.Fatal("completion receipt repeated a state change", err)
	}
	changed := report
	changed.Outcome = entity.PersonalIntegrationCredentialAuthenticationRejected
	if _, err := complete("personal-work-complete", changed); !errors.Is(err, errs.ErrIdempotencyReuse) {
		t.Fatal("changed completion reused an old receipt", err)
	}
	queue("personal-work-expanded-read-test")
	expanded := claim()
	if err := begin(expanded); err != nil {
		t.Fatal("begin expanded READ probe", err)
	}
	expanded.Outcome = entity.PersonalIntegrationCredentialAuthenticated
	expanded.ConfirmedCapabilityKeys = []string{"gitlab.project.metadata.read", "gitlab.issue.create"}
	if _, err := complete("personal-work-forged-write-proof", expanded); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("worker fabricated permission outside immutable READ snapshot", err)
	}
	expanded.ConfirmedCapabilityKeys = []string{"gitlab.project.metadata.read", "gitlab.branch.list"}
	expanded.DeniedCapabilityKeys = []string{"gitlab.issue.list"}
	if _, err := complete("personal-work-observed-read-proof", expanded); err != nil {
		t.Fatal("persist actual READ observations", err)
	}
	ready = read()
	if len(ready.VerifiedCapabilities) != 2 || !slices.Contains(ready.VerifiedCapabilities, "gitlab.branch.list") || len(ready.DeniedCapabilities) != 1 || ready.DeniedCapabilities[0] != "gitlab.issue.list" || ready.ExpiresAt != nil {
		t.Fatal("owner lost exact confirmed/denied/unknown observation distinction")
	}
	// A real configuration command invalidates proof without turning another user's revoked key READY.
	// Реальная configuration command сбрасывает proof, не возвращая READY отозванному ключу другого пользователя.
	owner.Permission = "platform.command.integrations.update"
	updated, err := service.Execute(ctx, command.Command{Kind: command.UpdateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-work-config-change", ExpectedVersion: &connection.Version},
		Payload:  command.ConnectionInput{Ref: connection.Ref, Name: connection.Name, PublicConfiguration: map[string]any{"base_url": "https://next.example.invalid", "project_path": "fixture/project"}}})
	if err != nil || updated.Connection == nil {
		t.Fatal("change verified Connection configuration", err)
	}
	connection = *updated.Connection
	if read().State != "CHECK_REQUIRED" {
		t.Fatal("configuration change kept stale verification READY")
	}
	if _, err := complete("personal-work-complete", report); err != nil || read().State != "CHECK_REQUIRED" {
		t.Fatal("historical receipt recreated invalidated proof", err)
	}
	checking = queue("personal-work-revoke-test")
	probe = claim()
	user.Permission = "platform.personal-integrations.revoke"
	_, err = service.Execute(ctx, command.Command{Kind: command.RevokePersonalIntegrationCredential, Principal: user,
		Mutation: value.Mutation{IdempotencyKey: "personal-work-revoke", ExpectedVersion: &checking.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref}})
	if err != nil {
		t.Fatal(err)
	}
	if err := begin(probe); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("revoked claimed probe reached admission", err)
	}
	revoked := read()
	user.Permission = "platform.personal-integrations.configure"
	_, err = service.ConfigurePersonalIntegrationCredential(ctx, user, value.Mutation{IdempotencyKey: "personal-work-restore-key", ExpectedVersion: &revoked.Version}, connection.Ref, connection.Version, []byte("synthetic-worker-restored-key"))
	if err != nil {
		t.Fatal("restore key after revoke", err)
	}
	checking = queue("personal-work-rotate-test")
	probe = claim()
	if err := begin(probe); err != nil {
		t.Fatal(err)
	}
	user.Permission = "platform.personal-integrations.configure"
	_, err = service.ConfigurePersonalIntegrationCredential(ctx, user, value.Mutation{IdempotencyKey: "personal-work-rotate-key", ExpectedVersion: &checking.Version}, connection.Ref, connection.Version, []byte("synthetic-worker-replacement-key"))
	if err != nil {
		t.Fatal(err)
	}
	report = probe
	report.Outcome = entity.PersonalIntegrationCredentialAuthenticated
	if _, err := complete("personal-work-late-rotation", report); !errors.Is(err, errs.ErrForbidden) || read().State != "CHECK_REQUIRED" {
		t.Fatal("late completion verified a replacement key", err)
	}
	queue("personal-work-disable-test")
	probe = claim()
	if err := begin(probe); err != nil {
		t.Fatal(err)
	}
	owner.Permission = "platform.command.integrations.enable"
	disabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-work-disable", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref, Enabled: false}})
	if err != nil || disabled.Connection == nil {
		t.Fatal(err)
	}
	connection = *disabled.Connection
	if read().State != "CHECK_REQUIRED" {
		t.Fatal("Connection disable stranded a checking binding")
	}
	report = probe
	report.Outcome = entity.PersonalIntegrationCredentialAuthenticated
	if _, err := complete("personal-work-late-disable", report); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("disabled Connection accepted late proof", err)
	}
	enabled, err = service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-work-reenable", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref, Enabled: true}})
	if err != nil || enabled.Connection == nil {
		t.Fatal(err)
	}
	connection = *enabled.Connection
	checking = queue("personal-work-expiry-test")
	// Expiry is an explicit isolated lease fixture, not a fabricated provider timeout.
	// Expiry — явная изолированная lease fixture, не выдуманный provider timeout.
	var jobID string
	if err := r.pool.QueryRow(ctx, "SELECT id::text FROM control_plane.integration_personal_credential_tests WHERE binding_id=(SELECT id FROM control_plane.integration_personal_credentials WHERE ref=$1) AND state='DUE'", checking.Ref).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	_, err = r.pool.Exec(ctx, queryPersonalCredentialTestClaim, pgx.StrictNamedArgs{"organization_id": worker.AuthorityTenant, "id": jobID, "version": int64(1), "lease_ref": "lea_expiryfixture", "fence_digest": strings.Repeat("a", 64), "instance": "expired-fixture", "expires_at": time.Now().Add(-time.Minute)})
	if err != nil {
		t.Fatal("prepare expired lease fixture", err)
	}
	worker.Permission = "platform.personal-integration-tests.claim"
	items, err := workerService.ClaimPersonalIntegrationCredentialTests(ctx, worker, "expiry-native-worker", 1)
	if err != nil || len(items) != 0 || read().State != "CHECK_REQUIRED" {
		t.Fatal("expired verification claim did not release the binding", err)
	}
	queue("personal-work-concurrent-test")
	var wg sync.WaitGroup
	results := make(chan []map[string]any, 2)
	failures := make(chan error, 2)
	for _, instance := range []string{"personal-worker-a", "personal-worker-b"} {
		wg.Add(1)
		go func(instance string) {
			defer wg.Done()
			items, err := workerService.ClaimPersonalIntegrationCredentialTests(ctx, worker, instance, 1)
			results <- items
			failures <- err
		}(instance)
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal("concurrent claim failed", err)
		}
	}
	count := 0
	for items := range results {
		count += len(items)
		if len(items) == 1 {
			probe = personalTestCompletionFromClaim(items[0])
		}
	}
	if count != 1 {
		t.Fatal("two workers claimed the same personal verification")
	}
	if err := begin(probe); err != nil {
		t.Fatal(err)
	}
	report = probe
	report.Outcome = entity.PersonalIntegrationCredentialVerificationUnavailable
	if _, err := complete("personal-work-unavailable", report); err != nil || read().State != "CHECK_REQUIRED" {
		t.Fatal("transient verification failure became a rejected key", err)
	}
	var admitted, audited int
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.integration_personal_credential_tests WHERE binding_id=(SELECT id FROM control_plane.integration_personal_credentials WHERE ref=$1) AND probe_admitted_at IS NOT NULL", binding.Ref).Scan(&admitted); err != nil {
		t.Fatal("read durable probe admission", err)
	}
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.audit_events WHERE resource_kind='PERSONAL_INTEGRATION_CREDENTIAL_TEST' AND action='platform.personal-integration-tests.begin'").Scan(&audited); err != nil {
		t.Fatal("read probe admission audit", err)
	}
	if admitted != 5 || audited != admitted {
		t.Fatalf("probe admission and audit differ: admitted=%d audited=%d", admitted, audited)
	}
	return connection, read()
}
