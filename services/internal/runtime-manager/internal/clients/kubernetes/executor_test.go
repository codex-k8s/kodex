package kubernetes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	fleetclient "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/clients/fleet"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientkubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestExecutorStartCreatesRestrictedHealthCheckJob(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	job := testHealthCheckJob()
	job.JobInputJSON = []byte(`{"labels":{"runtime.kodex.io/test":"true"}}`)

	started, err := executor.Start(context.Background(), job)
	if err != nil {
		t.Fatalf("Start(): %v", err)
	}
	if started.Namespace != "runtime-jobs" || started.JobName == "" || started.ExternalRef == "" {
		t.Fatalf("started job = %+v, want namespace/name/ref", started)
	}
	created, err := client.BatchV1().Jobs("runtime-jobs").Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	if got := created.Spec.Template.Spec.Containers[0].Image; got != "busybox:1.36" {
		t.Fatalf("image = %q, want configured image", got)
	}
	if got := created.Spec.Template.Spec.Containers[0].Env; len(got) != 0 {
		t.Fatalf("container env = %v, want no literal env from job input", got)
	}
	if len(created.Annotations) != 0 || len(created.Spec.Template.Annotations) != 0 {
		t.Fatalf("annotations = %v/%v, want none from job input", created.Annotations, created.Spec.Template.Annotations)
	}
	if created.Labels[runtimeJobLabel] != job.ID.String() || created.Labels["app.kubernetes.io/managed-by"] != managedBy {
		t.Fatalf("managed labels = %v", created.Labels)
	}
	if len(started.ArtifactRefs) != 2 {
		t.Fatalf("artifact refs = %d, want job and namespace refs", len(started.ArtifactRefs))
	}
}

func TestExecutorStartCreatesRestrictedAgentRunJob(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	job := testAgentRunJob()

	started, err := executor.Start(context.Background(), job)
	if err != nil {
		t.Fatalf("Start(agent_run): %v", err)
	}
	if started.Namespace != "runtime-jobs" || started.JobName == "" || started.ExternalRef == "" {
		t.Fatalf("started agent_run job = %+v, want namespace/name/ref", started)
	}
	created, err := client.BatchV1().Jobs("runtime-jobs").Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	podSpec := created.Spec.Template.Spec
	if podSpec.AutomountServiceAccountToken == nil || *podSpec.AutomountServiceAccountToken {
		t.Fatalf("automount service account token = %v, want disabled", podSpec.AutomountServiceAccountToken)
	}
	container := podSpec.Containers[0]
	if container.Name != agentRunContainerName || container.Image != "ghcr.io/codex-k8s/agent-runner@sha256:runner" {
		t.Fatalf("container = %s/%s, want fixed agent runner container/image", container.Name, container.Image)
	}
	if strings.Join(container.Command, " ") != agentRunCommand || strings.Join(container.Args, " ") != agentRunCommandKind {
		t.Fatalf("command/args = %v/%v, want fixed runner command", container.Command, container.Args)
	}
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].MountPath != agentRunWorkspacePath {
		t.Fatalf("volume mounts = %+v, want fixed workspace mount", container.VolumeMounts)
	}
	if len(podSpec.Volumes) != 1 || podSpec.Volumes[0].PersistentVolumeClaim == nil || podSpec.Volumes[0].PersistentVolumeClaim.ClaimName != "runtime-workspace-549" {
		t.Fatalf("volumes = %+v, want workspace PVC ref", podSpec.Volumes)
	}
	env := envMap(container.Env)
	if env["KODEX_AGENT_RUN_ID"] == "" || env["KODEX_AGENT_RUN_CONTEXT_PATH"] != agentRunContextPath {
		t.Fatalf("agent_run env = %v, want safe runner refs", env)
	}
	if strings.Contains(env["KODEX_AGENT_RUN_ALLOWED_SECRET_REFS_JSON"], "secret-value") {
		t.Fatalf("allowed secret refs env contains a secret value: %q", env["KODEX_AGENT_RUN_ALLOWED_SECRET_REFS_JSON"])
	}
	if len(created.Annotations) != 0 || len(created.Spec.Template.Annotations) != 0 {
		t.Fatalf("annotations = %v/%v, want none from agent_run spec", created.Annotations, created.Spec.Template.Annotations)
	}
	if len(started.ArtifactRefs) != 3 {
		t.Fatalf("artifact refs = %d, want job, namespace and image refs", len(started.ArtifactRefs))
	}
}

