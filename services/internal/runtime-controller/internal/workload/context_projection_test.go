package workload

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureWarmRejectsStaleContextCompatibilityBeforeReplacement(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	revision := testWarmRevision()
	input, binding, err := manager.BuildWarmInput(revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnsureWarm(t.Context(), input, binding); err != nil {
		t.Fatal(err)
	}
	previous, err := client.CoreV1().Pods("kodex-runtime").Get(t.Context(), "system-assistant-warm", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Global warm context пуст; устаревшая совместимость не даёт права
	// присоединить процесс даже при совпадении остальных annotations.
	previous.Annotations[warmCompatibilityAnnotation] = strings.Repeat("f", 64)
	if _, err := client.CoreV1().Pods("kodex-runtime").Update(t.Context(), previous, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ready, err := manager.EnsureWarm(t.Context(), input, binding)
	if err != nil || ready {
		t.Fatalf("changed warm context reused ready Pod: %v", err)
	}
	if _, err := client.CoreV1().Pods("kodex-runtime").Get(t.Context(), "system-assistant-warm", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("old warm Pod survived context replacement")
	}
	if _, err := manager.EnsureWarm(t.Context(), input, binding); err != nil {
		t.Fatal(err)
	}
	current, err := client.CoreV1().Pods("kodex-runtime").Get(t.Context(), "system-assistant-warm", metav1.GetOptions{})
	if err != nil || current.Annotations[warmCompatibilityAnnotation] == previous.Annotations[warmCompatibilityAnnotation] ||
		current.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest {
		t.Fatal("new Pod did not receive exact changed context")
	}
}

func testContextRevision(revision *cp.RuntimeRevisionSnapshot) {
	now := timestamppb.New(time.Now().UTC())
	provenance := &cp.ContextProvenance{ActorRef: "act_abcdefgh", SourceKind: "UI", Digest: strings.Repeat("a", 64), CreatedAt: now}
	revision.SkillBundles = []*cp.RuntimeSkillBundleSnapshot{{BindingRef: "ctxb_abcdefgh", BindingVersion: 2, BundleRef: "sklb_abcdefgh",
		RevisionRef: "sklv_abcdefgh", Revision: 3, Digest: strings.Repeat("b", 64), ScanEngine: "fixture", ScanDigest: strings.Repeat("c", 64),
		ScannedAt: now, Name: "fixture", Description: "Approved fixture", Provenance: provenance,
		Files: []*cp.SkillBundleFile{{Path: "SKILL.md", ArtifactRef: "art_abcdefgh", ArtifactRevision: 4, Digest: "sha256:" + strings.Repeat("d", 64), SizeBytes: 42}}}}
	revision.MemoryRecords = []*cp.RuntimeMemoryRecordSnapshot{{BindingRef: "ctxb_ijklmnop", BindingVersion: 5, RecordRef: "memr_abcdefgh",
		RevisionRef: "memv_abcdefgh", Revision: 6, Digest: strings.Repeat("e", 64), Title: "Fixture", Summary: "Synthetic memory",
		RetentionUntil: timestamppb.New(time.Now().Add(time.Hour)), Provenance: provenance}}
}

func TestHydratesTypedContextAndPublishesOnlyExactRecords(t *testing.T) {
	execution := testExecution(false)
	testContextRevision(execution.Revision)
	sealTestTurnExecution(execution)
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, _, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := input.RequiredContextSnapshot(time.Now())
	if err != nil || len(snapshot.Skills) != 1 || len(snapshot.Memories) != 1 || snapshot.ProjectRef != execution.Run.ProjectRef ||
		snapshot.Skills[0].Files[0].ArtifactRevision != 4 || snapshot.Memories[0].BindingVersion != 5 {
		t.Fatal("typed context pins were lost")
	}
	compiled, _, _ := loadRunnerInputSchema(t)
	validateRunnerInputSchema(t, compiled, input)
	raw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		t.Fatal(err)
	}
	object := decodeJSONInstance(t, raw).(map[string]any)
	delete(object, "context_snapshot")
	if compiled.Validate(object) == nil {
		t.Fatal("executable schema accepted a missing context snapshot")
	}
	data, err := runtimeProjectionData(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{skillManifestKey, memoryManifestKey} {
		var projection map[string]json.RawMessage
		if json.Unmarshal([]byte(data[key]), &projection) != nil || projection["tools"] != nil || projection["artifacts"] != nil || projection["context_digest"] == nil {
			t.Fatal("context projection used an unrelated resource kind")
		}
	}
	for name, mutate := range map[string]func(*cp.ClaimedExecution){
		"changed memory":  func(e *cp.ClaimedExecution) { e.Revision.MemoryRecords[0].Summary = "changed" },
		"removed skill":   func(e *cp.ClaimedExecution) { e.Revision.SkillBundles = nil },
		"foreign project": func(e *cp.ClaimedExecution) { e.Run.ProjectRef = "proj_ijklmnop" },
		"changed attempt": func(e *cp.ClaimedExecution) { e.Revision.Attempt++ },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := proto.Clone(execution).(*cp.ClaimedExecution)
			mutate(candidate)
			if _, _, err := manager.BuildTurnInput(candidate); err == nil {
				t.Fatal("unsealed runtime context drift was accepted")
			}
		})
	}
}

func TestContextHydrationRejectsInvalidPinsBeforeMaterialization(t *testing.T) {
	for name, mutate := range map[string]func(*cp.RuntimeRevisionSnapshot){
		"nil skill":          func(r *cp.RuntimeRevisionSnapshot) { r.SkillBundles[0] = nil },
		"nil file":           func(r *cp.RuntimeRevisionSnapshot) { r.SkillBundles[0].Files[0] = nil },
		"missing provenance": func(r *cp.RuntimeRevisionSnapshot) { r.SkillBundles[0].Provenance = nil },
		"invalid timestamp":  func(r *cp.RuntimeRevisionSnapshot) { r.SkillBundles[0].ScannedAt = &timestamppb.Timestamp{Nanos: -1} },
		"traversal":          func(r *cp.RuntimeRevisionSnapshot) { r.SkillBundles[0].Files[0].Path = "../SKILL.md" },
		"oversized": func(r *cp.RuntimeRevisionSnapshot) {
			r.SkillBundles[0].Files[0].SizeBytes = runtimecontract.MaximumSkillFileBytes + 1
		},
		"expired": func(r *cp.RuntimeRevisionSnapshot) {
			r.MemoryRecords[0].RetentionUntil = timestamppb.New(time.Now().Add(-time.Second))
		},
	} {
		t.Run(name, func(t *testing.T) {
			revision := &cp.RuntimeRevisionSnapshot{}
			testContextRevision(revision)
			mutate(revision)
			input := runtimecontract.RunnerInput{OrganizationRef: "org_abcdefgh", ProjectRef: "proj_abcdefgh", AgentRef: "agt_abcdefgh"}
			if hydrateRuntimeContext(&input, revision) == nil || input.ContextSnapshot != nil {
				t.Fatal("invalid context was partially materialized")
			}
		})
	}
}

func TestContextVolumeSeparatesInitWriterFromRuntimeReaders(t *testing.T) {
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	pod := manager.runtimePod(input, binding, nil, "runtime-ticket-fixture", "fixture", "turn")
	if pod.Spec.SecurityContext.FSGroup == nil || *pod.Spec.SecurityContext.FSGroup != 29000 {
		t.Fatal("context mount group is invalid")
	}
	volumes := 0
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "runtime-context" {
			volumes++
			if volume.EmptyDir == nil || volume.EmptyDir.SizeLimit == nil || volume.EmptyDir.SizeLimit.Cmp(resource.MustParse("520Mi")) != 0 || volume.EmptyDir.Medium != "" {
				t.Fatal("context volume must be bounded ephemeral disk")
			}
		}
	}
	if volumes != 1 {
		t.Fatal("context volume is missing or duplicated")
	}
	for _, container := range append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...) {
		count := 0
		for _, mount := range container.VolumeMounts {
			if mount.Name != "runtime-context" {
				continue
			}
			count++
			if mount.MountPath != runtimecontract.RuntimeContextRoot || mount.ReadOnly != (container.Name != "workspace-init") ||
				mount.SubPath != "" || mount.SubPathExpr != "" || mount.MountPropagation != nil {
				t.Fatal("context mount authority is invalid")
			}
		}
		want := 1
		if container.Name == "provider-credential-relay" {
			want = 0
		}
		if count != want {
			t.Fatal("context mount consumer is invalid")
		}
	}
}
