package projectpurge

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

// sourceFixture provides explicit synthetic inventory/ownership answers and records finalization for purge unit tests; it performs no SQL work or authorization.
// sourceFixture задаёт явные синтетические ответы inventory/ownership и записывает финализацию в unit-тестах очистки; SQL и авторизацию не выполняет.
type sourceFixture struct {
	items                                []platform.ProjectPurgeExternalItem
	shared, finalized                    bool
	archiveOwned                         bool
	subjectArtifactOwned, globalPVCOwned bool
}

// ClaimProjectPurgeRecovery returns no recovery work in ordinary purge tests so storage/PVC scenarios are isolated from the recovery queue.
// ClaimProjectPurgeRecovery возвращает пустую очередь восстановления в обычных тестах очистки для изоляции object/PVC сценариев.
func (*sourceFixture) ClaimProjectPurgeRecovery(context.Context, string, int32) ([]platform.ProjectPurgeRecoveryClaim, error) {
	return nil, nil
}

// PinProjectPurgeRecovery is a no-op default fixture implementation; tests of durable recovery pinning override it in recoverySourceFixture.
// PinProjectPurgeRecovery является пустой реализацией fixture по умолчанию; тесты устойчивого recovery pin переопределяют её в recoverySourceFixture.
func (*sourceFixture) PinProjectPurgeRecovery(context.Context, string, platform.ProjectPurgeRecoveryClaim, objectstorage.Receipt) error {
	return nil
}

// FinishProjectPurgeRecovery is the unused successful recovery receipt stub for ordinary purge tests, not evidence of an actual owner commit.
// FinishProjectPurgeRecovery является неиспользуемой успешной заглушкой квитанции восстановления в обычных тестах, а не доказательством реального commit владельца.
func (*sourceFixture) FinishProjectPurgeRecovery(context.Context, string, platform.ProjectPurgeRecoveryClaim, string) error {
	return nil
}

// PromoteDueProjectPurges returns success without altering fixture candidates; these tests invoke the candidate processor directly.
// PromoteDueProjectPurges возвращает успех без изменения кандидатов fixture; эти тесты вызывают обработчик кандидата напрямую.
func (*sourceFixture) PromoteDueProjectPurges(context.Context, int32) error { return nil }

// ListPendingProjectPurges returns an empty candidate page because each unit test supplies its exact candidate directly to process.
// ListPendingProjectPurges возвращает пустую страницу кандидатов, поскольку каждый unit-тест передаёт точного кандидата прямо в process.
func (*sourceFixture) ListPendingProjectPurges(context.Context, int32) ([]platform.ProjectPurgeCandidate, error) {
	return nil, nil
}

// ProjectPurgeInventory returns the fixture-owned inventory slice unchanged so the test can assert exactly which objects/PVCs were processed.
// ProjectPurgeInventory возвращает принадлежащий fixture срез inventory без изменения для проверки точного набора обработанных объектов/PVC.
func (source *sourceFixture) ProjectPurgeInventory(context.Context, platform.ProjectPurgeCandidate) ([]platform.ProjectPurgeExternalItem, error) {
	return source.items, nil
}

// ProjectPurgeObjectIsShared returns the configured shared-object answer without inferring it from key prefixes or object names.
// ProjectPurgeObjectIsShared возвращает настроенный ответ об общем объекте без вывода из префикса ключа и имени объекта.
func (source *sourceFixture) ProjectPurgeObjectIsShared(context.Context, platform.ProjectPurgeCandidate, string) (bool, error) {
	return source.shared, nil
}

// ProjectPurgeArchiveIsOwned returns the explicit archive-lineage fixture answer, keeping archive-store ownership separate from project key syntax.
// ProjectPurgeArchiveIsOwned возвращает явный ответ fixture об архивном lineage, отделяя владение архивным хранилищем от синтаксиса ключа проекта.
func (source *sourceFixture) ProjectPurgeArchiveIsOwned(context.Context, platform.ProjectPurgeCandidate, platform.ProjectPurgeExternalItem) (bool, error) {
	return source.archiveOwned, nil
}

