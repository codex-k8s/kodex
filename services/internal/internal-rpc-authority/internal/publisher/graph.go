package publisher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/snapshot"
)

const (
	authorizationAudience = "urn:mattercodex:internal-rpc:control-plane"
	proofAudience         = "urn:mattercodex:internal-rpc-authority-issuer:control-api-gateway"
)

// GraphConfig задаёт independently rooted входы полного publisher graph.
type GraphConfig struct {
	Registry                   model.DeliveryTargetRegistry
	Store                      repository.PublisherStore
	Vault                      repository.SecretDelivery
	Snapshot                   repository.SnapshotDelivery
	ManifestSigner             internalrpcauth.ES256Key
	ManifestSignerGeneration   uint64
	ManifestRootPublicJWKFile  string
	ManifestRootMetadataFile   string
	ManifestTrustBundleJWSFile string
	PolicyFile                 string
}

// Graph публикует auth/proof/manifest/snapshot как единую intent-цепочку.
type Graph struct {
	config GraphConfig
	now    func() time.Time
}

type rotatingKeySet struct {
	current            internalrpcauth.ES256Key
	next               internalrpcauth.ES256Key
	previous           *internalrpcauth.ES256Key
	currentGeneration  uint64
	nextGeneration     uint64
	previousGeneration uint64
}

// NewGraph создаёт полный publisher graph из закрытого registry.
func NewGraph(config GraphConfig) (*Graph, error) {
	if config.Registry.Version != model.ContractVersion ||
		config.Registry.SourceRevision == 0 ||
		!registryDigestPattern.MatchString(config.Registry.SourceDigest) ||
		len(config.Registry.Targets) < 3 ||
		config.Store == nil ||
		config.Vault == nil ||
		config.Snapshot == nil ||
		config.ManifestSigner.Private == nil ||
		config.ManifestSignerGeneration == 0 ||
		config.ManifestRootPublicJWKFile == "" ||
		config.ManifestRootMetadataFile == "" ||
		config.ManifestTrustBundleJWSFile == "" ||
		config.PolicyFile == "" {
		return nil, errors.New("authority publisher graph configuration is invalid")
	}
	return &Graph{config: config, now: time.Now}, nil
}

