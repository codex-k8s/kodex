package prototypematerial

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestWorkloadRegistryRejectsUnknownPathOperationAndField(t *testing.T) {
	t.Parallel()

	registry := testWorkloadRegistry(t)
	if _, err := registry.target("kv/data/mattercodex/internal-rpc-authority/foreign/readback"); err == nil {
		t.Fatal("unknown logical path was accepted")
	}
	target, err := registry.target(testPath("readback-credential"))
	if err != nil {
		t.Fatalf("registered path rejected: %v", err)
	}
	if err := target.validateData(map[string]string{"foreign": "value"}); err == nil {
		t.Fatal("unknown material field was accepted")
	}
	delivery, err := NewFileDelivery(registry)
	if err != nil {
		t.Fatalf("construct file delivery: %v", err)
	}
	if _, err := delivery.CreateKV2(context.Background(), testPath("readback-credential"), map[string]string{"foreign": "value"}); err == nil {
		t.Fatal("write through read-only file backend was accepted")
	}
}

func TestMaterialReadbackRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	path := testPath("auth-private")
	registry := DeliveryRegistry{targets: map[string]deliveryTarget{}}
	if err := registry.addDirect(path, "internal-rpc-authority-test-issuer-key", rotatingKeyFields()); err != nil {
		t.Fatalf("add direct target: %v", err)
	}
	target, _ := registry.target(path)
	secret := boundSecret(target.resourceName, "11")
	secret.Data = map[string][]byte{
		"private.jwk":          []byte("private"),
		"current_private_jwk":  []byte("current"),
		"next_private_jwk":     []byte("next"),
		"current_generation":   []byte("1"),
		"next_generation":      []byte("2"),
		"source_revision":      []byte("1"),
		"source_digest_sha256": []byte(strings.Repeat("a", 64)),
	}
	versionKey, digestKey := metadataKeys(path)
	secret.Metadata.Annotations = map[string]string{
		versionKey: "1",
		digestKey:  strings.Repeat("f", 64),
	}
	if _, _, err := materialFromSecret(secret, target); err == nil {
		t.Fatal("material with mismatched digest was accepted")
	}
	secret.Data["foreign"] = []byte("value")
	if _, _, err := materialFromSecret(secret, target); err == nil {
		t.Fatal("material Secret with unknown key was accepted")
	}
}

func TestKubernetesDeliveryRejectsSemanticCASBeforeUpdate(t *testing.T) {
	t.Parallel()

	path := testPath("readback-possession")
	registry := DeliveryRegistry{targets: map[string]deliveryTarget{}}
	if err := registry.addDocument(
		path,
		"internal-rpc-authority-test-issuer-delivery",
		"readback-possession.json",
		"",
		materialFields(materialReadbackPossession),
	); err != nil {
		t.Fatalf("add document target: %v", err)
	}
	target, _ := registry.target(path)
	data := map[string]string{
		"possession_private_jwk":           "private",
		"possession_key_kid":               "kid",
		"possession_key_generation":        "1",
		"possession_key_thumbprint_sha256": strings.Repeat("a", 64),
	}
	digest, err := canonicalDigest(data)
	if err != nil {
		t.Fatalf("digest test material: %v", err)
	}
	raw, err := json.Marshal(materialDocument{Version: 2, Digest: digest, Data: data})
	if err != nil {
		t.Fatalf("encode test material: %v", err)
	}
	secret := boundSecret(target.resourceName, "17")
	secret.Data = map[string][]byte{target.storageKey: raw}
	encodedSecret, _ := json.Marshal(secret)
	putCount := 0
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("bounded-token"), 0o600); err != nil {
		t.Fatalf("write test token: %v", err)
	}
	delivery := &KubernetesDelivery{
		config:   KubernetesConfig{Address: "https://kubernetes.default.svc:443", Namespace: Namespace, TokenFile: tokenFile},
		registry: registry,
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method == http.MethodPut {
				putCount++
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(encodedSecret)))}, nil
		})},
	}
	if _, err := delivery.WriteKV2CAS(context.Background(), path, 1, data); err == nil {
		t.Fatal("stale semantic CAS was accepted")
	}
	if putCount != 0 {
		t.Fatalf("stale semantic CAS reached update: %d", putCount)
	}
}