// ProjectPurgeSubjectArtifactIsOwned returns the explicit moved-subject-artifact lineage answer used to test the fail-closed ownership path.
// ProjectPurgeSubjectArtifactIsOwned возвращает явный ответ о lineage перенесённого артефакта субъекта для проверки закрытого отказа по владению.
func (source *sourceFixture) ProjectPurgeSubjectArtifactIsOwned(context.Context, platform.ProjectPurgeCandidate, platform.ProjectPurgeExternalItem) (bool, error) {
	return source.subjectArtifactOwned, nil
}

// ProjectPurgeGlobalPVCIsOwned returns the configured moved-system-session ownership answer independently of PVC metadata hashes.
// ProjectPurgeGlobalPVCIsOwned возвращает настроенный ответ о владении перенесённой системной сессией независимо от хешей метаданных PVC.
func (source *sourceFixture) ProjectPurgeGlobalPVCIsOwned(context.Context, platform.ProjectPurgeCandidate, string) (bool, error) {
	return source.globalPVCOwned, nil
}

// FinalizeProjectPurge rejects a changed inventory length and records that finalization was reached; the fixture does not emulate the production transaction or compare every inventory field.
// FinalizeProjectPurge отклоняет изменение длины inventory и записывает достижение финализации; fixture не имитирует production-транзакцию и не сравнивает каждое поле inventory.
func (source *sourceFixture) FinalizeProjectPurge(_ context.Context, _ platform.ProjectPurgeCandidate, items []platform.ProjectPurgeExternalItem) error {
	if len(items) != len(source.items) {
		return errors.New("project purge inventory changed")
	}
	source.finalized = true
	return nil
}

// TestProcessorDeletesExactExternalResources checks removal of the exact object version and marked session PVC before the fake owner finalizes the project.
// TestProcessorDeletesExactExternalResources проверяет удаление точной версии объекта и помеченного PVC сессии до финализации проекта подставным владельцем.
func TestProcessorDeletesExactExternalResources(t *testing.T) {
	ctx := t.Context()
	candidate := platform.ProjectPurgeCandidate{OrganizationRef: "org_fixture", ProjectRef: "prj_fixture"}
	sessionRef := "ses_fixture_01"
	pvcName, err := runtimecontract.SessionPVCName(sessionRef)
	if err != nil {
		t.Fatal(err)
	}
	key := "organizations/org_fixture/projects/prj_fixture/artifacts/art_fixture/digest"
	objects := objectstoragetest.New()
	receipt, err := objects.Put(ctx, objectstorage.PutInput{Key: key, MediaType: "text/plain", Digest: "sha256:" + strings.Repeat("a", 64),
		SizeBytes: 1, Body: strings.NewReader("x")})
	if err != nil {
		t.Fatal(err)
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: "kodex-runtime", UID: types.UID("exact-pvc-uid"),
		Labels:      map[string]string{managedLabel: "true", sessionHashLabel: shortHash(sessionRef)},
		Annotations: map[string]string{organizationHashAnnotation: shortHash(candidate.OrganizationRef), projectHashAnnotation: shortHash(candidate.ProjectRef)}}}
	client := fake.NewSimpleClientset(pvc)
	source := &sourceFixture{items: []platform.ProjectPurgeExternalItem{
		{Kind: "OBJECT", Target: key, Version: receipt.VersionID},
		{Kind: "PVC", Target: pvcName, Version: sessionRef},
	}}
	processor := New(source, objects, nil, client, "kodex-runtime")
	if err := processor.process(ctx, candidate); err != nil || !source.finalized {
		t.Fatalf("external cleanup did not finalize: finalized=%t err=%v", source.finalized, err)
	}
	if _, err := objects.Head(ctx, key, receipt.VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatalf("object remains after purge: %v", err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("kodex-runtime").Get(ctx, pvcName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("PVC remains after purge: %v", err)
	}
}

// TestProcessorRejectsSharedObjectAndForeignPVC proves that shared objects and differently owned PVCs survive and block finalization under synthetic object storage/Kubernetes clients.
// TestProcessorRejectsSharedObjectAndForeignPVC доказывает сохранение общих объектов и чужих PVC с блокировкой финализации на синтетических клиентах хранилища/Kubernetes.
func TestProcessorRejectsSharedObjectAndForeignPVC(t *testing.T) {
	ctx := t.Context()
	candidate := platform.ProjectPurgeCandidate{OrganizationRef: "org_fixture", ProjectRef: "prj_fixture"}
	key := "organizations/org_fixture/projects/prj_fixture/artifacts/art_fixture/digest"
	objects := objectstoragetest.New()
	receipt, err := objects.Put(ctx, objectstorage.PutInput{Key: key, MediaType: "text/plain", Digest: "sha256:" + strings.Repeat("a", 64),
		SizeBytes: 1, Body: strings.NewReader("x")})
	if err != nil {
		t.Fatal(err)
	}
	source := &sourceFixture{shared: true, items: []platform.ProjectPurgeExternalItem{{Kind: "OBJECT", Target: key, Version: receipt.VersionID}}}
	processor := New(source, objects, nil, fake.NewSimpleClientset(), "kodex-runtime")
	if err := processor.process(ctx, candidate); err == nil || source.finalized {
		t.Fatal("shared object was purged")
	}
	if _, err := objects.Head(ctx, key, receipt.VersionID); err != nil {
		t.Fatalf("shared object was deleted: %v", err)
	}
	sessionRef := "ses_fixture_01"
	pvcName, _ := runtimecontract.SessionPVCName(sessionRef)
	foreign := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: "kodex-runtime", UID: types.UID("foreign-pvc-uid"),
		Labels:      map[string]string{managedLabel: "true", sessionHashLabel: shortHash(sessionRef)},
		Annotations: map[string]string{organizationHashAnnotation: shortHash(candidate.OrganizationRef), projectHashAnnotation: shortHash("another-project")}}}
	client := fake.NewSimpleClientset(foreign)
	source.items = []platform.ProjectPurgeExternalItem{{Kind: "PVC", Target: pvcName, Version: sessionRef}}
	processor = New(source, objects, nil, client, "kodex-runtime")
	if err := processor.process(ctx, candidate); err == nil || source.finalized {
		t.Fatal("foreign PVC was accepted for project purge")
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("kodex-runtime").Get(ctx, pvcName, metav1.GetOptions{}); err != nil {
		t.Fatalf("foreign PVC was deleted: %v", err)
	}
}

