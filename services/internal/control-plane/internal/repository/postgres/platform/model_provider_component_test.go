package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/providercredentialclient"
)

// TestModelProviderBootstrapComponent proves optional Git-owned registration and restart affinity in disposable PostgreSQL.
// It never contacts a provider or interprets synthetic credential pins as verified secret material.
// Catalog claims additionally prove API-key method, live owner proof, source binding and terminal replay.
// TestModelProviderBootstrapComponent проверяет optional Git-owned registration и restart affinity в disposable PostgreSQL.
// Не обращается к provider и не считает synthetic credential pins проверенным secret material.
// Catalog claims дополнительно проверяют API-key method, live owner proof, source binding и terminal replay.
func TestModelProviderBootstrapComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	configureComponentArtifactScanner(t, repository)
	configureComponentProviderAndRoleImages(t, repository)
	config := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway", BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.4/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "vendor/model-v1", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
	if err := repository.ConfigureModelProvider(config, "vendor/model-v1"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatal("gateway bootstrap failed", err)
		}
	}
	tasks, err := repository.ClaimProviderModelCatalogTasks(ctx, "gateway-bootstrap-fixture", 16, &providercredentialclient.Client{})
	if err != nil || len(tasks) != 1 || tasks[0].AuthorizationMethod != "API_KEY" || tasks[0].ProviderDefinitionKey != config.DefinitionKey() {
		t.Fatalf("bootstrap gateway catalog did not select API_KEY: tasks=%+v err=%v", tasks, err)
	}
	task := tasks[0]
	savedProvider := repository.modelProviderCatalog
	repository.modelProviderCatalog = nil
	if _, err := repository.ResolveProviderModelCatalogProof(ctx, platformrepo.ProofPrincipalInput{CallerWorkload: "control-plane", Operation: platformrepo.ProviderModelCatalogOperation, RequestDigestSHA256: task.RequestDigest}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unconfigured provider retained catalog authority", err)
	}
	if claims, err := repository.ClaimProviderModelCatalogTasks(ctx, "unconfigured-fixture", 16, &providercredentialclient.Client{}); err != nil || len(claims) != 0 {
		t.Fatal("unconfigured provider created catalog work", err)
	}
	repository.modelProviderCatalog = savedProvider
	tasks, err = repository.ClaimProviderModelCatalogTasks(ctx, "restored-provider-fixture", 16, &providercredentialclient.Client{})
	if err != nil || len(tasks) != 1 || tasks[0].Ref == task.Ref {
		t.Fatal("retired catalog task was reused", err)
	}
	task = tasks[0]
	principal := platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "control-plane", Operation: platformrepo.ProviderModelCatalogOperation, RequestDigestSHA256: task.RequestDigest}
	if _, err := repository.ResolveProviderModelCatalogProof(ctx, principal); err != nil {
		t.Fatal("bootstrap catalog proof rejected", err)
	}
	observation := platformrepo.ProviderModelCatalogObservation{AccountRef: task.AccountRef, CredentialRef: task.CredentialRef, Source: "REMOTE_API", Failure: "NONE", ObservedAt: time.Now(), Models: []platformrepo.ProviderModelCatalogRecord{{ID: "vendor/model-v1"}}}
	wrong := observation
	wrong.Source = "REMOTE_CODEX"
	if err := repository.CompleteProviderModelCatalogTask(ctx, task, wrong); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("gateway accepted a device-code catalog source", err)
	}
	for i := 0; i < 2; i++ {
		if err := repository.CompleteProviderModelCatalogTask(ctx, task, observation); err != nil {
			t.Fatal("bootstrap catalog completion/replay rejected", err)
		}
	}
	if _, err := repository.ResolveProviderModelCatalogProof(ctx, principal); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("completed bootstrap catalog retained proof authority", err)
	}
	var provider, model string
	if err := pool.QueryRow(ctx, `SELECT definition_key FROM control_plane.provider_accounts WHERE stable_key=$1`, "default-"+config.DefinitionKey()).Scan(&provider); err != nil || provider != config.DefinitionKey() {
		t.Fatal("bootstrap credential affinity changed")
	}
	if err := pool.QueryRow(ctx, `SELECT provider,model FROM control_plane.runtime_profiles WHERE stable_key=$1`, repository.defaultRuntimeProfileKey()).Scan(&provider, &model); err != nil || provider != config.DefinitionKey() || model != "vendor/model-v1" {
		t.Fatal("default runtime profile affinity changed")
	}
	var organizationID, agentRef string
	if err := pool.QueryRow(ctx, `SELECT organization_id::text,ref FROM control_plane.agents WHERE system_key='system-assistant'`).Scan(&organizationID, &agentRef); err != nil {
		t.Fatal("system assistant lookup failed")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	agents, readErr := readProviderUsageAgents(ctx, tx, organizationID, []string{agentRef})
	rollbackErr := tx.Rollback(ctx)
	if readErr != nil || rollbackErr != nil || agents[agentRef].Provider != config.DefinitionKey() || agents[agentRef].NativeToolCount != 0 {
		t.Fatal("managed assistant preflight snapshot failed", readErr)
	}
	changed := config
	changed.BaseURL = "https://other.example.invalid/v1"
	if err := repository.ConfigureModelProvider(changed, "vendor/model-v1"); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err == nil {
		t.Fatal("existing credential endpoint was overwritten")
	}
	var endpoint string
	if err := pool.QueryRow(ctx, `SELECT capabilities->'modelProvider'->>'base_url' FROM control_plane.provider_definitions WHERE stable_key=$1`, config.DefinitionKey()).Scan(&endpoint); err != nil || endpoint != config.BaseURL {
		t.Fatal("failed reconcile changed endpoint")
	}
}

