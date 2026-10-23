package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
)

// testNamespace isolates fake runtime-secret objects in the in-memory client; it names no live namespace
// used by these tests.
// testNamespace изолирует fake runtime-secret объекты в клиенте в памяти; эти тесты не используют живой
// namespace.
const testNamespace = "test-namespace"

// TestCreateReadbackAndDeleteExact exercises immutable create, metadata, exact resolve and plaintext
// copy, then checks delete options and idempotent absence using a fake client.
// TestCreateReadbackAndDeleteExact проверяет immutable создание, metadata, точное разрешение и копию
// plaintext, затем delete options и идемпотентное отсутствие через fake клиент.
func TestCreateReadbackAndDeleteExact(t *testing.T) {
	t.Parallel()
	store, client := newTestStore(t)
	effect, value := testEffect("secop_test123456789", 7, "sec_test123456789", 1, "synthetic-secret-value-for-tests")

	materialized, err := store.CreateImmutableForEffect(context.Background(), effect, value)
	if err != nil {
		t.Fatal(err)
	}
	object, err := client.CoreV1().Secrets(testNamespace).Get(context.Background(), materialized.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if object.Annotations[operationRefAnnotation] != effect.OperationRef ||
		object.Annotations[claimGenerationAnnotation] != "7" ||
		object.Annotations[secretRefAnnotation] != effect.SecretRef ||
		object.Annotations[revisionAnnotation] != "1" ||
		object.Annotations[digestAnnotation] != effect.ContentSHA256 {
		t.Fatalf("unexpected Kubernetes Secret annotations: %#v", object.Annotations)
	}
	readback, err := store.ReadbackExact(context.Background(), materialized)
	if err != nil || !sameMaterialization(readback, materialized) {
		t.Fatalf("exact readback: %#v err=%v", readback, err)
	}
	descriptor := exactDescriptor(materialized)
	resolved, err := store.ResolveExact(context.Background(), descriptor)
	if err != nil || !sameMaterialization(resolved, materialized) {
		t.Fatalf("resolve exact immutable Secret: materialization=%#v err=%v", resolved, err)
	}
	resolved, read, err := store.ReadExactValue(context.Background(), descriptor)
	if err != nil || !sameMaterialization(resolved, materialized) || string(read) != string(value) {
		t.Fatalf("read exact immutable Secret: value=%q materialization=%#v err=%v", read, resolved, err)
	}
	if err := store.DeleteExact(context.Background(), materialized); err != nil {
		t.Fatal(err)
	}
	assertDeletePreconditions(t, client, materialized)
	if err := store.DeleteExact(context.Background(), materialized); err != nil {
		t.Fatalf("NotFound delete must be idempotent: %v", err)
	}
}

// TestCreateImmutableIsIdempotentOnlyForSameEffect accepts exact retries and rejects changed operation,
// claim generation or content at the same deterministic name.
// TestCreateImmutableIsIdempotentOnlyForSameEffect допускает точный повтор и отклоняет другие operation,
// claim generation или содержимое по тому же детерминированному имени.
func TestCreateImmutableIsIdempotentOnlyForSameEffect(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	effect, value := testEffect("secop_idempotent123", 3, "sec_idempotent123", 1, "synthetic-value-a")
	first, err := store.CreateImmutableForEffect(context.Background(), effect, value)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateImmutableForEffect(context.Background(), effect, value)
	if err != nil || !sameMaterialization(second, first) {
		t.Fatalf("exact effect retry must be idempotent: %#v err=%v", second, err)
	}

	otherOperation := effect
	otherOperation.OperationRef = "secop_other123456"
	if _, err := store.CreateImmutableForEffect(context.Background(), otherOperation, value); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("different operation must conflict, got %v", err)
	}
	otherGeneration := effect
	otherGeneration.ClaimGeneration++
	if _, err := store.CreateImmutableForEffect(context.Background(), otherGeneration, value); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("different claim generation must conflict, got %v", err)
	}
	otherDigest, otherValue := testEffect(effect.OperationRef, effect.ClaimGeneration, effect.SecretRef, effect.Revision, "synthetic-value-b")
	if _, err := store.CreateImmutableForEffect(context.Background(), otherDigest, otherValue); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("different content digest must conflict, got %v", err)
	}
}

