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
	runtimeapi "k8s.io/apimachinery/pkg/runtime"
	clientkubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
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
	if created.Spec.ActiveDeadlineSeconds == nil || *created.Spec.ActiveDeadlineSeconds != int64(1) {
		t.Fatalf("activeDeadlineSeconds = %v, want 1 second from executor config", created.Spec.ActiveDeadlineSeconds)
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
	if podSpec.SecurityContext == nil || podSpec.SecurityContext.RunAsNonRoot == nil || !*podSpec.SecurityContext.RunAsNonRoot {
		t.Fatalf("pod security context = %+v, want runAsNonRoot", podSpec.SecurityContext)
	}
	if podSpec.SecurityContext.SeccompProfile == nil || podSpec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("pod seccomp profile = %+v, want RuntimeDefault", podSpec.SecurityContext.SeccompProfile)
	}
	if container.SecurityContext == nil {
		t.Fatalf("container security context is nil, want restricted context")
	}
	if container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("allowPrivilegeEscalation = %+v, want false", container.SecurityContext.AllowPrivilegeEscalation)
	}
	if container.SecurityContext.Privileged == nil || *container.SecurityContext.Privileged {
		t.Fatalf("privileged = %+v, want false", container.SecurityContext.Privileged)
	}
	if container.SecurityContext.RunAsNonRoot == nil || !*container.SecurityContext.RunAsNonRoot {
		t.Fatalf("container runAsNonRoot = %+v, want true", container.SecurityContext.RunAsNonRoot)
	}
	if container.SecurityContext.SeccompProfile == nil || container.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("container seccomp profile = %+v, want RuntimeDefault", container.SecurityContext.SeccompProfile)
	}
	if container.SecurityContext.Capabilities == nil || !hasCapability(container.SecurityContext.Capabilities.Drop, "ALL") {
		t.Fatalf("container dropped capabilities = %+v, want ALL", container.SecurityContext.Capabilities)
	}
	env := envMap(container.Env)
	if env["KODEX_AGENT_RUN_ID"] == "" || env["KODEX_AGENT_RUN_CONTEXT_PATH"] != agentRunContextPath {
		t.Fatalf("agent_run env = %v, want safe runner refs", env)
	}
	if strings.Contains(env["KODEX_AGENT_RUN_ALLOWED_SECRET_REFS_JSON"], "secret-value") {
		t.Fatalf("allowed secret refs env contains a secret value: %q", env["KODEX_AGENT_RUN_ALLOWED_SECRET_REFS_JSON"])
	}
	if env["KODEX_CODEX_SESSION_EXECUTION_SPEC_JSON"] == "" {
		t.Fatal("codex session execution spec env is empty, want safe refs")
	}
	if strings.Contains(env["KODEX_CODEX_SESSION_EXECUTION_SPEC_JSON"], "prompt_body") || strings.Contains(env["KODEX_CODEX_SESSION_EXECUTION_SPEC_JSON"], "secret-value") {
		t.Fatalf("codex execution spec env contains unsafe marker: %q", env["KODEX_CODEX_SESSION_EXECUTION_SPEC_JSON"])
	}
	if env[agentManagerGRPCAddrEnv] != "agent-manager:9090" || env[agentManagerTimeoutEnv] != "3s" {
		t.Fatalf("agent-manager reporter env = %v, want addr and timeout", env)
	}
	authEnv := envVarByName(container.Env, agentManagerAuthTokenEnv)
	if authEnv == nil || authEnv.Value != "" || authEnv.ValueFrom == nil || authEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("agent-manager auth env = %+v, want SecretKeyRef without literal token", authEnv)
	}
	if authEnv.ValueFrom.SecretKeyRef.Name != "kodex-platform-runtime" || authEnv.ValueFrom.SecretKeyRef.Key != "KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN" {
		t.Fatalf("agent-manager auth SecretKeyRef = %+v, want platform runtime token ref", authEnv.ValueFrom.SecretKeyRef)
	}
	if len(created.Annotations) != 0 || len(created.Spec.Template.Annotations) != 0 {
		t.Fatalf("annotations = %v/%v, want none from agent_run spec", created.Annotations, created.Spec.Template.Annotations)
	}
	if len(started.ArtifactRefs) != 3 {
		t.Fatalf("artifact refs = %d, want job, namespace and image refs", len(started.ArtifactRefs))
	}
}