// TestProcessorWaitsForPodEvenWhenPVCIsAbsent ensures a nonterminal Pod referencing an inventory PVC blocks cleanup even when the Kubernetes PVC itself is absent.
// TestProcessorWaitsForPodEvenWhenPVCIsAbsent проверяет блокировку очистки неитоговым Pod, ссылающимся на PVC inventory, даже если сам PVC Kubernetes отсутствует.
func TestProcessorWaitsForPodEvenWhenPVCIsAbsent(t *testing.T) {
	ctx := t.Context()
	candidate := platform.ProjectPurgeCandidate{OrganizationRef: "org_fixture", ProjectRef: "prj_fixture"}
	sessionRef := "ses_fixture_01"
	pvcName, _ := runtimecontract.SessionPVCName(sessionRef)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "active", Namespace: "kodex-runtime"},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "session", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName},
		}}}}}
	source := &sourceFixture{items: []platform.ProjectPurgeExternalItem{{Kind: "PVC", Target: pvcName, Version: sessionRef}}}
	processor := New(source, objectstoragetest.New(), nil, fake.NewSimpleClientset(pod), "kodex-runtime")
	if err := processor.process(ctx, candidate); err == nil || source.finalized {
		t.Fatal("active PVC consumer was ignored")
	}
}

// TestArchiveCleanupUsesExactArchiveStoreAndServerLineage checks project/system archive keys with required server lineage and an actual archive store, verifies idempotent replay and proves the same key in the artifact store remains untouched.
// TestArchiveCleanupUsesExactArchiveStoreAndServerLineage проверяет project/system архивные ключи с обязательным серверным lineage и архивным хранилищем, идемпотентный повтор и сохранение одноимённого ключа в хранилище артефактов.
func TestArchiveCleanupUsesExactArchiveStoreAndServerLineage(t *testing.T) {
	ctx := t.Context()
	candidate := platform.ProjectPurgeCandidate{OrganizationRef: "org_fixture", ProjectRef: "prj_fixture"}
	for _, prefix := range []string{"prj_fixture", "_system"} {
		t.Run(prefix, func(t *testing.T) {
			key := "session-archive/v1/org_fixture/" + prefix + "/ses_fixture/g1/sat_fixture-a1.tar"
			artifacts, archives := objectstoragetest.New(), objectstoragetest.New()
			put := func(store objectstorage.Store) objectstorage.Receipt {
				t.Helper()
				receipt, err := store.Put(ctx, objectstorage.PutInput{Key: key, MediaType: "application/octet-stream", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1, Body: strings.NewReader("x")})
				if err != nil {
					t.Fatal(err)
				}
				return receipt
			}
			other, owned := put(artifacts), put(archives)
			source := &sourceFixture{archiveOwned: true, items: []platform.ProjectPurgeExternalItem{{Kind: "OBJECT", Target: key, Version: owned.VersionID}}}
			for _, processor := range []*Processor{
				New(source, artifacts, nil, fake.NewSimpleClientset(), "kodex-runtime"),
				New(&sourceFixture{items: source.items}, artifacts, archives, fake.NewSimpleClientset(), "kodex-runtime"),
			} {
				if processor.process(ctx, candidate) == nil {
					t.Fatal("unconfirmed archive ownership/store was admitted")
				}
			}
			processor := New(source, artifacts, archives, fake.NewSimpleClientset(), "kodex-runtime")
			for range 2 {
				if err := processor.process(ctx, candidate); err != nil || !source.finalized {
					t.Fatalf("archive cleanup/replay: %v", err)
				}
			}
			if _, err := archives.Head(ctx, key, owned.VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
				t.Fatal("archive remained in its actual store")
			}
			if _, err := artifacts.Head(ctx, key, other.VersionID); err != nil {
				t.Fatal("wrong bucket was touched")
			}
		})
	}
}