// TestLookupExpectedEffectDistinguishesExactMissingAndConflict verifies successful metadata lookup,
// absent deterministic name and rejection of a changed claim fence.
// TestLookupExpectedEffectDistinguishesExactMissingAndConflict проверяет успешный поиск metadata,
// отсутствие детерминированного имени и отказ при другом поколении claim.
func TestLookupExpectedEffectDistinguishesExactMissingAndConflict(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	effect, value := testEffect("secop_lookup12345", 6, "sec_lookup123456", 4, "synthetic-lookup-value")
	created, err := store.CreateImmutableForEffect(context.Background(), effect, value)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := store.LookupExpectedEffect(context.Background(), effect)
	if err != nil || !sameMaterialization(resolved, created) {
		t.Fatalf("exact effect не найден: materialization=%#v err=%v", resolved, err)
	}

	missing := effect
	missing.SecretRef = "sec_missing123456"
	if _, err := store.LookupExpectedEffect(context.Background(), missing); !errors.Is(err, ErrMaterializationNotFound) {
		t.Fatalf("отсутствующий effect должен вернуть NotFound, получено %v", err)
	}

	conflict := effect
	conflict.ClaimGeneration++
	if _, err := store.LookupExpectedEffect(context.Background(), conflict); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("чужой fence должен вернуть conflict, получено %v", err)
	}
}

// TestCreateImmutableRejectsForeignSecret preloads an unmanaged colliding object and verifies that
// neither create nor exact delete adopts or removes it.
// TestCreateImmutableRejectsForeignSecret заранее помещает неуправляемый объект с совпадающим именем и
// проверяет, что create и exact delete не принимают его и не удаляют.
func TestCreateImmutableRejectsForeignSecret(t *testing.T) {
	t.Parallel()
	effect, value := testEffect("secop_foreign1234", 1, "sec_foreign123456", 2, "synthetic-value")
	name, err := runtimesecret.VersionedKubernetesName(effect.SecretRef, effect.Revision)
	if err != nil {
		t.Fatal(err)
	}
	foreign := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: testNamespace, UID: types.UID("foreign-uid"), ResourceVersion: "91",
	}, Data: map[string][]byte{effect.Key: value}}
	store, client := newTestStore(t, foreign)

	if _, err := store.CreateImmutableForEffect(context.Background(), effect, value); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("foreign Secret must conflict, got %v", err)
	}
	expected := materializationForEffect(effect, name, "foreign-uid", "91")
	if err := store.DeleteExact(context.Background(), expected); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("foreign Secret delete must fail closed, got %v", err)
	}
	if _, err := client.CoreV1().Secrets(testNamespace).Get(context.Background(), name, metav1.GetOptions{}); err != nil {
		t.Fatalf("foreign Secret was removed: %v", err)
	}
}