func TestExecutorStartRejectsAgentRunWithoutExecutionSpec(t *testing.T) {
	t.Parallel()

	executor := newTestExecutor(t, fake.NewClientset(), fakeClusterProvider{access: testClusterAccess()})
	job := testAgentRunJob()
	job.JobInputJSON = []byte(`{}`)

	_, err := executor.Start(context.Background(), job)

	assertExecutionCode(t, err, "agent_run_execution_spec_required")
}

func TestExecutorStartRejectsAgentRunWithoutWorkspacePVCRef(t *testing.T) {
	t.Parallel()

	executor := newTestExecutor(t, fake.NewClientset(), fakeClusterProvider{access: testClusterAccess()})
	job := testAgentRunJob()
	job.JobInputJSON = []byte(`{"agent_run_execution_spec":{"agent_run_id":"00000000-0000-0000-0000-000000000031","slot_id":"00000000-0000-0000-0000-000000000032","expected_materialization_id":"00000000-0000-0000-0000-000000000033","expected_materialization_fingerprint":"sha256:workspace","workspace_ref":"runtime://workspace/31","workspace_mount_ref":"mount://workspace/31","context_ref":"runtime://workspace/31/.kodex/context/agent-run.json","context_digest":"sha256:context","runner_profile_ref":"runner-profile://codex-agent/default","runner_image_ref":"image://ghcr.io/codex-k8s/agent-runner@sha256:runner","runner_mode":"codex_agent"}}`)

	_, err := executor.Start(context.Background(), job)

	assertExecutionCode(t, err, "invalid_agent_run_execution_spec")
}

func TestExecutorStartReusesExistingManagedJob(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	job := testHealthCheckJob()

	first, err := executor.Start(context.Background(), job)
	if err != nil {
		t.Fatalf("first Start(): %v", err)
	}
	second, err := executor.Start(context.Background(), job)
	if err != nil {
		t.Fatalf("second Start(): %v", err)
	}
	if second.JobName != first.JobName || second.ExternalRef != first.ExternalRef {
		t.Fatalf("second start = %+v, want same job/ref as %+v", second, first)
	}
	jobs, err := client.BatchV1().Jobs(first.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("jobs = %d, want one reused Kubernetes Job", len(jobs.Items))
	}
}

func TestExecutorStartRejectsNameConflict(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	job := testHealthCheckJob()
	_, err := client.BatchV1().Jobs("runtime-jobs").Create(context.Background(), &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runtimeJobName(job.ID),
			Namespace: "runtime-jobs",
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "other"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create conflicting job: %v", err)
	}

	_, err = executor.Start(context.Background(), job)

	assertExecutionCode(t, err, "kubernetes_job_name_conflict")
}

func TestExecutorStartRejectsMismatchedFleetScope(t *testing.T) {
	t.Parallel()

	access := testClusterAccess()
	access.FleetScopeID = uuid.MustParse("00000000-0000-0000-0000-000000000099")
	executor := newTestExecutor(t, fake.NewClientset(), fakeClusterProvider{access: access})

	_, err := executor.Start(context.Background(), testHealthCheckJob())
	assertExecutionCode(t, err, "cluster_scope_mismatch")
}

func TestExecutorStartRejectsUnknownInputFields(t *testing.T) {
	t.Parallel()

	executor := newTestExecutor(t, fake.NewClientset(), fakeClusterProvider{access: testClusterAccess()})
	job := testHealthCheckJob()
	job.JobInputJSON = []byte(`{"command":["rm","-rf","/"]}`)

	_, err := executor.Start(context.Background(), job)
	assertExecutionCode(t, err, "invalid_job_input")
}