// TestModelCatalogBootstrapMethodComponent checks configured API-key catalog work and rejects retired or missing routes in separate migrated databases. No provider is contacted and no authorization attempt is synthesized by production code.
// TestModelCatalogBootstrapMethodComponent проверяет каталог настроенного API-key маршрута и отказ для удалённых или отсутствующих маршрутов в отдельных мигрированных БД. Обращения к provider отсутствуют; production code не создаёт фиктивной authorization attempt.
func TestModelCatalogBootstrapMethodComponent(t *testing.T) {
	for _, scenario := range []struct {
		name, explicit, configured, want string
		gateway, disabled                bool
	}{
		{name: "configured-bootstrap", want: "API_KEY"},
		{name: "configured-explicit-api-key", explicit: "API_KEY", want: "API_KEY"},
		{name: "gateway-bootstrap", gateway: true, configured: "active", want: "API_KEY"},
		{name: "retired-device-method-rejected", gateway: true, configured: "active", explicit: "DEVICE_CODE"},
		{name: "gateway-explicit-api-key", gateway: true, configured: "active", explicit: "API_KEY", want: "API_KEY"},
		{name: "gateway-not-configured", gateway: true},
		{name: "gateway-different-definition", gateway: true, configured: "different"},
		{name: "disabled-gateway", gateway: true, configured: "active", disabled: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool := newIsolatedAssistantComponentPool(t, ctx)
			defer pool.Close()
			repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
			if err != nil {
				t.Fatal(err)
			}
			configureComponentArtifactScanner(t, repository)
			configureComponentProviderAndRoleImages(t, repository)
			config := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway", BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.4/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "vendor/model-v1", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
			if scenario.gateway {
				if err := repository.ConfigureModelProvider(config, "vendor/model-v1"); err != nil {
					t.Fatal(err)
				}
			}
			if err := repository.Bootstrap(ctx); err != nil {
				t.Fatal("bootstrap method fixture", err)
			}
			if scenario.gateway && scenario.configured == "" {
				repository.modelProviderCatalog = nil
			} else if scenario.configured == "different" {
				config.ProviderID = "different-gateway"
				if err := repository.ConfigureModelProvider(config, "vendor/model-v1"); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.explicit != "" {
				if _, err := pool.Exec(ctx, `INSERT INTO control_plane.provider_authorization_attempts(ref, organization_id, provider_account_id, method, state, created_by)
					SELECT 'pauth_catalogfixture', account.organization_id, account.id, $1, 'AUTHORIZED', account.created_by
					FROM control_plane.provider_accounts account`, scenario.explicit); err != nil {
					t.Fatal("insert explicit method fixture", err)
				}
			}
			if scenario.disabled {
				if _, err := pool.Exec(ctx, "UPDATE control_plane.provider_definitions SET enabled=false"); err != nil {
					t.Fatal("disable method fixture", err)
				}
			}
			tasks, err := repository.ClaimProviderModelCatalogTasks(ctx, "method-fixture", 16, &providercredentialclient.Client{})
			if err != nil {
				t.Fatal("claim method fixture", err)
			}
			if scenario.want == "" {
				if len(tasks) != 0 {
					t.Fatal("ineligible definition or authorization method produced a catalog task")
				}
			} else if len(tasks) != 1 || tasks[0].AuthorizationMethod != scenario.want {
				t.Fatalf("wrong catalog method: tasks=%+v want=%s", tasks, scenario.want)
			}
			var attempts int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.provider_authorization_attempts").Scan(&attempts); err != nil {
				t.Fatal(err)
			}
			wantAttempts := 0
			if scenario.explicit != "" {
				wantAttempts = 1
			}
			if attempts != wantAttempts {
				t.Fatal("catalog claim created a fictitious authorization attempt")
			}
		})
	}
}