func TestExecutorStartCreatesRestrictedKanikoBuildJob(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	job := testBuildJob()

	started, err := executor.Start(context.Background(), job)
	if err != nil {
		t.Fatalf("Start(build): %v", err)
	}
	if started.Namespace != "runtime-jobs" || started.JobName == "" || started.ExternalRef == "" {
		t.Fatalf("started build job = %+v, want namespace/name/ref", started)
	}
	created, err := client.BatchV1().Jobs("runtime-jobs").Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	if created.Spec.ActiveDeadlineSeconds == nil || *created.Spec.ActiveDeadlineSeconds != int64(1) {
		t.Fatalf("build activeDeadlineSeconds = %v, want 1 second from executor config", created.Spec.ActiveDeadlineSeconds)
	}
	podSpec := created.Spec.Template.Spec
	if podSpec.AutomountServiceAccountToken == nil || *podSpec.AutomountServiceAccountToken {
		t.Fatalf("automount service account token = %v, want disabled", podSpec.AutomountServiceAccountToken)
	}
	container := podSpec.Containers[0]
	if container.Name != buildContainerName || container.Image != "gcr.io/kaniko-project/executor:v1.24.0" {
		t.Fatalf("container = %s/%s, want Kaniko container/image", container.Name, container.Image)
	}
	if strings.Join(container.Command, " ") != kanikoCommand {
		t.Fatalf("command = %v, want fixed Kaniko executor", container.Command)
	}
	wantArgs := []string{
		"--context=dir:///workspace/context",
		"--dockerfile=/workspace/context/services/internal/runtime-manager/Dockerfile",
		"--destination=registry.local:5000/kodex/runtime-manager:0.1.0",
		"--target=runtime-manager",
		"--cache=false",
		"--snapshot-mode=redo",
		"--verbosity=info",
	}
	if strings.Join(container.Args, "\n") != strings.Join(wantArgs, "\n") {
		t.Fatalf("args = %v, want %v", container.Args, wantArgs)
	}
	if len(container.Env) != 0 {
		t.Fatalf("container env = %v, want no literal build env", container.Env)
	}
	if len(podSpec.Volumes) != 2 {
		t.Fatalf("volumes = %+v, want context PVC and registry config Secret", podSpec.Volumes)
	}
	contextVolume := podSpec.Volumes[0]
	if contextVolume.PersistentVolumeClaim == nil || contextVolume.PersistentVolumeClaim.ClaimName != "runtime-build-context-001" || !contextVolume.PersistentVolumeClaim.ReadOnly {
		t.Fatalf("context volume = %+v, want read-only build context PVC", contextVolume)
	}
	secretVolume := podSpec.Volumes[1]
	if secretVolume.Secret == nil || secretVolume.Secret.SecretName != "registry-push" {
		t.Fatalf("registry volume = %+v, want Kubernetes Secret ref", secretVolume)
	}
	if len(secretVolume.Secret.Items) != 1 || secretVolume.Secret.Items[0].Key != ".dockerconfigjson" || secretVolume.Secret.Items[0].Path != "config.json" {
		t.Fatalf("registry secret items = %+v, want docker config mounted as config.json", secretVolume.Secret.Items)
	}
	if len(container.VolumeMounts) != 2 || container.VolumeMounts[0].MountPath != buildContextMountPath || !container.VolumeMounts[0].ReadOnly || container.VolumeMounts[1].MountPath != kanikoDockerConfigPath || !container.VolumeMounts[1].ReadOnly {
		t.Fatalf("volume mounts = %+v, want fixed read-only Kaniko mounts", container.VolumeMounts)
	}
	if container.SecurityContext == nil {
		t.Fatalf("container security context is nil, want restricted context")
	}
	if container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("allowPrivilegeEscalation = %+v, want false", container.SecurityContext.AllowPrivilegeEscalation)
	}
	if container.SecurityContext.Privileged == nil || *container.SecurityContext.Privileged {
		t.Fatalf("privileged = %+v, want false", container.SecurityContext.Privileged)
	}
	if container.SecurityContext.Capabilities == nil || !hasCapability(container.SecurityContext.Capabilities.Drop, "ALL") {
		t.Fatalf("container dropped capabilities = %+v, want ALL", container.SecurityContext.Capabilities)
	}
	if strings.Contains(strings.Join(container.Args, " "), "secret-value") || strings.Contains(strings.Join(container.Args, " "), "provider payload") {
		t.Fatalf("Kaniko args contain unsafe raw marker: %v", container.Args)
	}
	if len(started.ArtifactRefs) != 3 || started.ArtifactRefs[2].ExternalRef != "registry.local:5000/kodex/runtime-manager:0.1.0" {
		t.Fatalf("artifact refs = %+v, want job, namespace and destination image ref", started.ArtifactRefs)
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

func TestExecutorStartRejectsUnsafeCodexSessionSpecBeforeCreatingJob(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	job := testAgentRunJob()
	job.JobInputJSON = []byte(strings.Replace(
		string(job.JobInputJSON),
		`"instruction_object_ref":"object://instructions/31"`,
		`"instruction_object_ref":"object://instructions/prompt_body_secret_value"`,
		1,
	))

	_, err := executor.Start(context.Background(), job)

	assertExecutionCode(t, err, "invalid_agent_run_execution_spec")
	jobs, listErr := client.BatchV1().Jobs("runtime-jobs").List(context.Background(), metav1.ListOptions{})
	if listErr != nil {
		t.Fatalf("list Jobs: %v", listErr)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("created Jobs = %d, want none before unsafe spec reaches env", len(jobs.Items))
	}
}

func TestExecutorStartRejectsIncompleteAgentRunReporterConfig(t *testing.T) {
	t.Parallel()

	_, err := NewExecutorWithClientFactory(
		fakeClusterProvider{access: testClusterAccess()},
		fakeSecretResolver{value: secretresolver.NewSecretValue([]byte("kubeconfig"))},
		Config{
			DefaultNamespace:       "runtime-jobs",
			DefaultImage:           "busybox:1.36",
			ImagePullPolicy:        "IfNotPresent",
			JobTimeout:             time.Second,
			PollInterval:           time.Millisecond,
			AgentManagerGRPCAddr:   "agent-manager:9090",
			AgentManagerAuthSecret: SecretKeyRef{Name: "kodex-platform-runtime"},
		},
		fakeClientFactory{client: fake.NewClientset()},
	)

	assertExecutionCode(t, err, "invalid_agent_run_reporter_config")
}

func TestExecutorStartRejectsBuildWithoutExecutionSpec(t *testing.T) {
	t.Parallel()

	executor := newTestExecutor(t, fake.NewClientset(), fakeClusterProvider{access: testClusterAccess()})
	job := testBuildJob()
	job.JobInputJSON = []byte(`{}`)

	_, err := executor.Start(context.Background(), job)

	assertExecutionCode(t, err, "build_execution_spec_required")
}

func TestExecutorStartRejectsBuildWithUnsupportedContextRef(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	job := testBuildJob()
	job.JobInputJSON = []byte(strings.Replace(
		string(job.JobInputJSON),
		`"build_context_ref":"pvc://runtime-jobs/runtime-build-context-001"`,
		`"build_context_ref":"stack://services/runtime-manager/context"`,
		1,
	))

	_, err := executor.Start(context.Background(), job)

	assertExecutionCode(t, err, "invalid_build_context_ref")
	jobs, listErr := client.BatchV1().Jobs("runtime-jobs").List(context.Background(), metav1.ListOptions{})
	if listErr != nil {
		t.Fatalf("list Jobs: %v", listErr)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("created Jobs = %d, want none before unsupported build context reaches Kubernetes", len(jobs.Items))
	}
}

func TestExecutorStartRejectsBuildWithUnsafeDockerfileRef(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	job := testBuildJob()
	job.JobInputJSON = []byte(strings.Replace(
		string(job.JobInputJSON),
		`"dockerfile_ref":"context://services/internal/runtime-manager/Dockerfile"`,
		`"dockerfile_ref":"context://../secret/Dockerfile"`,
		1,
	))

	_, err := executor.Start(context.Background(), job)

	assertExecutionCode(t, err, "invalid_build_dockerfile_ref")
	jobs, listErr := client.BatchV1().Jobs("runtime-jobs").List(context.Background(), metav1.ListOptions{})
	if listErr != nil {
		t.Fatalf("list Jobs: %v", listErr)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("created Jobs = %d, want none before unsafe Dockerfile ref reaches Kubernetes", len(jobs.Items))
	}
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

func TestExecutorObserveReportsBuildRunningStatus(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testBuildJob())
	if err != nil {
		t.Fatalf("Start(build): %v", err)
	}
	created, err := client.BatchV1().Jobs(started.Namespace).Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	created.Status.Active = 1
	if _, err := client.BatchV1().Jobs(started.Namespace).UpdateStatus(context.Background(), created, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update job status: %v", err)
	}

	result, done := executor.observe(context.Background(), started)

	if done || result.Phase != ExecutionPhaseRunning || result.StatusSummary == "" || strings.Contains(result.StatusSummary, "secret") {
		t.Fatalf("observe(build running) = %+v done=%v, want running safe summary", result, done)
	}
}

func TestExecutorObserveFallsBackToRuntimeJobLabels(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testBuildJob())
	if err != nil {
		t.Fatalf("Start(build): %v", err)
	}
	created, err := client.BatchV1().Jobs(started.Namespace).Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	if err := client.BatchV1().Jobs(started.Namespace).Delete(context.Background(), started.JobName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete created Job: %v", err)
	}
	created.Name = "kodex-rt-observed-by-label"
	created.ResourceVersion = ""
	created.Status.Active = 1
	if _, err := client.BatchV1().Jobs(started.Namespace).Create(context.Background(), created, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create label-selected Job: %v", err)
	}

	result, done := executor.observe(context.Background(), started)

	if done || result.Phase != ExecutionPhaseRunning {
		t.Fatalf("observe(label fallback) = %+v done=%v, want running from labels", result, done)
	}
}

