package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCredentialBrokerJobsAreUnprivilegedAuthorityClients(t *testing.T) {
	execution := testExecution()
	execution.WorkloadTicket = "ticket"
	execution.ArchiveWorkloadTicket = "archive-ticket"
	execution.RestoreWorkloadTicket = "restore-ticket"
	adapter := &Adapter{config: Config{
		Namespace: "mattercodex-system", Environment: "test",
		ControllerImage:                  "mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/runtime-controller@sha256:" + strings.Repeat("a", 64),
		CredentialBrokerServiceAccount:   "runtime-credential-broker",
		ProjectReadBrokerServiceAccount:  "runtime-project-read-broker",
		ClusterAdminBrokerServiceAccount: "runtime-cluster-admin-broker",
		S3ArchiveBrokerServiceAccount:    "runtime-s3-archive-broker",
		S3RestoreBrokerServiceAccount:    "runtime-s3-restore-broker",
		JobTTL:                           time.Hour,
	}}
	for name, job := range map[string]*batchv1.Job{
		"snapshot": adapter.credentialBrokerJob(execution),
		"archive":  adapter.s3CredentialBrokerJob(execution, "archive"),
		"restore":  adapter.s3CredentialBrokerJob(execution, "restore"),
	} {
		container := job.Spec.Template.Spec.Containers[0]
		if !envHasPrefix(container.Env, "RUNTIME_CREDENTIAL_AUTHORITY_URL", "https://runtime-") {
			t.Fatalf("%s broker misses exact HTTPS authority endpoint", name)
		}
		for _, volume := range job.Spec.Template.Spec.Volumes {
			if volume.Name == "kube-api-access" || volume.Name == "vault-token" || volume.Name == "s3-broker-config" {
				t.Fatalf("%s broker retained privileged volume %s", name, volume.Name)
			}
		}
	}
}

func TestRolePodProviderRuntimeCannotMountRunnerAuthority(t *testing.T) {
	execution := testExecution()
	execution.WorkloadTicket = strings.Repeat("a", 64) + "." + strings.Repeat("b", 43)
	adapter := &Adapter{config: Config{
		Namespace:                       "mattercodex-system",
		Environment:                     "production",
		RoleImageRepository:             "registry.example.test/agent-runner",
		RunnerControlPlaneTarget:        "control-plane:8443",
		RunnerControlPlaneTLSServerName: "control-plane.mattercodex-system.svc.cluster.local",
		InteractionGatewayURL:           "https://interaction-gateway.mattercodex-system.svc.cluster.local:8443",
		SessionMCPURL:                   "https://matter-codex-bot-service.mattercodex-system.svc.cluster.local:8443",
		AuthorityImage:                  "registry.example.test/internal-rpc-authority@sha256:" + strings.Repeat("c", 64),
	}}
	revision := entity.Revision{ImageDigest: "sha256:" + strings.Repeat("d", 64)}
	pod, err := adapter.rolePod(t.Context(), execution, revision)
	if err != nil {
		t.Fatalf("rolePod() error = %v", err)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("role Pod automatically mounts a Kubernetes token")
	}
	var provider *corev1.Container
	for index := range pod.Spec.Containers {
		if pod.Spec.Containers[index].Name == "provider-runtime" {
			provider = &pod.Spec.Containers[index]
			break
		}
	}
	if provider == nil || provider.SecurityContext == nil || provider.SecurityContext.RunAsUser == nil ||
		*provider.SecurityContext.RunAsUser != 10002 || provider.SecurityContext.RunAsGroup == nil ||
		*provider.SecurityContext.RunAsGroup != 10002 {
		t.Fatalf("provider runtime does not use its isolated UID: %#v", provider)
	}
	wantMounts := map[string]string{
		"session": "/workspace", "provider-socket": "/run/mattercodex/provider", "provider-tmp": "/tmp",
	}
	if len(provider.VolumeMounts) != len(wantMounts) {
		t.Fatalf("provider runtime mount count = %d, want %d", len(provider.VolumeMounts), len(wantMounts))
	}
	for _, mount := range provider.VolumeMounts {
		if wantMounts[mount.Name] != mount.MountPath {
			t.Fatalf("provider runtime received authority mount %q at %q", mount.Name, mount.MountPath)
		}
	}
	if len(provider.Env) != 2 || provider.Env[0].Name != "HOME" || provider.Env[1].Name != "CODEX_HOME" {
		t.Fatalf("provider runtime received unexpected environment: %#v", provider.Env)
	}
}

