package publisher

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	domainrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// rotationSecretDelivery is an in-memory key-delivery fixture with write counting and one ambiguous
// post-write failure; it does not implement Kubernetes ownership or API-server CAS enforcement.
// rotationSecretDelivery — оснастка доставки ключей в памяти со счётчиком записей и одним неопределённым
// отказом после записи; владение Kubernetes и серверная проверка CAS здесь не реализованы.
type rotationSecretDelivery struct {
	material          domainrepository.SecretMaterial
	writes            int
	unknownAfterWrite bool
}

// TestNormalRotationOperationIDMatchesDeliveryGoldenVector pins the established
// namespace/revision/digest mapping so refactoring cannot silently change persisted operation IDs.
// TestNormalRotationOperationIDMatchesDeliveryGoldenVector закрепляет принятое отображение пространства,
// ревизии и хеша, чтобы рефакторинг не менял устойчивые идентификаторы операций.
func TestNormalRotationOperationIDMatchesDeliveryGoldenVector(t *testing.T) {
	t.Parallel()
	graph := &Graph{config: GraphConfig{Registry: model.DeliveryTargetRegistry{
		SourceRevision: 8,
		SourceDigest:   strings.Repeat("c", 64),
	}}}
	if got, want := graph.rotationOperationID(), "13657d5a-4e35-52fc-b82d-0529ee0914ca"; got != want {
		t.Fatalf("rotation operation ID = %s, want %s", got, want)
	}
}

// ReadVersioned returns the fixture's current material as present regardless of the supplied path; tests
// use it only for one synthetic target.
// ReadVersioned возвращает текущий материал оснастки как существующий независимо от пути; тесты
// используют его только для одной синтетической цели.
func (delivery *rotationSecretDelivery) ReadVersioned(context.Context, string) (domainrepository.SecretMaterial, bool, error) {
	return delivery.material, true, nil
}

// CreateVersioned counts a creation attempt and returns empty material; this limited fixture is not a
// general create/readback backend.
// CreateVersioned учитывает попытку создания и возвращает пустой материал; ограниченная оснастка не
// заменяет полноценное хранилище создания и чтения.
func (delivery *rotationSecretDelivery) CreateVersioned(context.Context, string, map[string]string) (domainrepository.SecretMaterial, error) {
	delivery.writes++
	return domainrepository.SecretMaterial{}, nil
}

// WriteVersionedCAS counts attempts, compares the synthetic version and stores updated material before
// optionally returning an ambiguous error so recovery must read rather than write again.
// WriteVersionedCAS считает попытки, сверяет синтетическую версию и сохраняет новый материал до
// необязательной неопределённой ошибки, чтобы восстановление требовало чтения, а не повторной записи.
func (delivery *rotationSecretDelivery) WriteVersionedCAS(_ context.Context, _ string, version uint64, data map[string]string) (domainrepository.SecretMaterial, error) {
	delivery.writes++
	if version != delivery.material.Version {
		return domainrepository.SecretMaterial{}, fmt.Errorf("fixture CAS mismatch")
	}
	delivery.material = domainrepository.SecretMaterial{Version: version + 1, Digest: strings.Repeat("d", 64), Data: data}
	if delivery.unknownAfterWrite {
		delivery.unknownAfterWrite = false
		return domainrepository.SecretMaterial{}, fmt.Errorf("unknown CAS outcome")
	}
	return delivery.material, nil
}

// rotationPublicationStore embeds unused port methods and implements only publication/predecessor reads
// with exact synthetic pins, phase, rejection control and read counting.
// rotationPublicationStore встраивает неиспользуемые методы порта и реализует лишь чтение публикации и
// предшественника с точными синтетическими привязками, фазой, управляемым отказом и счётчиком чтений.
type rotationPublicationStore struct {
	domainrepository.PublisherStore
	publication model.AuthoritySnapshotPublication
	registry    string
	phase       string
	reject      bool
	reads       int
}

