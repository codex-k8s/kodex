package publisher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
)

// GraphConfig binds the closed delivery registry, durable store and delivery ports to independently
// rooted manifest signing/trust inputs and policy; private key material must not be logged.
// GraphConfig связывает закрытый реестр, устойчивое хранилище и порты доставки с независимо укоренёнными
// материалами подписи и доверия манифеста и политикой; закрытый ключ нельзя журналировать.
type GraphConfig struct {
	Registry                   model.DeliveryTargetRegistry
	Store                      repository.PublisherStore
	Secrets                    repository.SecretDelivery
	Snapshot                   repository.SnapshotDelivery
	ManifestSigner             internalrpcauth.ES256Key
	ManifestSignerGeneration   uint64
	ManifestRootPublicJWKFile  string
	ManifestRootMetadataFile   string
	ManifestTrustBundleJWSFile string
	PolicyFile                 string
}

// Graph coordinates durable publication intents, key delivery and phase-bound rotation; its mutable
// phase fields are per-graph execution inputs, not a new owner of persistent state.
// Graph координирует устойчивые намерения публикации, доставку ключей и фазовую ротацию; изменяемые поля
// фаз относятся к выполнению графа и не заменяют владельца устойчивого состояния.
type Graph struct {
	config            GraphConfig
	now               func() time.Time
	rotationPhase     string
	rotationOperation model.AuthorityRotationOperation
}

// rotatingKeySet holds current/next private signing keys with an optional previous public key and their
// linked generations; the struct is sensitive even when previous is public-only.
// rotatingKeySet содержит текущий и следующий закрытые ключи, необязательный предыдущий открытый ключ и
// связанные поколения; структура конфиденциальна, хотя предыдущий ключ только открытый.
type rotatingKeySet struct {
	current            internalrpcauth.ES256Key
	next               internalrpcauth.ES256Key
	previous           *internalrpcauth.ES256Key
	currentGeneration  uint64
	nextGeneration     uint64
	previousGeneration uint64
}

// maximumSnapshotHistoryEntries bounds the predecessor window included when rebuilding a publication; it
// does not truncate the store's durable history.
// maximumSnapshotHistoryEntries ограничивает окно предшественников при пересборке публикации, но не
// обрезает устойчивую историю хранилища.
const maximumSnapshotHistoryEntries = 32