func TestExecutorObserveRejectsAmbiguousRuntimeJobLabels(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testBuildJob())
	if err != nil {
		t.Fatalf("Start(build): %v", err)
	}
	created, err := client.BatchV1().Jobs(started.Namespace).Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	if err := client.BatchV1().Jobs(started.Namespace).Delete(context.Background(), started.JobName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete created Job: %v", err)
	}
	for _, name := range []string{"kodex-rt-observed-by-label-a", "kodex-rt-observed-by-label-b"} {
		clone := created.DeepCopy()
		clone.Name = name
		clone.ResourceVersion = ""
		clone.Status.Active = 1
		if _, err := client.BatchV1().Jobs(started.Namespace).Create(context.Background(), clone, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create ambiguous label-selected Job %s: %v", name, err)
		}
	}

	result, done := executor.observe(context.Background(), started)

	if !done || result.ErrorCode != "kubernetes_job_status_ambiguous" || result.Phase != ExecutionPhaseUnknown {
		t.Fatalf("observe(ambiguous labels) = %+v done=%v, want ambiguous status error", result, done)
	}
}

func TestExecutorObserveRejectsMismatchedManagedLabels(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testBuildJob())
	if err != nil {
		t.Fatalf("Start(build): %v", err)
	}
	created, err := client.BatchV1().Jobs(started.Namespace).Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	created.Labels[runtimeJobTypeLabel] = string(enum.JobTypeHealthCheck)
	if _, err := client.BatchV1().Jobs(started.Namespace).Update(context.Background(), created, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update job labels: %v", err)
	}

	result, done := executor.observe(context.Background(), started)

	if !done || result.ErrorCode != "kubernetes_job_label_mismatch" || result.Phase != ExecutionPhaseUnknown {
		t.Fatalf("observe(label mismatch) = %+v done=%v, want label mismatch", result, done)
	}
}

