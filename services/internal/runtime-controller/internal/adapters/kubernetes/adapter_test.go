package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCommandKeyIsStableAndVersionFenced(t *testing.T) {
	execution := testExecution()
	first := commandKey(execution, "heartbeat")
	if first != commandKey(execution, "heartbeat") {
		t.Fatal("command key is not stable")
	}
	execution.Version++
	if first == commandKey(execution, "heartbeat") {
		t.Fatal("command key ignored version fence")
	}
}

func TestCredentialVolumeUsesExecutionOwnedImmutableSnapshot(t *testing.T) {
	execution := testExecution()
	volume, mount := executionCredentialVolume(execution, "credential-0", 3)
	if volume.Secret == nil || volume.Secret.SecretName != executionCredentialSecretName(execution, 3) ||
		!mount.ReadOnly || mount.MountPath != "/var/run/secrets/mattercodex/runtime/credential-0" {
		t.Fatal("credential volume is not bound to the execution-owned immutable snapshot")
	}
}

func TestDeletePVCRetriesCrashAfterDeletionEvidence(t *testing.T) {
	execution := testExecution()
	admission := execution
	execution.State = enum.ExecutionSucceeded
	execution.CleanupAuthorizationState = "ACTIVE"
	execution.CleanupAuthorizationID = uuid.NewString()
	execution.CleanupAuthorizationGeneration = 1
	pvcUID := types.UID(uuid.NewString())
	execution.ArchiveReference = "s3://bucket/archive?versionId=v1"
	execution.ArchiveSHA256 = strings.Repeat("c", 64)
	execution.ArchiveObjectKey = "archive"
	execution.ArchiveVersionID = "v1"
	execution.ArchiveKMSKeyARN = "arn:aws:kms:region:account:key/exact"
	execution.ArchiveObjectLockMode = "COMPLIANCE"
	execution.ArchiveProvenanceSHA256 = strings.Repeat("e", 64)
	execution.RestoreProofReference = "s3://bucket/proof?versionId=v1"
	execution.RestoreProofSHA256 = strings.Repeat("d", 64)
	execution.CleanupAuthorizationExpiresAt = time.Now().UTC().Add(time.Hour)
	execution.CleanupPVCName = "session"
	execution.CleanupPVCUID = string(pvcUID)
	execution.CleanupPVCResourceVersion = "7"
	execution.CleanupClaimedAt = time.Now().UTC()
	execution.CleanupEligibleAt = execution.CleanupClaimedAt
	document := journalDocument{
		Execution: execution, AdmitKey: uuid.NewString(), HeartbeatKey: uuid.NewString(),
		CompleteKey: uuid.NewString(), IncidentKey: uuid.NewString(), ArchiveKey: uuid.NewString(),
		RestoreKey: uuid.NewString(), CleanupKey: uuid.NewString(), Phase: "MATERIALIZED", RehydratePhase: "NOT_REQUIRED",
		PodName: "pod", PVCName: "session", CreatedAt: time.Now().UTC(), LastTransition: time.Now().UTC(),
		PVCUID: string(pvcUID), PVCResourceVersion: "7",
	}
	document.AdmissionRequest = admission
	raw, err := marshalJournal(document)
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: journalName(execution.ID), Namespace: "test"}, Data: map[string]string{journalDataKey: string(raw)}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "session", Namespace: "test", UID: pvcUID, ResourceVersion: "7", Annotations: map[string]string{
			"runtime.mattercodex.dev/retention-owner-execution-id": execution.ID,
			"runtime.mattercodex.dev/retention-owner-journal":      journalName(execution.ID),
		}}},
	)
	adapter := &Adapter{client: client, config: Config{Namespace: "test"}, now: time.Now}
	status := entity.RuntimeStatus{ExecutionID: execution.ID, PVCName: "session", PVCUID: string(pvcUID),
		PVCResourceVersion: "7", PVCDeletionStarted: true, RetentionOwner: true}
	if _, err := adapter.DeletePVC(context.Background(), execution, status); err != nil {
		t.Fatalf("guarded retry failed: %v", err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("test").Get(context.Background(), "session", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("PVC remains after guarded retry: %v", err)
	}
	updated, err := client.CoreV1().ConfigMaps("test").Get(context.Background(), journalName(execution.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var stored journalDocument
	if json.Unmarshal([]byte(updated.Data[journalDataKey]), &stored) != nil || !stored.PVCDeleted {
		t.Fatal("journal did not persist confirmed PVC deletion")
	}
}

func TestRevokeAccessRemovesGrantButPreservesStableWarmIdentity(t *testing.T) {
	execution := testExecution()
	execution.State = enum.ExecutionCancelled
	execution.AccessProfile = enum.AccessProjectRead
	name := accessServiceAccountName(execution)
	namespaceName := projectNamespaceName(execution.ProjectID)
	subject := rbacv1.Subject{Kind: "ServiceAccount", Name: name, Namespace: "test"}
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName, Annotations: map[string]string{
			"mattercodex.dev/project-id": execution.ProjectID, "mattercodex.dev/organization-id": execution.OrganizationID,
		}}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", Annotations: map[string]string{
			"runtime.mattercodex.dev/execution-id": execution.ID,
		}}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespaceName},
			Subjects: []rbacv1.Subject{subject}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "runtime-role-project-read"}},
	)
	adapter := &Adapter{client: client, config: Config{Namespace: "test", ReadClusterRole: "runtime-role-project-read"}, now: time.Now}
	if err := adapter.RevokeAccess(context.Background(), execution); err != nil {
		t.Fatalf("terminal access revocation failed: %v", err)
	}
	if _, err := client.CoreV1().ServiceAccounts("test").Get(context.Background(), name, metav1.GetOptions{}); err != nil {
		t.Fatalf("stable warm runtime identity was removed: %v", err)
	}
	if _, err := client.RbacV1().RoleBindings(namespaceName).Get(context.Background(), name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("runtime access role binding remains: %v", err)
	}
}