// Publish материализует полный graph и читает каждый опубликованный слой.
func (graph *Graph) Publish(
	ctx context.Context,
) (model.AuthoritySnapshotPublication, error) {
	if err := graph.ensureWritable(ctx); err != nil {
		return model.AuthoritySnapshotPublication{}, err
	}
	now := graph.now().UTC().Truncate(time.Second)
	manifestBundle, err := readGraphFile(
		graph.config.ManifestTrustBundleJWSFile,
		1<<20,
	)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"read independently signed manifest trust bundle",
		)
	}
	policyRaw, err := readGraphFile(graph.config.PolicyFile, 1<<20)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"read authority graph policy",
		)
	}
	if err := snapshot.VerifyPublisherManifestSigner(
		graph.config.ManifestSigner,
		graph.config.ManifestSignerGeneration,
		graph.config.ManifestRootPublicJWKFile,
		graph.config.ManifestRootMetadataFile,
		graph.config.ManifestTrustBundleJWSFile,
		now,
	); err != nil {
		return model.AuthoritySnapshotPublication{}, err
	}
	history, err := graph.config.Store.LoadSnapshotHistory(ctx)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"load durable authority snapshot history",
		)
	}
	inputDigest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		ManifestBundle string `json:"manifest_bundle"`
		Policy         string `json:"policy"`
		RegistryDigest string `json:"registry_digest_sha256"`
	}{
		ManifestBundle: string(manifestBundle),
		Policy:         string(policyRaw),
		RegistryDigest: graph.config.Registry.SourceDigest,
	})
	if err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"digest authority graph publication inputs",
		)
	}
	existing, found, err := graph.config.Store.LoadSnapshotPublication(
		ctx,
		graph.config.Registry.SourceRevision,
		inputDigest,
	)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"load durable same-input authority snapshot",
		)
	}
	historyForBuild := history.Current
	buildNow := now
	if found {
		if len(historyForBuild) == 0 ||
			historyForBuild[len(historyForBuild)-1].Revision !=
				existing.SourceRevision ||
			historyForBuild[len(historyForBuild)-1].DigestSHA256 !=
				existing.SourceDigestSHA256 {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"durable authority snapshot history readback rejected",
			)
		}
		historyForBuild = historyForBuild[:len(historyForBuild)-1]
		buildNow = existing.PublishedAt
	} else {
		if len(historyForBuild) == 0 &&
			graph.config.Registry.SourceRevision != 1 {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"authority graph bootstrap source revision is not one",
			)
		}
		if len(historyForBuild) > 0 {
			lastRevision := historyForBuild[len(historyForBuild)-1].Revision
			if lastRevision+1 != graph.config.Registry.SourceRevision {
				return model.AuthoritySnapshotPublication{}, errors.New(
					"authority graph source revision is not the next revision",
				)
			}
		}
	}
	var authorizationKeys []snapshot.PublisherKey
	var proofKeys []snapshot.PublisherKey
	for _, target := range graph.config.Registry.Targets {
		switch target.Role {
		case "AUTHORIZATION_ISSUER":
			keySet, ensureErr := graph.ensureKeySet(
				ctx,
				target.AuthPrivateKeyVaultPath,
				target.TargetID+"-auth",
			)
			if ensureErr != nil {
				return model.AuthoritySnapshotPublication{}, ensureErr
			}
			authorizationKeys = append(
				authorizationKeys,
				publisherKeys(
					target.WorkloadSPIFFEID,
					target.WorkloadID,
					"AUTHORIZATION_CONTEXT",
					[]string{authorizationAudience},
					keySet,
				)...,
			)
		case "AUTHORITY_PROOF_RESOLVER":
			keySet, ensureErr := graph.ensureKeySet(
				ctx,
				target.ProofPrivateKeyVaultPath,
				target.TargetID+"-proof",
			)
			if ensureErr != nil {
				return model.AuthoritySnapshotPublication{}, ensureErr
			}
			proofKeys = append(
				proofKeys,
				publisherKeys(
					target.WorkloadSPIFFEID,
					target.WorkloadID,
					"AUTHORITY_PROOF",
					[]string{proofAudience},
					keySet,
				)...,
			)
		}
	}
	if len(authorizationKeys) < 2 || len(proofKeys) < 2 {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"authority publisher key lifecycle is incomplete",
		)
	}
	built, err := snapshot.BuildForPublisher(snapshot.PublisherBuildOptions{
		ManifestSigner:             graph.config.ManifestSigner,
		ManifestSignerGeneration:   graph.config.ManifestSignerGeneration,
		ManifestRootPublicJWKFile:  graph.config.ManifestRootPublicJWKFile,
		ManifestRootMetadataFile:   graph.config.ManifestRootMetadataFile,
		ManifestTrustBundleJWSFile: graph.config.ManifestTrustBundleJWSFile,
		PolicyFile:                 graph.config.PolicyFile,
		SourceRevision:             graph.config.Registry.SourceRevision,
		KeySetRevision:             graph.config.Registry.SourceRevision,
		History:                    historyForBuild,
		AuthorizationKeys:          authorizationKeys,
		AuthorityProofKeys:         proofKeys,
		SourceRegistryDigestSHA256: graph.config.Registry.SourceDigest,
		Now:                        buildNow,
	})
	if err != nil {
		return model.AuthoritySnapshotPublication{}, err
	}
	predecessorRevision := uint64(0)
	predecessorDigest := strings.Repeat("0", 64)
	if len(historyForBuild) > 0 {
		last := historyForBuild[len(historyForBuild)-1]
		predecessorRevision = last.Revision
		predecessorDigest = last.DigestSHA256
	}
	publication := model.AuthoritySnapshotPublication{
		IntentID: deterministicUUID(
			"authority-snapshot",
			strconv.FormatUint(graph.config.Registry.SourceRevision, 10),
			graph.config.Registry.SourceDigest,
			built.SourceDigestSHA256,
		),
		InputDigestSHA256:       inputDigest,
		SourceRevision:          graph.config.Registry.SourceRevision,
		SourceDigestSHA256:      built.SourceDigestSHA256,
		KeySetRevision:          graph.config.Registry.SourceRevision,
		PolicyRevision:          built.PolicyRevision,
		SignerGeneration:        graph.config.ManifestSignerGeneration,
		PredecessorRevision:     predecessorRevision,
		PredecessorDigestSHA256: predecessorDigest,
		SnapshotCompactJWS:      built.SnapshotCompactJWS,
		PublishedAt:             buildNow,
	}
	persisted := publication
	if found {
		if existing.IntentID != publication.IntentID ||
			existing.InputDigestSHA256 != publication.InputDigestSHA256 ||
			existing.SourceDigestSHA256 != publication.SourceDigestSHA256 ||
			existing.PolicyRevision != publication.PolicyRevision ||
			existing.SignerGeneration != publication.SignerGeneration {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"durable authority snapshot publication mutation rejected",
			)
		}
		verifiedPayload, verifyErr := snapshot.VerifyPublisherSnapshotCompact(
			existing.SnapshotCompactJWS,
			graph.config.ManifestSigner.PublicOnly(),
		)
		if verifyErr != nil ||
			!bytes.Equal(verifiedPayload, built.SnapshotPayload) {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"durable authority snapshot compact readback rejected",
			)
		}
		persisted = existing
	} else {
		persisted, err = graph.config.Store.AppendSnapshot(
			ctx,
			publication,
			len(graph.config.Registry.Targets),
		)
		if err != nil {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"persist authority snapshot publication intent",
			)
		}
	}
	// Durable intent precedes external delivery so concurrent replicas and a
	// restart after partial delivery rebuild the exact signed publication.
	for _, target := range graph.config.Registry.Targets {
		if _, err := graph.putExact(
			ctx,
			target.ManifestTrustVaultPath,
			map[string]string{
				"manifest-trust.jws": string(manifestBundle),
				"source_revision": strconv.FormatUint(
					graph.config.Registry.SourceRevision,
					10,
				),
				"source_digest_sha256": graph.config.Registry.SourceDigest,
			},
		); err != nil {
			return model.AuthoritySnapshotPublication{}, err
		}
		if target.ProofTrustVaultPath != "" {
			if _, err := graph.putExact(
				ctx,
				target.ProofTrustVaultPath,
				map[string]string{
					"proof-trust.jwk": string(built.ProofTrustJSON),
					"source_revision": strconv.FormatUint(
						graph.config.Registry.SourceRevision,
						10,
					),
					"source_digest_sha256": graph.config.Registry.SourceDigest,
				},
			); err != nil {
				return model.AuthoritySnapshotPublication{}, err
			}
		}
	}
	served, err := graph.config.Snapshot.Publish(ctx, persisted)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, err
	}
	if served.SourceRevision != persisted.SourceRevision ||
		served.SourceDigestSHA256 != persisted.SourceDigestSHA256 ||
		served.SnapshotCompactJWS != persisted.SnapshotCompactJWS ||
		served.SnapshotResourceVersion == "" {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"authority snapshot served readback rejected",
		)
	}
	return served, nil
}

