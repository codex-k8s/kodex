package stagingguard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// configMapFake shares one mutex-protected synthetic object across replicas and injects conflicts, a
// competing state or lost acknowledgements; it is not a real Kubernetes API server.
// configMapFake разделяет один защищённый mutex synthetic object между replicas и внедряет conflicts,
// конкурирующее state либо потерянные acknowledgements; реальным Kubernetes API server не является.
type configMapFake struct {
	mu                       sync.Mutex
	object                   *corev1.ConfigMap
	gets, updates, conflicts int
	lostResponse             bool
	raceState                string
}

// newStore supplies an owned/purpose-labeled synthetic ConfigMap at RV1 with strict empty genesis counters
// and no manifest.
// newStore предоставляет synthetic ConfigMap с owner/purpose labels и RV1, strict пустыми genesis counters
// и без manifest.
func newStore() *configMapFake {
	return &configMapFake{object: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "secret-draft-key-guard", Namespace: "kodex-system", UID: "fixture-guard-uid", ResourceVersion: "1", Labels: map[string]string{OwnerLabel: OwnerValue, PurposeLabel: PurposeValue}}, Data: map[string]string{StateKey: `{"v":1,"manifest":null,"uses":[]}`}}}
}

// Get checks cancellation, counts under lock and returns a DeepCopy of the configured exact name or
// synthetic NotFound.
// Get проверяет cancellation, учитывает вызов под lock и возвращает DeepCopy настроенного exact name либо
// synthetic NotFound.
func (store *configMapFake) Get(ctx context.Context, name string, _ metav1.GetOptions) (*corev1.ConfigMap, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.gets++
	if store.object == nil || name != "secret-draft-key-guard" {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, name)
	}
	return store.object.DeepCopy(), nil
}

// Update checks scope/UID/RV under lock, can inject conflicts or competing state, then increments RV and
// may lose the success reply after persisting the synthetic change.
// Update проверяет scope/UID/RV под lock, может внедрить conflicts либо конкурирующее state, затем
// увеличивает RV и может потерять success reply после сохранения synthetic change.
func (store *configMapFake) Update(ctx context.Context, next *corev1.ConfigMap, _ metav1.UpdateOptions) (*corev1.ConfigMap, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.updates++
	conflict := func() (*corev1.ConfigMap, error) {
		return nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, next.Name, errors.New("fixture conflict"))
	}
	if store.conflicts > 0 {
		store.conflicts--
		return conflict()
	}
	if store.raceState != "" {
		store.object.Data[StateKey] = store.raceState
		store.raceState = ""
		old, _ := strconv.Atoi(store.object.ResourceVersion)
		store.object.ResourceVersion = strconv.Itoa(old + 1)
		return conflict()
	}
	if next.Namespace != "kodex-system" || next.Name != "secret-draft-key-guard" || next.UID != store.object.UID || next.ResourceVersion != store.object.ResourceVersion {
		return conflict()
	}
	old, _ := strconv.Atoi(next.ResourceVersion)
	store.object = next.DeepCopy()
	store.object.ResourceVersion = strconv.Itoa(old + 1)
	if store.lostResponse {
		store.lostResponse = false
		return nil, errors.New("fixture response lost")
	}
	return store.object.DeepCopy(), nil
}

// fixtureGuard binds a Guard to the shared synthetic store and exact fixture namespace/name without
// touching a cluster.
// fixtureGuard связывает Guard с общим synthetic store и exact fixture namespace/name без обращения к
// кластеру.
func fixtureGuard(t *testing.T, store *configMapFake) *Guard {
	t.Helper()
	guard, err := New(store, "kodex-system", "secret-draft-key-guard")
	if err != nil {
		t.Fatal(err)
	}
	return guard
}

// key builds a synthetic ID by repeating a generation's hexadecimal text 64 times; callers use small
// fixture generations, not arbitrary production identities.
// key создаёт synthetic ID повторением hexadecimal generation 64 раза; caller использует малые fixture
// generations, не произвольные production identities.
func key(generation int64) value.DraftEncryptionKey {
	return value.DraftEncryptionKey{ID: strings.Repeat(strconv.FormatInt(generation, 16), 64), Generation: generation}
}

// manifest hashes the supplied nonempty key list and selects its last entry as current without sorting or
// validating it, allowing deliberate invalid fixtures.
// manifest хеширует переданный непустой key list и выбирает последний entry как current без сортировки и
// проверки, позволяя намеренно неверные fixtures.
func manifest(revision int64, keys ...value.DraftEncryptionKey) value.DraftKeyManifest {
	v := value.DraftKeyManifest{Revision: revision, Keys: keys, Current: keys[len(keys)-1]}
	raw, _ := json.Marshal(v)
	digest := sha256.Sum256(raw)
	v.Digest = hex.EncodeToString(digest[:])
	return v
}

