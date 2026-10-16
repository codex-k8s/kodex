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

type sourceFixture struct {
	items                                []platform.ProjectPurgeExternalItem
	shared, finalized                    bool
	archiveOwned                         bool
	subjectArtifactOwned, globalPVCOwned bool
}

func (*sourceFixture) PromoteDueProjectPurges(context.Context, int32) error { return nil }
func (*sourceFixture) ListPendingProjectPurges(context.Context, int32) ([]platform.ProjectPurgeCandidate, error) {
	return nil, nil
}
func (source *sourceFixture) ProjectPurgeInventory(context.Context, platform.ProjectPurgeCandidate) ([]platform.ProjectPurgeExternalItem, error) {
	return source.items, nil
}
func (source *sourceFixture) ProjectPurgeObjectIsShared(context.Context, platform.ProjectPurgeCandidate, string) (bool, error) {
	return source.shared, nil
}
func (source *sourceFixture) ProjectPurgeArchiveIsOwned(context.Context, platform.ProjectPurgeCandidate, platform.ProjectPurgeExternalItem) (bool, error) {
	return source.archiveOwned, nil
}
func (source *sourceFixture) ProjectPurgeSubjectArtifactIsOwned(context.Context, platform.ProjectPurgeCandidate, platform.ProjectPurgeExternalItem) (bool, error) {
	return source.subjectArtifactOwned, nil
}
func (source *sourceFixture) ProjectPurgeGlobalPVCIsOwned(context.Context, platform.ProjectPurgeCandidate, string) (bool, error) {
	return source.globalPVCOwned, nil
}
func (source *sourceFixture) FinalizeProjectPurge(_ context.Context, _ platform.ProjectPurgeCandidate, items []platform.ProjectPurgeExternalItem) error {
	if len(items) != len(source.items) {
		return errors.New("project purge inventory changed")
	}
	source.finalized = true
	return nil
}

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

func TestFailureLogOmitsUnderlyingObjectAndProviderDetails(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logFailures(t.Context(), logger, errors.Join(&failure{stage: "object", projectRef: "prj_fixture", cause: errors.New("private-object-key credential-value")}))
	message := output.String()
	if !strings.Contains(message, `"safe_stage":"object"`) || !strings.Contains(message, `"project_ref":"prj_fixture"`) || strings.Contains(message, "private-object-key") || strings.Contains(message, "credential-value") {
		t.Fatal("unsafe or incomplete purge diagnostic")
	}
}

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