func TestSecretUpdatePreservesKubernetesMetadata(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"apiVersion":"v1","kind":"Secret","type":"Opaque","data":{"state.json":""},
		"metadata":{
			"name":"internal-rpc-authority-prototype-static-role-state",
			"namespace":"mattercodex-system","resourceVersion":"31","uid":"fixed-uid",
			"labels":{"mattercodex.dev/profile":"direct-production-single-node-prototype"},
			"creationTimestamp":"2026-08-10T00:00:00Z"
		}
	}`)
	var secret secretDocument
	if err := decodeStrictJSON(raw, &secret); err != nil {
		t.Fatalf("decode Kubernetes Secret: %v", err)
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("encode Kubernetes Secret: %v", err)
	}
	var served map[string]any
	if err := json.Unmarshal(encoded, &served); err != nil {
		t.Fatalf("decode preserved Secret: %v", err)
	}
	metadata := served["metadata"].(map[string]any)
	if metadata["uid"] != "fixed-uid" || metadata["creationTimestamp"] == nil || metadata["labels"] == nil {
		t.Fatal("Kubernetes metadata was not preserved across update")
	}
}

func TestStaticRoleLifecycleRejectsRollbackAndReachableRetiredSecret(t *testing.T) {
	t.Parallel()

	registry := staticRoleRegistry()
	manager := &StaticRoleManager{
		registry: registry, sourceRevision: 1, sourceDigest: strings.Repeat("a", 64),
	}
	digests := make(map[string]string)
	for role, definition := range registry {
		if definition.Status != "RETIRED" {
			digests[role] = strings.Repeat("a", 64)
		}
	}
	state := manager.initialState(digests)
	foreignState := state
	foreignState.SourceRevision++
	if err := manager.validateState(foreignState); err == nil {
		t.Fatal("foreign static role source revision was accepted")
	}
	stateRaw, _ := json.Marshal(state)
	stateSecret := boundSecret(StaticRoleState, "23")
	stateSecret.Data = map[string][]byte{staticRoleStateKey: stateRaw}
	stateEncoded, _ := json.Marshal(stateSecret)
	retired := repository.VaultStaticRoleExpectation{
		Role: "internal-rpc-authority-publisher-g1", Principal: "ira_publisher_g1", DatabaseName: "internal-rpc-authority",
	}
	previous := repository.VaultStaticRoleExpectation{
		Role: "internal-rpc-authority-publisher-g3", Principal: "ira_publisher_g3", DatabaseName: "internal-rpc-authority",
	}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("bounded-token"), 0o600); err != nil {
		t.Fatalf("write test token: %v", err)
	}
	retiredReachable := false
	manager.api = &KubernetesDelivery{
		config: KubernetesConfig{Address: "https://kubernetes.default.svc:443", Namespace: Namespace, TokenFile: tokenFile},
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if strings.HasSuffix(request.URL.Path, "/"+StaticRoleState) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(stateEncoded)))}, nil
			}
			if retiredReachable {
				secret := boundSecret("internal-rpc-authority-publisher-database-g1", "24")
				raw, _ := json.Marshal(secret)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		})},
	}
	if err := manager.RotateStaticRoles(context.Background(), []repository.VaultStaticRoleExpectation{previous}); err == nil {
		t.Fatal("previous generation rollback was accepted")
	}
	if err := manager.RevokeStaticRoles(context.Background(), []repository.VaultStaticRoleExpectation{retired}); err != nil {
		t.Fatalf("idempotent retired generation was rejected: %v", err)
	}
	if err := manager.VerifyRevokedStaticRoles(context.Background(), []repository.VaultStaticRoleExpectation{retired}); err != nil {
		t.Fatalf("retired generation absence was rejected: %v", err)
	}
	retiredReachable = true
	if err := manager.VerifyRevokedStaticRoles(context.Background(), []repository.VaultStaticRoleExpectation{retired}); err == nil {
		t.Fatal("reachable retired credential Secret was accepted")
	}
}

func testWorkloadRegistry(t *testing.T) DeliveryRegistry {
	t.Helper()
	registry, err := NewWorkloadFileRegistry(map[string]string{
		testPath("restore-credential"):  "restore-credential.json",
		testPath("restore-ack"):         "restore-ack.json",
		testPath("readback-credential"): "readback-credential.json",
		testPath("readback-possession"): "readback-possession.json",
	}, nil)
	if err != nil {
		t.Fatalf("construct workload registry: %v", err)
	}
	return registry
}

func testPath(name string) string {
	return "kv/data/mattercodex/internal-rpc-authority/test/issuer/" + name
}

func boundSecret(name, resourceVersion string) secretDocument {
	secret := secretDocument{APIVersion: "v1", Kind: "Secret", Type: "Opaque", Data: map[string][]byte{}}
	secret.Metadata.Name = name
	secret.Metadata.Namespace = Namespace
	secret.Metadata.ResourceVersion = resourceVersion
	return secret
}

func canonicalDigest(data map[string]string) (string, error) {
	return internalrpcauth.CanonicalJSONSHA256(data)
}