// LoadSnapshotPublication returns fixture presence only for an exact revision/input-digest match and
// increments the read count; it does not persist new state.
// LoadSnapshotPublication возвращает наличие только при точном совпадении ревизии и хеша входа оснастки
// и увеличивает счётчик чтений; нового состояния здесь нет.
func (store *rotationPublicationStore) LoadSnapshotPublication(_ context.Context, revision uint64, inputDigest string) (model.AuthoritySnapshotPublication, bool, error) {
	store.reads++
	return store.publication, store.publication.SourceRevision == revision && store.publication.InputDigestSHA256 == inputDigest, nil
}

// LoadSnapshotPredecessor requires exact revision, snapshot and registry digests plus the fixture's
// presence flag and returns the recorded phase for historical validation.
// LoadSnapshotPredecessor требует точные ревизию, хеши снимка и реестра и разрешённое наличие оснастки,
// затем возвращает записанную фазу для исторической проверки.
func (store *rotationPublicationStore) LoadSnapshotPredecessor(_ context.Context, revision uint64, snapshotDigest, registryDigest string) (model.AuthoritySnapshotPredecessor, bool, error) {
	store.reads++
	value := model.AuthoritySnapshotPredecessor{
		Publication:          store.publication,
		RegistryDigestSHA256: store.registry,
		RotationPhase:        store.phase,
	}
	found := !store.reject && store.publication.SourceRevision == revision &&
		store.publication.SourceDigestSHA256 == snapshotDigest && store.registry == registryDigest
	return value, found, nil
}

// TestSnapshotHistoryForBuildKeepsStableBoundaryWindow proves that initial publication and rejoin after
// its persistence use the same 32 predecessors rather than shifting the signed history window.
// TestSnapshotHistoryForBuildKeepsStableBoundaryWindow проверяет одинаковые 32 предшественника при
// первой публикации и повторе после сохранения, без сдвига подписанного окна истории.
func TestSnapshotHistoryForBuildKeepsStableBoundaryWindow(t *testing.T) {
	t.Parallel()

	beforePersist := revisionHistory(1, 32)
	initial, err := snapshotHistoryForBuild(beforePersist, 33, "", false)
	if err != nil {
		t.Fatalf("initial history: %v", err)
	}
	afterPersist := revisionHistory(1, 33)
	restarted, err := snapshotHistoryForBuild(
		afterPersist,
		33,
		fmt.Sprintf("%064x", 33),
		true,
	)
	if err != nil {
		t.Fatalf("restart history: %v", err)
	}
	assertRevisionWindow(t, initial, 1, 32)
	assertRevisionWindow(t, restarted, 1, 32)
}

// TestSnapshotHistoryForBuildTrimsNewRevisionToLatestWindow requires a new publication to retain only
// the latest 32 predecessor revisions when more are available.
// TestSnapshotHistoryForBuildTrimsNewRevisionToLatestWindow требует сохранения только последних 32
// ревизий предшественников для новой публикации при более длинной истории.
func TestSnapshotHistoryForBuildTrimsNewRevisionToLatestWindow(t *testing.T) {
	t.Parallel()

	history, err := snapshotHistoryForBuild(revisionHistory(1, 33), 34, "", false)
	if err != nil {
		t.Fatalf("new revision history: %v", err)
	}
	assertRevisionWindow(t, history, 2, 33)
}

// TestSnapshotHistoryForBuildRejectsMissingPersistedRevision rejects rejoin when the supposedly
// persisted current revision is not the exact history tail.
// TestSnapshotHistoryForBuildRejectsMissingPersistedRevision отклоняет повтор, когда предположительно
// сохранённая текущая ревизия не является точным хвостом истории.
func TestSnapshotHistoryForBuildRejectsMissingPersistedRevision(t *testing.T) {
	t.Parallel()

	if _, err := snapshotHistoryForBuild(
		revisionHistory(1, 32),
		33,
		fmt.Sprintf("%064x", 33),
		true,
	); err == nil {
		t.Fatal("missing persisted revision was accepted")
	}
}