func TestExecutorWaitReportsBuildTimeoutCondition(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testBuildJob())
	if err != nil {
		t.Fatalf("Start(build): %v", err)
	}
	created, err := client.BatchV1().Jobs(started.Namespace).Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	created.Status.Conditions = []batchv1.JobCondition{{
		Type:    batchv1.JobFailed,
		Status:  corev1.ConditionTrue,
		Reason:  "DeadlineExceeded",
		Message: "Job exceeded active deadline",
	}}
	if _, err := client.BatchV1().Jobs(started.Namespace).UpdateStatus(context.Background(), created, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update job status: %v", err)
	}

	result := executor.Wait(context.Background(), started)

	if result.Succeeded || result.ErrorCode != "kubernetes_job_timeout" || result.Phase != ExecutionPhaseTimedOut {
		t.Fatalf("Wait(build timeout) = %+v, want timeout", result)
	}
}

func TestExecutorWaitRedactsUnsafeBuildFailureSummary(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testBuildJob())
	if err != nil {
		t.Fatalf("Start(build): %v", err)
	}
	created, err := client.BatchV1().Jobs(started.Namespace).Get(context.Background(), started.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created Job: %v", err)
	}
	created.Status.Conditions = []batchv1.JobCondition{{
		Type:    batchv1.JobFailed,
		Status:  corev1.ConditionTrue,
		Reason:  "BackoffLimitExceeded",
		Message: "registry token=secret-value",
	}}
	if _, err := client.BatchV1().Jobs(started.Namespace).UpdateStatus(context.Background(), created, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update job status: %v", err)
	}

	result := executor.Wait(context.Background(), started)

	if result.ErrorCode != "kubernetes_job_failed" || result.ErrorMessage != "Kubernetes Job failed" || strings.Contains(result.StatusSummary, "secret-value") {
		t.Fatalf("Wait(build unsafe failure) = %+v, want generic safe failure summary", result)
	}
}

