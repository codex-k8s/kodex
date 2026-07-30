package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/snapshot"
)

const testZeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

func TestGraphПубликуетПолныйProductionRegistryИForwardRotation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	registry, err := LoadRegistry(productionRegistryPath(t))
	if err != nil {
		t.Fatal(err)
	}
	files, signer := writeManifestTrustFixture(t, now)
	store := &graphMemoryStore{}
	vault := &graphMemoryVault{values: make(map[string]repository.SecretMaterial)}
	served := &graphMemorySnapshot{}
	graph, err := NewGraph(GraphConfig{
		Registry: registry, Store: store, Vault: vault, Snapshot: served,
		ManifestSigner: signer, ManifestSignerGeneration: 1,
		ManifestRootPublicJWKFile:  files.root,
		ManifestRootMetadataFile:   files.metadata,
		ManifestTrustBundleJWSFile: files.bundle,
		PolicyFile:                 productionPolicyPath(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	graph.now = func() time.Time { return now }

	first, err := graph.Publish(ctx)
	if err != nil {
		t.Fatalf("publish bootstrap graph: %v", err)
	}
	if first.SourceRevision != 1 || first.SnapshotResourceVersion == "" {
		t.Fatalf("bootstrap publication is incomplete: %#v", first)
	}
	assertProductionDelivery(t, registry, vault)
	assertIssuerCanLoadPublishedGraph(t, files, first, registry, vault, now)
	assertResolverCanReadBackPublishedGraph(t, first, registry, vault, now)
	if err := graph.Ready(ctx, first); err == nil {
		t.Fatal("publisher became ready before the complete role readback set")
	}
	store.ready = true
	if err := graph.Ready(ctx, first); err != nil {
		t.Fatalf("complete role readback set rejected: %v", err)
	}
	graph.now = func() time.Time {
		return now.Add(snapshot.PublisherSnapshotValidity)
	}
	if err := graph.Ready(ctx, first); err == nil {
		t.Fatal("expired authority snapshot publication remained ready")
	}
	graph.now = func() time.Time { return now }
	retried, err := graph.Publish(ctx)
	if err != nil {
		t.Fatalf("same-input retry failed: %v", err)
	}
	if retried.IntentID != first.IntentID ||
		retried.SourceDigestSHA256 != first.SourceDigestSHA256 ||
		retried.SnapshotCompactJWS != first.SnapshotCompactJWS {
		t.Fatal("same-input retry changed the immutable publication")
	}

	nextRegistry := registry
	resolver := nextRegistry.Targets["control-plane.authority-proof-resolver"]
	beforeGap := vault.mustRead(t, resolver.ProofPrivateKeyVaultPath)
	gappedRegistry := registry
	gappedRegistry.SourceRevision = 3
	gappedRegistry.SourceDigest = repeatedGraphDigest("3")
	gapped, err := NewGraph(GraphConfig{
		Registry: gappedRegistry, Store: store, Vault: vault, Snapshot: served,
		ManifestSigner: signer, ManifestSignerGeneration: 1,
		ManifestRootPublicJWKFile:  files.root,
		ManifestRootMetadataFile:   files.metadata,
		ManifestTrustBundleJWSFile: files.bundle,
		PolicyFile:                 productionPolicyPath(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gapped.Publish(ctx); err == nil {
		t.Fatal("gapped source revision was accepted")
	}
	afterGap := vault.mustRead(t, resolver.ProofPrivateKeyVaultPath)
	if afterGap.Version != beforeGap.Version {
		t.Fatal("rejected source-revision gap mutated the key lifecycle")
	}

	nextRegistry.SourceRevision = 2
	nextRegistry.SourceDigest = repeatedGraphDigest("2")
	next, err := NewGraph(GraphConfig{
		Registry: nextRegistry, Store: store, Vault: vault, Snapshot: served,
		ManifestSigner: signer, ManifestSignerGeneration: 1,
		ManifestRootPublicJWKFile:  files.root,
		ManifestRootMetadataFile:   files.metadata,
		ManifestTrustBundleJWSFile: files.bundle,
		PolicyFile:                 productionPolicyPath(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	next.now = func() time.Time { return now.Add(time.Minute) }
	second, err := next.Publish(ctx)
	if err != nil {
		t.Fatalf("publish forward rotation: %v", err)
	}
	if second.SourceRevision != 2 ||
		second.PredecessorRevision != first.SourceRevision ||
		second.PredecessorDigestSHA256 != first.SourceDigestSHA256 {
		t.Fatalf("forward predecessor chain is incomplete: %#v", second)
	}
	material := vault.mustRead(t, resolver.ProofPrivateKeyVaultPath)
	if material.Data["previous_public_jwk"] == "" ||
		material.Data["current_generation"] != "2" ||
		material.Data["next_generation"] != "3" {
		t.Fatalf("CURRENT/NEXT/PREVIOUS lifecycle is incomplete: %#v", material.Data)
	}
}

func TestGraphОтклоняетSameRevisionMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	registry, err := LoadRegistry(productionRegistryPath(t))
	if err != nil {
		t.Fatal(err)
	}
	files, signer := writeManifestTrustFixture(t, now)
	store := &graphMemoryStore{}
	vault := &graphMemoryVault{values: make(map[string]repository.SecretMaterial)}
	served := &graphMemorySnapshot{}
	graph, err := NewGraph(GraphConfig{
		Registry: registry, Store: store, Vault: vault, Snapshot: served,
		ManifestSigner: signer, ManifestSignerGeneration: 1,
		ManifestRootPublicJWKFile:  files.root,
		ManifestRootMetadataFile:   files.metadata,
		ManifestTrustBundleJWSFile: files.bundle,
		PolicyFile:                 productionPolicyPath(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	graph.now = func() time.Time { return now }
	if _, err := graph.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	mutated := registry
	mutated.SourceDigest = repeatedGraphDigest("f")
	conflicting, err := NewGraph(GraphConfig{
		Registry: mutated, Store: store, Vault: vault, Snapshot: served,
		ManifestSigner: signer, ManifestSignerGeneration: 1,
		ManifestRootPublicJWKFile:  files.root,
		ManifestRootMetadataFile:   files.metadata,
		ManifestTrustBundleJWSFile: files.bundle,
		PolicyFile:                 productionPolicyPath(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	conflicting.now = func() time.Time { return now }
	if _, err := conflicting.Publish(ctx); err == nil {
		t.Fatal("same-revision registry mutation was accepted")
	}
}

func TestGraphОтклоняетНеукоренённыйManifestSignerДоВнешнейМутации(
	t *testing.T,
) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	registry, err := LoadRegistry(productionRegistryPath(t))
	if err != nil {
		t.Fatal(err)
	}
	files, _ := writeManifestTrustFixture(t, now)
	substituteSigner, err := internalrpcauth.GenerateES256Key(
		"untrusted-manifest-signer-g1",
	)
	if err != nil {
		t.Fatal(err)
	}
	store := &graphMemoryStore{}
	vault := &graphMemoryVault{values: make(map[string]repository.SecretMaterial)}
	graph, err := NewGraph(GraphConfig{
		Registry: registry, Store: store, Vault: vault,
		Snapshot:       &graphMemorySnapshot{},
		ManifestSigner: substituteSigner, ManifestSignerGeneration: 1,
		ManifestRootPublicJWKFile:  files.root,
		ManifestRootMetadataFile:   files.metadata,
		ManifestTrustBundleJWSFile: files.bundle,
		PolicyFile:                 productionPolicyPath(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	graph.now = func() time.Time { return now }

	if _, err := graph.Publish(ctx); err == nil {
		t.Fatal("unrooted manifest signer was accepted")
	}
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if len(vault.values) != 0 {
		t.Fatal("rejected manifest signer mutated Vault")
	}
	if len(store.publications) != 0 || len(store.history) != 0 {
		t.Fatal("rejected manifest signer mutated durable publication state")
	}
}

func TestGraphОтклоняетRestoreFenceДоВнешнейМутации(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	registry, err := LoadRegistry(productionRegistryPath(t))
	if err != nil {
		t.Fatal(err)
	}
	files, signer := writeManifestTrustFixture(t, now)
	store := &graphMemoryStore{publisherReadyErr: errors.New("restore fence")}
	vault := &graphMemoryVault{values: make(map[string]repository.SecretMaterial)}
	graph, err := NewGraph(GraphConfig{
		Registry: registry, Store: store, Vault: vault,
		Snapshot:       &graphMemorySnapshot{},
		ManifestSigner: signer, ManifestSignerGeneration: 1,
		ManifestRootPublicJWKFile:  files.root,
		ManifestRootMetadataFile:   files.metadata,
		ManifestTrustBundleJWSFile: files.bundle,
		PolicyFile:                 productionPolicyPath(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Publish(ctx); err == nil {
		t.Fatal("restore fence allowed publisher traffic")
	}
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if len(vault.values) != 0 {
		t.Fatal("restore fence rejection mutated Vault")
	}
}

func TestGraphConcurrentBootstrapВозвращаетPersistedPublication(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	registry, err := LoadRegistry(productionRegistryPath(t))
	if err != nil {
		t.Fatal(err)
	}
	files, signer := writeManifestTrustFixture(t, now)
	store := &graphMemoryStore{}
	vault := &graphMemoryVault{values: make(map[string]repository.SecretMaterial)}
	served := &graphMemorySnapshot{}
	graph, err := NewGraph(GraphConfig{
		Registry: registry, Store: store, Vault: vault, Snapshot: served,
		ManifestSigner: signer, ManifestSignerGeneration: 1,
		ManifestRootPublicJWKFile:  files.root,
		ManifestRootMetadataFile:   files.metadata,
		ManifestTrustBundleJWSFile: files.bundle,
		PolicyFile:                 productionPolicyPath(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	graph.now = func() time.Time { return now }
	const replicas = 8
	results := make(chan model.AuthoritySnapshotPublication, replicas)
	failures := make(chan error, replicas)
	var group sync.WaitGroup
	for replica := 0; replica < replicas; replica++ {
		group.Add(1)
		go func() {
			defer group.Done()
			value, publishErr := graph.Publish(ctx)
			if publishErr != nil {
				failures <- publishErr
				return
			}
			results <- value
		}()
	}
	group.Wait()
	close(results)
	close(failures)
	for publishErr := range failures {
		t.Fatalf("concurrent bootstrap failed: %v", publishErr)
	}
	var expected model.AuthoritySnapshotPublication
	for value := range results {
		if expected.IntentID == "" {
			expected = value
			continue
		}
		if value.IntentID != expected.IntentID ||
			value.SourceDigestSHA256 != expected.SourceDigestSHA256 ||
			value.SnapshotCompactJWS != expected.SnapshotCompactJWS {
			t.Fatal("concurrent replica did not return the persisted publication")
		}
	}
}

type manifestFixtureFiles struct {
	root     string
	metadata string
	bundle   string
}

func writeManifestTrustFixture(
	t *testing.T,
	now time.Time,
) (manifestFixtureFiles, internalrpcauth.ES256Key) {
	t.Helper()
	root, err := internalrpcauth.GenerateES256Key("test-manifest-root-g1")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := internalrpcauth.GenerateES256Key("test-manifest-signer-g1")
	if err != nil {
		t.Fatal(err)
	}
	nextSigner, err := internalrpcauth.GenerateES256Key(
		"test-manifest-signer-g2",
	)
	if err != nil {
		t.Fatal(err)
	}
	rootPublic, err := internalrpcauth.MarshalPublicJWK(root.PublicOnly())
	if err != nil {
		t.Fatal(err)
	}
	signerPublic, err := internalrpcauth.MarshalPublicJWK(signer.PublicOnly())
	if err != nil {
		t.Fatal(err)
	}
	nextSignerPublic, err := internalrpcauth.MarshalPublicJWK(
		nextSigner.PublicOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	rootThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(root.PublicOnly())
	if err != nil {
		t.Fatal(err)
	}
	nextSignerThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(
		nextSigner.PublicOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	signerThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(
		signer.PublicOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	metadata := struct {
		Version        int    `json:"v"`
		RootID         string `json:"root_id"`
		RootGeneration uint64 `json:"root_generation"`
		Purpose        string `json:"purpose"`
		Audience       string `json:"aud"`
		KeyID          string `json:"kid"`
		JWKThumbprint  string `json:"jwk_thumbprint_sha256"`
		SourceRevision uint64 `json:"source_revision"`
		SourceDigest   string `json:"source_digest_sha256"`
		NotBefore      int64  `json:"not_before"`
		NotAfter       int64  `json:"not_after"`
	}{
		Version: 1, RootID: "internal-rpc-authority-manifest-root-v1",
		RootGeneration: 1, Purpose: "AUTHORITY_SNAPSHOT_MANIFEST_ROOT",
		Audience: "urn:mattercodex:internal-rpc-authority:manifest-root",
		KeyID:    root.KeyID, JWKThumbprint: rootThumbprint, SourceRevision: 1,
		SourceDigest: repeatedGraphDigest("a"),
		NotBefore:    now.Add(-time.Hour).Unix(), NotAfter: now.Add(48 * time.Hour).Unix(),
	}
	bundle := struct {
		Version        int              `json:"v"`
		RootID         string           `json:"root_id"`
		RootGeneration uint64           `json:"root_generation"`
		Purpose        string           `json:"purpose"`
		Audience       string           `json:"aud"`
		BundleRevision uint64           `json:"bundle_revision"`
		BundleDigest   string           `json:"bundle_digest_sha256"`
		Predecessor    map[string]any   `json:"predecessor"`
		History        []map[string]any `json:"history"`
		Keys           []map[string]any `json:"keys"`
		PublishedAt    int64            `json:"published_at"`
		ValidUntil     int64            `json:"valid_until"`
	}{
		Version: 1, RootID: metadata.RootID, RootGeneration: 1,
		Purpose:        "AUTHORITY_SNAPSHOT_MANIFEST_VERIFICATION",
		Audience:       "urn:mattercodex:internal-rpc-authority:manifest-bundle",
		BundleRevision: 1, BundleDigest: repeatedGraphDigest("b"),
		Predecessor: map[string]any{
			"revision": uint64(0), "digest_sha256": testZeroDigest,
		},
		History: []map[string]any{},
		Keys: []map[string]any{
			{
				"status": "CURRENT", "generation": uint64(1),
				"kid": signer.KeyID, "public_jwk": json.RawMessage(signerPublic),
				"jwk_thumbprint_sha256": signerThumbprint,
				"not_before":            now.Add(-time.Hour).Unix(),
				"not_after":             now.Add(24 * time.Hour).Unix(),
			},
			{
				"status": "NEXT", "generation": uint64(2),
				"kid":                   nextSigner.KeyID,
				"public_jwk":            json.RawMessage(nextSignerPublic),
				"jwk_thumbprint_sha256": nextSignerThumbprint,
				"not_before":            now.Add(-time.Hour).Unix(),
				"not_after":             now.Add(48 * time.Hour).Unix(),
			},
		},
		PublishedAt: now.Unix(), ValidUntil: now.Add(24 * time.Hour).Unix(),
	}
	metadataRaw, err := internalrpcauth.CanonicalJSON(metadata)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := internalrpcauth.SignCanonicalJSON(
		bundle,
		root,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  "mattercodex-internal-rpc-manifest-trust+jws",
			KeyID: root.KeyID,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	files := manifestFixtureFiles{
		root:     filepath.Join(directory, "root.jwk"),
		metadata: filepath.Join(directory, "root-metadata.json"),
		bundle:   filepath.Join(directory, "manifest-trust.jws"),
	}
	if err := os.WriteFile(files.root, rootPublic, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files.metadata, metadataRaw, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files.bundle, []byte(compact), 0o440); err != nil {
		t.Fatal(err)
	}
	return files, signer
}

func assertProductionDelivery(
	t *testing.T,
	registry model.DeliveryTargetRegistry,
	vault *graphMemoryVault,
) {
	t.Helper()
	issuer := registry.Targets["control-api-gateway.authorization-issuer"]
	resolver := registry.Targets["control-plane.authority-proof-resolver"]
	verifier := registry.Targets["control-plane.authorization-verifier"]
	for _, path := range []string{
		issuer.AuthPrivateKeyVaultPath,
		resolver.ProofPrivateKeyVaultPath,
		issuer.ManifestTrustVaultPath,
		verifier.ManifestTrustVaultPath,
		resolver.ManifestTrustVaultPath,
		issuer.ProofTrustVaultPath,
		resolver.ProofTrustVaultPath,
	} {
		if _, found, _ := vault.ReadKV2(context.Background(), path); !found {
			t.Fatalf("production delivery path %q was not materialized", path)
		}
	}
}

func assertIssuerCanLoadPublishedGraph(
	t *testing.T,
	files manifestFixtureFiles,
	publication model.AuthoritySnapshotPublication,
	registry model.DeliveryTargetRegistry,
	vault *graphMemoryVault,
	now time.Time,
) {
	t.Helper()
	issuer := registry.Targets["control-api-gateway.authorization-issuer"]
	snapshotFile := filepath.Join(t.TempDir(), "snapshot.jws")
	authFile := filepath.Join(t.TempDir(), "private.jwk")
	proofFile := filepath.Join(t.TempDir(), "proof-trust.json")
	manifestFile := filepath.Join(t.TempDir(), "manifest-trust.jws")
	writeGraphFile(t, snapshotFile, publication.SnapshotCompactJWS, 0o440)
	writeGraphFile(
		t,
		authFile,
		vault.mustRead(t, issuer.AuthPrivateKeyVaultPath).Data["private.jwk"],
		0o440,
	)
	writeGraphFile(
		t,
		proofFile,
		vault.mustRead(t, issuer.ProofTrustVaultPath).Data["proof-trust.jwk"],
		0o440,
	)
	writeGraphFile(
		t,
		manifestFile,
		vault.mustRead(t, issuer.ManifestTrustVaultPath).Data["manifest-trust.jws"],
		0o440,
	)
	if _, err := snapshot.Load(snapshot.LoadOptions{
		Role: snapshot.RoleIssuer, WorkloadID: issuer.WorkloadID,
		SnapshotJWSFile:            snapshotFile,
		ManifestRootPublicJWKFile:  files.root,
		ManifestRootMetadataFile:   files.metadata,
		ManifestTrustBundleJWSFile: manifestFile,
		ContextPrivateJWKFile:      authFile,
		ProofTrustJWKFile:          proofFile,
		Now:                        now,
	}); err != nil {
		t.Fatalf("issuer could not load publisher output: %v", err)
	}
}

func assertResolverCanReadBackPublishedGraph(
	t *testing.T,
	publication model.AuthoritySnapshotPublication,
	registry model.DeliveryTargetRegistry,
	vault *graphMemoryVault,
	now time.Time,
) {
	t.Helper()
	resolver := registry.Targets["control-plane.authority-proof-resolver"]
	privateFile := filepath.Join(t.TempDir(), "private.jwk")
	trustFile := filepath.Join(t.TempDir(), "proof-trust.json")
	writeGraphFile(
		t,
		privateFile,
		vault.mustRead(t, resolver.ProofPrivateKeyVaultPath).Data["private.jwk"],
		0o440,
	)
	writeGraphFile(
		t,
		trustFile,
		vault.mustRead(t, resolver.ProofTrustVaultPath).Data["proof-trust.jwk"],
		0o440,
	)
	if err := snapshot.VerifyProofSigner(
		privateFile,
		trustFile,
		resolver.WorkloadSPIFFEID,
		proofAudience,
		publication.SourceRevision,
		publication.SourceDigestSHA256,
		1,
		now,
	); err != nil {
		t.Fatalf("resolver could not read back publisher output: %v", err)
	}
}

func writeGraphFile(t *testing.T, path string, value string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatal(err)
	}
}

func productionPolicyPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(
		filepath.Dir(productionRegistryPath(t)),
		"authority-policy.json",
	)
}

func repeatedGraphDigest(value string) string {
	result := ""
	for len(result) < 64 {
		result += value
	}
	return result[:64]
}

type graphMemoryStore struct {
	mu                sync.Mutex
	history           []model.RevisionDigest
	publications      map[uint64]model.AuthoritySnapshotPublication
	ready             bool
	publisherReadyErr error
}

func (store *graphMemoryStore) LoadSnapshotHistory(
	context.Context,
) (model.AuthoritySnapshotHistory, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return model.AuthoritySnapshotHistory{
		Current: append([]model.RevisionDigest(nil), store.history...),
	}, nil
}

func (store *graphMemoryStore) LoadSnapshotPublication(
	_ context.Context,
	revision uint64,
	inputDigest string,
) (model.AuthoritySnapshotPublication, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.publications[revision]
	if !found || value.InputDigestSHA256 != inputDigest {
		return model.AuthoritySnapshotPublication{}, false, nil
	}
	return value, true, nil
}

func (store *graphMemoryStore) AppendSnapshot(
	_ context.Context,
	value model.AuthoritySnapshotPublication,
	_ int,
) (model.AuthoritySnapshotPublication, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.publications == nil {
		store.publications = make(map[uint64]model.AuthoritySnapshotPublication)
	}
	if existing, found := store.publications[value.SourceRevision]; found {
		if existing.InputDigestSHA256 != value.InputDigestSHA256 {
			return model.AuthoritySnapshotPublication{}, repository.ErrSnapshotRollback
		}
		return existing, nil
	}
	if value.SourceRevision != uint64(len(store.history)+1) {
		return model.AuthoritySnapshotPublication{}, repository.ErrSnapshotRollback
	}
	store.publications[value.SourceRevision] = value
	store.history = append(store.history, model.RevisionDigest{
		Revision: value.SourceRevision, DigestSHA256: value.SourceDigestSHA256,
	})
	return value, nil
}

func (store *graphMemoryStore) SnapshotPublicationReady(
	context.Context,
	model.AuthoritySnapshotPublication,
	int,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.ready {
		return errors.New("readback set is incomplete")
	}
	return nil
}

func (*graphMemoryStore) LoadPublishedCredential(
	context.Context,
	string,
) (model.PublishedCredential, bool, error) {
	return model.PublishedCredential{}, false, nil
}

func (*graphMemoryStore) SavePublishedCredential(
	_ context.Context,
	value model.PublishedCredential,
) (model.PublishedCredential, error) {
	return value, nil
}

func (*graphMemoryStore) PinReadbackIntent(
	_ context.Context,
	value model.ReadbackIntent,
) (model.ReadbackIntent, error) {
	return value, nil
}

func (store *graphMemoryStore) PublisherReady(context.Context) error {
	return store.publisherReadyErr
}

type graphMemoryVault struct {
	mu     sync.Mutex
	values map[string]repository.SecretMaterial
}

func (vault *graphMemoryVault) ReadKV2(
	_ context.Context,
	path string,
) (repository.SecretMaterial, bool, error) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	value, found := vault.values[path]
	return value, found, nil
}

func (vault *graphMemoryVault) CreateKV2(
	_ context.Context,
	path string,
	data map[string]string,
) (repository.SecretMaterial, error) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if _, found := vault.values[path]; found {
		return repository.SecretMaterial{}, repository.ErrIdempotencyConflict
	}
	value := repository.SecretMaterial{
		Version: 1, Data: cloneGraphData(data),
	}
	vault.values[path] = value
	return value, nil
}

func (vault *graphMemoryVault) WriteKV2CAS(
	_ context.Context,
	path string,
	expectedVersion uint64,
	data map[string]string,
) (repository.SecretMaterial, error) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	existing, found := vault.values[path]
	if !found || existing.Version != expectedVersion {
		return repository.SecretMaterial{}, repository.ErrIdempotencyConflict
	}
	value := repository.SecretMaterial{
		Version: expectedVersion + 1, Data: cloneGraphData(data),
	}
	vault.values[path] = value
	return value, nil
}

func (vault *graphMemoryVault) mustRead(
	t *testing.T,
	path string,
) repository.SecretMaterial {
	t.Helper()
	value, found, err := vault.ReadKV2(context.Background(), path)
	if err != nil || !found {
		t.Fatalf("read %q: found=%v err=%v", path, found, err)
	}
	return value
}

func cloneGraphData(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

type graphMemorySnapshot struct {
	mu      sync.Mutex
	value   model.AuthoritySnapshotPublication
	version uint64
}

func (delivery *graphMemorySnapshot) Publish(
	_ context.Context,
	value model.AuthoritySnapshotPublication,
) (model.AuthoritySnapshotPublication, error) {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	if delivery.value.SourceRevision > value.SourceRevision ||
		delivery.value.SourceRevision == value.SourceRevision &&
			delivery.value.SourceDigestSHA256 != "" &&
			delivery.value.SourceDigestSHA256 != value.SourceDigestSHA256 {
		return model.AuthoritySnapshotPublication{}, repository.ErrSnapshotRollback
	}
	if delivery.value.SourceRevision != value.SourceRevision {
		delivery.version++
	}
	value.SnapshotResourceVersion = repeatedGraphDigest("a")[:8] +
		repeatedGraphDigest(string(rune('0' + delivery.version)))[:8]
	delivery.value = value
	return value, nil
}

func (delivery *graphMemorySnapshot) Read(
	context.Context,
) (model.AuthoritySnapshotPublication, error) {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	return delivery.value, nil
}

func (*graphMemorySnapshot) Close() {}

var _ repository.PublisherStore = (*graphMemoryStore)(nil)
var _ repository.SecretDelivery = (*graphMemoryVault)(nil)
var _ repository.SnapshotDelivery = (*graphMemorySnapshot)(nil)