// TestPVCConsumerInventoryIsReadOnceBeforeAnyDeletion counts one Pod-list snapshot for multiple inventory PVCs and one exact PVC GET per absent item, guarding against per-session list fanout.
// TestPVCConsumerInventoryIsReadOnceBeforeAnyDeletion считает один снимок списка Pods для нескольких PVC inventory и один точный GET PVC для отсутствующего элемента, защищая от отдельного обхода Pods для каждой сессии.
func TestPVCConsumerInventoryIsReadOnceBeforeAnyDeletion(t *testing.T) {
	client := fake.NewSimpleClientset()
	var items []platform.ProjectPurgeExternalItem
	for _, session := range []string{"ses_fixture_a", "ses_fixture_b", "ses_fixture_c"} {
		name, err := runtimecontract.SessionPVCName(session)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, platform.ProjectPurgeExternalItem{Kind: "PVC", Target: name, Version: session})
	}
	processor := New(&sourceFixture{items: items}, objectstoragetest.New(), nil, client, "kodex-runtime")
	if err := processor.process(t.Context(), platform.ProjectPurgeCandidate{OrganizationRef: "org_fixture", ProjectRef: "prj_fixture"}); err != nil {
		t.Fatal(err)
	}
	var lists, gets int
	for _, action := range client.Actions() {
		if action.GetVerb() == "list" && action.GetResource().Resource == "pods" {
			lists++
		}
		if action.GetVerb() == "get" && action.GetResource().Resource == "persistentvolumeclaims" {
			gets++
		}
	}
	if lists != 1 || gets != len(items) {
		t.Fatalf("unexpected API fanout: lists=%d gets=%d", lists, gets)
	}
}