// NewGraph validates required registry shape, startup targets, ports and manifest inputs before
// constructing an idle graph with the wall clock; it performs no publication or key generation.
// NewGraph проверяет форму реестра, обязательные цели запуска, порты и материалы манифеста, затем
// создаёт неактивный граф с системными часами; публикации и генерации ключей здесь нет.
func NewGraph(config GraphConfig) (*Graph, error) {
	if config.Registry.Version != model.ContractVersion ||
		config.Registry.SourceRevision == 0 ||
		!registryDigestPattern.MatchString(config.Registry.SourceDigest) ||
		len(config.Registry.Targets) < 3 ||
		config.Registry.StartupReadbackTargetCount() == 0 ||
		config.Store == nil ||
		config.Secrets == nil ||
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

// Publish resolves durable history and rotation state, selects the existing phase or reuses a stored
// waiting-phase publication, and otherwise executes one phase without skipping readiness-driven
// advancement.
// Publish разрешает устойчивую историю и состояние ротации, выбирает фазу либо возвращает сохранённую
// публикацию ожидающей фазы, иначе выполняет одну фазу без пропуска продвижения по готовности.
func (graph *Graph) Publish(
	ctx context.Context,
) (model.AuthoritySnapshotPublication, error) {
	history, err := graph.config.Store.LoadSnapshotHistory(ctx)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New("load durable authority snapshot history")
	}
	if len(history.Current) == 0 {
		return graph.publishOne(ctx)
	}
	operationID := graph.rotationOperationID()
	operation, found, err := graph.config.Store.LoadRotationOperation(ctx, operationID)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New("load durable authority rotation operation")
	}
	latest := history.Current[len(history.Current)-1]
	if !found && latest.Revision == graph.config.Registry.SourceRevision {
		return graph.publishOne(ctx)
	}
	if !found {
		operation = model.AuthorityRotationOperation{
			OperationID:          operationID,
			RegistryRevision:     graph.config.Registry.SourceRevision,
			RegistryDigestSHA256: graph.config.Registry.SourceDigest,
			BaseRevision:         latest.Revision, BaseDigestSHA256: latest.DigestSHA256,
			ExpectedReadbackCount: graph.config.Registry.StartupReadbackTargetCount(),
		}
	}
	operation, err = graph.config.Store.LoadOrPrepareRotationOperation(ctx, operation)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New("load durable authority rotation operation")
	}
	phase, offset := "", uint64(0)
	switch operation.Status {
	case "DISTRIBUTING":
		phase, offset = "DISTRIBUTE", 1
	case "WAITING_SWITCH":
		phase, offset = "DISTRIBUTE", 1
	case "SWITCHING":
		phase, offset = "SWITCH", 2
	case "WAITING_RETIRE":
		phase, offset = "SWITCH", 2
	case "RETIRING", "RETIRED":
		phase, offset = "RETIRE", 3
	default:
		return model.AuthoritySnapshotPublication{}, errors.New("authority rotation operation status rejected")
	}
	phaseGraph := *graph
	phaseGraph.rotationPhase = phase
	phaseGraph.rotationOperation = operation
	phaseGraph.config.Registry.SourceRevision = operation.BaseRevision + offset
	if operation.Status == "WAITING_SWITCH" || operation.Status == "WAITING_RETIRE" || operation.Status == "RETIRED" {
		manifestBundle, readErr := readGraphFile(graph.config.ManifestTrustBundleJWSFile, 1<<20)
		if readErr != nil {
			return model.AuthoritySnapshotPublication{}, errors.New("read independently signed manifest trust bundle")
		}
		policyRaw, readErr := readGraphFile(graph.config.PolicyFile, 1<<20)
		if readErr != nil {
			return model.AuthoritySnapshotPublication{}, errors.New("read authority graph policy")
		}
		inputDigest, digestErr := publicationInputDigestForPhase(graph.config.Registry.SourceDigest, manifestBundle, policyRaw, phase)
		if digestErr != nil {
			return model.AuthoritySnapshotPublication{}, errors.New("digest authority graph publication inputs")
		}
		publication, found, loadErr := graph.config.Store.LoadSnapshotPublication(ctx, operation.BaseRevision+offset, inputDigest)
		if loadErr != nil || !found {
			return model.AuthoritySnapshotPublication{}, errors.New("load durable rotation phase publication")
		}
		return publication, nil
	}
	return phaseGraph.publishOne(ctx)
}

