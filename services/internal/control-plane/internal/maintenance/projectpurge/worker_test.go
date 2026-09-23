package projectpurge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

type sourceFixture struct {
	items             []platform.ProjectPurgeExternalItem
	shared, finalized bool
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
	processor := New(source, objects, client, "kodex-runtime")
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
	processor := New(source, objects, fake.NewSimpleClientset(), "kodex-runtime")
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
	processor = New(source, objects, client, "kodex-runtime")
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
	processor := New(source, objectstoragetest.New(), fake.NewSimpleClientset(pod), "kodex-runtime")
	if err := processor.process(ctx, candidate); err == nil || source.finalized {
		t.Fatal("active PVC consumer was ignored")
	}
}