// TestKeyRotationRejectsAValidButUnknownPredecessorDigestBeforeCAS requires a syntactically valid but
// unregistered predecessor digest to fail before any synthetic external write.
// TestKeyRotationRejectsAValidButUnknownPredecessorDigestBeforeCAS требует отказа для синтаксически
// допустимого, но неизвестного хеша предшественника до любой синтетической внешней записи.
func TestKeyRotationRejectsAValidButUnknownPredecessorDigestBeforeCAS(t *testing.T) {
	t.Parallel()
	current, err := internalrpcauth.GenerateES256Key("rotation-current")
	if err != nil {
		t.Fatal("generate current key")
	}
	next, err := internalrpcauth.GenerateES256Key("rotation-next")
	if err != nil {
		t.Fatal("generate next key")
	}
	predecessorDigest := strings.Repeat("a", 64)
	graph := &Graph{config: GraphConfig{Registry: model.DeliveryTargetRegistry{
		SourceRevision: 2,
		SourceDigest:   strings.Repeat("b", 64),
	}}}
	data, err := graph.keySetData(current, next, nil, 1, 2, 0)
	if err != nil {
		t.Fatal("encode previous key set")
	}
	data["source_revision"] = "1"
	data["source_digest_sha256"] = strings.Repeat("c", 64)
	delivery := &rotationSecretDelivery{material: domainrepository.SecretMaterial{
		Version: 1,
		Digest:  strings.Repeat("d", 64),
		Data:    data,
	}}
	graph.config.Secrets = delivery
	graph.config.Store = &rotationPublicationStore{}
	if _, err := graph.ensureKeySet(t.Context(), "secret", "rotation", 1, predecessorDigest, ""); err == nil {
		t.Fatal("unknown predecessor digest was accepted")
	}
	if delivery.writes != 0 {
		t.Fatal("unknown predecessor reached external CAS")
	}
}

// revisionHistory constructs inclusive consecutive fixture revisions with fixed-width hexadecimal
// digests; these values are deterministic metadata, not signed publications.
// revisionHistory создаёт включительный последовательный диапазон ревизий оснастки с шестнадцатеричными
// хешами фиксированной ширины; это воспроизводимые метаданные, а не подписанные публикации.
func revisionHistory(first, last uint64) []model.RevisionDigest {
	result := make([]model.RevisionDigest, 0, last-first+1)
	for revision := first; revision <= last; revision++ {
		result = append(result, model.RevisionDigest{
			Revision:     revision,
			DigestSHA256: fmt.Sprintf("%064x", revision),
		})
	}
	return result
}

// assertRevisionWindow checks the expected inclusive window length and first/last revision for locally
// constructed histories; it does not independently validate every interior digest.
// assertRevisionWindow проверяет ожидаемую длину включительного окна и первую и последнюю ревизии
// локальной истории; каждый внутренний хеш отдельно не проверяется.
func assertRevisionWindow(
	t *testing.T,
	history []model.RevisionDigest,
	first uint64,
	last uint64,
) {
	t.Helper()
	if len(history) != int(last-first+1) {
		t.Fatalf("history length = %d, want %d", len(history), last-first+1)
	}
	if history[0].Revision != first || history[len(history)-1].Revision != last {
		t.Fatalf(
			"history revisions = %d..%d, want %d..%d",
			history[0].Revision,
			history[len(history)-1].Revision,
			first,
			last,
		)
	}
}