func (graph *Graph) ensureWritable(ctx context.Context) error {
	if err := graph.config.Store.PublisherReady(ctx); err != nil {
		return errors.New("authority publisher restore fence is active")
	}
	return nil
}

// Ready проверяет durable history, все role readbacks и фактически served Secret.
func (graph *Graph) Ready(
	ctx context.Context,
	expected model.AuthoritySnapshotPublication,
) error {
	now := graph.now().UTC()
	if expected.PublishedAt.IsZero() ||
		now.Before(expected.PublishedAt.Add(-5*time.Second)) ||
		!now.Before(expected.PublishedAt.Add(snapshot.PublisherSnapshotValidity)) {
		return errors.New("authority snapshot publication validity rejected")
	}
	served, err := graph.config.Snapshot.Read(ctx)
	if err != nil {
		return err
	}
	if served.SourceRevision != expected.SourceRevision ||
		served.SourceDigestSHA256 != expected.SourceDigestSHA256 ||
		served.SnapshotCompactJWS != expected.SnapshotCompactJWS ||
		served.SnapshotResourceVersion == "" {
		return errors.New("served authority snapshot no longer matches publication")
	}
	if err := graph.config.Store.SnapshotPublicationReady(
		ctx,
		expected,
		len(graph.config.Registry.Targets),
	); err != nil {
		return err
	}
	return graph.deliveryReady(ctx)
}