// publishOne checks the write fence and rooted signer, reserves rotation intent
// before key writes and persists the exact snapshot before trust/snapshot delivery.
// Rejoin rebuilds stored output; delivered snapshot bindings are checked before
// recording delivery. These effects are not one transaction or consumer acceptance.
// publishOne проверяет запрет записи и доверенного подписанта, резервирует намерение
// ротации до записи ключей и сохраняет точный снимок до доставки доверия и снимка.
// Повтор восстанавливает сохранённый результат; привязки доставленного снимка
// сверяются до фиксации доставки. Это не одна транзакция и не принятие потребителями.
func (graph *Graph) publishOne(
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
	inputDigest, err := publicationInputDigestForPhase(graph.config.Registry.SourceDigest, manifestBundle, policyRaw, graph.rotationPhase)
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
	historyForBuild, err := snapshotHistoryForBuild(
		history.Current,
		graph.config.Registry.SourceRevision,
		existing.SourceDigestSHA256,
		found,
	)
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
	intentID := existing.IntentID
	if !found {
		intentID = value.JoinedAuthorityUUID(
			"authority-rotation-v2",
			strconv.FormatUint(graph.config.Registry.SourceRevision, 10),
			graph.config.Registry.SourceDigest,
			inputDigest,
		)
		rotation := model.AuthorityRotationIntent{
			IntentID:                     intentID,
			SourceRevision:               graph.config.Registry.SourceRevision,
			SourceDigestSHA256:           graph.config.Registry.SourceDigest,
			PredecessorRevision:          predecessorRevision,
			PredecessorDigestSHA256:      predecessorDigest,
			ExpectedReadbackCount:        graph.config.Registry.StartupReadbackTargetCount(),
			PublicationInputDigestSHA256: inputDigest,
		}
		var prepareErr error
		if graph.rotationPhase == "" {
			prepareErr = graph.config.Store.PrepareRotation(ctx, rotation)
		} else {
			prepareErr = graph.config.Store.PrepareRotationPhase(ctx, graph.rotationOperation, graph.rotationPhase, rotation)
		}
		if prepareErr != nil {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"prepare durable authority key rotation",
			)
		}
		if err := graph.config.Store.BeginRotationDelivery(ctx, rotation); err != nil {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"begin durable authority key delivery",
			)
		}
	}
	buildNow := now
	if found {
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
				target.AuthPrivateKeySecret,
				target.TargetID+"-auth",
				predecessorRevision,
				predecessorDigest,
				graph.rotationPhase,
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
					nil,
					keySet,
				)...,
			)
		case "AUTHORITY_PROOF_RESOLVER":
			keySet, ensureErr := graph.ensureKeySet(
				ctx,
				target.ProofPrivateKeySecret,
				target.TargetID+"-proof",
				predecessorRevision,
				predecessorDigest,
				graph.rotationPhase,
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
					nil,
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
		PreviousKeyNotAfter:        graph.rotationOperation.PreviousNotAfter,
	})
	if err != nil {
		return model.AuthoritySnapshotPublication{}, err
	}
	publication := model.AuthoritySnapshotPublication{
		IntentID:                intentID,
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
		if existing.IntentID != publication.IntentID {
			return model.AuthoritySnapshotPublication{}, fmt.Errorf(
				"durable authority snapshot intent mutation rejected: stored content %s, desired content %s",
				existing.SourceDigestSHA256,
				publication.SourceDigestSHA256,
			)
		}
		if existing.InputDigestSHA256 != publication.InputDigestSHA256 {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"durable authority snapshot input mutation rejected",
			)
		}
		if existing.SourceDigestSHA256 != publication.SourceDigestSHA256 {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"durable authority snapshot content mutation rejected",
			)
		}
		if existing.PolicyRevision != publication.PolicyRevision {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"durable authority snapshot policy mutation rejected",
			)
		}
		if existing.SignerGeneration != publication.SignerGeneration {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"durable authority snapshot signer mutation rejected",
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
			graph.config.Registry.StartupReadbackTargetCount(),
		)
		if err != nil {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"persist authority snapshot publication intent",
			)
		}
	}
	// Durable intent precedes external delivery so concurrent replicas and a
	// restart after partial delivery rebuild the exact signed publication.
	// Устойчивое намерение предшествует внешней доставке, чтобы параллельные
	// реплики и перезапуск после частичной доставки восстанавливали точную публикацию.
	for _, target := range graph.config.Registry.Targets {
		if _, err := graph.putExact(
			ctx,
			target.ManifestTrustSecret,
			map[string]string{
				"bundle.jws": string(manifestBundle),
				"source_revision": strconv.FormatUint(
					graph.config.Registry.SourceRevision,
					10,
				),
				"source_digest_sha256": graph.config.Registry.SourceDigest,
			},
		); err != nil {
			return model.AuthoritySnapshotPublication{}, err
		}
		if target.ProofTrustSecret != "" {
			if _, err := graph.putExact(
				ctx,
				target.ProofTrustSecret,
				map[string]string{
					"jwks.json": string(built.ProofTrustJSON),
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
	if err := graph.config.Store.MarkRotationDelivered(ctx, model.AuthorityRotationIntent{
		IntentID:           persisted.IntentID,
		SourceRevision:     persisted.SourceRevision,
		SourceDigestSHA256: persisted.SourceDigestSHA256,
	}); err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"persist authority key delivery readback",
		)
	}
	return served, nil
}

// snapshotHistoryForBuild copies history, requires and removes the exact persisted current tail on
// rejoin, then retains the latest bounded predecessor window without mutating the input slice.
// snapshotHistoryForBuild копирует историю, при повторе требует и удаляет точный сохранённый текущий
// хвост, затем оставляет ограниченное окно последних предшественников, не меняя входной срез.
func snapshotHistoryForBuild(
	history []model.RevisionDigest,
	sourceRevision uint64,
	sourceDigest string,
	found bool,
) ([]model.RevisionDigest, error) {
	result := append([]model.RevisionDigest(nil), history...)
	if found {
		if len(result) == 0 ||
			result[len(result)-1].Revision != sourceRevision ||
			result[len(result)-1].DigestSHA256 != sourceDigest {
			return nil, errors.New(
				"durable authority snapshot history readback rejected",
			)
		}
		result = result[:len(result)-1]
	}
	if len(result) > maximumSnapshotHistoryEntries {
		result = result[len(result)-maximumSnapshotHistoryEntries:]
	}
	return result, nil
}

// ensureWritable asks the durable store whether publishing is allowed and maps any refusal to the fixed
// restore-fence diagnostic before graph effects.
// ensureWritable спрашивает устойчивое хранилище о допустимости публикации и до эффектов графа заменяет
// любой отказ фиксированной диагностикой блокировки восстановления.
func (graph *Graph) ensureWritable(ctx context.Context) error {
	if err := graph.config.Store.PublisherReady(ctx); err != nil {
		return errors.New("authority publisher restore fence is active")
	}
	return nil
}

// Ready checks publication lifetime, exact served snapshot, required consumer readbacks and delivered
// materials, then may advance the durable rotation phase; unlike a pure health query it can record phase
// progress.
// Ready проверяет срок публикации, точный обслуживаемый снимок, обязательные подтверждения потребителей
// и доставленные материалы, затем может продвинуть устойчивую фазу ротации; это не чистый запрос
// здоровья.
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
		graph.config.Registry.StartupReadbackTargets(),
	); err != nil {
		return err
	}
	if err := graph.deliveryReady(ctx, expected.SourceRevision); err != nil {
		return err
	}
	operation, found, err := graph.config.Store.LoadRotationOperation(ctx, graph.rotationOperationID())
	if err != nil {
		return errors.New("load durable authority rotation operation")
	}
	if !found {
		return nil
	}
	phase := ""
	switch expected.SourceRevision {
	case operation.BaseRevision + 1:
		phase = "DISTRIBUTE"
	case operation.BaseRevision + 2:
		phase = "SWITCH"
	case operation.BaseRevision + 3:
		phase = "RETIRE"
	}
	if phase != "" {
		_, err = graph.config.Store.AdvanceRotationOperation(ctx, operation, phase, expected)
		if err != nil {
			return errors.New("advance durable authority rotation operation")
		}
	}
	return nil
}