// TestListManagedReturnsMetadataOnlyAndSorted verifies label filtering and stable metadata order for two
// managed objects beside an unmanaged Secret; the fake API still stores full bytes.
// TestListManagedReturnsMetadataOnlyAndSorted проверяет фильтр метки и устойчивый порядок metadata двух
// управляемых объектов рядом с неуправляемым Secret; fake API по-прежнему хранит полные байты.
func TestListManagedReturnsMetadataOnlyAndSorted(t *testing.T) {
	t.Parallel()
	store, client := newTestStore(t)
	secondEffect, secondValue := testEffect("secop_second12345", 2, "sec_zeta12345678", 3, "second-value")
	firstEffect, firstValue := testEffect("secop_first123456", 4, "sec_alpha1234567", 2, "first-value")
	second, err := store.CreateImmutableForEffect(context.Background(), secondEffect, secondValue)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateImmutableForEffect(context.Background(), firstEffect, firstValue)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CoreV1().Secrets(testNamespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "foreign-secret", Namespace: testNamespace},
		Data:       map[string][]byte{"value": []byte("must-not-be-listed")},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	listed, err := store.ListManaged(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || !sameMaterialization(listed[0], first) || !sameMaterialization(listed[1], second) {
		t.Fatalf("unexpected managed materializations: %#v", listed)
	}
}

// TestReadbackRejectsIntegrityAndStaleIdentity rejects stale UID and resourceVersion before delete, then
// mutates the fake immutable object to verify readback and plaintext reads detect digest corruption.
// TestReadbackRejectsIntegrityAndStaleIdentity отклоняет устаревшие UID и resourceVersion до удаления,
// затем изменяет fake immutable объект и проверяет обнаружение повреждения digest при чтении metadata и
// plaintext.
func TestReadbackRejectsIntegrityAndStaleIdentity(t *testing.T) {
	t.Parallel()
	store, client := newTestStore(t)
	effect, value := testEffect("secop_readback123", 9, "sec_readback12345", 5, "synthetic-original-value")
	materialized, err := store.CreateImmutableForEffect(context.Background(), effect, value)
	if err != nil {
		t.Fatal(err)
	}

	staleUID := materialized
	staleUID.UID = "stale-uid"
	if _, err := store.ReadbackExact(context.Background(), staleUID); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("stale UID must fail closed, got %v", err)
	}
	staleRV := materialized
	staleRV.ResourceVersion = "stale-rv"
	if _, err := store.ReadbackExact(context.Background(), staleRV); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("stale resourceVersion must fail closed, got %v", err)
	}
	if err := store.DeleteExact(context.Background(), staleUID); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("stale UID delete must fail closed, got %v", err)
	}

	object, err := client.CoreV1().Secrets(testNamespace).Get(context.Background(), materialized.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	object.Data[effect.Key] = []byte("synthetic-tampered-value")
	if _, err := client.CoreV1().Secrets(testNamespace).Update(context.Background(), object, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadbackExact(context.Background(), materialized); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("tampered value must fail closed, got %v", err)
	}
	if _, _, err := store.ReadExactValue(context.Background(), exactDescriptor(materialized)); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("expected exact integrity error, got %v", err)
	}
}

// TestExactReadRejectsForeignOperationMetadata removes the operation annotation in the fake store and
// verifies that otherwise exact plaintext reads fail closed.
// TestExactReadRejectsForeignOperationMetadata удаляет annotation операции в fake хранилище и проверяет
// закрытый отказ формально точного чтения plaintext.
func TestExactReadRejectsForeignOperationMetadata(t *testing.T) {
	t.Parallel()
	store, client := newTestStore(t)
	effect, value := testEffect("secop_exact123456", 5, "sec_exact1234567", 3, "synthetic-value")
	materialized, err := store.CreateImmutableForEffect(context.Background(), effect, value)
	if err != nil {
		t.Fatal(err)
	}
	object, err := client.CoreV1().Secrets(testNamespace).Get(context.Background(), materialized.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	object.Annotations[operationRefAnnotation] = ""
	if _, err := client.CoreV1().Secrets(testNamespace).Update(context.Background(), object, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadExactValue(context.Background(), exactDescriptor(materialized)); !errors.Is(err, ErrMaterializationConflict) {
		t.Fatalf("missing operation metadata must fail closed, got %v", err)
	}
}

// TestListManagedRejectsMalformedManagedObject verifies that a managed immutable object lacking required
// operation annotations fails the whole reconciliation list.
// TestListManagedRejectsMalformedManagedObject проверяет, что управляемый immutable объект без
// обязательных annotations операции прерывает весь список для сверки.
func TestListManagedRejectsMalformedManagedObject(t *testing.T) {
	t.Parallel()
	immutable := true
	malformed := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "runtime-secret-malformed-r1", Namespace: testNamespace,
			UID: types.UID("malformed-uid"), ResourceVersion: "12",
			Labels: map[string]string{managedLabel: "true"},
		},
		Immutable: &immutable,
		Type:      corev1.SecretTypeOpaque,
	}
	store, _ := newTestStore(t, malformed)
	if _, err := store.ListManaged(context.Background()); err == nil {
		t.Fatal("malformed managed Secret must fail the recovery scan")
	}
}