func (graph *Graph) deliveryReady(ctx context.Context) error {
	for _, target := range graph.config.Registry.Targets {
		for _, path := range []string{
			target.AuthPrivateKeyVaultPath,
			target.ManifestTrustVaultPath,
			target.ProofTrustVaultPath,
			target.ProofPrivateKeyVaultPath,
			target.ReadbackCredentialPath,
			target.ReadbackPossessionKeyPath,
		} {
			if path == "" {
				continue
			}
			material, found, err := graph.config.Vault.ReadKV2(ctx, path)
			if err != nil || !found || material.Version == 0 ||
				len(material.Digest) != 64 || len(material.Data) == 0 {
				return errors.New("authority graph delivery backend readback rejected")
			}
			if (path == target.ManifestTrustVaultPath || path == target.ProofTrustVaultPath) &&
				(material.Data["source_revision"] != strconv.FormatUint(graph.config.Registry.SourceRevision, 10) ||
					material.Data["source_digest_sha256"] != graph.config.Registry.SourceDigest) {
				return errors.New("authority graph delivery source binding rejected")
			}
		}
	}
	return nil
}

func (graph *Graph) ensureKeySet(
	ctx context.Context,
	path string,
	prefix string,
) (rotatingKeySet, error) {
	existing, found, err := graph.config.Vault.ReadKV2(ctx, path)
	if err != nil {
		return rotatingKeySet{}, errors.New("read authority signing key lifecycle")
	}
	if !found {
		current, keyErr := internalrpcauth.GenerateES256Key(prefix + "-g1")
		if keyErr != nil {
			return rotatingKeySet{}, errors.New("generate CURRENT authority signing key")
		}
		next, keyErr := internalrpcauth.GenerateES256Key(prefix + "-g2")
		if keyErr != nil {
			return rotatingKeySet{}, errors.New("generate NEXT authority signing key")
		}
		data, dataErr := graph.keySetData(current, next, nil, 1, 2, 0)
		if dataErr != nil {
			return rotatingKeySet{}, dataErr
		}
		existing, err = graph.config.Vault.CreateKV2(ctx, path, data)
		if err != nil {
			existing, found, err = graph.config.Vault.ReadKV2(ctx, path)
			if err != nil || !found {
				return rotatingKeySet{}, errors.New(
					"recover concurrent authority signing key creation",
				)
			}
		}
		return decodeRotatingKeySet(existing, graph.config.Registry)
	}
	keySet, err := decodeRotatingKeySet(existing, graph.config.Registry)
	if err == nil {
		return keySet, nil
	}
	storedRevision, revisionErr := strconv.ParseUint(
		existing.Data["source_revision"],
		10,
		64,
	)
	if revisionErr != nil ||
		storedRevision >= graph.config.Registry.SourceRevision {
		return rotatingKeySet{}, err
	}
	oldCurrent, parseErr := internalrpcauth.ParsePrivateJWK(
		[]byte(existing.Data["current_private_jwk"]),
	)
	if parseErr != nil {
		return rotatingKeySet{}, errors.New("parse previous CURRENT authority key")
	}
	oldNext, parseErr := internalrpcauth.ParsePrivateJWK(
		[]byte(existing.Data["next_private_jwk"]),
	)
	if parseErr != nil {
		return rotatingKeySet{}, errors.New("parse previous NEXT authority key")
	}
	oldNextGeneration, parseErr := strconv.ParseUint(
		existing.Data["next_generation"],
		10,
		64,
	)
	if parseErr != nil || oldNextGeneration < 2 {
		return rotatingKeySet{}, errors.New("parse previous NEXT generation")
	}
	next, keyErr := internalrpcauth.GenerateES256Key(
		prefix + "-g" + strconv.FormatUint(oldNextGeneration+1, 10),
	)
	if keyErr != nil {
		return rotatingKeySet{}, errors.New("generate rotated NEXT authority key")
	}
	data, dataErr := graph.keySetData(
		oldNext,
		next,
		&oldCurrent,
		oldNextGeneration,
		oldNextGeneration+1,
		oldNextGeneration-1,
	)
	if dataErr != nil {
		return rotatingKeySet{}, dataErr
	}
	updated, err := graph.config.Vault.WriteKV2CAS(
		ctx,
		path,
		existing.Version,
		data,
	)
	if err != nil {
		updated, found, err = graph.config.Vault.ReadKV2(ctx, path)
		if err != nil || !found {
			return rotatingKeySet{}, errors.New(
				"recover concurrent authority signing key rotation",
			)
		}
	}
	return decodeRotatingKeySet(updated, graph.config.Registry)
}