// TestPVCConsumerPaginationBlocksBeforeObjectDeletion puts an active PVC consumer on the second fake Pod page and proves the cursor is followed before an earlier inventory object can be deleted.
// TestPVCConsumerPaginationBlocksBeforeObjectDeletion помещает активного потребителя PVC на вторую подставную страницу Pods и доказывает проход курсора до удаления предшествующего объекта inventory.
func TestPVCConsumerPaginationBlocksBeforeObjectDeletion(t *testing.T) {
	ctx := t.Context()
	session := "ses_fixture_paged"
	name, _ := runtimecontract.SessionPVCName(session)
	key := "organizations/org_fixture/projects/prj_fixture/artifacts/art_fixture/digest"
	objects := objectstoragetest.New()
	receipt, err := objects.Put(ctx, objectstorage.PutInput{Key: key, MediaType: "text/plain", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1, Body: strings.NewReader("x")})
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset()
	calls := 0
	client.PrependReactor("list", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		calls++
		options := action.(ktesting.ListActionImpl).GetListOptions()
		if options.Limit != 500 {
			t.Fatalf("unbounded consumer list: %d", options.Limit)
		}
		if calls == 1 {
			if options.Continue != "" {
				t.Fatal("unexpected initial cursor")
			}
			return true, &corev1.PodList{ListMeta: metav1.ListMeta{Continue: "next-page"}}, nil
		}
		if calls != 2 || options.Continue != "next-page" {
			t.Fatal("consumer cursor was not followed")
		}
		return true, &corev1.PodList{Items: []corev1.Pod{{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name}}}}}}}}, nil
	})
	source := &sourceFixture{items: []platform.ProjectPurgeExternalItem{{Kind: "OBJECT", Target: key, Version: receipt.VersionID}, {Kind: "PVC", Target: name, Version: session}}}
	processor := New(source, objects, nil, client, "kodex-runtime")
	if processor.process(ctx, platform.ProjectPurgeCandidate{OrganizationRef: "org_fixture", ProjectRef: "prj_fixture"}) == nil || source.finalized {
		t.Fatal("later-page active consumer was ignored")
	}
	if _, err := objects.Head(ctx, key, receipt.VersionID); err != nil {
		t.Fatal("object deleted before complete consumer preflight")
	}
}

// TestPVCReplacementIsNeverDeleted requires the original UID deletion precondition, replaces the PVC during Delete and proves cleanup fails without a second delete or finalization.
// TestPVCReplacementIsNeverDeleted требует UID precondition исходного удаления, подменяет PVC во время Delete и доказывает отказ очистки без второго удаления и финализации.
func TestPVCReplacementIsNeverDeleted(t *testing.T) {
	session := "ses_fixture_replacement"
	name, _ := runtimecontract.SessionPVCName(session)
	candidate := platform.ProjectPurgeCandidate{OrganizationRef: "org_fixture", ProjectRef: "prj_fixture"}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kodex-runtime", UID: "original", Labels: map[string]string{managedLabel: "true", sessionHashLabel: shortHash(session)}, Annotations: map[string]string{organizationHashAnnotation: shortHash(candidate.OrganizationRef), projectHashAnnotation: shortHash(candidate.ProjectRef)}}}
	client := fake.NewSimpleClientset(pvc)
	deletes := 0
	client.PrependReactor("delete", "persistentvolumeclaims", func(action ktesting.Action) (bool, runtime.Object, error) {
		deletes++
		options := action.(ktesting.DeleteAction).GetDeleteOptions()
		if options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != "original" {
			t.Fatal("missing exact UID precondition")
		}
		replacement := pvc.DeepCopy()
		replacement.UID = "replacement"
		if err := client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("persistentvolumeclaims"), replacement, "kodex-runtime"); err != nil {
			t.Fatal(err)
		}
		return true, nil, nil
	})
	source := &sourceFixture{items: []platform.ProjectPurgeExternalItem{{Kind: "PVC", Target: name, Version: session}}}
	if New(source, objectstoragetest.New(), nil, client, "kodex-runtime").process(t.Context(), candidate) == nil || source.finalized || deletes != 1 {
		t.Fatal("replacement did not close cleanup")
	}
	after, err := client.CoreV1().PersistentVolumeClaims("kodex-runtime").Get(t.Context(), name, metav1.GetOptions{})
	if err != nil || after.UID != "replacement" {
		t.Fatal("replacement was deleted")
	}
}