// newTestStore seeds an in-memory Kubernetes tracker and assigns synthetic UID and increasing
// resourceVersion on Secret create. It does not emulate API validation, immutability or server-side
// delete precondition enforcement.
// newTestStore заполняет Kubernetes tracker в памяти и назначает синтетические UID и возрастающий
// resourceVersion при создании Secret. Проверки API, immutability и серверное применение delete
// preconditions не эмулируются.
func newTestStore(t *testing.T, objects ...runtime.Object) (*Store, *fake.Clientset) {
	t.Helper()
	client := fake.NewSimpleClientset(objects...)
	var resourceVersion atomic.Int64
	resourceVersion.Store(100)
	client.Fake.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createAction, ok := action.(k8stesting.CreateAction)
		if !ok {
			return false, nil, nil
		}
		secret, ok := createAction.GetObject().(*corev1.Secret)
		if !ok {
			return false, nil, nil
		}
		created := secret.DeepCopy()
		created.UID = types.UID("uid-" + created.Name)
		created.ResourceVersion = strconv.FormatInt(resourceVersion.Add(1), 10)
		err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), created, created.Namespace)
		return true, created, err
	})
	store, err := New(client, testNamespace)
	if err != nil {
		t.Fatal(err)
	}
	return store, client
}

// testEffect creates an immediate materialization intent and synthetic bytes with a matching SHA-256 for
// caller-supplied operation, claim and revision.
// testEffect создаёт immediate намерение материализации и синтетические байты с соответствующим SHA-256
// для переданных operation, claim и revision.
func testEffect(operationRef string, generation int64, secretRef string, revision int64, value string) (MaterializationEffect, []byte) {
	content := []byte(value)
	digest := sha256.Sum256(content)
	return MaterializationEffect{
		WorkKind:        WorkKindImmediate,
		OperationRef:    operationRef,
		ClaimGeneration: generation,
		SecretRef:       secretRef,
		Key:             "value",
		Revision:        revision,
		ContentSHA256:   hex.EncodeToString(digest[:]),
	}, content
}

// materializationForEffect combines a synthetic effect with supplied name and API identity pins in
// testNamespace without reading or validating a Kubernetes object.
// materializationForEffect объединяет синтетический эффект с заданными именем и привязками API identity
// в testNamespace без чтения или проверки объекта Kubernetes.
func materializationForEffect(effect MaterializationEffect, name, uid, resourceVersion string) Materialization {
	return Materialization{
		WorkKind:        effect.WorkKind,
		Namespace:       testNamespace,
		Name:            name,
		OperationRef:    effect.OperationRef,
		ClaimGeneration: effect.ClaimGeneration,
		SecretRef:       effect.SecretRef,
		Key:             effect.Key,
		Revision:        effect.Revision,
		UID:             uid,
		ResourceVersion: resourceVersion,
		ContentSHA256:   effect.ContentSHA256,
	}
}

// exactDescriptor copies only descriptor pins from a test materialization, intentionally excluding the
// original operation and claim fence.
// exactDescriptor копирует только привязки descriptor из тестовой materialization, намеренно исключая
// исходные operation и claim generation.
func exactDescriptor(materialized Materialization) ExactDescriptor {
	return ExactDescriptor{
		Namespace: materialized.Namespace, Name: materialized.Name, SecretRef: materialized.SecretRef,
		Key: materialized.Key, Revision: materialized.Revision, UID: materialized.UID,
		ResourceVersion: materialized.ResourceVersion, ContentSHA256: materialized.ContentSHA256,
	}
}

// assertDeletePreconditions finds the recorded Secret delete action and checks its UID and
// resourceVersion options; it does not prove that a real API server enforced those options.
// assertDeletePreconditions находит записанное действие удаления Secret и проверяет options UID и
// resourceVersion; применение этих options реальным API server здесь не доказывается.
func assertDeletePreconditions(t *testing.T, client *fake.Clientset, expected Materialization) {
	t.Helper()
	for _, action := range client.Actions() {
		deleteAction, ok := action.(k8stesting.DeleteAction)
		if !ok || action.GetResource().Resource != "secrets" || deleteAction.GetName() != expected.Name {
			continue
		}
		preconditions := deleteAction.GetDeleteOptions().Preconditions
		if preconditions == nil || preconditions.UID == nil || string(*preconditions.UID) != expected.UID ||
			preconditions.ResourceVersion == nil || *preconditions.ResourceVersion != expected.ResourceVersion {
			t.Fatalf("unexpected delete preconditions: %#v", preconditions)
		}
		return
	}
	t.Fatal("Secret delete action was not recorded")
}