func TestAdmissionAndRBACSelectOnlyExactRuntimeProfile(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "..", "..", "deploy", "k8s", "base", "runtime-controller")
	admission, err := os.ReadFile(filepath.Join(root, "workload-admission.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(admission), "values: [role-runtime, runtime-credential-copy, runtime-s3-credential-readback]") ||
		!strings.Contains(string(admission), "values: [runtime-credential-snapshot, runtime-s3-credential]") {
		t.Fatal("webhook is not limited to exact role/copy/credential profiles")
	}
	rbac, err := os.ReadFile(filepath.Join(root, "serviceaccounts-rbac.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(rbac)
	for _, routine := range []string{"runtime-credential-broker", "runtime-project-read-broker", "runtime-cluster-admin-broker", "runtime-s3-archive-broker", "runtime-s3-restore-broker"} {
		if strings.Contains(text, "subjects:\n  - {kind: ServiceAccount, name: "+routine+"}") {
			t.Fatalf("routine broker %s still has namespace mutation RBAC", routine)
		}
	}
	if !strings.Contains(text, "runtime-workload-materializer") ||
		!strings.Contains(text, "runtime-s3-archive-exchanger") ||
		!strings.Contains(text, "runtime-s3-restore-exchanger") {
		t.Fatal("trusted materializer/exchanger identities are absent")
	}
}

func TestS3CredentialRejoinUsesExactOwnerReceiptWithoutSecretRead(t *testing.T) {
	execution := testExecution()
	execution.Version = 7
	execution.Fence = 9
	action := "archive"
	expiresAt := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)
	data := map[string]string{
		"request_sha256": strings.Repeat("1", 64), "execution_id": execution.ID,
		"mode": "s3-archive", "version": "7", "fence": "9",
		"s3_secret_name": s3CredentialSecretName(execution, action),
		"s3_secret_uid":  "credential-secret-uid", "s3_secret_resource_version": "11",
		"s3_execution_id": execution.ID, "s3_organization_id": execution.OrganizationID,
		"s3_project_id": execution.ProjectID, "s3_session_id": execution.SessionID,
		"s3_source_execution_id": execution.ID, "s3_action": action,
		"s3_policy_sha256": strings.Repeat("2", 64), "s3_readback_sha256": strings.Repeat("3", 64),
		"s3_secret_data_sha256": strings.Repeat("4", 64), "s3_expires_at": expiresAt.Format(time.RFC3339),
	}
	payload, err := json.Marshal(struct {
		Name, UID, ResourceVersion, ExecutionID, OrganizationID, ProjectID string
		SessionID, SourceExecutionID, Action, PolicySHA256, ReadbackSHA256 string
		SecretDataSHA256, ExpiresAt                                        string
	}{data["s3_secret_name"], data["s3_secret_uid"], data["s3_secret_resource_version"],
		execution.ID, execution.OrganizationID, execution.ProjectID, execution.SessionID,
		execution.ID, action, data["s3_policy_sha256"], data["s3_readback_sha256"],
		data["s3_secret_data_sha256"], expiresAt.Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	data["s3_content_sha256"] = hex.EncodeToString(digest[:])
	immutable := true
	client := fake.NewSimpleClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "runtime-authority-receipt-" + stableHash(execution.ID+":s3-archive", 24), Namespace: "test",
	}, Immutable: &immutable, Data: data})
	adapter := &Adapter{client: client, config: Config{Namespace: "test"}, now: time.Now}
	actual, err := adapter.readS3CredentialSnapshot(t.Context(), execution, action)
	if err != nil || actual != data["s3_content_sha256"] {
		t.Fatalf("owner receipt rejoin failed: digest=%s err=%v", actual, err)
	}
}

func envHasPrefix(values []corev1.EnvVar, name, prefix string) bool {
	for _, value := range values {
		if value.Name == name && strings.HasPrefix(value.Value, prefix) {
			return true
		}
	}
	return false
}

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

func TestAccessIdentityIsExecutionScoped(t *testing.T) {
	first := testExecution()
	second := first
	second.ID = uuid.NewString()
	if accessServiceAccountName(first) == accessServiceAccountName(second) {
		t.Fatal("different runtime executions share one Kubernetes identity")
	}
	if !strings.HasPrefix(accessServiceAccountName(first), "runtime-access-") ||
		!strings.HasPrefix(accessServiceAccountName(second), "runtime-access-") {
		t.Fatal("runtime access identity does not use the admitted prefix")
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
		ProviderBindingID: uuid.NewString(), ProviderBindingVersion: 1,
		ProviderBindingSHA256:     strings.Repeat("7", 64),
		CleanupAuthorizationState: "NONE"}
	execution.Materializations = []entity.Materialization{
		{Kind: "PROMPT", ArtifactID: uuid.NewString(), ArtifactVersion: 1, SHA256: strings.Repeat("1", 64), SizeBytes: 1, RelativePath: ".matter-codex/inbox/prompt.md", MediaType: "text/markdown", StorageRef: "s3://runtime/prompt"},
		{Kind: "INSTRUCTION", ArtifactID: uuid.NewString(), ArtifactVersion: 1, SHA256: strings.Repeat("2", 64), SizeBytes: 1, RelativePath: "AGENTS.md", MediaType: "text/markdown", StorageRef: "s3://runtime/instructions"},
	}
	raw, _ := json.Marshal(struct {
		ExecutionID string
		Credentials []struct{}
	}{execution.ID, []struct{}{}})
	digest := sha256.Sum256(raw)
	execution.CredentialSnapshotSHA256 = hex.EncodeToString(digest[:])
	return execution
}