func TestListSelectsAnnotatedJournalAsSingleRetentionOwner(t *testing.T) {
	first := testExecution()
	first.State = enum.ExecutionSucceeded
	second := testExecution()
	second.State = enum.ExecutionSucceeded
	second.OrganizationID, second.ProjectID, second.SessionID = first.OrganizationID, first.ProjectID, first.SessionID
	now := time.Now().UTC()
	pvcName := pvcName(first)
	objects := []runtime.Object{&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: pvcName, Namespace: "test", Labels: map[string]string{componentLabel: "session-storage"},
		Annotations: map[string]string{
			"runtime.mattercodex.dev/organization-id":              first.OrganizationID,
			"runtime.mattercodex.dev/project-id":                   first.ProjectID,
			"runtime.mattercodex.dev/session-id":                   first.SessionID,
			"runtime.mattercodex.dev/retention-owner-execution-id": second.ID,
			"runtime.mattercodex.dev/retention-owner-journal":      journalName(second.ID),
		},
	}}}
	for index, execution := range []entity.Execution{first, second} {
		created := now.Add(time.Duration(index) * time.Minute)
		document := journalDocument{Execution: execution, AdmitKey: uuid.NewString(), HeartbeatKey: uuid.NewString(),
			CompleteKey: uuid.NewString(), IncidentKey: uuid.NewString(), ArchiveKey: uuid.NewString(),
			RestoreKey: uuid.NewString(), CleanupKey: uuid.NewString(), Phase: "MATERIALIZED", RehydratePhase: "NOT_REQUIRED",
			PodName: "pod-" + strconv.Itoa(index), PVCName: pvcName, CreatedAt: created, LastTransition: created}
		document.AdmissionRequest = execution
		document.AdmissionRequest.State = enum.ExecutionPending
		raw, err := marshalJournal(document)
		if err != nil {
			t.Fatal(err)
		}
		objects = append(objects, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: journalName(execution.ID), Namespace: "test", CreationTimestamp: metav1.NewTime(created),
			Labels: map[string]string{managedLabel: "true", componentLabel: journalComponent},
		}, Data: map[string]string{journalDataKey: string(raw)}})
	}
	adapter := &Adapter{client: fake.NewSimpleClientset(objects...), config: Config{Namespace: "test"}, now: time.Now}
	statuses, err := adapter.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	owners := 0
	for _, status := range statuses {
		if status.RetentionOwner {
			owners++
			if status.ExecutionID != second.ID {
				t.Fatalf("old journal selected as retention owner: %s", status.ExecutionID)
			}
		}
	}
	if owners != 1 {
		t.Fatalf("retention owners = %d, want 1", owners)
	}
}

func testExecution() entity.Execution {
	execution := entity.Execution{ID: uuid.NewString(), OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(),
		ProcessID: uuid.NewString(), SessionID: uuid.NewString(), ThreadID: "thread", RoleID: uuid.NewString(),
		TurnID: uuid.NewString(), Attempt: 1, RuntimeRevisionID: uuid.NewString(), RuntimeRevisionVersion: 1,
		RuntimeRevisionSHA256: strings.Repeat("a", 64), EffectiveRuntimeSHA256: strings.Repeat("f", 64),
		ImmutableInputSHA256: strings.Repeat("b", 64), AgentSessionKey: "agent-session", AgentSessionID: 1,
		AgentSessionTurnID: 1, AgentRunID: "run-1", AgentBindingSHA256: strings.Repeat("8", 64),
		ResourceClass: enum.ResourceStandard, AccessProfile: enum.AccessNone, WorkloadID: "runtime-controller",
		WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		GrantGeneration:  1, Version: 1, Fence: 1, State: enum.ExecutionPending,
		RetentionPolicyID: "default", RetentionPolicyVersion: 1, PVCRetentionSeconds: 86400,
		ArchiveRetentionSeconds: 7776000, PVCCleanupEligibleAt: time.Now().UTC().Add(24 * time.Hour),
		ArchiveRetainUntil:           time.Now().UTC().Add(90 * 24 * time.Hour),
		CapacityObservationExpiresAt: time.Now().UTC().Add(time.Hour), RescheduleAfter: time.Now().UTC().Add(time.Minute),
		RestoreAssignmentState: "NONE", WorkloadTicketSHA256: strings.Repeat("9", 64),
		CleanupAuthorizationState: "NONE"}
	raw, _ := json.Marshal(struct {
		ExecutionID string
		Credentials []struct{}
	}{execution.ID, []struct{}{}})
	digest := sha256.Sum256(raw)
	execution.CredentialSnapshotSHA256 = hex.EncodeToString(digest[:])
	return execution
}
