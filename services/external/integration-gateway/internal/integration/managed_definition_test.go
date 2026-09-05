package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integrationfixture"
)

func managedDefinitionFixture(t *testing.T, baseline integrationpackage.Package, name, origin string) integrationpackage.Package {
	t.Helper()
	raw, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	var candidate integrationpackage.Package
	if json.Unmarshal(raw, &candidate) != nil {
		t.Fatal("invalid package fixture")
	}
	candidate.Metadata.Origin, candidate.Metadata.Version, candidate.Spec.Name = origin, "2.0.0", name
	raw, err = json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := integrationpackage.Parse(raw)
	if err != nil {
		t.Fatal("managed package fixture was rejected")
	}
	return parsed
}

func sealedDefinitionFixture(t *testing.T, candidate integrationpackage.Package) integrationpackage.Package {
	t.Helper()
	raw, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := integrationpackage.Parse(raw)
	if err != nil {
		t.Fatal("managed package fixture was rejected")
	}
	return parsed
}

func TestManagedExecutionUsesSelectedRevisionDeadline(t *testing.T) {
	for _, provider := range []string{"synthetic", "github", "gitlab"} {
		t.Run(provider, func(t *testing.T) {
			adapter := testAdapter(t)
			definition := managedDefinitionFixture(t, adapter.definitions[provider], "Ограниченная конфигурация", "UI")
			operation := definition.Spec.HealthCheck.Operation
			for index := range definition.Spec.Capabilities {
				definition.Spec.Capabilities[index].Execution.TimeoutSeconds = 1
				definition.Spec.Capabilities[index].Execution.MaxAttempts = 1
			}
			definition = sealedDefinitionFixture(t, definition)
			var credential *CredentialRevision
			if definition.Spec.Credential != nil {
				credential = testCredential(t, adapter, "deadline-fixture-token")
			}
			request := invocationRequest(t, definition, operation, map[string]any{}, credential)
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := request.Context().Deadline()
				if !ok || time.Until(deadline) > time.Second || time.Until(deadline) <= 0 {
					t.Fatal("provider request did not inherit selected revision deadline")
				}
				return nil, context.DeadlineExceeded
			})}
			adapter.syntheticClient, adapter.githubHTTPClient, adapter.providerHTTPClient = client, client, client
			if _, err := adapter.Execute(t.Context(), request); err == nil || calls != 1 {
				t.Fatal("bounded provider failure was retried or accepted")
			}
		})
	}
}

func TestManagedHealthCheckHonorsNarrowedBudgetAndGate(t *testing.T) {
	for _, mode := range []string{"budget", "human_gate"} {
		t.Run(mode, func(t *testing.T) {
			adapter := testAdapter(t)
			definition := managedDefinitionFixture(t, adapter.definitions["gitlab"], "Проверка конфигурации", "GIT")
			definition.Spec.HealthCheck.TimeoutSeconds, definition.Spec.HealthCheck.MaxAttempts = 1, 1
			if mode == "human_gate" {
				for index := range definition.Spec.Capabilities {
					if definition.Spec.Capabilities[index].Operation == definition.Spec.HealthCheck.Operation {
						definition.Spec.Capabilities[index].ApprovalPolicy = "HUMAN_EACH_EFFECT"
					}
				}
			}
			definition = sealedDefinitionFixture(t, definition)
			request := invocationRequest(t, definition, definition.Spec.HealthCheck.Operation, map[string]any{}, testCredential(t, adapter, "health-fixture-token"))
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if mode == "human_gate" {
					t.Fatal("gated health operation reached provider without owner approval")
				}
				deadline, ok := request.Context().Deadline()
				if !ok || time.Until(deadline) > time.Second {
					t.Fatal("health check ignored selected revision timeout")
				}
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}
			if _, err := adapter.Test(t.Context(), request); err == nil {
				t.Fatal("failed health check accepted")
			}
			if mode == "budget" && calls != 1 || mode == "human_gate" && calls != 0 {
				t.Fatal("health check ignored selected revision attempt or approval policy")
			}
		})
	}
}

func TestManagedDefinitionsRemainLocalToConcurrentConnections(t *testing.T) {
	fixture := integrationfixture.NewHandler(integrationfixture.NewStore())
	fixture.SetReady(true)
	provider := httptest.NewServer(fixture)
	defer provider.Close()
	adapter := testAdapter(t)
	adapter.syntheticBaseURL, adapter.syntheticClient = mustParseURL(t, provider.URL), provider.Client()
	baseline := adapter.definitions["synthetic"]
	first := managedDefinitionFixture(t, baseline, "Первая конфигурация", "UI")
	second := managedDefinitionFixture(t, baseline, "Вторая конфигурация", "GIT")
	if first.Digest == second.Digest {
		t.Fatal("fixture revisions are equal")
	}
	var group sync.WaitGroup
	for i, definition := range []integrationpackage.Package{first, second} {
		request := invocationRequest(t, definition, "synthetic.journal.read", map[string]any{}, nil)
		request.ConnectionRef += string(rune('a' + i))
		group.Go(func() {
			for range 10 {
				if _, err := adapter.Execute(t.Context(), request); err != nil {
					t.Error("managed connection execution failed")
					return
				}
				if _, err := adapter.Test(t.Context(), request); err != nil {
					t.Error("managed connection health check failed")
					return
				}
			}
		})
	}
	group.Wait()
	if adapter.definitions["synthetic"].Digest != baseline.Digest || adapter.definitions["synthetic"].Metadata.Origin != integrationpackage.Origin {
		t.Fatal("managed revision replaced shared adapter baseline")
	}
}

func TestManagedDefinitionRejectsDetachedPinsBeforeProvider(t *testing.T) {
	adapter := testAdapter(t)
	calls := 0
	adapter.syntheticClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		t.Fatal("invalid package reached provider")
		return nil, nil
	})}
	definition := managedDefinitionFixture(t, adapter.definitions["synthetic"], "Конфигурация", "UI")
	for _, mode := range []string{"missing", "malformed", "digest", "version", "key", "adapter", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			request := invocationRequest(t, definition, "synthetic.journal.read", map[string]any{}, nil)
			switch mode {
			case "missing":
				request.DefinitionPackage = nil
			case "malformed":
				request.DefinitionPackage = []byte("invalid")
			case "digest":
				request.DefinitionDigest = adapter.definitions["synthetic"].Digest
			case "version":
				request.DefinitionVersion = "3.0.0"
			case "key":
				request.DefinitionKey = "github"
			case "adapter":
				changed := definition
				changed.Spec.Adapter = "GITHUB"
				request.DefinitionPackage, _ = json.Marshal(changed)
			case "oversize":
				request.DefinitionPackage = make([]byte, (256<<10)+1)
			}
			if _, err := adapter.Execute(t.Context(), request); err == nil {
				t.Fatal("detached package accepted")
			}
		})
	}
	if calls != 0 {
		t.Fatal("provider called for detached package")
	}
}