func (graph *Graph) keySetData(
	current internalrpcauth.ES256Key,
	next internalrpcauth.ES256Key,
	previous *internalrpcauth.ES256Key,
	currentGeneration uint64,
	nextGeneration uint64,
	previousGeneration uint64,
) (map[string]string, error) {
	currentPrivate, err := internalrpcauth.MarshalPrivateJWK(current)
	if err != nil {
		return nil, errors.New("encode CURRENT authority private key")
	}
	nextPrivate, err := internalrpcauth.MarshalPrivateJWK(next)
	if err != nil {
		return nil, errors.New("encode NEXT authority private key")
	}
	data := map[string]string{
		"private.jwk":         string(currentPrivate),
		"current_private_jwk": string(currentPrivate),
		"next_private_jwk":    string(nextPrivate),
		"current_generation":  strconv.FormatUint(currentGeneration, 10),
		"next_generation":     strconv.FormatUint(nextGeneration, 10),
		"source_revision": strconv.FormatUint(
			graph.config.Registry.SourceRevision,
			10,
		),
		"source_digest_sha256": graph.config.Registry.SourceDigest,
	}
	if previous != nil {
		previousPublic, marshalErr := internalrpcauth.MarshalPublicJWK(
			previous.PublicOnly(),
		)
		if marshalErr != nil {
			return nil, errors.New("encode PREVIOUS authority public key")
		}
		data["previous_public_jwk"] = string(previousPublic)
		data["previous_generation"] = strconv.FormatUint(previousGeneration, 10)
	}
	return data, nil
}

func decodeRotatingKeySet(
	material repository.SecretMaterial,
	registry model.DeliveryTargetRegistry,
) (rotatingKeySet, error) {
	sourceRevision, err := strconv.ParseUint(
		material.Data["source_revision"],
		10,
		64,
	)
	if err != nil ||
		sourceRevision != registry.SourceRevision ||
		material.Data["source_digest_sha256"] != registry.SourceDigest {
		return rotatingKeySet{}, errors.New("authority key lifecycle source mutation rejected")
	}
	current, err := internalrpcauth.ParsePrivateJWK(
		[]byte(material.Data["current_private_jwk"]),
	)
	if err != nil {
		return rotatingKeySet{}, errors.New("parse CURRENT authority key")
	}
	next, err := internalrpcauth.ParsePrivateJWK(
		[]byte(material.Data["next_private_jwk"]),
	)
	if err != nil || current.KeyID == next.KeyID {
		return rotatingKeySet{}, errors.New("parse distinct NEXT authority key")
	}
	currentGeneration, currentErr := strconv.ParseUint(
		material.Data["current_generation"],
		10,
		64,
	)
	nextGeneration, nextErr := strconv.ParseUint(
		material.Data["next_generation"],
		10,
		64,
	)
	if currentErr != nil ||
		nextErr != nil ||
		currentGeneration == 0 ||
		nextGeneration != currentGeneration+1 ||
		material.Data["private.jwk"] != material.Data["current_private_jwk"] {
		return rotatingKeySet{}, errors.New("authority key generation lifecycle rejected")
	}
	result := rotatingKeySet{
		current:           current,
		next:              next,
		currentGeneration: currentGeneration,
		nextGeneration:    nextGeneration,
	}
	if raw := material.Data["previous_public_jwk"]; raw != "" {
		previous, parseErr := internalrpcauth.ParsePublicJWK([]byte(raw))
		previousGeneration, generationErr := strconv.ParseUint(
			material.Data["previous_generation"],
			10,
			64,
		)
		if parseErr != nil ||
			generationErr != nil ||
			previousGeneration+1 != currentGeneration {
			return rotatingKeySet{}, errors.New("PREVIOUS authority key lifecycle rejected")
		}
		result.previous = &previous
		result.previousGeneration = previousGeneration
	}
	return result, nil
}