// TestKeyRotationBindsDistinctRegistryAndSnapshotDigestsAndResumesWithoutSecondCAS verifies distinct
// digest domains, exact predecessor eligibility and rejoin without a second write, including mutation
// failures before CAS.
// TestKeyRotationBindsDistinctRegistryAndSnapshotDigestsAndResumesWithoutSecondCAS проверяет разные
// области хеширования, точного допустимого предшественника и повтор без второй записи, включая отказ при
// подменах до CAS.
func TestKeyRotationBindsDistinctRegistryAndSnapshotDigestsAndResumesWithoutSecondCAS(t *testing.T) {
	t.Parallel()
	oldManifest, oldPolicy := []byte("exact manifest"), []byte("policy revision 76")
	manifest, policy := oldManifest, []byte("policy revision 77")
	registryDigest, snapshotDigest := strings.Repeat("a", 64), strings.Repeat("b", 64)
	oldInput, err := publicationInputDigest(registryDigest, oldManifest, oldPolicy)
	if err != nil {
		t.Fatal("publication input digest")
	}
	newInput, err := publicationInputDigest(registryDigest, manifest, policy)
	if err != nil || newInput == oldInput {
		t.Fatal("policy change did not produce a distinct current input")
	}
	current, err := internalrpcauth.GenerateES256Key("rotation-g1")
	if err != nil {
		t.Fatal("generate current")
	}
	next, err := internalrpcauth.GenerateES256Key("rotation-g2")
	if err != nil {
		t.Fatal("generate next")
	}
	graph := &Graph{config: GraphConfig{Registry: model.DeliveryTargetRegistry{SourceRevision: 1, SourceDigest: registryDigest}}}
	data, err := graph.keySetData(current, next, nil, 1, 2, 0)
	if err != nil {
		t.Fatal("encode keys")
	}
	delivery := &rotationSecretDelivery{material: domainrepository.SecretMaterial{Version: 1, Digest: strings.Repeat("d", 64), Data: data}}
	store := &rotationPublicationStore{publication: model.AuthoritySnapshotPublication{SourceRevision: 1, InputDigestSHA256: oldInput, SourceDigestSHA256: snapshotDigest}, registry: registryDigest}
	graph.config.Secrets, graph.config.Store = delivery, store
	graph.config.Registry = model.DeliveryTargetRegistry{SourceRevision: 2, SourceDigest: strings.Repeat("c", 64)}
	rotated, err := graph.ensureKeySet(t.Context(), "secret", "rotation", 1, snapshotDigest, "")
	if err != nil {
		t.Fatalf("valid cross-digest rotation: %v", err)
	}
	if rotated.current.KeyID != next.KeyID || rotated.previous.KeyID != current.KeyID || delivery.writes != 1 || store.reads != 1 {
		t.Fatal("rotation lost exact key provenance")
	}
	rejoined, err := graph.ensureKeySet(t.Context(), "secret", "rotation", 1, snapshotDigest, "")
	if err != nil || rejoined.current.KeyID != rotated.current.KeyID || rejoined.next.KeyID != rotated.next.KeyID || delivery.writes != 1 {
		t.Fatal("rejoin repeated rotation effect")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*rotationPublicationStore)
	}{
		{"snapshot", func(s *rotationPublicationStore) { s.publication.SourceDigestSHA256 = strings.Repeat("e", 64) }},
		{"missing", func(s *rotationPublicationStore) { s.reject = true }},
		{"revision", func(s *rotationPublicationStore) { s.publication.SourceRevision = 3 }},
		{"registry", func(s *rotationPublicationStore) { s.registry = strings.Repeat("e", 64) }},
		{"phase", func(s *rotationPublicationStore) { s.phase = "SWITCH" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyStore := &rotationPublicationStore{publication: store.publication, registry: store.registry, phase: store.phase}
			tc.mutate(copyStore)
			copyDelivery := &rotationSecretDelivery{material: domainrepository.SecretMaterial{Version: 1, Data: data}}
			graph.config.Secrets, graph.config.Store = copyDelivery, copyStore
			if _, err := graph.ensureKeySet(t.Context(), "secret", "rotation", 1, snapshotDigest, ""); err == nil || copyDelivery.writes != 0 {
				t.Fatal("unbound predecessor reached CAS")
			}
		})
	}
}