// TestFailureLogOmitsUnderlyingObjectAndProviderDetails checks that joined stage failures retain safe stage/project fields while synthetic object keys and credential-like cause text are absent from JSON logs.
// TestFailureLogOmitsUnderlyingObjectAndProviderDetails проверяет сохранение безопасных stage/project полей объединённого отказа при отсутствии синтетических ключей объектов и похожего на credential текста причины в JSON-журнале.
func TestFailureLogOmitsUnderlyingObjectAndProviderDetails(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logFailures(t.Context(), logger, errors.Join(&failure{stage: "object", projectRef: "prj_fixture", cause: errors.New("private-object-key credential-value")}))
	message := output.String()
	if !strings.Contains(message, `"safe_stage":"object"`) || !strings.Contains(message, `"project_ref":"prj_fixture"`) || strings.Contains(message, "private-object-key") || strings.Contains(message, "credential-value") {
		t.Fatal("unsafe or incomplete purge diagnostic")
	}
}

// TestMovedSystemSessionPVCRequiresServerLineage tests both outcomes of the server lineage proof for a PVC carrying the empty-project hash and checks that only the confirmed moved session is removed.
// TestMovedSystemSessionPVCRequiresServerLineage проверяет оба исхода серверного lineage для PVC с хешем пустого проекта и удаление лишь подтверждённой перенесённой сессии.
func TestMovedSystemSessionPVCRequiresServerLineage(t *testing.T) {
	for _, owned := range []bool{false, true} {
		candidate := platform.ProjectPurgeCandidate{OrganizationRef: "org_fixture", ProjectRef: "prj_fixture"}
		session := "ses_fixture_moved"
		name, _ := runtimecontract.SessionPVCName(session)
		pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kodex-runtime", UID: "exact-moved", Labels: map[string]string{managedLabel: "true", sessionHashLabel: shortHash(session)}, Annotations: map[string]string{organizationHashAnnotation: shortHash(candidate.OrganizationRef), projectHashAnnotation: shortHash("")}}}
		client := fake.NewSimpleClientset(pvc)
		source := &sourceFixture{globalPVCOwned: owned, items: []platform.ProjectPurgeExternalItem{{Kind: "PVC", Target: name, Version: session}}}
		err := New(source, objectstoragetest.New(), nil, client, "kodex-runtime").process(t.Context(), candidate)
		if owned != (err == nil && source.finalized) {
			t.Fatalf("global PVC lineage=%t cleanup=%v", owned, err)
		}
		_, readErr := client.CoreV1().PersistentVolumeClaims("kodex-runtime").Get(t.Context(), name, metav1.GetOptions{})
		if owned != apierrors.IsNotFound(readErr) {
			t.Fatal("unconfirmed global PVC removed or confirmed PVC retained")
		}
	}
}

// TestMovedSubjectArtifactRequiresExactServerLineage rejects an unconfirmed subject-prefixed object without deleting it, then accepts the explicitly confirmed moved artifact and its replay.
// TestMovedSubjectArtifactRequiresExactServerLineage отклоняет неподтверждённый объект с префиксом субъекта без удаления, затем принимает явно подтверждённый перенесённый артефакт и повтор очистки.
func TestMovedSubjectArtifactRequiresExactServerLineage(t *testing.T) {
	ctx := t.Context()
	key := "organizations/org_fixture/subjects/act_fixture/artifacts/art_fixture/digest"
	objects := objectstoragetest.New()
	receipt, err := objects.Put(ctx, objectstorage.PutInput{Key: key, MediaType: "text/plain", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1, Body: strings.NewReader("x")})
	if err != nil {
		t.Fatal(err)
	}
	source := &sourceFixture{items: []platform.ProjectPurgeExternalItem{{Kind: "OBJECT", Target: key, Version: receipt.VersionID}}}
	processor := New(source, objects, nil, fake.NewSimpleClientset(), "kodex-runtime")
	candidate := platform.ProjectPurgeCandidate{OrganizationRef: "org_fixture", ProjectRef: "prj_fixture"}
	if processor.process(ctx, candidate) == nil || source.finalized {
		t.Fatal("unconfirmed subject artifact admitted")
	}
	if _, err := objects.Head(ctx, key, receipt.VersionID); err != nil {
		t.Fatal("unconfirmed subject artifact deleted")
	}
	source.subjectArtifactOwned = true
	for range 2 {
		if err := processor.process(ctx, candidate); err != nil || !source.finalized {
			t.Fatal("confirmed moved subject artifact cleanup/retry", err)
		}
	}
}
