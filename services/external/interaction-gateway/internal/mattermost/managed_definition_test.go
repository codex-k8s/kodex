package mattermost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"google.golang.org/protobuf/proto"
)

// invalidPackageFixture serializes a deliberately invalid managed definition with its exact
// raw digest, so gateway rejection cannot pass solely because of a detached digest.
// RU: invalidPackageFixture сериализует намеренно недопустимое управляемое определение
// с точным дайджестом, чтобы отказ шлюза не сводился к несовпадению дайджеста.
func invalidPackageFixture(t *testing.T, candidate integrationpackage.Package) ([]byte, string) {
	t.Helper()
	raw, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := integrationpackage.Parse(raw); err == nil {
		t.Fatal("invalid managed fixture passed package validation")
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:])
}

// managedPackageFixture narrows one shipped definition for managed connection tests;
// gated fixtures deliberately violate validation and remain digest-bound negative inputs.
// RU: managedPackageFixture сужает поставленное определение для тестов управляемого подключения;
// gated-оснастки намеренно нарушают валидацию, сохраняя привязку отрицательного входа к дайджесту.
func managedPackageFixture(t *testing.T, baseline integrationpackage.Package, origin string, gated bool) (integrationpackage.Package, []byte) {
	t.Helper()
	raw, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	var candidate integrationpackage.Package
	if json.Unmarshal(raw, &candidate) != nil {
		t.Fatal("invalid package fixture")
	}
	candidate.Metadata.Origin, candidate.Metadata.Version = origin, "3.0.0"
	candidate.Spec.HealthCheck.TimeoutSeconds, candidate.Spec.HealthCheck.MaxAttempts = 1, 1
	for index := range candidate.Spec.Capabilities {
		candidate.Spec.Capabilities[index].Execution.TimeoutSeconds = 1
		candidate.Spec.Capabilities[index].Execution.MaxAttempts = 1
		if gated && candidate.Spec.Capabilities[index].Operation == candidate.Spec.HealthCheck.Operation {
			candidate.Spec.Capabilities[index].ApprovalPolicy = "HUMAN_EACH_EFFECT"
		}
	}
	if gated {
		raw, candidate.Digest = invalidPackageFixture(t, candidate)
		return candidate, raw
	}
	raw, err = json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := integrationpackage.Parse(raw)
	if err != nil || integrationpackage.ValidateExecutableRevision(parsed, baseline) != nil {
		t.Fatal("managed fixture exceeded executable baseline")
	}
	return parsed, raw
}

// TestManagedMattermostPackagesDoNotReplaceConcurrentConnections verifies that UI and Git
// revisions can execute concurrently without mutating the shipped adapter definition.
// RU: TestManagedMattermostPackagesDoNotReplaceConcurrentConnections проверяет параллельные
// UI/Git-версии без изменения поставленного определения адаптера.
func TestManagedMattermostPackagesDoNotReplaceConcurrentConnections(t *testing.T) {
	adapter, original := claimFixture(t, "mattermost.team.read", map[string]any{})
	baseline := adapter.definition.Digest
	credential := configureProviderFixture(t, adapter, func(request *http.Request) (*http.Response, bool) {
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatal("managed invocation ignored selected revision deadline")
		}
		return nil, false
	})
	var group sync.WaitGroup
	for _, origin := range []string{"UI", "GIT"} {
		definition, raw := managedPackageFixture(t, adapter.definition, origin, false)
		claim := proto.Clone(original).(*cp.IntegrationInvocationClaim)
		claim.DefinitionPackage, claim.DefinitionVersion, claim.DefinitionDigest = raw, definition.Metadata.Version, definition.Digest
		claim.CredentialRevision = credential
		group.Go(func() {
			for range 5 {
				if _, err := adapter.Execute(t.Context(), claim); err != nil {
					t.Error("managed invocation failed")
					return
				}
				health := &cp.IntegrationConnectionTestClaim{DefinitionKey: claim.DefinitionKey, DefinitionVersion: claim.DefinitionVersion, DefinitionDigest: claim.DefinitionDigest, DefinitionPackage: claim.DefinitionPackage, PublicConfiguration: claim.PublicConfiguration, CredentialRevision: credential}
				if _, err := adapter.TestConnection(t.Context(), health); err != nil {
					t.Error("managed health check failed")
					return
				}
			}
		})
	}
	group.Wait()
	if adapter.definition.Digest != baseline || adapter.definition.Metadata.Origin != integrationpackage.Origin {
		t.Fatal("managed connection replaced shared registry")
	}
}

// TestManagedMattermostRejectsDetachedPackageAndUngatedHealth verifies that malformed,
// detached and forbidden-gate definitions fail before any provider request.
// RU: TestManagedMattermostRejectsDetachedPackageAndUngatedHealth проверяет отказ для
// некорректных, отсоединённых и запрещённых gate-определений до обращения к провайдеру.
func TestManagedMattermostRejectsDetachedPackageAndUngatedHealth(t *testing.T) {
	adapter, original := claimFixture(t, "mattermost.team.read", map[string]any{})
	credential := configureProviderFixture(t, adapter, func(*http.Request) (*http.Response, bool) {
		t.Fatal("invalid managed package or missing approval reached provider")
		return nil, false
	})
	for _, mode := range []string{"missing", "malformed", "oversize", "digest", "version", "gated_health"} {
		t.Run(mode, func(t *testing.T) {
			definition, raw := managedPackageFixture(t, adapter.definition, "UI", mode == "gated_health")
			claim := proto.Clone(original).(*cp.IntegrationInvocationClaim)
			claim.DefinitionPackage, claim.DefinitionVersion, claim.DefinitionDigest = raw, definition.Metadata.Version, definition.Digest
			claim.CredentialRevision = credential
			switch mode {
			case "missing":
				claim.DefinitionPackage = nil
			case "malformed":
				claim.DefinitionPackage = []byte("invalid")
			case "oversize":
				claim.DefinitionPackage = make([]byte, (256<<10)+1)
			case "digest":
				claim.DefinitionDigest = adapter.definition.Digest
			case "version":
				claim.DefinitionVersion = "4.0.0"
			}
			health := &cp.IntegrationConnectionTestClaim{DefinitionKey: claim.DefinitionKey, DefinitionVersion: claim.DefinitionVersion, DefinitionDigest: claim.DefinitionDigest, DefinitionPackage: claim.DefinitionPackage, PublicConfiguration: claim.PublicConfiguration, CredentialRevision: credential}
			if _, err := adapter.TestConnection(t.Context(), health); err == nil {
				t.Fatal("invalid package or gated health accepted")
			}
			if mode != "gated_health" {
				if _, err := adapter.Execute(t.Context(), claim); err == nil {
					t.Fatal("detached invocation package accepted")
				}
			}
		})
	}
}