// TestKeyRotationResolvesHistoricalManifestChange requires rotation to use the persisted predecessor
// publication even when current manifest bytes produce a different input digest.
// TestKeyRotationResolvesHistoricalManifestChange требует использования сохранённой публикации
// предшественника при ротации, даже если новый манифест даёт другой хеш входа.
func TestKeyRotationResolvesHistoricalManifestChange(t *testing.T) {
	t.Parallel()
	oldManifest, newManifest, policy := []byte("old manifest"), []byte("new manifest"), []byte("exact policy")
	oldRegistryDigest, snapshotDigest := strings.Repeat("1", 64), strings.Repeat("2", 64)
	oldInput, err := publicationInputDigest(oldRegistryDigest, oldManifest, policy)
	if err != nil {
		t.Fatal("publication input digest")
	}
	newInput, err := publicationInputDigest(oldRegistryDigest, newManifest, policy)
	if err != nil || newInput == oldInput {
		t.Fatal("manifest change did not produce a distinct current input")
	}
	current := mustRotationKey(t, "manifest-g1")
	next := mustRotationKey(t, "manifest-g2")
	graph := &Graph{config: GraphConfig{Registry: model.DeliveryTargetRegistry{SourceRevision: 1, SourceDigest: oldRegistryDigest}}}
	data, err := graph.keySetData(current, next, nil, 1, 2, 0)
	if err != nil {
		t.Fatal("encode keys")
	}
	delivery := &rotationSecretDelivery{material: domainrepository.SecretMaterial{Version: 1, Data: data}}
	store := &rotationPublicationStore{
		publication: model.AuthoritySnapshotPublication{SourceRevision: 1, SourceDigestSHA256: snapshotDigest, InputDigestSHA256: oldInput},
		registry:    oldRegistryDigest,
	}
	graph.config.Secrets, graph.config.Store = delivery, store
	graph.config.Registry = model.DeliveryTargetRegistry{SourceRevision: 2, SourceDigest: strings.Repeat("3", 64)}
	rotated, err := graph.ensureKeySet(t.Context(), "secret", "manifest", 1, snapshotDigest, "")
	if err != nil {
		t.Fatalf("manifest change rejected historical predecessor: %v", err)
	}
	if rotated.current.KeyID != next.KeyID || delivery.writes != 1 {
		t.Fatal("manifest change did not rotate exact historical keys")
	}
}