// rotationOperationID derives the stable normal-rotation identifier from a fixed namespace and registry
// revision/digest; it neither generates randomness nor authenticates authority.
// rotationOperationID вычисляет устойчивый идентификатор обычной ротации из фиксированного пространства
// и ревизии и хеша реестра; случайность и подтверждение полномочий здесь отсутствуют.
func (graph *Graph) rotationOperationID() string {
	return value.JoinedAuthorityUUID(
		"authority-normal-rotation-v3",
		strconv.FormatUint(graph.config.Registry.SourceRevision, 10),
		graph.config.Registry.SourceDigest,
	)
}

// deliveryReady checks configured delivery references for nonempty versioned material and pins source
// revision/digest for manifest and proof trust; it does not cryptographically revalidate every secret's
// contents.
// deliveryReady проверяет ссылки доставки на непустые версионированные материалы и закрепляет ревизию и
// хеш источника для доверия манифеста и доказательств; криптографической перепроверки каждого секрета
// здесь нет.
func (graph *Graph) deliveryReady(ctx context.Context, sourceRevision uint64) error {
	for _, target := range graph.config.Registry.Targets {
		for _, path := range []string{
			target.AuthPrivateKeySecret,
			target.ManifestTrustSecret,
			target.ProofTrustSecret,
			target.ProofPrivateKeySecret,
			target.ReadbackCredentialPath,
			target.ReadbackPossessionKeyPath,
		} {
			if path == "" {
				continue
			}
			material, found, err := graph.config.Secrets.ReadVersioned(ctx, path)
			if err != nil || !found || material.Version == 0 ||
				len(material.Digest) != 64 || len(material.Data) == 0 {
				return errors.New("authority graph delivery backend readback rejected")
			}
			if (path == target.ManifestTrustSecret || path == target.ProofTrustSecret) &&
				(material.Data["source_revision"] != strconv.FormatUint(sourceRevision, 10) ||
					material.Data["source_digest_sha256"] != graph.config.Registry.SourceDigest) {
				return errors.New("authority graph delivery source binding rejected")
			}
		}
	}
	return nil
}

