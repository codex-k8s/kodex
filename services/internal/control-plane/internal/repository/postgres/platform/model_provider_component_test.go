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
	savedProvider := repository.modelProvider
	repository.modelProvider = nil
	if _, err := repository.ResolveProviderModelCatalogProof(ctx, platformrepo.ProofPrincipalInput{CallerWorkload: "control-plane", Operation: platformrepo.ProviderModelCatalogOperation, RequestDigestSHA256: task.RequestDigest}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unconfigured provider retained catalog authority", err)
	}
	if claims, err := repository.ClaimProviderModelCatalogTasks(ctx, "unconfigured-fixture", 16, &providercredentialclient.Client{}); err != nil || len(claims) != 0 {
		t.Fatal("unconfigured provider created catalog work", err)
	}
	repository.modelProvider = savedProvider
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
				repository.modelProvider = nil
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
