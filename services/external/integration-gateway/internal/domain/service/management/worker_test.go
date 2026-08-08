package management

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/managementeffect"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/secretstore"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
)

func TestSingleDispatchEffectClosesReclaimedExternalWork(t *testing.T) {
	for _, kind := range []string{
		"PROVIDER_AUTHORIZE",
		"PROVIDER_REFERENCE_SYNC",
		"PROVIDER_REVOKE",
		"PROVIDER_POOL_SYNC",
		"INTEGRATION_TEST",
		"GIT_APPLY",
	} {
		if !singleDispatchEffect(kind) {
			t.Fatalf("external effect %s can be dispatched after a reclaimed lease", kind)
		}
	}
	if singleDispatchEffect("GIT_FETCH") {
		t.Fatal("read-only Git fetch was classified as an ambiguous mutation")
	}
}

func TestEffectFailureStatusPreservesAmbiguousOutcome(t *testing.T) {
	if got := effectFailureStatus(errors.Join(managementeffect.ErrOutcomeUnknown, errors.New("transport failed"))); got != "UNKNOWN" {
		t.Fatalf("ambiguous dispatch classified as %s", got)
	}
	if got := effectFailureStatus(errors.New("request validation failed")); got != "FAILED" {
		t.Fatalf("pre-dispatch failure classified as %s", got)
	}
}

func TestClosedFailureCategoryDoesNotReflectDiagnostics(t *testing.T) {
	secretBearing := errors.New("provider rejected bearer super-secret-value")
	if got := closedFailureCategory(secretBearing); got != "PROTOCOL_ERROR" {
		t.Fatalf("unbounded provider diagnostic escaped taxonomy: %s", got)
	}
}

func TestIntegrationTestTuplePinsDefinitionConfigurationConnectionAndCredential(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("a", 64)
	receipt := entity.IntegrationTestReceipt{
		ConnectionID: "connection", ConnectionVersion: 2, ConnectionGeneration: 3,
		DefinitionID: "definition", DefinitionVersion: 4, DefinitionDigest: digest,
		ConfigurationID: "configuration", ConfigurationVersion: 5, ConfigurationDigest: digest,
		CredentialGeneration: 3, CredentialBindingID: "binding", CredentialBindingVersion: 6,
		CredentialBindingDigest: digest,
	}
	connection := entity.ManagedProviderConnection{
		ID: receipt.ConnectionID, Version: receipt.ConnectionVersion, Generation: receipt.ConnectionGeneration,
		Status: "VALID", ActiveCredential: receipt.CredentialGeneration, CredentialBindingID: receipt.CredentialBindingID,
		CredentialBindingVersion: receipt.CredentialBindingVersion, CredentialBindingDigest: receipt.CredentialBindingDigest,
	}
	configuration := entity.IntegrationConfiguration{
		ID: receipt.ConfigurationID, Version: receipt.ConfigurationVersion, Digest: receipt.ConfigurationDigest,
		Status: "ACTIVE", ConnectionID: receipt.ConnectionID, ConnectionVersion: receipt.ConnectionVersion,
		ConnectionGeneration: receipt.ConnectionGeneration, DefinitionID: receipt.DefinitionID,
		DefinitionVersion: receipt.DefinitionVersion, DefinitionDigest: receipt.DefinitionDigest,
	}
	definition := entity.Definition{
		ID: receipt.DefinitionID, Version: receipt.DefinitionVersion, Digest: receipt.DefinitionDigest,
		ValidationEndpointRef: "health",
	}
	credential := entity.CredentialGeneration{
		ConnectionID: receipt.ConnectionID, Generation: receipt.CredentialGeneration, Status: "ACTIVE",
		CredentialBindingID: receipt.CredentialBindingID, CredentialBindingVersion: receipt.CredentialBindingVersion,
		CredentialBindingDigest: receipt.CredentialBindingDigest,
	}
	if !integrationTestTupleCurrent(receipt, connection, configuration, definition, credential) {
		t.Fatal("exact integration execution tuple was rejected")
	}
	for name, stale := range map[string]func(){
		"connection":    func() { connection.Generation++ },
		"configuration": func() { configuration.Version++ },
		"definition":    func() { definition.Digest = strings.Repeat("b", 64) },
		"credential":    func() { credential.Status = "REVOKED" },
	} {
		t.Run(name, func(t *testing.T) {
			connectionCopy, configurationCopy, definitionCopy, credentialCopy := connection, configuration, definition, credential
			connection, configuration, definition, credential = connectionCopy, configurationCopy, definitionCopy, credentialCopy
			stale()
			if integrationTestTupleCurrent(receipt, connection, configuration, definition, credential) {
				t.Fatal("stale integration execution tuple was accepted")
			}
			connection, configuration, definition, credential = connectionCopy, configurationCopy, definitionCopy, credentialCopy
		})
	}
}

func TestProviderLogoutIsNeverRepeatedAfterCrashCheckpoint(t *testing.T) {
	t.Parallel()

	if !providerLogoutDispatchAllowed("DISPATCHED", 1, 7) {
		t.Fatal("first fenced provider logout was not dispatched")
	}
	for _, test := range []struct {
		phase      string
		attempts   uint32
		generation uint64
	}{
		{"DISPATCHED", 2, 7},
		{"PENDING", 1, 7},
		{"SUCCEEDED", 1, 7},
		{"DISPATCHED", 1, 0},
	} {
		if providerLogoutDispatchAllowed(test.phase, test.attempts, test.generation) {
			t.Fatalf("provider logout could repeat after checkpoint: %#v", test)
		}
	}
}

type orphanSecretStore struct {
	raw        []byte
	version    secretstore.Version
	getErr     error
	revokeErr  error
	revokeCall int
}

func (*orphanSecretStore) Put(context.Context, string, []byte) (secretstore.Version, error) {
	return secretstore.Version{}, errors.New("unused")
}
func (store *orphanSecretStore) Get(context.Context, string) ([]byte, secretstore.Version, error) {
	return append([]byte(nil), store.raw...), store.version, store.getErr
}
func (store *orphanSecretStore) Revoke(context.Context, string, uint64) error {
	store.revokeCall++
	return store.revokeErr
}
func (*orphanSecretStore) Check(context.Context) error { return nil }

func TestOrphanAuthorizationCleanupRequiresDestroyReadback(t *testing.T) {
	t.Parallel()

	for name, store := range map[string]*orphanSecretStore{
		"absent":  {getErr: secretstore.ErrNotFound},
		"present": {raw: []byte("credential"), version: secretstore.Version{Version: 7}},
	} {
		t.Run(name, func(t *testing.T) {
			service := &Service{worker: &WorkerDependencies{Secrets: store}}
			if err := service.cleanupOrphanAuthorizationSecret(context.Background(), "ref"); err != nil {
				t.Fatal(err)
			}
			if name == "present" && store.revokeCall != 1 || name == "absent" && store.revokeCall != 0 {
				t.Fatalf("unexpected durable cleanup calls: %d", store.revokeCall)
			}
		})
	}
	unknown := &orphanSecretStore{getErr: errors.New("Vault unavailable")}
	service := &Service{worker: &WorkerDependencies{Secrets: unknown}}
	if err := service.cleanupOrphanAuthorizationSecret(context.Background(), "ref"); err == nil {
		t.Fatal("unknown orphan secret state was terminalized")
	}
}

var _ secretstore.Store = (*orphanSecretStore)(nil)