// ensureKeySet reads or creates a key set, validates an exact historical predecessor before rotation,
// applies phase-specific generation changes through CAS and resolves ambiguous writes by readback rather
// than blindly repeating effects.
// ensureKeySet читает либо создаёт набор ключей, перед ротацией проверяет точного исторического
// предшественника, меняет поколения по фазе через CAS и разрешает неопределённые записи чтением вместо
// слепого повтора эффектов.
func (graph *Graph) ensureKeySet(
	ctx context.Context,
	path string,
	prefix string,
	predecessorRevision uint64,
	predecessorDigest string,
	phase string,
) (rotatingKeySet, error) {
	existing, found, err := graph.config.Secrets.ReadVersioned(ctx, path)
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
		existing, err = graph.config.Secrets.CreateVersioned(ctx, path, data)
		if err != nil {
			existing, found, err = graph.config.Secrets.ReadVersioned(ctx, path)
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
		graph.config.Registry.SourceRevision <= 1 ||
		storedRevision != predecessorRevision ||
		storedRevision+1 != graph.config.Registry.SourceRevision ||
		!registryDigestPattern.MatchString(existing.Data["source_digest_sha256"]) ||
		!registryDigestPattern.MatchString(predecessorDigest) {
		return rotatingKeySet{}, err
	}
	// Registry digest, publication input and snapshot digest belong to different
	// domains; resolve their exact historical binding from immutable publication,
	// not from new policy/manifest inputs or an assumed phase.
	// Хеш реестра, вход публикации и хеш снимка принадлежат разным областям;
	// точная историческая связь разрешается из неизменяемой публикации,
	// а не из новых политики и манифеста либо предполагаемой фазы.
	previous, previousFound, previousErr := graph.config.Store.LoadSnapshotPredecessor(
		ctx,
		predecessorRevision,
		predecessorDigest,
		existing.Data["source_digest_sha256"],
	)
	previousPublication := previous.Publication
	if previousErr != nil || !previousFound ||
		previousPublication.SourceRevision != predecessorRevision ||
		previousPublication.SourceDigestSHA256 != predecessorDigest ||
		!registryDigestPattern.MatchString(previousPublication.InputDigestSHA256) ||
		previous.RegistryDigestSHA256 != existing.Data["source_digest_sha256"] ||
		!validHistoricalPredecessorPhase(phase, previous.RotationPhase) {
		return rotatingKeySet{}, errors.New("authority key predecessor publication rejected")
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
	if phase == "DISTRIBUTE" {
		data, dataErr := graph.keySetData(oldCurrent, oldNext, nil, oldNextGeneration-1, oldNextGeneration, 0)
		if dataErr != nil {
			return rotatingKeySet{}, dataErr
		}
		updated, writeErr := graph.config.Secrets.WriteVersionedCAS(ctx, path, existing.Version, data)
		if writeErr != nil {
			updated, found, writeErr = graph.config.Secrets.ReadVersioned(ctx, path)
			if writeErr != nil || !found || !sameExactMaterial(updated.Data, data) {
				return rotatingKeySet{}, errors.New("recover authority signing key candidate delivery")
			}
		}
		return decodeRotatingKeySet(updated, graph.config.Registry)
	}
	if phase == "RETIRE" {
		data, dataErr := graph.keySetData(oldCurrent, oldNext, nil, oldNextGeneration-1, oldNextGeneration, 0)
		if dataErr != nil {
			return rotatingKeySet{}, dataErr
		}
		updated, writeErr := graph.config.Secrets.WriteVersionedCAS(ctx, path, existing.Version, data)
		if writeErr != nil {
			updated, found, writeErr = graph.config.Secrets.ReadVersioned(ctx, path)
			if writeErr != nil || !found || !sameExactMaterial(updated.Data, data) {
				return rotatingKeySet{}, errors.New("recover PREVIOUS authority key retirement")
			}
		}
		return decodeRotatingKeySet(updated, graph.config.Registry)
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
	updated, err := graph.config.Secrets.WriteVersionedCAS(
		ctx,
		path,
		existing.Version,
		data,
	)
	if err != nil {
		updated, found, err = graph.config.Secrets.ReadVersioned(ctx, path)
		if err != nil || !found {
			return rotatingKeySet{}, errors.New(
				"recover concurrent authority signing key rotation",
			)
		}
	}
	return decodeRotatingKeySet(updated, graph.config.Registry)
}

// validHistoricalPredecessorPhase allows bootstrap/retired predecessors for DISTRIBUTE, only DISTRIBUTE
// for SWITCH and only SWITCH for RETIRE; an unphased operation accepts only an unphased predecessor.
// validHistoricalPredecessorPhase разрешает начального либо завершившего RETIRE предшественника для
// DISTRIBUTE, только DISTRIBUTE для SWITCH и только SWITCH для RETIRE; операция без фазы принимает лишь
// предшественника без фазы.
func validHistoricalPredecessorPhase(current, historical string) bool {
	switch current {
	case "DISTRIBUTE":
		return historical == "" || historical == "RETIRE"
	case "SWITCH":
		return historical == "DISTRIBUTE"
	case "RETIRE":
		return historical == "SWITCH"
	default:
		return historical == ""
	}
}

// publicationInputDigest computes the canonical registry/manifest/policy input digest without a rotation
// phase; this identity differs from the built snapshot's content digest.
// publicationInputDigest вычисляет канонический хеш входных реестра, манифеста и политики без фазы
// ротации; он отличается от хеша содержимого собранного снимка.
func publicationInputDigest(registryDigest string, manifestBundle, policyRaw []byte) (string, error) {
	return publicationInputDigestForPhase(registryDigest, manifestBundle, policyRaw, "")
}

// publicationInputDigestForPhase hashes exact manifest/policy bytes and the registry digest with an
// optional phase using canonical JSON; empty phase preserves the unphased representation.
// publicationInputDigestForPhase хеширует точные байты манифеста и политики и хеш реестра с
// необязательной фазой через канонический JSON; пустая фаза сохраняет представление без фазы.
func publicationInputDigestForPhase(registryDigest string, manifestBundle, policyRaw []byte, phase string) (string, error) {
	return internalrpcauth.CanonicalJSONSHA256(struct {
		ManifestBundle string `json:"manifest_bundle"`
		Policy         string `json:"policy"`
		RegistryDigest string `json:"registry_digest_sha256"`
		RotationPhase  string `json:"rotation_phase,omitempty"`
	}{
		ManifestBundle: string(manifestBundle),
		Policy:         string(policyRaw),
		RegistryDigest: registryDigest,
		RotationPhase:  phase,
	})
}

// keySetData encodes private current/next JWKs, linked generations and registry source pins, adding only
// the previous public JWK when supplied; the returned map contains secrets.
// keySetData кодирует текущий и следующий закрытые JWK, связанные поколения и привязки источника
// реестра, добавляя только открытый JWK предыдущего ключа при его наличии; возвращаемая карта содержит
// секреты.
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

// decodeRotatingKeySet requires exact registry source pins, distinct current/next private keys, adjacent
// generations and the current-key alias; optional previous public material must have the preceding
// generation.
// decodeRotatingKeySet требует точные привязки источника реестра, различные текущий и следующий закрытые
// ключи, соседние поколения и совпадение псевдонима текущего ключа; необязательный предыдущий открытый
// ключ должен иметь предшествующее поколение.
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

// publisherKeys projects current/next and optional previous keys into public-only signing metadata with
// caller-supplied issuer, workload, purpose and audiences; it does not assign policy permissions.
// publisherKeys преобразует текущий, следующий и необязательный предыдущий ключи в открытые метаданные с
// переданными подписантом, нагрузкой, назначением и аудиториями; прав политики функция не назначает.
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

// putExact rejoins identical source-bound delivery, rejects same-revision mutation and rollback, and
// otherwise creates or CAS-updates material with exact content readback on success or uncertain outcome.
// putExact повторно использует идентичную доставку с привязкой к источнику, отклоняет изменение той же
// ревизии и откат, иначе создаёт либо меняет материал через CAS с точной сверкой содержимого при успехе
// и неопределённом исходе.
func (graph *Graph) putExact(
	ctx context.Context,
	path string,
	data map[string]string,
) (repository.SecretMaterial, error) {
	existing, found, err := graph.config.Secrets.ReadVersioned(ctx, path)
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
		updated, updateErr := graph.config.Secrets.WriteVersionedCAS(
			ctx,
			path,
			existing.Version,
			data,
		)
		if updateErr != nil {
			updated, found, updateErr = graph.config.Secrets.ReadVersioned(ctx, path)
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
	created, createErr := graph.config.Secrets.CreateVersioned(ctx, path, data)
	if createErr != nil {
		created, found, createErr = graph.config.Secrets.ReadVersioned(ctx, path)
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

// sameExactMaterial compares map lengths and values for each expected key without normalization; it
// relies on delivered material using the established nonempty value schema rather than acting as a
// general map validator.
// sameExactMaterial сравнивает длины карт и значения ожидаемых ключей без нормализации; функция
// опирается на принятую схему непустых значений доставки, а не является общей проверкой карт.
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

// readGraphFile applies the shared bounded read-only file checks and returns a fixed safe diagnostic on
// failure without exposing the path or contents.
// readGraphFile применяет общие ограниченные проверки файлов только для чтения и при ошибке возвращает
// фиксированную безопасную диагностику без пути и содержимого.
func readGraphFile(path string, limit int64) ([]byte, error) {
	value, err := securefile.Read(path, limit)
	if err != nil {
		return nil, errors.New("authority graph file is unsafe")
	}
	return value, nil
}

var _ repository.AuthorityGraphLifecycle = (*Graph)(nil)