// encodeState serializes fixture state or fails the test; it intentionally does not enforce the production
// decoder's schema or counter invariants.
// encodeState сериализует fixture state либо завершает тест; production schema и counter invariants
// намеренно не проверяет.
func encodeState(t *testing.T, state guardState) string {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// readState decodes a copy of the shared synthetic state under its mutex for assertions, not as a
// replacement for strict Guard.decode.
// readState декодирует shared synthetic state под mutex для assertions, не заменяя strict Guard.decode.
func readState(t *testing.T, store *configMapFake) guardState {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	var state guardState
	if json.Unmarshal([]byte(store.object.Data[StateKey]), &state) != nil {
		t.Fatal("invalid stored state")
	}
	return state
}

// TestGuardRestartRotationAndSkippedDelivery reuses one shared state across new Guard instances, accepts a
// forward revision jump and verifies old-key denial and retained counters; no Pod is actually restarted.
// TestGuardRestartRotationAndSkippedDelivery использует общее state в новых Guard instances, допускает
// forward revision jump и проверяет отказ old key и сохранение counters; реальный Pod не перезапускается.
func TestGuardRestartRotationAndSkippedDelivery(t *testing.T) {
	ctx := context.Background()
	store := newStore()
	guard := fixtureGuard(t, store)
	first := manifest(1, key(1))
	if guard.Reserve(ctx, key(1)) == nil {
		t.Fatal("genesis allowed encryption")
	}
	if err := guard.Observe(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := guard.Reserve(ctx, key(1)); err != nil {
		t.Fatal(err)
	}
	restarted := fixtureGuard(t, store)
	if err := restarted.Observe(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Reserve(ctx, key(1)); err != nil {
		t.Fatal(err)
	}
	later := manifest(4, key(1), key(3), key(4))
	if err := restarted.Observe(ctx, later); err != nil {
		t.Fatal(err)
	}
	if guard.Reserve(ctx, key(1)) == nil || guard.Observe(ctx, first) == nil {
		t.Fatal("old replica bypassed rotation")
	}
	if err := guard.Reserve(ctx, key(4)); err != nil {
		t.Fatal(err)
	}
	state := readState(t, store)
	if state.Manifest.Digest != later.Digest || len(state.Uses) != 3 || state.Uses[0].Encryptions != 2 || state.Uses[1].Encryptions != 0 || state.Uses[2].Encryptions != 1 {
		t.Fatal("rotation reset encryption counters")
	}
}

// TestGuardRejectsRollbackRetirementAndKeyReuse rejects older/changed-same-revision manifests, removed
// read keys and reused IDs/generations without changing stored bytes.
// TestGuardRejectsRollbackRetirementAndKeyReuse отклоняет старые/изменённые same-revision manifests,
// удалённые read keys и reused IDs/generations без изменения stored bytes.
func TestGuardRejectsRollbackRetirementAndKeyReuse(t *testing.T) {
	accepted := manifest(4, key(1), key(4))
	for name, candidate := range map[string]value.DraftKeyManifest{
		"rollback":              manifest(3, key(1)),
		"same revision changed": manifest(4, key(1), key(3), key(4)),
		"retired read key":      manifest(5, key(4), key(5)),
		"reused ID":             manifest(5, key(1), value.DraftEncryptionKey{ID: key(4).ID, Generation: 5}),
		"reused generation":     manifest(5, key(1), value.DraftEncryptionKey{ID: key(5).ID, Generation: 4}),
	} {
		t.Run(name, func(t *testing.T) {
			store := newStore()
			guard := fixtureGuard(t, store)
			if err := guard.Observe(context.Background(), accepted); err != nil {
				t.Fatal(err)
			}
			before := store.object.Data[StateKey]
			if guard.Observe(context.Background(), candidate) == nil || store.object.Data[StateKey] != before {
				t.Fatal("manifest downgrade accepted")
			}
		})
	}
}

// TestGuardConcurrentReplicasAndBudget runs 32 concurrent synthetic replicas, checks no successful
// reservation is lost and enforces the final allowance/cap across new Guard instances; it is not a live
// multi-replica cluster test.
// TestGuardConcurrentReplicasAndBudget запускает 32 concurrent synthetic replicas, проверяет сохранность
// successful reservations и соблюдение последнего allowance/cap в новых Guard instances; live
// multi-replica cluster не проверяется.
func TestGuardConcurrentReplicasAndBudget(t *testing.T) {
	store := newStore()
	guard := fixtureGuard(t, store)
	if err := guard.Observe(context.Background(), manifest(1, key(1))); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Uint64
	var group sync.WaitGroup
	for index := 0; index < 32; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			replica := fixtureGuard(t, store)
			for attempt := 0; attempt < 64; attempt++ {
				if replica.Reserve(context.Background(), key(1)) == nil {
					successes.Add(1)
					return
				}
			}
		}()
	}
	group.Wait()
	state := readState(t, store)
	if successes.Load() != 32 || state.Uses[0].Encryptions != successes.Load() {
		t.Fatalf("lost reservations: %d/%d", successes.Load(), state.Uses[0].Encryptions)
	}
	state.Uses[0].Encryptions = MaximumEncryptions - 1
	store.object.Data[StateKey] = encodeState(t, state)
	if err := guard.Reserve(context.Background(), key(1)); err != nil {
		t.Fatal(err)
	}
	if fixtureGuard(t, store).Reserve(context.Background(), key(1)) == nil || readState(t, store).Uses[0].Encryptions != MaximumEncryptions {
		t.Fatal("encryption cap exceeded after restart")
	}
}

// TestGuardCASConflictCancellationAndUnknownOutcome exercises conflict refetch/bounds, lost committed
// acknowledgement, early cancellation and a concurrent key rotation, refusing reuse of an uncertain
// reservation or retired current key.
// TestGuardCASConflictCancellationAndUnknownOutcome проверяет conflict refetch/bounds, потерю committed
// acknowledgement, early cancellation и concurrent key rotation, запрещая повтор uncertain reservation и
// retired current key.
func TestGuardCASConflictCancellationAndUnknownOutcome(t *testing.T) {
	ctx := context.Background()
	store := newStore()
	guard := fixtureGuard(t, store)
	first := manifest(1, key(1))
	store.conflicts = 2
	if err := guard.Observe(ctx, first); err != nil || store.updates != 3 {
		t.Fatal("CAS did not refetch")
	}
	store.lostResponse = true
	if guard.Reserve(ctx, key(1)) == nil {
		t.Fatal("lost update response allowed encryption")
	}
	if err := guard.Reserve(ctx, key(1)); err != nil || readState(t, store).Uses[0].Encryptions != 2 {
		t.Fatal("uncertain reservation was reused")
	}
	store.conflicts = maximumAttempts + 2
	before := store.gets
	if guard.Reserve(ctx, key(1)) == nil || store.gets-before != maximumAttempts {
		t.Fatal("CAS retry unbounded")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	before = store.gets
	if !errors.Is(guard.Reserve(cancelled, key(1)), context.Canceled) || store.gets != before {
		t.Fatal("cancelled operation reached Kubernetes")
	}
	store.conflicts = 0
	rotated := manifest(2, key(1), key(2))
	state := guardState{Version: 1, Manifest: &rotated, Uses: []keyUse{{ID: key(1).ID, Generation: 1, Encryptions: 2}, {ID: key(2).ID, Generation: 2}}}
	store.raceState = encodeState(t, state)
	if guard.Reserve(ctx, key(1)) == nil || readState(t, store).Uses[1].Encryptions != 0 {
		t.Fatal("CAS retry ignored new current key")
	}
}

// TestGuardReadinessUsesDurableBudgetWithoutReservation requires no writes or counter spend for readiness
// and checks genesis, exhausted and rotated-key refusal using shared synthetic state.
// TestGuardReadinessUsesDurableBudgetWithoutReservation требует отсутствия writes и counter spend для
// readiness и проверяет отказы genesis, exhausted и rotated key на shared synthetic state.
func TestGuardReadinessUsesDurableBudgetWithoutReservation(t *testing.T) {
	ctx := context.Background()
	store := newStore()
	guard := fixtureGuard(t, store)
	if guard.CheckCurrent(ctx, key(1)) == nil {
		t.Fatal("genesis became ready")
	}
	if err := guard.Observe(ctx, manifest(1, key(1))); err != nil {
		t.Fatal(err)
	}
	before := store.updates
	for range 3 {
		if err := guard.CheckCurrent(ctx, key(1)); err != nil {
			t.Fatal(err)
		}
	}
	if store.updates != before || readState(t, store).Uses[0].Encryptions != 0 {
		t.Fatal("readiness consumed encryption budget")
	}
	state := readState(t, store)
	state.Uses[0].Encryptions = MaximumEncryptions
	store.object.Data[StateKey] = encodeState(t, state)
	if fixtureGuard(t, store).CheckCurrent(ctx, key(1)) == nil {
		t.Fatal("exhausted key remained ready")
	}
	if err := guard.Observe(ctx, manifest(2, key(1), key(2))); err != nil {
		t.Fatal(err)
	}
	if guard.CheckCurrent(ctx, key(1)) == nil || guard.CheckCurrent(ctx, key(2)) != nil {
		t.Fatal("readiness ignored current key rotation")
	}
}

// TestGuardRejectsCorruptOrWrongObject mutates exact object labels/identity/shape or strict JSON and
// requires failure before Update, including a missing guard rather than implicit creation.
// TestGuardRejectsCorruptOrWrongObject меняет exact object labels/identity/shape либо strict JSON и
// требует отказа до Update, включая missing guard вместо неявного создания.
func TestGuardRejectsCorruptOrWrongObject(t *testing.T) {
	for name, mutate := range map[string]func(*corev1.ConfigMap){
		"namespace": func(v *corev1.ConfigMap) { v.Namespace = "foreign" }, "name": func(v *corev1.ConfigMap) { v.Name = "foreign" }, "uid": func(v *corev1.ConfigMap) { v.UID = "" }, "resource version": func(v *corev1.ConfigMap) { v.ResourceVersion = "" },
		"owner": func(v *corev1.ConfigMap) { v.Labels[OwnerLabel] = "foreign" }, "purpose": func(v *corev1.ConfigMap) { delete(v.Labels, PurposeLabel) }, "deleting": func(v *corev1.ConfigMap) { v.DeletionTimestamp = &metav1.Time{} }, "immutable": func(v *corev1.ConfigMap) { yes := true; v.Immutable = &yes },
		"binary data": func(v *corev1.ConfigMap) { v.BinaryData = map[string][]byte{"private": {1}} }, "extra data": func(v *corev1.ConfigMap) { v.Data["other"] = "x" }, "missing state": func(v *corev1.ConfigMap) { v.Data = nil },
		"unknown JSON": func(v *corev1.ConfigMap) { v.Data[StateKey] = `{"v":1,"manifest":null,"uses":[],"unknown":1}` }, "duplicate JSON": func(v *corev1.ConfigMap) { v.Data[StateKey] = `{"v":2,"v":1,"manifest":null,"uses":[]}` }, "null uses": func(v *corev1.ConfigMap) { v.Data[StateKey] = `{"v":1,"manifest":null,"uses":null}` }, "oversize": func(v *corev1.ConfigMap) { v.Data[StateKey] = strings.Repeat("x", maximumStateBytes+1) },
	} {
		t.Run(name, func(t *testing.T) {
			store := newStore()
			mutate(store.object)
			guard := fixtureGuard(t, store)
			if guard.Observe(context.Background(), manifest(1, key(1))) == nil || store.updates != 0 {
				t.Fatal("invalid guard reached update")
			}
		})
	}
	store := newStore()
	store.object = nil
	if fixtureGuard(t, store).Observe(context.Background(), manifest(1, key(1))) == nil || store.updates != 0 {
		t.Fatal("missing guard accepted")
	}
}

// TestGuardRejectsMalformedManifestAndCounters rejects malformed manifest before Get and then corrupted
// IDs/generations/counts/budgets before a reservation can succeed.
// TestGuardRejectsMalformedManifestAndCounters отклоняет malformed manifest до Get, затем corrupted
// IDs/generations/counts/budgets до успешного reservation.
func TestGuardRejectsMalformedManifestAndCounters(t *testing.T) {
	for name, mutate := range map[string]func(*value.DraftKeyManifest){"digest": func(v *value.DraftKeyManifest) { v.Digest = strings.Repeat("b", 64) }, "current": func(v *value.DraftKeyManifest) { v.Current = key(2) }, "unsorted": func(v *value.DraftKeyManifest) { v.Keys = []value.DraftEncryptionKey{key(2), key(1)} }, "empty": func(v *value.DraftKeyManifest) { v.Keys = nil }, "generation": func(v *value.DraftKeyManifest) { v.Keys[0].Generation = 0 }, "malformed ID": func(v *value.DraftKeyManifest) { v.Keys[0].ID = "private" }} {
		t.Run(name, func(t *testing.T) {
			store := newStore()
			candidate := manifest(1, key(1))
			mutate(&candidate)
			if fixtureGuard(t, store).Observe(context.Background(), candidate) == nil || store.gets != 0 {
				t.Fatal("bad manifest reached Kubernetes")
			}
		})
	}
	for _, corrupt := range []func(*guardState){func(s *guardState) { s.Uses[0].Encryptions = MaximumEncryptions + 1 }, func(s *guardState) { s.Uses[0].ID = key(2).ID }, func(s *guardState) { s.Uses = nil }, func(s *guardState) { s.Uses[0].Generation = 2 }} {
		store := newStore()
		guard := fixtureGuard(t, store)
		if err := guard.Observe(context.Background(), manifest(1, key(1))); err != nil {
			t.Fatal(err)
		}
		state := readState(t, store)
		corrupt(&state)
		store.object.Data[StateKey] = encodeState(t, state)
		if guard.Reserve(context.Background(), key(1)) == nil {
			t.Fatal("corrupt counter accepted")
		}
	}
}