func TestExecutorWaitDoesNotCollectAgentRunPodLogs(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	client.Fake.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtimeapi.Object, error) {
		t.Fatalf("agent_run diagnostics must not list pods for raw log collection")
		return true, &corev1.PodList{}, nil
	})
	executor := newTestExecutor(t, client, fakeClusterProvider{access: testClusterAccess()})
	started, err := executor.Start(context.Background(), testAgentRunJob())
	if err != nil {
		t.Fatalf("Start(agent_run): %v", err)
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

	if !result.Succeeded || result.ShortLogTail != "" {
		t.Fatalf("Wait(agent_run) = %+v, want success without raw log tail", result)
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
		AgentManagerGRPCAddr:    "agent-manager:9090",
		AgentManagerAuthSecret:  SecretKeyRef{Name: "kodex-platform-runtime", Key: "KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN"},
		AgentManagerTimeout:     3 * time.Second,
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
		JobInputJSON: []byte(`{"agent_run_execution_spec":{"agent_run_id":"00000000-0000-0000-0000-000000000031","slot_id":"00000000-0000-0000-0000-000000000032","expected_materialization_id":"00000000-0000-0000-0000-000000000033","expected_materialization_fingerprint":"sha256:workspace","workspace_ref":"runtime://workspace/31","workspace_mount_ref":"mount://workspace/31","workspace_pvc_ref":"pvc://runtime-jobs/runtime-workspace-549","context_ref":"runtime://workspace/31/.kodex/context/agent-run.json","context_digest":"sha256:context","runner_profile_ref":"runner-profile://codex-agent/default","runner_image_ref":"image://ghcr.io/codex-k8s/agent-runner@sha256:runner","runner_mode":"codex_agent","allowed_secret_refs":[{"kind":"runtime_api","ref":"secret://runtime/agent-token"}],"reporting_target_refs":[{"kind":"agent_run_state","ref":"agent-manager://runs/00000000-0000-0000-0000-000000000031"}],"codex_session_execution_spec":{"instruction_object_ref":"object://instructions/31","instruction_object_digest":"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","result_schema_ref":"object://schemas/codex-result-v1","result_schema_digest":"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","workspace_snapshot_ref":"runtime://workspace-snapshots/31","hook_endpoint_ref":"hook://codex-hook-ingress/agent-runner","callback_refs":[{"kind":"agent_run_state","ref":"agent-manager://runs/00000000-0000-0000-0000-000000000031"}],"timeout_seconds":1800,"runner_profile_ref":"runner-profile://codex-agent/default","runner_mode":"codex_agent","output_refs":[{"kind":"last_message","ref":"object://codex-output/last-message"}],"result_refs":[{"kind":"result_metadata","ref":"object://codex-output/result-metadata"}],"allowed_secret_refs":[{"kind":"runtime_api","ref":"secret://runtime/agent-token"}]}}}`),
		FleetScopeID: &fleetScopeID,
		ClusterID:    &clusterID,
	}
}

func testBuildJob() entity.Job {
	fleetScopeID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	clusterID := uuid.MustParse("00000000-0000-0000-0000-000000000011")
	return entity.Job{
		Base:         entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000044"), Version: 2},
		JobType:      enum.JobTypeBuild,
		Status:       enum.JobStatusClaimed,
		JobInputJSON: []byte(`{"build_execution_spec":{"source_ref":"git://github.com/codex-k8s/kodex","source_commit_sha":"0123456789abcdef0123456789abcdef01234567","service_key":"runtime-manager","image_ref":"image://registry.local:5000/kodex/runtime-manager","image_tag":"0.1.0","build_context_ref":"pvc://runtime-jobs/runtime-build-context-001","build_context_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","dockerfile_ref":"context://services/internal/runtime-manager/Dockerfile","dockerfile_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","dockerfile_target":"runtime-manager","builder_image_ref":"image://gcr.io/kaniko-project/executor:v1.24.0","build_plan_fingerprint":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","allowed_secret_refs":[{"kind":"registry","ref":"secret://runtime/registry-push"}],"output_refs":[{"kind":"image_ref","ref":"runtime://artifacts/images/runtime-manager"}]}}`),
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

func envVarByName(values []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range values {
		if values[i].Name == name {
			return &values[i]
		}
	}
	return nil
}

func hasCapability(values []corev1.Capability, want corev1.Capability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