func TestExecutorStartRejectsLiteralEnvAndAnnotations(t *testing.T) {
	t.Parallel()

	for _, payload := range [][]byte{
		[]byte(`{"env":{"TOKEN":"secret-value"}}`),
		[]byte(`{"annotations":{"runtime.kodex.io/token":"secret-value"}}`),
	} {
		executor := newTestExecutor(t, fake.NewClientset(), fakeClusterProvider{access: testClusterAccess()})
		job := testHealthCheckJob()
		job.JobInputJSON = payload

		_, err := executor.Start(context.Background(), job)
		assertExecutionCode(t, err, "invalid_job_input")
	}
}

func TestExecutorWaitReportsCompletedJob(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testHealthCheckJob())
	if err != nil {
		t.Fatalf("Start(): %v", err)
	}
	created, err := client.BatchV1().Jobs(started.Namespace).Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	created.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs(started.Namespace).UpdateStatus(context.Background(), created, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update job status: %v", err)
	}

	result := executor.Wait(context.Background(), started)
	if !result.Succeeded || result.ErrorCode != "" {
		t.Fatalf("Wait() = %+v, want success", result)
	}
}

func TestExecutorWaitReportsFailedJob(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testHealthCheckJob())
	if err != nil {
		t.Fatalf("Start(): %v", err)
	}
	created, err := client.BatchV1().Jobs(started.Namespace).Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	created.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs(started.Namespace).UpdateStatus(context.Background(), created, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update job status: %v", err)
	}

	result := executor.Wait(context.Background(), started)

	if result.Succeeded || result.ErrorCode != "kubernetes_job_failed" {
		t.Fatalf("Wait() = %+v, want Kubernetes failure", result)
	}
}

func TestExecutorWaitReportsDeletedJobAsCancelled(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testHealthCheckJob())
	if err != nil {
		t.Fatalf("Start(): %v", err)
	}
	if err := client.BatchV1().Jobs(started.Namespace).Delete(context.Background(), started.JobName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete created Job: %v", err)
	}

	result := executor.Wait(context.Background(), started)

	if result.Succeeded || result.ErrorCode != "kubernetes_job_cancelled" {
		t.Fatalf("Wait() = %+v, want cancelled/deleted diagnostic", result)
	}
}