// TestModelProviderCatalogComponent proves independent registration, exact native selection and catalog grants for two accounts sharing a model alias; removing one route revokes only its pending work.
// TestModelProviderCatalogComponent проверяет независимую регистрацию, точный native selection и catalog grants двух accounts с общим alias; удаление маршрута отзывает только его pending work.
func TestModelProviderCatalogComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	repository, err := New(pool, "opencode-test-gateway", "vendor/model-v1", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	configureComponentArtifactScanner(t, repository)
	configureComponentProviderAndRoleImages(t, repository)
	first := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway", BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.4/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "vendor/model-v1", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
	second, err := first.Clone()
	if err != nil {
		t.Fatal(err)
	}
	second.ProviderID = "second-gateway"
	second.BaseURL = "https://second.example.invalid/v1"
	second.AddressCIDRs = []string{"192.0.2.5/32"}
	catalog := runtimecontract.ModelProviderCatalog{Schema: runtimecontract.ModelProviderCatalogSchema, ManagedBy: "git", SourceRevision: first.SourceRevision, DefaultProvider: first.DefinitionKey(), DefaultModel: first.Models[0].ID, Providers: []runtimecontract.ModelProviderConfiguration{first, second}}
	if err := repository.ConfigureModelProviderCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	for _, provider := range catalog.Providers {
		engine, selected, err := repository.runtimeProviderSelection(provider.DefinitionKey(), "vendor/model-v1")
		if err != nil || engine != "opencode" || selected.BaseURL != provider.BaseURL {
			t.Fatal("exact route selection failed", err)
		}
		var registered int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.runtime_profiles WHERE provider=$1 AND model=$2 AND stable_key=$3`, provider.DefinitionKey(), "vendor/model-v1", modelProviderProfileKey(provider, "vendor/model-v1")).Scan(&registered); err != nil || registered != 1 {
			t.Fatal("independent profile registration failed", err)
		}
	}
	if _, _, err := repository.runtimeProviderSelection("vendor/model-v1", "vendor/model-v1"); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("model alias selected a route")
	}
	// Only fixture setup inserts synthetic credential metadata; no secret value or remote reachability is claimed.
	// Только fixture setup вставляет synthetic credential metadata; secret value или remote reachability не предполагаются.
	if _, err := pool.Exec(ctx, `INSERT INTO control_plane.provider_accounts(ref,organization_id,definition_key,stable_key,name,state,created_by)
 SELECT 'pacc_secondcatalog',organization_id,$1,'second-catalog','Second catalog','AUTHORIZED',created_by FROM control_plane.provider_accounts WHERE stable_key=$2`, second.DefinitionKey(), "default-"+first.DefinitionKey()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO control_plane.provider_credential_revisions(ref,organization_id,provider_account_id,revision_number,secret_name,secret_uid,secret_resource_version,content_sha256,observed_at)
 SELECT 'pcred_secondcatalog',organization_id,id,1,'synthetic-second-catalog',gen_random_uuid(),'1',repeat('b',64),clock_timestamp() FROM control_plane.provider_accounts WHERE ref='pacc_secondcatalog'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.provider_accounts SET current_credential_revision_id=(SELECT id FROM control_plane.provider_credential_revisions WHERE ref='pcred_secondcatalog') WHERE ref='pacc_secondcatalog'`); err != nil {
		t.Fatal(err)
	}
	tasks, err := repository.ClaimProviderModelCatalogTasks(ctx, "catalog-two-routes", 16, &providercredentialclient.Client{})
	if err != nil || len(tasks) != 2 {
		t.Fatal("two accounts did not receive independent catalog tasks", len(tasks), err)
	}
	principals := map[string]platformrepo.ProofPrincipalInput{}
	for _, task := range tasks {
		principal := platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "control-plane", Operation: platformrepo.ProviderModelCatalogOperation, RequestDigestSHA256: task.RequestDigest}
		if _, err := repository.ResolveProviderModelCatalogProof(ctx, principal); err != nil {
			t.Fatal("active route proof rejected", err)
		}
		principals[task.ProviderDefinitionKey] = principal
	}
	if len(principals) != 2 {
		t.Fatal("catalog identities collapsed")
	}
	catalog.Providers = catalog.Providers[:1]
	if err := repository.ConfigureModelProviderCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ResolveProviderModelCatalogProof(ctx, principals[second.DefinitionKey()]); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("removed route retained authority", err)
	}
	if _, err := repository.ResolveProviderModelCatalogProof(ctx, principals[first.DefinitionKey()]); err != nil {
		t.Fatal("remaining route lost authority", err)
	}
	if claimed, err := repository.ClaimProviderModelCatalogTasks(ctx, "catalog-after-removal", 16, &providercredentialclient.Client{}); err != nil || len(claimed) != 0 {
		t.Fatal("removed route reissued work", err)
	}
	for _, task := range tasks {
		var state, fence, digest string
		if err := pool.QueryRow(ctx, `SELECT state,fence,request_digest FROM control_plane.provider_model_catalog_tasks WHERE ref=$1`, task.Ref).Scan(&state, &fence, &digest); err != nil {
			t.Fatal(err)
		}
		if task.ProviderDefinitionKey == second.DefinitionKey() {
			if state != "CANCELLED" || fence != "" || digest != "" {
				t.Fatal("removed route task did not close atomically")
			}
		} else if state != "CLAIMED" || fence == "" || digest != task.RequestDigest {
			t.Fatal("remaining route task changed")
		}
	}
	if _, _, err := repository.runtimeProviderSelection(second.DefinitionKey(), "vendor/model-v1"); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("removed route runtime selection survived")
	}
}