func publisherKeys(
	issuer string,
	workloadID string,
	purpose string,
	audiences []string,
	keys rotatingKeySet,
) []snapshot.PublisherKey {
	result := []snapshot.PublisherKey{
		{
			Issuer: issuer, WorkloadID: workloadID, Status: "CURRENT",
			Generation: keys.currentGeneration, Purpose: purpose,
			Audiences: audiences, Key: keys.current.PublicOnly(),
		},
		{
			Issuer: issuer, WorkloadID: workloadID, Status: "NEXT",
			Generation: keys.nextGeneration, Purpose: purpose,
			Audiences: audiences, Key: keys.next.PublicOnly(),
		},
	}
	if keys.previous != nil {
		result = append(result, snapshot.PublisherKey{
			Issuer: issuer, WorkloadID: workloadID, Status: "PREVIOUS",
			Generation: keys.previousGeneration, Purpose: purpose,
			Audiences: audiences, Key: keys.previous.PublicOnly(),
		})
	}
	return result
}

func (graph *Graph) putExact(
	ctx context.Context,
	path string,
	data map[string]string,
) (repository.SecretMaterial, error) {
	existing, found, err := graph.config.Vault.ReadKV2(ctx, path)
	if err != nil {
		return repository.SecretMaterial{}, errors.New("read authority graph delivery")
	}
	if found {
		if existing.Data["source_revision"] == data["source_revision"] &&
			existing.Data["source_digest_sha256"] == data["source_digest_sha256"] {
			if !sameExactMaterial(existing.Data, data) {
				desiredDigest, digestErr := internalrpcauth.CanonicalJSONSHA256(data)
				if digestErr != nil {
					return repository.SecretMaterial{}, errors.New(
						"digest rejected same-revision authority graph delivery",
					)
				}
				return repository.SecretMaterial{}, fmt.Errorf(
					"same-revision authority graph delivery mutation rejected for %q: stored %s, desired %s",
					path,
					existing.Digest,
					desiredDigest,
				)
			}
			return existing, nil
		}
		storedRevision, parseErr := strconv.ParseUint(
			existing.Data["source_revision"],
			10,
			64,
		)
		nextRevision, nextErr := strconv.ParseUint(
			data["source_revision"],
			10,
			64,
		)
		if parseErr != nil || nextErr != nil || nextRevision <= storedRevision {
			return repository.SecretMaterial{}, errors.New(
				"authority graph delivery rollback rejected",
			)
		}
		updated, updateErr := graph.config.Vault.WriteKV2CAS(
			ctx,
			path,
			existing.Version,
			data,
		)
		if updateErr != nil {
			updated, found, updateErr = graph.config.Vault.ReadKV2(ctx, path)
			if updateErr != nil || !found || !sameExactMaterial(updated.Data, data) {
				return repository.SecretMaterial{}, errors.New(
					"recover concurrent authority graph delivery",
				)
			}
		}
		if !sameExactMaterial(updated.Data, data) {
			return repository.SecretMaterial{}, errors.New(
				"rotated authority graph delivery readback rejected",
			)
		}
		return updated, nil
	}
	created, createErr := graph.config.Vault.CreateKV2(ctx, path, data)
	if createErr != nil {
		created, found, createErr = graph.config.Vault.ReadKV2(ctx, path)
		if createErr != nil || !found || !sameExactMaterial(created.Data, data) {
			return repository.SecretMaterial{}, errors.New(
				"recover concurrent authority graph creation",
			)
		}
	}
	if !sameExactMaterial(created.Data, data) {
		return repository.SecretMaterial{}, errors.New(
			"created authority graph delivery readback rejected",
		)
	}
	return created, nil
}

func sameExactMaterial(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range right {
		if left[key] != value {
			return false
		}
	}
	return true
}

func readGraphFile(path string, limit int64) ([]byte, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(filepath.Dir(path), resolved)
	if err != nil ||
		relative == ".." ||
		filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, "../") {
		return nil, errors.New("authority graph file escapes its mounted directory")
	}
	info, err := os.Stat(resolved)
	if err != nil ||
		!info.Mode().IsRegular() ||
		info.Size() <= 0 ||
		info.Size() > limit {
		return nil, errors.New("authority graph file is unsafe")
	}
	return os.ReadFile(resolved)
}

func deterministicUUID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	value := digest[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32]
}

var _ repository.AuthorityGraphLifecycle = (*Graph)(nil)