// TestNormalRotationTransformsKeysAcrossThreeImmutablePublications checks DISTRIBUTE/SWITCH/RETIRE key
// transitions, the next cycle and uncertain-write rejoin using in-memory material with exactly counted
// writes.
// TestNormalRotationTransformsKeysAcrossThreeImmutablePublications проверяет переходы ключей
// DISTRIBUTE/SWITCH/RETIRE, следующий цикл и повтор после неопределённой записи на материале в памяти с
// точным счётчиком записей.
func TestNormalRotationTransformsKeysAcrossThreeImmutablePublications(t *testing.T) {
	t.Parallel()
	manifest, policy := []byte("exact manifest"), []byte("exact policy")
	oldRegistryDigest := strings.Repeat("1", 64)
	rotationRegistryDigest := strings.Repeat("2", 64)
	current := mustRotationKey(t, "rotation-g1")
	next := mustRotationKey(t, "rotation-g2")
	graph := &Graph{config: GraphConfig{Registry: model.DeliveryTargetRegistry{
		SourceRevision: 1,
		SourceDigest:   oldRegistryDigest,
	}}}
	data, err := graph.keySetData(current, next, nil, 1, 2, 0)
	if err != nil {
		t.Fatal("encode initial keys")
	}
	delivery := &rotationSecretDelivery{material: domainrepository.SecretMaterial{
		Version: 1, Digest: strings.Repeat("d", 64), Data: data,
	}}
	store := &rotationPublicationStore{}
	graph.config.Secrets, graph.config.Store = delivery, store

	previousSnapshotDigest := strings.Repeat("3", 64)
	store.publication = phasePublication(t, 1, oldRegistryDigest, previousSnapshotDigest, manifest, policy, "")
	store.registry, store.phase = oldRegistryDigest, ""
	graph.config.Registry.SourceRevision = 2
	graph.config.Registry.SourceDigest = rotationRegistryDigest
	distributed, err := graph.ensureKeySet(t.Context(), "secret", "rotation", 1, previousSnapshotDigest, "DISTRIBUTE")
	if err != nil {
		t.Fatalf("distribute: %v", err)
	}
	if distributed.current.KeyID != current.KeyID || distributed.next.KeyID != next.KeyID || distributed.previous != nil {
		t.Fatal("DISTRIBUTE changed signing generations")
	}

	previousSnapshotDigest = strings.Repeat("4", 64)
	store.publication = phasePublication(t, 2, rotationRegistryDigest, previousSnapshotDigest, manifest, policy, "DISTRIBUTE")
	store.registry, store.phase = rotationRegistryDigest, "DISTRIBUTE"
	graph.config.Registry.SourceRevision = 3
	switched, err := graph.ensureKeySet(t.Context(), "secret", "rotation", 2, previousSnapshotDigest, "SWITCH")
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if switched.current.KeyID != next.KeyID || switched.previous == nil || switched.previous.KeyID != current.KeyID || switched.nextGeneration != 3 {
		t.Fatal("SWITCH did not promote exact NEXT and retain PREVIOUS")
	}

	previousSnapshotDigest = strings.Repeat("5", 64)
	store.publication = phasePublication(t, 3, rotationRegistryDigest, previousSnapshotDigest, manifest, policy, "SWITCH")
	store.registry, store.phase = rotationRegistryDigest, "SWITCH"
	graph.config.Registry.SourceRevision = 4
	retired, err := graph.ensureKeySet(t.Context(), "secret", "rotation", 3, previousSnapshotDigest, "RETIRE")
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if retired.current.KeyID != switched.current.KeyID || retired.next.KeyID != switched.next.KeyID || retired.previous != nil {
		t.Fatal("RETIRE changed CURRENT/NEXT or retained PREVIOUS")
	}

	store.publication = phasePublication(t, 4, rotationRegistryDigest, strings.Repeat("6", 64), manifest, policy, "RETIRE")
	store.registry, store.phase = rotationRegistryDigest, "RETIRE"
	graph.config.Registry = model.DeliveryTargetRegistry{SourceRevision: 5, SourceDigest: strings.Repeat("7", 64)}
	delivery.unknownAfterWrite = true
	second, err := graph.ensureKeySet(t.Context(), "secret", "rotation", 4, strings.Repeat("6", 64), "DISTRIBUTE")
	if err != nil {
		t.Fatalf("second distribute after retired predecessor: %v", err)
	}
	if second.current.KeyID != retired.current.KeyID || second.next.KeyID != retired.next.KeyID || second.previous != nil || delivery.writes != 4 {
		t.Fatal("second DISTRIBUTE changed keys or failed UNKNOWN recovery")
	}
	rejoined, err := graph.ensureKeySet(t.Context(), "secret", "rotation", 4, strings.Repeat("6", 64), "DISTRIBUTE")
	if err != nil || rejoined.current.KeyID != second.current.KeyID || delivery.writes != 4 {
		t.Fatal("second DISTRIBUTE restart repeated key CAS")
	}
}

// mustRotationKey generates a fresh synthetic ES256 key or stops the calling test without printing
// private key material.
// mustRotationKey создаёт новый синтетический ключ ES256 либо останавливает тест, не печатая закрытый
// материал.
func mustRotationKey(t *testing.T, keyID string) internalrpcauth.ES256Key {
	t.Helper()
	key, err := internalrpcauth.GenerateES256Key(keyID)
	if err != nil {
		t.Fatalf("generate %s: %v", keyID, err)
	}
	return key
}

// phasePublication builds a metadata-only publication fixture from explicit revision, registry/snapshot
// digests and phase-bound inputs; it neither signs nor delivers a snapshot.
// phasePublication создаёт публикацию-оснастку только с метаданными из явных ревизии, хешей реестра и
// снимка и входов фазы; подписи и доставки снимка здесь нет.
func phasePublication(
	t *testing.T,
	revision uint64,
	registryDigest string,
	snapshotDigest string,
	manifest []byte,
	policy []byte,
	phase string,
) model.AuthoritySnapshotPublication {
	t.Helper()
	inputDigest, err := publicationInputDigestForPhase(registryDigest, manifest, policy, phase)
	if err != nil {
		t.Fatalf("digest %s publication input: %v", phase, err)
	}
	return model.AuthoritySnapshotPublication{
		SourceRevision: revision, InputDigestSHA256: inputDigest,
		SourceDigestSHA256: snapshotDigest,
	}
}