func TestExecutorWaitDoesNotTurnContextCancelIntoJobFailure(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testHealthCheckJob())
	if err != nil {
		t.Fatalf("Start(): %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := executor.Wait(ctx, started)

	if !result.Interrupted || result.ErrorCode != "runtime_worker_stopped" || result.Succeeded {
		t.Fatalf("Wait() = %+v, want interrupted worker result", result)
	}
}

func TestBoundedLogTailLimitsBytesAndPreservesUTF8(t *testing.T) {
	t.Parallel()

	result := boundedLogTail("prefix-"+strings.Repeat("я", 20), 9)

	if len(result) > 9 {
		t.Fatalf("bounded tail length = %d, want <= 9", len(result))
	}
	if !utf8.ValidString(result) {
		t.Fatalf("bounded tail is not valid UTF-8: %q", result)
	}
}

func newTestExecutor(t *testing.T, client clientkubernetes.Interface, provider fakeClusterProvider) *Executor {
	t.Helper()
	executor, err := NewExecutorWithClientFactory(provider, fakeSecretResolver{value: secretresolver.NewSecretValue([]byte("kubeconfig"))}, Config{
		DefaultNamespace:        "runtime-jobs",
		DefaultImage:            "busybox:1.36",
		ImagePullPolicy:         "IfNotPresent",
		JobTimeout:              time.Second,
		PollInterval:            time.Millisecond,
		BackoffLimit:            0,
		TTLSecondsAfterFinished: 30,
		LogTailBytes:            1024,
	}, fakeClientFactory{client: client})
	if err != nil {
		t.Fatalf("NewExecutorWithClientFactory(): %v", err)
	}
	return executor
}

func testHealthCheckJob() entity.Job {
	fleetScopeID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	clusterID := uuid.MustParse("00000000-0000-0000-0000-000000000011")
	return entity.Job{
		Base:         entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000012"), Version: 2},
		JobType:      enum.JobTypeHealthCheck,
		Status:       enum.JobStatusClaimed,
		JobInputJSON: []byte(`{}`),
		FleetScopeID: &fleetScopeID,
		ClusterID:    &clusterID,
	}
}

func testAgentRunJob() entity.Job {
	fleetScopeID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	clusterID := uuid.MustParse("00000000-0000-0000-0000-000000000011")
	agentRunID := uuid.MustParse("00000000-0000-0000-0000-000000000031")
	slotID := uuid.MustParse("00000000-0000-0000-0000-000000000032")
	return entity.Job{
		Base:         entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000034"), Version: 2},
		JobType:      enum.JobTypeAgentRun,
		Status:       enum.JobStatusClaimed,
		AgentRunID:   &agentRunID,
		SlotID:       &slotID,
		JobInputJSON: []byte(`{"agent_run_execution_spec":{"agent_run_id":"00000000-0000-0000-0000-000000000031","slot_id":"00000000-0000-0000-0000-000000000032","expected_materialization_id":"00000000-0000-0000-0000-000000000033","expected_materialization_fingerprint":"sha256:workspace","workspace_ref":"runtime://workspace/31","workspace_mount_ref":"mount://workspace/31","workspace_pvc_ref":"pvc://runtime-jobs/runtime-workspace-549","context_ref":"runtime://workspace/31/.kodex/context/agent-run.json","context_digest":"sha256:context","runner_profile_ref":"runner-profile://codex-agent/default","runner_image_ref":"image://ghcr.io/codex-k8s/agent-runner@sha256:runner","runner_mode":"codex_agent","allowed_secret_refs":[{"kind":"runtime_api","ref":"secret://runtime/agent-token"}],"reporting_target_refs":[{"kind":"agent_run_state","ref":"agent-manager://runs/00000000-0000-0000-0000-000000000031"}]}}`),
		FleetScopeID: &fleetScopeID,
		ClusterID:    &clusterID,
	}
}

func envMap(values []corev1.EnvVar) map[string]string {
	result := make(map[string]string, len(values))
	for _, item := range values {
		result[item.Name] = item.Value
	}
	return result
}

func testClusterAccess() fleetclient.ClusterAccess {
	return fleetclient.ClusterAccess{
		ClusterID:       uuid.MustParse("00000000-0000-0000-0000-000000000011"),
		FleetScopeID:    uuid.MustParse("00000000-0000-0000-0000-000000000010"),
		ClusterKey:      "test-cluster",
		SecretStoreType: secretresolver.StoreTypeEnv,
		SecretStoreRef:  "KUBECONFIG",
	}
}

func assertExecutionCode(t *testing.T, err error, want string) {
	t.Helper()
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) {
		t.Fatalf("error = %v, want ExecutionError", err)
	}
	if executionErr.Code != want {
		t.Fatalf("error code = %q, want %q", executionErr.Code, want)
	}
}

type fakeClusterProvider struct {
	access fleetclient.ClusterAccess
	err    error
}

func (p fakeClusterProvider) GetClusterAccess(context.Context, uuid.UUID) (fleetclient.ClusterAccess, error) {
	if p.err != nil {
		return fleetclient.ClusterAccess{}, p.err
	}
	return p.access, nil
}

type fakeSecretResolver struct {
	value secretresolver.SecretValue
	err   error
}

func (r fakeSecretResolver) Resolve(context.Context, secretresolver.SecretRef) (secretresolver.SecretValue, error) {
	if r.err != nil {
		return secretresolver.SecretValue{}, r.err
	}
	return r.value, nil
}

type fakeClientFactory struct {
	client clientkubernetes.Interface
}

func (f fakeClientFactory) NewForKubeconfig([]byte) (clientkubernetes.Interface, error) {
	return f.client, nil
}
