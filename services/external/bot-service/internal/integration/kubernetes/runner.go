package kubernetes

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	defaultNamespaceFile         = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	runnerComponent              = "agent-run"
	sessionComponent             = "agent-session"
	sessionTokenComponent        = "agent-session-token"
	sessionTokenFinalizer        = "matter-codex.dev/session-token-protection"
	labelRunID                   = "matter-codex.dev/run-id"
	labelSessionKey              = "matter-codex.dev/session-key"
	labelAgentRole               = "matter-codex.dev/agent-role"
	labelOpenAIAccount           = "matter-codex.dev/openai-account"
	labelGitHubAccount           = "matter-codex.dev/github-account"
	labelAuthCheckJob            = "matter-codex.dev/auth-check-job"
	codexAuthSecretVolume        = "codex-auth-secret"
	gitHubSecretVolume           = "github-secret"
	sessionSecretVolume          = "session-secret"
	promptVolume                 = "agent-prompt"
	runnerHomeVolume             = "runner-home"
	runnerTmpVolume              = "runner-tmp"
	runnerDevShmVolume           = "runner-dev-shm"
	runnerHomePath               = "/home/matter-codex"
	runnerTmpPath                = "/tmp"
	runnerDevShmPath             = "/dev/shm"
	runtimeEnvAllowlist          = "MATTERCODEX_RUNTIME_ENV_ALLOWLIST"
	runtimeSensitiveEnvAllowlist = "MATTERCODEX_RUNTIME_SENSITIVE_ENV_ALLOWLIST"
	runnerInitPath               = "/sbin/tini"
	runnerBinaryName             = "matter-codex-agent-runner"
	runnerUID                    = int64(10001)
	runnerGID                    = int64(10001)
	runnerUtilityCPURequest      = "100m"
	runnerUtilityMemoryRequest   = "128Mi"
	runnerSessionCPURequest      = "500m"
	runnerSessionMemoryRequest   = "1Gi"
	runnerSessionMemoryLimit     = "64Gi"
	runnerUtilityMemoryLimit     = "4Gi"
	runnerDevShmSizeLimit        = "8Gi"
	kubernetesAccessReadOnly     = "read-only"
	kubernetesAccessClusterAdmin = "cluster-admin"
)

var (
	codexDeviceCodeRE         = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{5}\b`)
	codexAuthRevisionSuffixRE = regexp.MustCompile(`-rev-[a-f0-9]{12}$`)
	runtimeEnvNameRE          = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,127}$`)
	codexAuthSecretCheckWait  = 5 * time.Minute
)

type Config struct {
	Namespace                             string
	KubeconfigPath                        string
	SmokeImage                            string
	AgentRunnerImage                      string
	CodexPackage                          string
	WorkspaceStorageSize                  string
	SessionCPURequest                     string
	SessionMemoryRequest                  string
	SessionMemoryLimit                    string
	UtilityMemoryLimit                    string
	DevShmSizeLimit                       string
	JobTTLSecondsAfterFinish              int32
	AuthCheckJobTTLSecondsAfterFinish     int32
	LogTailLines                          int64
	AgentRunnerServiceAccount             string
	AgentRunnerClusterAdminServiceAccount string
	CodexAuthSecretName                   string
	GitHubSecretName                      string
}

type Runner struct {
	client                                kubernetes.Interface
	restConfig                            *rest.Config
	namespace                             string
	smokeImage                            string
	agentRunnerImage                      string
	codexPackage                          string
	workspaceStorage                      resource.Quantity
	sessionCPURequest                     resource.Quantity
	sessionMemoryRequest                  resource.Quantity
	sessionMemoryLimit                    resource.Quantity
	utilityMemoryLimit                    resource.Quantity
	devShmSizeLimit                       resource.Quantity
	jobTTLSecondsAfterFinish              int32
	authCheckJobTTLSecondsAfterFinish     int32
	logTailLines                          int64
	agentRunnerServiceAccount             string
	agentRunnerClusterAdminServiceAccount string
	codexAuthSecretName                   string
	gitHubSecretName                      string
}

func (runner *Runner) InspectSecretIntegrity(ctx context.Context, input runtimerepo.SecretIntegrityInput) (runtimerepo.SecretIntegrity, error) {
	input.SecretName = strings.TrimSpace(input.SecretName)
	input.SecretKey = strings.TrimSpace(input.SecretKey)
	if input.SecretName == "" || input.SecretKey == "" {
		return runtimerepo.SecretIntegrity{}, fmt.Errorf("secret name and key are required")
	}
	secret, err := runner.client.CoreV1().Secrets(runner.namespace).Get(ctx, input.SecretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return runtimerepo.SecretIntegrity{}, fmt.Errorf("%w: %s", runtimerepo.ErrSecretNotFound, input.SecretName)
		}
		return runtimerepo.SecretIntegrity{}, fmt.Errorf("inspect secret metadata: %w", err)
	}
	value, ok := secret.Data[input.SecretKey]
	if !ok || len(value) == 0 {
		return runtimerepo.SecretIntegrity{}, fmt.Errorf("secret key is missing")
	}
	return secretIntegrity(secret, input.SecretKey, value), nil
}

func secretIntegrity(secret *corev1.Secret, secretKey string, value []byte) runtimerepo.SecretIntegrity {
	digest := sha256.Sum256(value)
	return runtimerepo.SecretIntegrity{
		SecretName: secret.Name, SecretKey: secretKey,
		ContentSHA256: hex.EncodeToString(digest[:]),
		UID:           string(secret.UID), ResourceVersion: secret.ResourceVersion,
	}
}

func runnerCommand() []string {
	return []string{runnerInitPath, "--", runnerBinaryName}
}

func normalizedKubernetesAccess(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case kubernetesAccessClusterAdmin:
		return kubernetesAccessClusterAdmin
	default:
		return kubernetesAccessReadOnly
	}
}

var _ runtimerepo.Runner = (*Runner)(nil)

func NewRunner(cfg Config) (*Runner, error) {
	restConfig, err := kubernetesConfig(cfg.KubeconfigPath)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return newRunnerWithClientAndConfig(client, rest.CopyConfig(restConfig), cfg)
}

func NewRunnerWithClient(client kubernetes.Interface, cfg Config) (*Runner, error) {
	return newRunnerWithClientAndConfig(client, nil, cfg)
}

func newRunnerWithClientAndConfig(client kubernetes.Interface, restConfig *rest.Config, cfg Config) (*Runner, error) {
	if client == nil {
		return nil, fmt.Errorf("kubernetes client is required")
	}
	namespace, err := runtimeNamespace(cfg.Namespace)
	if err != nil {
		return nil, err
	}
	storage, err := resource.ParseQuantity(defaultString(cfg.WorkspaceStorageSize, "1Gi"))
	if err != nil {
		return nil, fmt.Errorf("parse workspace storage size: %w", err)
	}
	sessionCPURequest, err := parsePositiveResourceQuantity(cfg.SessionCPURequest, runnerSessionCPURequest, "session cpu request")
	if err != nil {
		return nil, err
	}
	sessionMemoryRequest, err := parsePositiveResourceQuantity(cfg.SessionMemoryRequest, runnerSessionMemoryRequest, "session memory request")
	if err != nil {
		return nil, err
	}
	sessionMemoryLimit, err := parsePositiveResourceQuantity(cfg.SessionMemoryLimit, runnerSessionMemoryLimit, "session memory limit")
	if err != nil {
		return nil, err
	}
	if sessionMemoryRequest.Cmp(sessionMemoryLimit) > 0 {
		return nil, fmt.Errorf("session memory request must not exceed session memory limit")
	}
	utilityMemoryLimit, err := parsePositiveResourceQuantity(cfg.UtilityMemoryLimit, runnerUtilityMemoryLimit, "utility memory limit")
	if err != nil {
		return nil, err
	}
	devShmSizeLimit, err := parsePositiveResourceQuantity(cfg.DevShmSizeLimit, runnerDevShmSizeLimit, "dev shm size limit")
	if err != nil {
		return nil, err
	}
	return &Runner{
		client:                                client,
		restConfig:                            restConfig,
		namespace:                             namespace,
		smokeImage:                            defaultString(cfg.SmokeImage, "busybox:1.36"),
		agentRunnerImage:                      defaultString(cfg.AgentRunnerImage, "matter-codex-agent-runner:dev"),
		codexPackage:                          defaultString(cfg.CodexPackage, "@openai/codex@0.144.1"),
		workspaceStorage:                      storage,
		sessionCPURequest:                     sessionCPURequest,
		sessionMemoryRequest:                  sessionMemoryRequest,
		sessionMemoryLimit:                    sessionMemoryLimit,
		utilityMemoryLimit:                    utilityMemoryLimit,
		devShmSizeLimit:                       devShmSizeLimit,
		jobTTLSecondsAfterFinish:              defaultInt32(cfg.JobTTLSecondsAfterFinish, 86400),
		authCheckJobTTLSecondsAfterFinish:     defaultInt32(cfg.AuthCheckJobTTLSecondsAfterFinish, 300),
		logTailLines:                          defaultInt64(cfg.LogTailLines, 40),
		agentRunnerServiceAccount:             defaultString(cfg.AgentRunnerServiceAccount, "matter-codex-agent-runner"),
		agentRunnerClusterAdminServiceAccount: defaultString(cfg.AgentRunnerClusterAdminServiceAccount, "matter-codex-agent-runner-cluster-admin"),
		codexAuthSecretName:                   defaultString(cfg.CodexAuthSecretName, "matter-codex-codex-auth"),
		gitHubSecretName:                      defaultString(cfg.GitHubSecretName, "matter-codex-github"),
	}, nil
}

func (runner *Runner) StartSmokeRun(ctx context.Context, input runtimerepo.SmokeRunInput) (runtimerepo.StartedRun, error) {
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("run id is required")
	}
	role := defaultString(input.Role, "smoke")
	pvcName := workspacePVCName(runID)
	jobName := runnerJobName(runID)

	created := false
	if _, err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Create(ctx, runner.smokePVC(runID, role), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create workspace pvc: %w", err)
		}
	} else {
		created = true
	}
	if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.smokeJob(runID, role), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create runner job: %w", err)
		}
	} else {
		created = true
	}
	return runtimerepo.StartedRun{
		RunID:     runID,
		Namespace: runner.namespace,
		JobName:   jobName,
		PVCName:   pvcName,
		Created:   created,
	}, nil
}

func (runner *Runner) agentRunnerServiceAccountForAccess(kubernetesAccess string) string {
	if normalizedKubernetesAccess(kubernetesAccess) == kubernetesAccessClusterAdmin {
		return runner.agentRunnerClusterAdminServiceAccount
	}
	return runner.agentRunnerServiceAccount
}

func (runner *Runner) StartCodexAuthSession(ctx context.Context, input runtimerepo.CodexAuthSessionInput) (runtimerepo.CodexAuthSession, error) {
	input.AccountName = strings.TrimSpace(input.AccountName)
	input.SecretName = strings.TrimSpace(input.SecretName)
	if input.AccountName == "" {
		return runtimerepo.CodexAuthSession{}, fmt.Errorf("openai account name is required")
	}
	if input.SecretName == "" {
		return runtimerepo.CodexAuthSession{}, fmt.Errorf("codex auth secret name is required")
	}
	jobName := codexAuthJobName(input.AccountName)
	created := false
	if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.codexAuthJob(input.AccountName, input.SecretName), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.CodexAuthSession{}, fmt.Errorf("create codex auth job: %w", err)
		}
		existing, getErr := runner.client.BatchV1().Jobs(runner.namespace).Get(ctx, jobName, metav1.GetOptions{})
		if getErr != nil {
			return runtimerepo.CodexAuthSession{}, fmt.Errorf("get existing codex auth job: %w", getErr)
		}
		if existing.Status.Active == 0 {
			if err := runner.deleteJobAndWait(ctx, jobName, 20*time.Second); err != nil {
				return runtimerepo.CodexAuthSession{}, err
			}
			if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.codexAuthJob(input.AccountName, input.SecretName), metav1.CreateOptions{}); err != nil {
				return runtimerepo.CodexAuthSession{}, fmt.Errorf("recreate codex auth job: %w", err)
			}
			created = true
		}
	} else {
		created = true
	}
	return runtimerepo.CodexAuthSession{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   runner.namespace,
		JobName:     jobName,
		Created:     created,
	}, nil
}

func (runner *Runner) GetCodexAuthStatus(ctx context.Context, accountName string, secretName string) (runtimerepo.CodexAuthStatus, error) {
	accountName = strings.TrimSpace(accountName)
	secretName = strings.TrimSpace(secretName)
	if accountName == "" {
		return runtimerepo.CodexAuthStatus{}, fmt.Errorf("openai account name is required")
	}
	status := runtimerepo.CodexAuthStatus{
		AccountName: accountName,
		SecretName:  secretName,
		Namespace:   runner.namespace,
		JobName:     codexAuthJobName(accountName),
	}
	job, err := runner.client.BatchV1().Jobs(runner.namespace).Get(ctx, status.JobName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return status, nil
		}
		return runtimerepo.CodexAuthStatus{}, fmt.Errorf("get codex auth job: %w", err)
	}
	status.Exists = true
	status.JobActive = job.Status.Active
	status.JobSucceeded = job.Status.Succeeded
	status.JobFailed = job.Status.Failed

	pod, ok, err := runner.latestPodByLabels(ctx, labels.Set{
		"app.kubernetes.io/component": "codex-auth",
		labelOpenAIAccount:            accountName,
	})
	if err != nil {
		return runtimerepo.CodexAuthStatus{}, err
	}
	if !ok {
		return status, nil
	}
	status.PodName = pod.Name
	status.PodPhase = string(pod.Status.Phase)
	status.LogTail = runner.logTail(ctx, pod.Name)
	status.DeviceURL, status.DeviceCode = parseCodexDeviceAuth(status.LogTail)
	status.AuthReady = runner.codexAuthJSONReady(ctx, pod.Name)
	return status, nil
}

func (runner *Runner) CheckCodexAuthSecret(ctx context.Context, input runtimerepo.CodexAuthSecretCheckInput) (runtimerepo.CodexAuthSecretCheckResult, error) {
	input.AccountName = strings.TrimSpace(input.AccountName)
	input.SecretName = strings.TrimSpace(input.SecretName)
	if input.AccountName == "" {
		return runtimerepo.CodexAuthSecretCheckResult{}, fmt.Errorf("openai account name is required")
	}
	if input.SecretName == "" {
		return runtimerepo.CodexAuthSecretCheckResult{}, fmt.Errorf("codex auth secret name is required")
	}
	jobName := codexAuthCheckJobName(input.AccountName)
	result := runtimerepo.CodexAuthSecretCheckResult{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   runner.namespace,
		JobName:     jobName,
	}
	if _, err := runner.client.CoreV1().Secrets(runner.namespace).Get(ctx, input.SecretName, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			result.LogTail = "codex auth secret is missing"
			return result, nil
		}
		return runtimerepo.CodexAuthSecretCheckResult{}, fmt.Errorf("get codex auth secret: %w", err)
	}
	if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.codexAuthSecretCheckJob(input, jobName), metav1.CreateOptions{}); err != nil {
		if quotaExceeded(err) {
			return runtimerepo.CodexAuthSecretCheckResult{}, runtimerepo.NewAgentSessionCapacityError("Kubernetes resource quota rejected the Codex auth check job", err)
		}
		return runtimerepo.CodexAuthSecretCheckResult{}, fmt.Errorf("create codex auth check job: %w", err)
	}
	defer runner.cleanupCodexAuthCheckJob(ctx, jobName)

	deadline := time.Now().Add(codexAuthSecretCheckWait)
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()

	for {
		runner.fillCodexAuthCheckPodStatus(ctx, &result)
		if result.PodName != "" {
			if capacityErr := runner.sessionPodSchedulingCapacityError(ctx, result.PodName); capacityErr != nil {
				return result, capacityErr
			}
		}
		job, err := runner.client.BatchV1().Jobs(runner.namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return result, nil
			}
			return runtimerepo.CodexAuthSecretCheckResult{}, fmt.Errorf("get codex auth check job: %w", err)
		}
		switch {
		case job.Status.Succeeded > 0:
			result.Ready = true
			runner.fillCodexAuthCheckPodStatus(ctx, &result)
			return result, nil
		case job.Status.Failed > 0:
			runner.fillCodexAuthCheckPodStatus(ctx, &result)
			return result, nil
		case time.Now().After(deadline):
			runner.fillCodexAuthCheckPodStatus(ctx, &result)
			return result, fmt.Errorf(
				"codex auth check timed out: job %s pod %s phase %s",
				jobName,
				defaultString(result.PodName, "unknown"),
				defaultString(result.PodPhase, "unknown"),
			)
		}

		select {
		case <-ctx.Done():
			return runtimerepo.CodexAuthSecretCheckResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (runner *Runner) CompleteCodexAuthSession(ctx context.Context, input runtimerepo.CodexAuthCompleteInput) (runtimerepo.CodexAuthCompleteResult, error) {
	input.AccountName = strings.TrimSpace(input.AccountName)
	input.SecretName = strings.TrimSpace(input.SecretName)
	if input.AccountName == "" {
		return runtimerepo.CodexAuthCompleteResult{}, fmt.Errorf("openai account name is required")
	}
	if input.SecretName == "" {
		return runtimerepo.CodexAuthCompleteResult{}, fmt.Errorf("codex auth secret name is required")
	}
	pod, ok, err := runner.latestPodByLabels(ctx, labels.Set{
		"app.kubernetes.io/component": "codex-auth",
		labelOpenAIAccount:            input.AccountName,
	})
	if err != nil {
		return runtimerepo.CodexAuthCompleteResult{}, err
	}
	if !ok {
		return runtimerepo.CodexAuthCompleteResult{}, fmt.Errorf("codex auth pod not found")
	}
	authJSON, err := runner.execPod(ctx, pod.Name, []string{"matter-codex-agent-runner", "print-auth-json"})
	if err != nil {
		return runtimerepo.CodexAuthCompleteResult{}, fmt.Errorf("read codex auth.json: %w", err)
	}
	if len(bytes.TrimSpace(authJSON)) == 0 {
		return runtimerepo.CodexAuthCompleteResult{}, fmt.Errorf("codex auth.json is empty")
	}
	revisionSecretName := codexAuthRevisionSecretName(input.SecretName, authJSON)
	integrity, err := runner.createCodexAuthRevisionSecret(ctx, input.AccountName, revisionSecretName, authJSON)
	if err != nil {
		return runtimerepo.CodexAuthCompleteResult{}, err
	}
	return runtimerepo.CodexAuthCompleteResult{
		AccountName: input.AccountName,
		SecretName:  revisionSecretName,
		Namespace:   runner.namespace,
		Integrity:   integrity,
		Saved:       true,
	}, nil
}

func (runner *Runner) CleanupCodexAuthSession(ctx context.Context, accountName string) (runtimerepo.CodexAuthCleanupResult, error) {
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return runtimerepo.CodexAuthCleanupResult{}, fmt.Errorf("openai account name is required")
	}
	result := runtimerepo.CodexAuthCleanupResult{
		AccountName: accountName,
		Namespace:   runner.namespace,
	}
	background := metav1.DeletePropagationBackground
	if err := runner.client.BatchV1().Jobs(runner.namespace).Delete(ctx, codexAuthJobName(accountName), metav1.DeleteOptions{PropagationPolicy: &background}); err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.CodexAuthCleanupResult{}, fmt.Errorf("delete codex auth job: %w", err)
		}
	} else {
		result.JobDeleted = true
	}
	return result, nil
}

func (runner *Runner) cleanupCodexAuthCheckJob(ctx context.Context, jobName string) {
	jobName = strings.TrimSpace(jobName)
	if jobName == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	background := metav1.DeletePropagationBackground
	_ = runner.client.BatchV1().Jobs(runner.namespace).Delete(cleanupCtx, jobName, metav1.DeleteOptions{PropagationPolicy: &background})
}

func (runner *Runner) DeleteCodexAuthAccount(ctx context.Context, accountName string, secretName string) (runtimerepo.CodexAuthAccountDeleteResult, error) {
	accountName = strings.TrimSpace(accountName)
	secretName = strings.TrimSpace(secretName)
	if accountName == "" {
		return runtimerepo.CodexAuthAccountDeleteResult{}, fmt.Errorf("openai account name is required")
	}
	if secretName == "" {
		return runtimerepo.CodexAuthAccountDeleteResult{}, fmt.Errorf("codex auth secret name is required")
	}
	cleanup, err := runner.CleanupCodexAuthSession(ctx, accountName)
	if err != nil {
		return runtimerepo.CodexAuthAccountDeleteResult{}, err
	}
	result := runtimerepo.CodexAuthAccountDeleteResult{
		AccountName: accountName,
		SecretName:  secretName,
		Namespace:   runner.namespace,
		JobDeleted:  cleanup.JobDeleted,
	}
	if err := runner.client.CoreV1().Secrets(runner.namespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.CodexAuthAccountDeleteResult{}, fmt.Errorf("delete codex auth secret: %w", err)
		}
	} else {
		result.SecretDeleted = true
	}
	return result, nil
}

func (runner *Runner) UpsertGitHubTokenSecret(ctx context.Context, input runtimerepo.GitHubTokenSecretInput) (runtimerepo.GitHubTokenSecret, error) {
	input.AccountName = strings.TrimSpace(input.AccountName)
	input.SecretName = strings.TrimSpace(input.SecretName)
	input.Token = strings.TrimSpace(input.Token)
	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.TrimSpace(input.Email)
	if input.AccountName == "" {
		return runtimerepo.GitHubTokenSecret{}, fmt.Errorf("github account name is required")
	}
	if input.SecretName == "" {
		return runtimerepo.GitHubTokenSecret{}, fmt.Errorf("github token secret name is required")
	}
	if input.Token == "" {
		return runtimerepo.GitHubTokenSecret{}, fmt.Errorf("github token is required")
	}
	if input.Username == "" {
		return runtimerepo.GitHubTokenSecret{}, fmt.Errorf("github username is required")
	}
	if input.Email == "" {
		return runtimerepo.GitHubTokenSecret{}, fmt.Errorf("github email is required")
	}
	created, err := runner.upsertGitHubTokenSecret(ctx, input)
	if err != nil {
		return runtimerepo.GitHubTokenSecret{}, err
	}
	return runtimerepo.GitHubTokenSecret{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   runner.namespace,
		Created:     created,
	}, nil
}

func (runner *Runner) GetGitHubTokenSecret(ctx context.Context, accountName string, secretName string) (runtimerepo.GitHubTokenSecretCredential, error) {
	accountName = strings.TrimSpace(accountName)
	secretName = strings.TrimSpace(secretName)
	if accountName == "" {
		return runtimerepo.GitHubTokenSecretCredential{}, fmt.Errorf("github account name is required")
	}
	if secretName == "" {
		return runtimerepo.GitHubTokenSecretCredential{}, fmt.Errorf("github token secret name is required")
	}
	secret, err := runner.client.CoreV1().Secrets(runner.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return runtimerepo.GitHubTokenSecretCredential{}, fmt.Errorf("get github token secret: %w", err)
	}
	token := strings.TrimSpace(string(secret.Data["github-token"]))
	username := strings.TrimSpace(string(secret.Data["github-username"]))
	email := strings.TrimSpace(string(secret.Data["github-email"]))
	if token == "" {
		return runtimerepo.GitHubTokenSecretCredential{}, fmt.Errorf("github token secret is missing token")
	}
	if username == "" {
		return runtimerepo.GitHubTokenSecretCredential{}, fmt.Errorf("github token secret is missing username")
	}
	if email == "" {
		return runtimerepo.GitHubTokenSecretCredential{}, fmt.Errorf("github token secret is missing email")
	}
	return runtimerepo.GitHubTokenSecretCredential{
		AccountName: accountName,
		SecretName:  secretName,
		Namespace:   runner.namespace,
		Token:       token,
		Username:    username,
		Email:       email,
	}, nil
}

func (runner *Runner) DeleteGitHubTokenSecret(ctx context.Context, accountName string, secretName string) (runtimerepo.GitHubTokenSecretDeleteResult, error) {
	accountName = strings.TrimSpace(accountName)
	secretName = strings.TrimSpace(secretName)
	if accountName == "" {
		return runtimerepo.GitHubTokenSecretDeleteResult{}, fmt.Errorf("github account name is required")
	}
	if secretName == "" {
		return runtimerepo.GitHubTokenSecretDeleteResult{}, fmt.Errorf("github token secret name is required")
	}
	result := runtimerepo.GitHubTokenSecretDeleteResult{
		AccountName: accountName,
		SecretName:  secretName,
		Namespace:   runner.namespace,
	}
	if err := runner.client.CoreV1().Secrets(runner.namespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.GitHubTokenSecretDeleteResult{}, fmt.Errorf("delete github token secret: %w", err)
		}
	} else {
		result.SecretDeleted = true
	}
	return result, nil
}

func (runner *Runner) UpsertProjectRuntimeVariableSecret(ctx context.Context, input runtimerepo.ProjectRuntimeVariableSecretInput) (runtimerepo.ProjectRuntimeVariableSecret, error) {
	secretName := strings.TrimSpace(input.Variable.SecretName)
	secretKey := defaultString(input.Variable.SecretKey, "value")
	value := input.Value
	if secretName == "" {
		return runtimerepo.ProjectRuntimeVariableSecret{}, fmt.Errorf("runtime variable secret name is required")
	}
	if strings.TrimSpace(input.Variable.Name) == "" {
		return runtimerepo.ProjectRuntimeVariableSecret{}, fmt.Errorf("runtime variable env name is required")
	}
	if value == "" {
		return runtimerepo.ProjectRuntimeVariableSecret{}, fmt.Errorf("runtime variable value is required")
	}
	labels := map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": "runtime-variable",
	}
	if projectSlug := kubernetesLabelValue(input.ProjectSlug); projectSlug != "" {
		labels["matter-codex.dev/project"] = projectSlug
	}
	secretClient := runner.client.CoreV1().Secrets(runner.namespace)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   secretName,
			Labels: labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{secretKey: []byte(value)},
	}
	if _, err := secretClient.Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.ProjectRuntimeVariableSecret{}, fmt.Errorf("create runtime variable secret: %w", err)
		}
		current, getErr := secretClient.Get(ctx, secretName, metav1.GetOptions{})
		if getErr != nil {
			return runtimerepo.ProjectRuntimeVariableSecret{}, fmt.Errorf("get runtime variable secret: %w", getErr)
		}
		if current.Data == nil {
			current.Data = map[string][]byte{}
		}
		current.Data[secretKey] = []byte(value)
		if current.Labels == nil {
			current.Labels = map[string]string{}
		}
		for key, labelValue := range labels {
			current.Labels[key] = labelValue
		}
		if _, updateErr := secretClient.Update(ctx, current, metav1.UpdateOptions{}); updateErr != nil {
			return runtimerepo.ProjectRuntimeVariableSecret{}, fmt.Errorf("update runtime variable secret: %w", updateErr)
		}
		return runtimerepo.ProjectRuntimeVariableSecret{SecretName: secretName, Namespace: runner.namespace, Created: false}, nil
	}
	return runtimerepo.ProjectRuntimeVariableSecret{SecretName: secretName, Namespace: runner.namespace, Created: true}, nil
}

func (runner *Runner) DeleteProjectRuntimeVariableSecret(ctx context.Context, secretName string) (runtimerepo.ProjectRuntimeVariableSecret, error) {
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		return runtimerepo.ProjectRuntimeVariableSecret{}, fmt.Errorf("runtime variable secret name is required")
	}
	result := runtimerepo.ProjectRuntimeVariableSecret{SecretName: secretName, Namespace: runner.namespace}
	if err := runner.client.CoreV1().Secrets(runner.namespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.ProjectRuntimeVariableSecret{}, fmt.Errorf("delete runtime variable secret: %w", err)
		}
		return result, nil
	}
	result.Created = true
	return result, nil
}

func (runner *Runner) StartDeveloperRun(ctx context.Context, input runtimerepo.DeveloperRunInput) (runtimerepo.StartedRun, error) {
	input = normalizeDeveloperRunInput(input)
	if input.RunID == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("run id is required")
	}
	if input.Prompt == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("prompt is required")
	}
	pvcName := workspacePVCName(input.RunID)
	jobName := runnerJobName(input.RunID)

	created := false
	if _, err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Create(ctx, runner.smokePVC(input.RunID, input.Profile), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create workspace pvc: %w", err)
		}
	} else {
		created = true
	}
	if _, err := runner.client.CoreV1().ConfigMaps(runner.namespace).Create(ctx, runner.promptConfigMap(input.RunID, input.Profile, input.Prompt), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create prompt configmap: %w", err)
		}
	} else {
		created = true
	}
	if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.developerJob(input), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create developer runner job: %w", err)
		}
	} else {
		created = true
	}
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: runner.namespace,
		JobName:   jobName,
		PVCName:   pvcName,
		Created:   created,
	}, nil
}

func (runner *Runner) StartReviewRun(ctx context.Context, input runtimerepo.ReviewRunInput) (runtimerepo.StartedRun, error) {
	input = normalizeReviewRunInput(input)
	if input.RunID == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("run id is required")
	}
	if input.Owner == "" || input.Name == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("repository is required")
	}
	if input.PRNumber <= 0 {
		return runtimerepo.StartedRun{}, fmt.Errorf("pr number is required")
	}
	if input.Prompt == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("prompt is required")
	}
	pvcName := workspacePVCName(input.RunID)
	jobName := runnerJobName(input.RunID)

	created := false
	if _, err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Create(ctx, runner.smokePVC(input.RunID, input.Profile), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create workspace pvc: %w", err)
		}
	} else {
		created = true
	}
	if _, err := runner.client.CoreV1().ConfigMaps(runner.namespace).Create(ctx, runner.promptConfigMap(input.RunID, input.Profile, input.Prompt), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create prompt configmap: %w", err)
		}
	} else {
		created = true
	}
	if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.reviewJob(input), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create reviewer runner job: %w", err)
		}
	} else {
		created = true
	}
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: runner.namespace,
		JobName:   jobName,
		PVCName:   pvcName,
		Created:   created,
	}, nil
}

func (runner *Runner) StartChatRun(ctx context.Context, input runtimerepo.ChatRunInput) (runtimerepo.StartedRun, error) {
	input = normalizeChatRunInput(input)
	if input.RunID == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("run id is required")
	}
	if input.Prompt == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("prompt is required")
	}
	if input.CodexAuthSecretName == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("codex auth secret name is required")
	}
	pvcName := workspacePVCName(input.RunID)
	jobName := runnerJobName(input.RunID)

	created := false
	if _, err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Create(ctx, runner.smokePVC(input.RunID, input.Profile), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create workspace pvc: %w", err)
		}
	} else {
		created = true
	}
	if _, err := runner.client.CoreV1().ConfigMaps(runner.namespace).Create(ctx, runner.promptConfigMap(input.RunID, input.Profile, input.Prompt), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create prompt configmap: %w", err)
		}
	} else {
		created = true
	}
	if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.chatJob(input), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create chat runner job: %w", err)
		}
	} else {
		created = true
	}
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: runner.namespace,
		JobName:   jobName,
		PVCName:   pvcName,
		Created:   created,
	}, nil
}

func (runner *Runner) StartAgentSession(ctx context.Context, input runtimerepo.AgentSessionPodInput) (runtimerepo.StartedAgentSession, error) {
	input.SessionKey = strings.TrimSpace(input.SessionKey)
	input.Role = strings.TrimSpace(input.Role)
	input.BotServiceURL = strings.TrimRight(strings.TrimSpace(input.BotServiceURL), "/")
	input.InternalToken = strings.TrimSpace(input.InternalToken)
	input.CodexAuthSecretName = strings.TrimSpace(input.CodexAuthSecretName)
	input.RepositoryProvider = strings.TrimSpace(input.RepositoryProvider)
	input.RepositoryOwner = strings.TrimSpace(input.RepositoryOwner)
	input.RepositoryName = strings.TrimSpace(input.RepositoryName)
	input.RepositoryDefaultBranch = strings.TrimSpace(input.RepositoryDefaultBranch)
	if input.SessionKey == "" {
		return runtimerepo.StartedAgentSession{}, fmt.Errorf("session key is required")
	}
	if input.Role == "" {
		return runtimerepo.StartedAgentSession{}, fmt.Errorf("session role is required")
	}
	if input.BotServiceURL == "" {
		return runtimerepo.StartedAgentSession{}, fmt.Errorf("bot service URL is required")
	}
	if input.InternalToken == "" {
		return runtimerepo.StartedAgentSession{}, fmt.Errorf("session internal token is required")
	}
	if input.CodexAuthSecretName == "" {
		return runtimerepo.StartedAgentSession{}, fmt.Errorf("codex auth secret name is required")
	}
	podName := sessionPodName(input.SessionKey)
	pvcName := sessionPVCName(input.SessionKey)
	secretName := sessionSecretName(input.SessionKey)
	created := false
	if input.TokenSecretIntegrity != nil {
		if err := runner.verifyExistingSessionTokenSecret(ctx, secretName, input.InternalToken, *input.TokenSecretIntegrity); err != nil {
			return runtimerepo.StartedAgentSession{}, err
		}
		var err error
		input.PodTokenSecretName, err = runner.materializeImmutableSessionTokenSecret(ctx, input.SessionKey, input.InternalToken, *input.TokenSecretIntegrity)
		if err != nil {
			return runtimerepo.StartedAgentSession{}, err
		}
		if err := runner.verifyFrozenSessionTokenBoundary(ctx, input.SessionKey, secretName, input.PodTokenSecretName, input.InternalToken, *input.TokenSecretIntegrity); err != nil {
			return runtimerepo.StartedAgentSession{}, err
		}
	}
	pvcCreated, err := runner.ensureSessionPVC(ctx, input.SessionKey, input.Role)
	if err != nil {
		return runtimerepo.StartedAgentSession{}, err
	}
	created = created || pvcCreated
	if input.TokenSecretIntegrity == nil {
		if _, err := runner.upsertSessionTokenSecret(ctx, input.SessionKey, secretName, input.InternalToken); err != nil {
			return runtimerepo.StartedAgentSession{}, err
		}
		input.PodTokenSecretName = secretName
	} else {
		if err := runner.verifyFrozenSessionTokenBoundary(ctx, input.SessionKey, secretName, input.PodTokenSecretName, input.InternalToken, *input.TokenSecretIntegrity); err != nil {
			return runtimerepo.StartedAgentSession{}, err
		}
	}
	recreatePod, err := runner.sessionPodShouldBeRecreated(
		ctx,
		podName,
		input.PodTokenSecretName,
		input.CodexAuthSecretName,
		input.GitHubSecretName,
	)
	if err != nil {
		return runtimerepo.StartedAgentSession{}, err
	}
	if recreatePod {
		if input.TokenSecretIntegrity != nil {
			if err := runner.verifyFrozenSessionTokenBoundary(ctx, input.SessionKey, secretName, input.PodTokenSecretName, input.InternalToken, *input.TokenSecretIntegrity); err != nil {
				return runtimerepo.StartedAgentSession{}, err
			}
		}
		if err := runner.deleteSessionPod(ctx, podName); err != nil {
			return runtimerepo.StartedAgentSession{}, err
		}
		created = true
	}
	if input.TokenSecretIntegrity != nil {
		if err := runner.verifyFrozenSessionTokenBoundary(ctx, input.SessionKey, secretName, input.PodTokenSecretName, input.InternalToken, *input.TokenSecretIntegrity); err != nil {
			return runtimerepo.StartedAgentSession{}, err
		}
	}
	if _, err := runner.client.CoreV1().Pods(runner.namespace).Create(ctx, runner.sessionPod(input), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			if quotaExceeded(err) {
				return runtimerepo.StartedAgentSession{}, runtimerepo.NewAgentSessionCapacityError("Kubernetes resource quota rejected the session pod", err)
			}
			return runtimerepo.StartedAgentSession{}, fmt.Errorf("create session pod: %w", err)
		}
	} else {
		created = true
	}
	if capacityErr := runner.sessionPodSchedulingCapacityError(ctx, podName); capacityErr != nil {
		return runtimerepo.StartedAgentSession{}, capacityErr
	}
	return runtimerepo.StartedAgentSession{
		SessionKey: input.SessionKey,
		Namespace:  runner.namespace,
		PodName:    podName,
		PVCName:    pvcName,
		SecretName: secretName,
		Created:    created,
	}, nil
}

func (runner *Runner) PrepareClusterAdminSessionRuntime(ctx context.Context, sessionKey string, proposedToken string) (runtimerepo.PreparedClusterAdminSessionRuntime, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	proposedToken = strings.TrimSpace(proposedToken)
	if sessionKey == "" || proposedToken == "" {
		return runtimerepo.PreparedClusterAdminSessionRuntime{}, fmt.Errorf("cluster-admin session key and token are required")
	}
	secretName := sessionSecretName(sessionKey)
	secretClient := runner.client.CoreV1().Secrets(runner.namespace)
	secret, err := secretClient.Get(ctx, secretName, metav1.GetOptions{})
	created := false
	if apierrors.IsNotFound(err) {
		secret, err = secretClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: runner.namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":      "matter-codex-agent-runner",
					"app.kubernetes.io/component": sessionTokenComponent,
					labelSessionKey:               kubernetesLabelValue(sessionKey),
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{"token": []byte(proposedToken)},
		}, metav1.CreateOptions{})
		created = err == nil
	}
	if err != nil {
		return runtimerepo.PreparedClusterAdminSessionRuntime{}, fmt.Errorf("prepare cluster-admin session token secret: %w", err)
	}
	token := secret.Data["token"]
	if !exactManagedSessionTokenSecret(secret, runner.namespace, secretName, kubernetesLabelValue(sessionKey), token, false, false) {
		return runtimerepo.PreparedClusterAdminSessionRuntime{}, fmt.Errorf("existing cluster-admin session token secret conflicts with the managed binding")
	}
	integrity := secretIntegrity(secret, "token", token)
	if strings.TrimSpace(integrity.ContentSHA256) == "" || strings.TrimSpace(integrity.UID) == "" || strings.TrimSpace(integrity.ResourceVersion) == "" {
		return runtimerepo.PreparedClusterAdminSessionRuntime{}, fmt.Errorf("cluster-admin session token secret has incomplete Kubernetes identity")
	}
	return runtimerepo.PreparedClusterAdminSessionRuntime{
		Namespace: runner.namespace,
		PodName:   sessionPodName(sessionKey),
		PVCName:   sessionPVCName(sessionKey),
		TokenSecret: runtimerepo.MattermostBotTokenSecret{
			SecretName: secretName,
			Namespace:  runner.namespace,
			Created:    created,
			Token:      string(token),
			Integrity:  integrity,
		},
	}, nil
}

func (runner *Runner) materializeImmutableSessionTokenSecret(ctx context.Context, sessionKey string, token string, expected runtimerepo.SecretIntegrity) (string, error) {
	if err := runner.verifyExistingSessionTokenSecret(ctx, expected.SecretName, token, expected); err != nil {
		return "", err
	}
	name := immutableSessionTokenSecretName(sessionKey, expected)
	immutable := true
	labels := map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": sessionTokenComponent,
		labelSessionKey:               kubernetesLabelValue(sessionKey),
	}
	secretClient := runner.client.CoreV1().Secrets(runner.namespace)
	created, err := secretClient.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: runner.namespace, Labels: labels, Finalizers: []string{sessionTokenFinalizer}},
		Immutable:  &immutable,
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"token": []byte(token)},
	}, metav1.CreateOptions{})
	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return "", fmt.Errorf("create immutable session token secret: %w", err)
		}
		created, err = secretClient.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("get immutable session token secret: %w", err)
		}
	}
	if !exactManagedSessionTokenSecret(created, runner.namespace, name, kubernetesLabelValue(sessionKey), []byte(token), true, true) {
		return "", fmt.Errorf("immutable session token secret verification failed")
	}
	actual := secretIntegrity(created, "token", created.Data["token"])
	if actual.ContentSHA256 != expected.ContentSHA256 {
		return "", fmt.Errorf("immutable session token secret content mismatch")
	}
	return name, nil
}

func (runner *Runner) verifyFrozenSessionTokenBoundary(ctx context.Context, sessionKey string, originalSecretName string, immutableSecretName string, token string, expected runtimerepo.SecretIntegrity) error {
	if err := runner.verifyExistingSessionTokenSecret(ctx, originalSecretName, token, expected); err != nil {
		return err
	}
	secret, err := runner.client.CoreV1().Secrets(runner.namespace).Get(ctx, immutableSecretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get immutable session token secret: %w", err)
	}
	if !exactManagedSessionTokenSecret(secret, runner.namespace, immutableSecretName, kubernetesLabelValue(sessionKey), []byte(token), true, true) {
		return fmt.Errorf("immutable session token secret verification failed")
	}
	actual := secretIntegrity(secret, "token", secret.Data["token"])
	if actual.ContentSHA256 != expected.ContentSHA256 {
		return fmt.Errorf("immutable session token secret content mismatch")
	}
	return nil
}

func immutableSessionTokenSecretName(sessionKey string, expected runtimerepo.SecretIntegrity) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		sessionKey,
		expected.SecretName,
		expected.SecretKey,
		expected.ContentSHA256,
		expected.UID,
		expected.ResourceVersion,
	}, "\x00")))
	return "mc-session-token-" + hex.EncodeToString(digest[:20])
}

func exactManagedSessionTokenSecret(secret *corev1.Secret, namespace string, name string, sessionLabel string, token []byte, versioned bool, protected bool) bool {
	if secret == nil || strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(sessionLabel) == "" || len(token) == 0 {
		return false
	}
	if secret.Name != name || secret.Namespace != namespace || secret.GenerateName != "" || secret.DeletionTimestamp != nil || secret.DeletionGracePeriodSeconds != nil {
		return false
	}
	if !maps.Equal(secret.Labels, map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": sessionTokenComponent,
		labelSessionKey:               sessionLabel,
	}) || len(secret.Annotations) != 0 || len(secret.OwnerReferences) != 0 {
		return false
	}
	if secret.Type != corev1.SecretTypeOpaque || len(secret.StringData) != 0 || len(secret.Data) != 1 || !bytes.Equal(secret.Data["token"], token) {
		return false
	}
	if versioned {
		if !isImmutableSessionTokenSecretName(secret.Name) || secret.Immutable == nil || !*secret.Immutable {
			return false
		}
	} else if secret.Immutable != nil {
		return false
	}
	expectedFinalizers := []string{}
	if protected {
		expectedFinalizers = []string{sessionTokenFinalizer}
	}
	return slices.Equal(secret.Finalizers, expectedFinalizers)
}

func isImmutableSessionTokenSecretName(name string) bool {
	const prefix = "mc-session-token-"
	digest := strings.TrimPrefix(name, prefix)
	if digest == name || len(digest) != 40 || strings.ToLower(digest) != digest {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func (runner *Runner) verifyExistingSessionTokenSecret(ctx context.Context, secretName string, token string, expected runtimerepo.SecretIntegrity) error {
	if strings.TrimSpace(expected.SecretName) != secretName || strings.TrimSpace(expected.SecretKey) != "token" ||
		strings.TrimSpace(expected.ContentSHA256) == "" || strings.TrimSpace(expected.UID) == "" || strings.TrimSpace(expected.ResourceVersion) == "" {
		return fmt.Errorf("existing session token secret integrity is invalid")
	}
	actual, err := runner.InspectSecretIntegrity(ctx, runtimerepo.SecretIntegrityInput{SecretName: secretName, SecretKey: "token"})
	if err != nil {
		return fmt.Errorf("inspect existing session token secret: %w", err)
	}
	tokenSHA256 := sha256.Sum256([]byte(token))
	if actual.ContentSHA256 != expected.ContentSHA256 || actual.UID != expected.UID || actual.ResourceVersion != expected.ResourceVersion ||
		hex.EncodeToString(tokenSHA256[:]) != expected.ContentSHA256 {
		return fmt.Errorf("existing session token secret integrity mismatch")
	}
	return nil
}

func (runner *Runner) sessionPodSchedulingCapacityError(ctx context.Context, podName string) error {
	pod, err := runner.client.CoreV1().Pods(runner.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type != corev1.PodScheduled || condition.Status != corev1.ConditionFalse || !strings.EqualFold(condition.Reason, corev1.PodReasonUnschedulable) {
			continue
		}
		message := strings.ToLower(strings.TrimSpace(condition.Message))
		if strings.Contains(message, "insufficient ") || strings.Contains(message, "too many pods") || strings.Contains(message, "exceeded quota") {
			return runtimerepo.NewAgentSessionCapacityError("Kubernetes scheduler cannot place the session pod", fmt.Errorf("%s", condition.Message))
		}
	}
	return nil
}

func (runner *Runner) ensureSessionPVC(ctx context.Context, sessionKey string, role string) (bool, error) {
	pvc := runner.sessionPVC(sessionKey, role)
	if _, err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return false, nil
		}
		if quotaExceeded(err) {
			return false, runtimerepo.NewAgentSessionPVCQuotaCapacityError("Kubernetes resource quota rejected the session PVC", err)
		}
		return false, fmt.Errorf("create session pvc: %w", err)
	}
	return true, nil
}

func (runner *Runner) sessionPodShouldBeRecreated(
	ctx context.Context,
	podName string,
	tokenSecretName string,
	codexAuthSecretName string,
	gitHubSecretName string,
) (bool, error) {
	pod, err := runner.client.CoreV1().Pods(runner.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get session pod: %w", err)
	}
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return true, nil
	}
	return !podUsesSessionRuntimeSecrets(pod, tokenSecretName, codexAuthSecretName, gitHubSecretName), nil
}

func podUsesSessionRuntimeSecrets(
	pod *corev1.Pod,
	tokenSecretName string,
	codexAuthSecretName string,
	gitHubSecretName string,
) bool {
	tokenSecretName = strings.TrimSpace(tokenSecretName)
	codexAuthSecretName = strings.TrimSpace(codexAuthSecretName)
	gitHubSecretName = strings.TrimSpace(gitHubSecretName)
	if pod == nil || tokenSecretName == "" || codexAuthSecretName == "" {
		return false
	}
	requiredVolumes := map[string]string{
		sessionSecretVolume:   tokenSecretName,
		codexAuthSecretVolume: codexAuthSecretName,
	}
	if gitHubSecretName != "" {
		requiredVolumes[gitHubSecretVolume] = gitHubSecretName
	}
	matchedVolumes := make(map[string]bool, len(requiredVolumes))
	for _, volume := range pod.Spec.Volumes {
		expectedSecret, required := requiredVolumes[volume.Name]
		if !required || volume.Secret == nil || volume.Secret.SecretName != expectedSecret {
			continue
		}
		matchedVolumes[volume.Name] = true
	}
	for volumeName := range requiredVolumes {
		if !matchedVolumes[volumeName] {
			return false
		}
	}
	if gitHubSecretName == "" {
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == gitHubSecretVolume {
				return false
			}
		}
	}
	requiredEnvironment := map[string]bool{
		"MATTERCODEX_SESSION_TOKEN": false,
		"MATTERCODEX_MCP_TOKEN":     false,
	}
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if _, required := requiredEnvironment[env.Name]; !required {
				continue
			}
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil || env.ValueFrom.SecretKeyRef.Name != tokenSecretName || env.ValueFrom.SecretKeyRef.Key != "token" {
				return false
			}
			requiredEnvironment[env.Name] = true
		}
	}
	return requiredEnvironment["MATTERCODEX_SESSION_TOKEN"] && requiredEnvironment["MATTERCODEX_MCP_TOKEN"]
}

func podUsesSessionTokenSecret(pod *corev1.Pod, tokenSecretName string) bool {
	tokenSecretName = strings.TrimSpace(tokenSecretName)
	if pod == nil || tokenSecretName == "" {
		return false
	}
	volumeMatches := false
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == sessionSecretVolume && volume.Secret != nil && volume.Secret.SecretName == tokenSecretName {
			volumeMatches = true
			break
		}
	}
	if !volumeMatches {
		return false
	}
	requiredEnvironment := map[string]bool{
		"MATTERCODEX_SESSION_TOKEN": false,
		"MATTERCODEX_MCP_TOKEN":     false,
	}
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if _, required := requiredEnvironment[env.Name]; !required {
				continue
			}
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil ||
				env.ValueFrom.SecretKeyRef.Name != tokenSecretName ||
				env.ValueFrom.SecretKeyRef.Key != "token" {
				return false
			}
			requiredEnvironment[env.Name] = true
		}
	}
	return requiredEnvironment["MATTERCODEX_SESSION_TOKEN"] && requiredEnvironment["MATTERCODEX_MCP_TOKEN"]
}

func (runner *Runner) GetAgentSessionRuntimeHealth(ctx context.Context, sessionKey string) (runtimerepo.AgentSessionRuntimeHealth, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return runtimerepo.AgentSessionRuntimeHealth{}, fmt.Errorf("session key is required")
	}
	podName := sessionPodName(sessionKey)
	health := runtimerepo.AgentSessionRuntimeHealth{
		SessionKey: sessionKey,
		Namespace:  runner.namespace,
		PodName:    podName,
	}
	pod, err := runner.client.CoreV1().Pods(runner.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			health.Terminal = true
			health.Reason = "pod not found"
			return health, nil
		}
		return runtimerepo.AgentSessionRuntimeHealth{}, fmt.Errorf("get session pod health: %w", err)
	}
	health.Exists = true
	health.Phase = string(pod.Status.Phase)
	health.Terminal = pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
	health.Reason = sessionPodHealthReason(pod)
	return health, nil
}

func sessionPodHealthReason(pod *corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Terminated != nil {
			return fmt.Sprintf("container %s terminated: %s exit=%d", status.Name, status.State.Terminated.Reason, status.State.Terminated.ExitCode)
		}
		if status.LastTerminationState.Terminated != nil {
			return fmt.Sprintf("container %s last terminated: %s exit=%d", status.Name, status.LastTerminationState.Terminated.Reason, status.LastTerminationState.Terminated.ExitCode)
		}
		if status.State.Waiting != nil && strings.TrimSpace(status.State.Waiting.Reason) != "" {
			return fmt.Sprintf("container %s waiting: %s", status.Name, status.State.Waiting.Reason)
		}
	}
	if strings.TrimSpace(pod.Status.Reason) != "" {
		return pod.Status.Reason
	}
	if pod.Status.Phase != "" {
		return string(pod.Status.Phase)
	}
	return "unknown"
}

func (runner *Runner) CleanupAgentSession(ctx context.Context, sessionKey string) (runtimerepo.AgentSessionCleanupResult, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return runtimerepo.AgentSessionCleanupResult{}, fmt.Errorf("session key is required")
	}
	podName := sessionPodName(sessionKey)
	if err := runner.deleteSessionPod(ctx, podName); err != nil {
		return runtimerepo.AgentSessionCleanupResult{}, err
	}
	return runtimerepo.AgentSessionCleanupResult{
		SessionKey: sessionKey,
		Namespace:  runner.namespace,
		PodName:    podName,
		PodDeleted: true,
	}, nil
}

func (runner *Runner) deleteSessionPod(ctx context.Context, podName string) error {
	grace := int64(0)
	background := metav1.DeletePropagationBackground
	if err := runner.client.CoreV1().Pods(runner.namespace).Delete(ctx, podName, metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
		PropagationPolicy:  &background,
	}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete completed session pod: %w", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := runner.client.CoreV1().Pods(runner.namespace).Get(ctx, podName, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("wait for session pod deletion: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("session pod %s was not deleted before timeout", podName)
}

func (runner *Runner) deleteJobAndWait(ctx context.Context, jobName string, timeout time.Duration) error {
	background := metav1.DeletePropagationBackground
	if err := runner.client.BatchV1().Jobs(runner.namespace).Delete(ctx, jobName, metav1.DeleteOptions{PropagationPolicy: &background}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete job %s: %w", jobName, err)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := runner.client.BatchV1().Jobs(runner.namespace).Get(ctx, jobName, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("wait for job deletion: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("job %s was not deleted before timeout", jobName)
}

func (runner *Runner) UpsertMattermostBotTokenSecret(ctx context.Context, input runtimerepo.MattermostBotTokenSecretInput) (runtimerepo.MattermostBotTokenSecret, error) {
	input.SecretName = strings.TrimSpace(input.SecretName)
	input.Token = strings.TrimSpace(input.Token)
	if input.SecretName == "" {
		return runtimerepo.MattermostBotTokenSecret{}, fmt.Errorf("mattermost bot token secret name is required")
	}
	if input.Token == "" {
		return runtimerepo.MattermostBotTokenSecret{}, fmt.Errorf("mattermost bot token is required")
	}
	created, err := runner.upsertSecret(ctx, input.SecretName, map[string][]byte{"token": []byte(input.Token)}, map[string]string{
		"app.kubernetes.io/name":      "matter-codex-bot-service",
		"app.kubernetes.io/component": "mattermost-role-bot-token",
	})
	if err != nil {
		return runtimerepo.MattermostBotTokenSecret{}, err
	}
	return runtimerepo.MattermostBotTokenSecret{
		SecretName: input.SecretName,
		Namespace:  runner.namespace,
		Created:    created,
		Token:      input.Token,
	}, nil
}

func (runner *Runner) GetMattermostBotTokenSecret(ctx context.Context, secretName string) (runtimerepo.MattermostBotTokenSecret, error) {
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		return runtimerepo.MattermostBotTokenSecret{}, fmt.Errorf("mattermost bot token secret name is required")
	}
	secret, err := runner.client.CoreV1().Secrets(runner.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return runtimerepo.MattermostBotTokenSecret{}, fmt.Errorf("get mattermost bot token secret: %w", err)
	}
	token := strings.TrimSpace(string(secret.Data["token"]))
	if token == "" {
		return runtimerepo.MattermostBotTokenSecret{}, fmt.Errorf("mattermost bot token secret is missing token")
	}
	return runtimerepo.MattermostBotTokenSecret{
		SecretName: secretName,
		Namespace:  runner.namespace,
		Token:      token,
		Integrity:  secretIntegrity(secret, "token", secret.Data["token"]),
	}, nil
}

func (runner *Runner) EnsureRuntimeMCPToken(ctx context.Context, sessionKey string) (runtimerepo.RuntimeMCPTokenBinding, error) {
	digest := sha256.Sum256([]byte(sessionKey))
	name := "matter-codex-mcp-" + hex.EncodeToString(digest[:12])
	secrets := runner.client.CoreV1().Secrets(runner.namespace)
	secret, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		tokenRaw := make([]byte, 32)
		if _, err = cryptorand.Read(tokenRaw); err != nil {
			return runtimerepo.RuntimeMCPTokenBinding{}, fmt.Errorf("generate runtime MCP token: %w", err)
		}
		immutable := true
		secret, err = secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name,
			Labels: map[string]string{"app.kubernetes.io/name": "matter-codex-bot-service",
				"app.kubernetes.io/component": "runtime-mcp-session-token"}}, Immutable: &immutable,
			Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"token": []byte(hex.EncodeToString(tokenRaw))}}, metav1.CreateOptions{})
	}
	if err != nil || secret.Immutable == nil || !*secret.Immutable || secret.Labels["app.kubernetes.io/component"] != "runtime-mcp-session-token" {
		return runtimerepo.RuntimeMCPTokenBinding{}, errors.New("runtime MCP token secret is unavailable")
	}
	token := strings.TrimSpace(string(secret.Data["token"]))
	if len(token) != 64 {
		return runtimerepo.RuntimeMCPTokenBinding{}, errors.New("runtime MCP token secret is invalid")
	}
	integrity := secretIntegrity(secret, "token", secret.Data["token"])
	return runtimerepo.RuntimeMCPTokenBinding{Namespace: runner.namespace, SecretName: name, Integrity: integrity}, nil
}

func (runner *Runner) GetRunStatus(ctx context.Context, runID string) (runtimerepo.RunStatus, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return runtimerepo.RunStatus{}, fmt.Errorf("run id is required")
	}
	status := runtimerepo.RunStatus{
		RunID:     runID,
		Namespace: runner.namespace,
		JobName:   runnerJobName(runID),
		PVCName:   workspacePVCName(runID),
	}
	job, err := runner.client.BatchV1().Jobs(runner.namespace).Get(ctx, status.JobName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return status, nil
		}
		return runtimerepo.RunStatus{}, fmt.Errorf("get runner job: %w", err)
	}
	status.Exists = true
	status.JobActive = job.Status.Active
	status.JobSucceeded = job.Status.Succeeded
	status.JobFailed = job.Status.Failed

	pods, err := runner.client.CoreV1().Pods(runner.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{labelRunID: runID}).String(),
	})
	if err != nil {
		return runtimerepo.RunStatus{}, fmt.Errorf("list runner pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return status, nil
	}
	sort.Slice(pods.Items, func(i int, j int) bool {
		return pods.Items[i].CreationTimestamp.After(pods.Items[j].CreationTimestamp.Time)
	})
	pod := pods.Items[0]
	status.PodName = pod.Name
	status.PodPhase = string(pod.Status.Phase)
	status.LogTail = runner.logTail(ctx, pod.Name)
	status.Artifacts = parseArtifacts(status.LogTail)
	return status, nil
}

func (runner *Runner) latestPodByLabels(ctx context.Context, selector labels.Set) (corev1.Pod, bool, error) {
	pods, err := runner.client.CoreV1().Pods(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(selector).String()})
	if err != nil {
		return corev1.Pod{}, false, fmt.Errorf("list runner pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return corev1.Pod{}, false, nil
	}
	sort.Slice(pods.Items, func(i int, j int) bool {
		return pods.Items[i].CreationTimestamp.After(pods.Items[j].CreationTimestamp.Time)
	})
	return pods.Items[0], true, nil
}

func (runner *Runner) CleanupRun(ctx context.Context, runID string) (runtimerepo.CleanupResult, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return runtimerepo.CleanupResult{}, fmt.Errorf("run id is required")
	}
	result := runtimerepo.CleanupResult{
		RunID:     runID,
		Namespace: runner.namespace,
	}
	background := metav1.DeletePropagationBackground
	if err := runner.client.BatchV1().Jobs(runner.namespace).Delete(ctx, runnerJobName(runID), metav1.DeleteOptions{PropagationPolicy: &background}); err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.CleanupResult{}, fmt.Errorf("delete runner job: %w", err)
		}
	} else {
		result.JobDeleted = true
	}
	if err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Delete(ctx, workspacePVCName(runID), metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.CleanupResult{}, fmt.Errorf("delete workspace pvc: %w", err)
		}
	} else {
		result.PVCDeleted = true
	}
	if err := runner.client.CoreV1().ConfigMaps(runner.namespace).Delete(ctx, promptConfigMapName(runID), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return runtimerepo.CleanupResult{}, fmt.Errorf("delete prompt configmap: %w", err)
	}
	return result, nil
}

func (runner *Runner) CleanupExpiredRuns(ctx context.Context, input runtimerepo.RetentionCleanupInput) (runtimerepo.RetentionCleanupResult, error) {
	if input.OlderThan <= 0 {
		return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("retention duration must be positive")
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.Add(-input.OlderThan)
	result := runtimerepo.RetentionCleanupResult{
		Namespace:       runner.namespace,
		DryRun:          input.DryRun,
		OlderThan:       input.OlderThan,
		SessionDataMode: runtimerepo.SessionDataRetentionModeInventoryOnly,
	}

	jobs, err := runner.client.BatchV1().Jobs(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: runnerLabelSelector()})
	if err != nil {
		return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("list runner jobs: %w", err)
	}
	jobByRunID := make(map[string]batchv1.Job, len(jobs.Items))
	matchedRunIDs := make(map[string]struct{})
	background := metav1.DeletePropagationBackground
	for _, job := range jobs.Items {
		runID := job.Labels[labelRunID]
		if runID == "" {
			continue
		}
		jobByRunID[runID] = job
		if job.Status.Active > 0 {
			result.SkippedActiveJobs++
			continue
		}
		if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
			continue
		}
		if !olderThan(jobCompletedOrCreatedAt(job), cutoff) {
			continue
		}
		result.JobsMatched++
		matchedRunIDs[runID] = struct{}{}
		if input.DryRun {
			continue
		}
		if err := runner.client.BatchV1().Jobs(runner.namespace).Delete(ctx, job.Name, metav1.DeleteOptions{PropagationPolicy: &background}); err != nil {
			if !apierrors.IsNotFound(err) {
				return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("delete runner job %s: %w", job.Name, err)
			}
		} else {
			result.JobsDeleted++
		}
	}

	pvcs, err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: runnerLabelSelector()})
	if err != nil {
		return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("list runner pvcs: %w", err)
	}
	for _, pvc := range pvcs.Items {
		if !runnerRetentionResourceExpired(pvc.Labels[labelRunID], pvc.CreationTimestamp.Time, cutoff, jobByRunID, matchedRunIDs) {
			continue
		}
		result.PVCsMatched++
		matchedRunIDs[pvc.Labels[labelRunID]] = struct{}{}
		if input.DryRun {
			continue
		}
		if err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("delete workspace pvc %s: %w", pvc.Name, err)
			}
		} else {
			result.PVCsDeleted++
		}
	}

	configMaps, err := runner.client.CoreV1().ConfigMaps(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: runnerLabelSelector()})
	if err != nil {
		return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("list runner configmaps: %w", err)
	}
	for _, configMap := range configMaps.Items {
		if !runnerRetentionResourceExpired(configMap.Labels[labelRunID], configMap.CreationTimestamp.Time, cutoff, jobByRunID, matchedRunIDs) {
			continue
		}
		result.ConfigMapsMatched++
		matchedRunIDs[configMap.Labels[labelRunID]] = struct{}{}
		if input.DryRun {
			continue
		}
		if err := runner.client.CoreV1().ConfigMaps(runner.namespace).Delete(ctx, configMap.Name, metav1.DeleteOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("delete prompt configmap %s: %w", configMap.Name, err)
			}
		} else {
			result.ConfigMapsDeleted++
		}
	}
	sessionResult, err := runner.cleanupExpiredSessionResources(ctx, cutoff, input.DryRun)
	if err != nil {
		return runtimerepo.RetentionCleanupResult{}, err
	}
	result.SessionPodsMatched = sessionResult.SessionPodsMatched
	result.SessionPodsDeleted = sessionResult.SessionPodsDeleted
	result.SessionPVCsMatched = sessionResult.SessionPVCsMatched
	result.SessionPVCsDeleted = sessionResult.SessionPVCsDeleted
	result.SessionSecretsMatched = sessionResult.SessionSecretsMatched
	result.SessionSecretsDeleted = sessionResult.SessionSecretsDeleted
	result.SessionDiagnostics = sessionResult.SessionDiagnostics
	result.MatchedRunIDs = sortedRunIDs(matchedRunIDs)
	result.RunsMatched = len(result.MatchedRunIDs)
	return result, nil
}

type sessionCleanupResult struct {
	SessionPodsMatched    int
	SessionPodsDeleted    int
	SessionPVCsMatched    int
	SessionPVCsDeleted    int
	SessionSecretsMatched int
	SessionSecretsDeleted int
	SessionDiagnostics    []runtimerepo.SessionRetentionDiagnostic
}

func (runner *Runner) cleanupExpiredSessionResources(ctx context.Context, cutoff time.Time, dryRun bool) (sessionCleanupResult, error) {
	result := sessionCleanupResult{}
	type inventory struct {
		facts   runtimerepo.SessionRetentionFacts
		pvcs    int
		secrets int
	}
	inventoryBySession := make(map[string]*inventory)
	getInventory := func(sessionKey string) *inventory {
		item, ok := inventoryBySession[sessionKey]
		if !ok {
			item = &inventory{}
			inventoryBySession[sessionKey] = item
		}
		return item
	}
	pods, err := runner.client.CoreV1().Pods(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: sessionLabelSelector()})
	if err != nil {
		return sessionCleanupResult{}, fmt.Errorf("list session pods: %w", err)
	}
	for _, pod := range pods.Items {
		sessionKey := pod.Labels[labelSessionKey]
		if sessionKey == "" {
			continue
		}
		item := getInventory(sessionKey)
		if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			item.facts.Active = true
			continue
		}
		if !olderThan(podFinishedOrCreatedAt(pod), cutoff) {
			item.facts.Grace = true
			continue
		}
		result.SessionPodsMatched++
		if dryRun {
			continue
		}
		deleted, err := runner.deleteExpiredSessionPod(ctx, pod)
		if err != nil {
			return sessionCleanupResult{}, fmt.Errorf("delete expired session pod %s: %w", pod.Name, err)
		}
		if deleted {
			result.SessionPodsDeleted++
		}
	}

	pvcs, err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: sessionLabelSelector()})
	if err != nil {
		return sessionCleanupResult{}, fmt.Errorf("list session pvcs: %w", err)
	}
	for _, pvc := range pvcs.Items {
		sessionKey := pvc.Labels[labelSessionKey]
		if sessionKey == "" {
			continue
		}
		item := getInventory(sessionKey)
		item.pvcs++
		item.facts.Grace = item.facts.Grace || !olderThan(pvc.CreationTimestamp.Time, cutoff)
		result.SessionPVCsMatched++
	}

	secrets, err := runner.client.CoreV1().Secrets(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: sessionTokenLabelSelector()})
	if err != nil {
		return sessionCleanupResult{}, fmt.Errorf("list session token secrets: %w", err)
	}
	for _, secret := range secrets.Items {
		sessionKey := secret.Labels[labelSessionKey]
		if sessionKey == "" {
			continue
		}
		item := getInventory(sessionKey)
		item.secrets++
		item.facts.Grace = item.facts.Grace || !olderThan(secret.CreationTimestamp.Time, cutoff)
		result.SessionSecretsMatched++
	}
	sessionKeys := make([]string, 0, len(inventoryBySession))
	for sessionKey := range inventoryBySession {
		sessionKeys = append(sessionKeys, sessionKey)
	}
	sort.Strings(sessionKeys)
	for _, sessionKey := range sessionKeys {
		item := inventoryBySession[sessionKey]
		item.facts.UnknownDB = true
		item.facts.UnknownS3 = true
		result.SessionDiagnostics = append(result.SessionDiagnostics, runtimerepo.DiagnoseSessionRetention(sessionKey, item.pvcs, item.secrets, item.facts))
	}
	return result, nil
}

func (runner *Runner) deleteExpiredSessionPod(ctx context.Context, pod corev1.Pod) (bool, error) {
	if pod.UID == "" {
		return false, fmt.Errorf("session pod %s has no UID", pod.Name)
	}
	uid := pod.UID
	preconditions := &metav1.Preconditions{UID: &uid}
	if pod.ResourceVersion != "" {
		resourceVersion := pod.ResourceVersion
		preconditions.ResourceVersion = &resourceVersion
	}
	grace := int64(0)
	background := metav1.DeletePropagationBackground
	err := runner.client.CoreV1().Pods(runner.namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
		PropagationPolicy:  &background,
		Preconditions:      preconditions,
	})
	if apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("delete retained session pod: %w", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := runner.client.CoreV1().Pods(runner.namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			return true, nil
		}
		if getErr != nil {
			return false, fmt.Errorf("wait for retained session pod deletion: %w", getErr)
		}
		if current.UID != pod.UID {
			return true, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false, fmt.Errorf("session pod %s identity %s was not deleted before timeout", pod.Name, pod.UID)
}

func (runner *Runner) smokePVC(runID string, role string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   workspacePVCName(runID),
			Labels: runnerLabels(runID, role),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: runner.workspaceStorage,
				},
			},
		},
	}
}

func (runner *Runner) promptConfigMap(runID string, role string, prompt string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   promptConfigMapName(runID),
			Labels: runnerLabels(runID, role),
		},
		Data: map[string]string{
			"prompt.md": prompt,
		},
	}
}

func (runner *Runner) smokeJob(runID string, role string) *batchv1.Job {
	backoffLimit := int32(0)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   runnerJobName(runID),
			Labels: runnerLabels(runID, role),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &runner.jobTTLSecondsAfterFinish,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: runnerLabels(runID, role)},
				Spec: corev1.PodSpec{
					ServiceAccountName:           runner.agentRunnerServiceAccount,
					AutomountServiceAccountToken: boolPtr(false),
					SecurityContext:              runnerPodSecurityContext(),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           runner.agentRunnerImage,
							Command:         runnerCommand(),
							Args:            []string{"smoke"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: runnerContainerSecurityContext(),
							Resources:       runner.runnerUtilityResourceRequirements(),
							Env: []corev1.EnvVar{
								{Name: "MATTERCODEX_RUN_ID", Value: runID},
								{Name: "MATTERCODEX_AGENT_ROLE", Value: role},
							},
							VolumeMounts: append([]corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
							}, runnerWritableVolumeMounts()...),
						},
					},
					Volumes: append([]corev1.Volume{
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: workspacePVCName(runID),
								},
							},
						},
					}, runner.runnerWritableVolumes()...),
				},
			},
		},
	}
}

func (runner *Runner) developerJob(input runtimerepo.DeveloperRunInput) *batchv1.Job {
	backoffLimit := int32(0)
	codexAuthSecretName := defaultString(input.CodexAuthSecretName, runner.codexAuthSecretName)
	gitHubSecretName := defaultString(input.GitHubSecretName, runner.gitHubSecretName)
	kubernetesAccess := normalizedKubernetesAccess(input.KubernetesAccess)
	runtimeEnv := runtimeEnvVars(input.RuntimeEnv)
	envAllowlist := runtimeEnvAllowlistValue(input.RuntimeEnv)
	sensitiveEnvAllowlist := runtimeSensitiveEnvAllowlistValue(input.RuntimeEnv)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   runnerJobName(input.RunID),
			Labels: runnerLabels(input.RunID, input.Profile),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &runner.jobTTLSecondsAfterFinish,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: runnerLabels(input.RunID, input.Profile)},
				Spec: corev1.PodSpec{
					ServiceAccountName:           runner.agentRunnerServiceAccountForAccess(kubernetesAccess),
					AutomountServiceAccountToken: boolPtr(true),
					SecurityContext:              runnerPodSecurityContext(),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           runner.agentRunnerImage,
							Command:         runnerCommand(),
							Args:            []string{"developer"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: runnerContainerSecurityContext(),
							Resources:       runner.runnerSessionResourceRequirements(),
							Env: append([]corev1.EnvVar{
								{Name: "MATTERCODEX_RUN_ID", Value: input.RunID},
								{Name: "MATTERCODEX_AGENT_PROFILE", Value: input.Profile},
								{Name: "MATTERCODEX_KUBERNETES_ACCESS", Value: kubernetesAccess},
								{Name: "MATTERCODEX_REPO_PROVIDER", Value: input.Provider},
								{Name: "MATTERCODEX_REPO_OWNER", Value: input.Owner},
								{Name: "MATTERCODEX_REPO_NAME", Value: input.Name},
								{Name: "MATTERCODEX_BASE_BRANCH", Value: input.BaseBranch},
								{Name: "MATTERCODEX_HEAD_BRANCH", Value: input.HeadBranch},
								{Name: "MATTERCODEX_PR_TITLE", Value: input.Title},
								{Name: "MATTERCODEX_CODEX_SANDBOX_MODE", Value: input.SandboxMode},
								{Name: "MATTERCODEX_CODEX_CONFIG_OVERLAY", Value: input.ConfigOverlay},
								{Name: runtimeEnvAllowlist, Value: envAllowlist},
								{Name: runtimeSensitiveEnvAllowlist, Value: sensitiveEnvAllowlist},
							}, runtimeEnv...),
							VolumeMounts: append([]corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
								{Name: codexAuthSecretVolume, MountPath: "/var/run/secrets/matter-codex-codex", ReadOnly: true},
								{Name: gitHubSecretVolume, MountPath: "/var/run/secrets/matter-codex-github", ReadOnly: true},
								{Name: promptVolume, MountPath: "/var/run/matter-codex-prompt", ReadOnly: true},
							}, runnerWritableVolumeMounts()...),
						},
					},
					Volumes: append([]corev1.Volume{
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: workspacePVCName(input.RunID),
								},
							},
						},
						{
							Name: codexAuthSecretVolume,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: codexAuthSecretName,
									Items: []corev1.KeyToPath{
										{Key: "auth.json", Path: "auth.json"},
									},
								},
							},
						},
						{
							Name: gitHubSecretVolume,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: gitHubSecretName,
									Items: []corev1.KeyToPath{
										{Key: "github-token", Path: "github-token"},
										{Key: "github-username", Path: "github-username"},
										{Key: "github-email", Path: "github-email"},
									},
								},
							},
						},
						{
							Name: promptVolume,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: promptConfigMapName(input.RunID)},
									Items: []corev1.KeyToPath{
										{Key: "prompt.md", Path: "prompt.md"},
									},
								},
							},
						},
					}, runner.runnerWritableVolumes()...),
				},
			},
		},
	}
}

func (runner *Runner) reviewJob(input runtimerepo.ReviewRunInput) *batchv1.Job {
	backoffLimit := int32(0)
	codexAuthSecretName := defaultString(input.CodexAuthSecretName, runner.codexAuthSecretName)
	gitHubSecretName := defaultString(input.GitHubSecretName, runner.gitHubSecretName)
	kubernetesAccess := normalizedKubernetesAccess(input.KubernetesAccess)
	runtimeEnv := runtimeEnvVars(input.RuntimeEnv)
	envAllowlist := runtimeEnvAllowlistValue(input.RuntimeEnv)
	sensitiveEnvAllowlist := runtimeSensitiveEnvAllowlistValue(input.RuntimeEnv)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   runnerJobName(input.RunID),
			Labels: runnerLabels(input.RunID, input.Profile),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &runner.jobTTLSecondsAfterFinish,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: runnerLabels(input.RunID, input.Profile)},
				Spec: corev1.PodSpec{
					ServiceAccountName:           runner.agentRunnerServiceAccountForAccess(kubernetesAccess),
					AutomountServiceAccountToken: boolPtr(true),
					SecurityContext:              runnerPodSecurityContext(),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           runner.agentRunnerImage,
							Command:         runnerCommand(),
							Args:            []string{"reviewer"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: runnerContainerSecurityContext(),
							Resources:       runner.runnerSessionResourceRequirements(),
							Env: append([]corev1.EnvVar{
								{Name: "MATTERCODEX_RUN_ID", Value: input.RunID},
								{Name: "MATTERCODEX_AGENT_PROFILE", Value: input.Profile},
								{Name: "MATTERCODEX_KUBERNETES_ACCESS", Value: kubernetesAccess},
								{Name: "MATTERCODEX_REPO_PROVIDER", Value: input.Provider},
								{Name: "MATTERCODEX_REPO_OWNER", Value: input.Owner},
								{Name: "MATTERCODEX_REPO_NAME", Value: input.Name},
								{Name: "MATTERCODEX_PR_NUMBER", Value: strconv.Itoa(input.PRNumber)},
								{Name: "MATTERCODEX_CODEX_SANDBOX_MODE", Value: input.SandboxMode},
								{Name: "MATTERCODEX_CODEX_CONFIG_OVERLAY", Value: input.ConfigOverlay},
								{Name: runtimeEnvAllowlist, Value: envAllowlist},
								{Name: runtimeSensitiveEnvAllowlist, Value: sensitiveEnvAllowlist},
							}, runtimeEnv...),
							VolumeMounts: append([]corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
								{Name: codexAuthSecretVolume, MountPath: "/var/run/secrets/matter-codex-codex", ReadOnly: true},
								{Name: gitHubSecretVolume, MountPath: "/var/run/secrets/matter-codex-github", ReadOnly: true},
								{Name: promptVolume, MountPath: "/var/run/matter-codex-prompt", ReadOnly: true},
							}, runnerWritableVolumeMounts()...),
						},
					},
					Volumes: append([]corev1.Volume{
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: workspacePVCName(input.RunID),
								},
							},
						},
						{
							Name: codexAuthSecretVolume,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: codexAuthSecretName,
									Items: []corev1.KeyToPath{
										{Key: "auth.json", Path: "auth.json"},
									},
								},
							},
						},
						{
							Name: gitHubSecretVolume,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: gitHubSecretName,
									Items: []corev1.KeyToPath{
										{Key: "github-token", Path: "github-token"},
										{Key: "github-username", Path: "github-username"},
										{Key: "github-email", Path: "github-email"},
									},
								},
							},
						},
						{
							Name: promptVolume,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: promptConfigMapName(input.RunID)},
									Items: []corev1.KeyToPath{
										{Key: "prompt.md", Path: "prompt.md"},
									},
								},
							},
						},
					}, runner.runnerWritableVolumes()...),
				},
			},
		},
	}
}

func (runner *Runner) chatJob(input runtimerepo.ChatRunInput) *batchv1.Job {
	backoffLimit := int32(0)
	codexAuthSecretName := defaultString(input.CodexAuthSecretName, runner.codexAuthSecretName)
	kubernetesAccess := normalizedKubernetesAccess(input.KubernetesAccess)
	env := []corev1.EnvVar{
		{Name: "MATTERCODEX_RUN_ID", Value: input.RunID},
		{Name: "MATTERCODEX_AGENT_PROFILE", Value: input.Profile},
		{Name: "MATTERCODEX_KUBERNETES_ACCESS", Value: kubernetesAccess},
		{Name: "MATTERCODEX_CODEX_SANDBOX_MODE", Value: input.SandboxMode},
		{Name: "MATTERCODEX_CODEX_CONFIG_OVERLAY", Value: input.ConfigOverlay},
		{Name: runtimeEnvAllowlist, Value: runtimeEnvAllowlistValue(input.RuntimeEnv)},
		{Name: runtimeSensitiveEnvAllowlist, Value: runtimeSensitiveEnvAllowlistValue(input.RuntimeEnv)},
	}
	env = append(env, runtimeEnvVars(input.RuntimeEnv)...)
	volumeMounts := []corev1.VolumeMount{
		{Name: "workspace", MountPath: "/workspace"},
		{Name: codexAuthSecretVolume, MountPath: "/var/run/secrets/matter-codex-codex", ReadOnly: true},
		{Name: promptVolume, MountPath: "/var/run/matter-codex-prompt", ReadOnly: true},
	}
	volumes := []corev1.Volume{
		{
			Name: "workspace",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: workspacePVCName(input.RunID),
				},
			},
		},
		{
			Name: codexAuthSecretVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: codexAuthSecretName,
					Items: []corev1.KeyToPath{
						{Key: "auth.json", Path: "auth.json"},
					},
				},
			},
		},
		{
			Name: promptVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: promptConfigMapName(input.RunID)},
					Items: []corev1.KeyToPath{
						{Key: "prompt.md", Path: "prompt.md"},
					},
				},
			},
		},
	}
	if strings.TrimSpace(input.GitHubSecretName) != "" {
		env = append(env, corev1.EnvVar{Name: "MATTERCODEX_GITHUB_ENABLED", Value: "true"})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{Name: gitHubSecretVolume, MountPath: "/var/run/secrets/matter-codex-github", ReadOnly: true})
		volumes = append(volumes, corev1.Volume{
			Name: gitHubSecretVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: input.GitHubSecretName,
					Items: []corev1.KeyToPath{
						{Key: "github-token", Path: "github-token"},
						{Key: "github-username", Path: "github-username"},
						{Key: "github-email", Path: "github-email"},
					},
				},
			},
		})
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   runnerJobName(input.RunID),
			Labels: runnerLabels(input.RunID, input.Profile),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &runner.jobTTLSecondsAfterFinish,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: runnerLabels(input.RunID, input.Profile)},
				Spec: corev1.PodSpec{
					ServiceAccountName:           runner.agentRunnerServiceAccountForAccess(kubernetesAccess),
					AutomountServiceAccountToken: boolPtr(true),
					SecurityContext:              runnerPodSecurityContext(),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           runner.agentRunnerImage,
							Command:         runnerCommand(),
							Args:            []string{"chat"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: runnerContainerSecurityContext(),
							Resources:       runner.runnerSessionResourceRequirements(),
							Env:             env,
							VolumeMounts:    append(volumeMounts, runnerWritableVolumeMounts()...),
						},
					},
					Volumes: append(volumes, runner.runnerWritableVolumes()...),
				},
			},
		},
	}
}

func (runner *Runner) sessionPVC(sessionKey string, role string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   sessionPVCName(sessionKey),
			Labels: sessionLabels(sessionKey, role),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: runner.workspaceStorage,
				},
			},
		},
	}
}

func (runner *Runner) sessionPod(input runtimerepo.AgentSessionPodInput) *corev1.Pod {
	codexAuthSecretName := defaultString(input.CodexAuthSecretName, runner.codexAuthSecretName)
	kubernetesAccess := normalizedKubernetesAccess(input.KubernetesAccess)
	tokenSecretName := defaultString(input.PodTokenSecretName, sessionSecretName(input.SessionKey))
	env := []corev1.EnvVar{
		{Name: "MATTERCODEX_SESSION_KEY", Value: input.SessionKey},
		{Name: "MATTERCODEX_AGENT_PROFILE", Value: input.Role},
		{Name: "MATTERCODEX_KUBERNETES_ACCESS", Value: kubernetesAccess},
		{Name: "MATTERCODEX_BOT_SERVICE_URL", Value: input.BotServiceURL},
		{Name: "MATTERCODEX_SESSION_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: tokenSecretName},
			Key:                  "token",
		}}},
		{Name: "MATTERCODEX_CODEX_SANDBOX_MODE", Value: input.SandboxMode},
		{Name: "MATTERCODEX_CODEX_CONFIG_OVERLAY", Value: input.ConfigOverlay},
		{Name: runtimeEnvAllowlist, Value: runtimeEnvAllowlistValue(input.RuntimeEnv)},
		{Name: runtimeSensitiveEnvAllowlist, Value: runtimeSensitiveEnvAllowlistValue(input.RuntimeEnv)},
		{Name: "MATTERCODEX_MCP_URL", Value: strings.TrimRight(input.BotServiceURL, "/") + "/mcp/sessions/" + input.SessionKey},
		{Name: "MATTERCODEX_MCP_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: tokenSecretName},
			Key:                  "token",
		}}},
	}
	env = append(env, runtimeEnvVars(input.RuntimeEnv)...)
	volumeMounts := []corev1.VolumeMount{
		{Name: "workspace", MountPath: "/workspace"},
		{Name: codexAuthSecretVolume, MountPath: "/var/run/secrets/matter-codex-codex", ReadOnly: true},
		{Name: sessionSecretVolume, MountPath: "/var/run/secrets/matter-codex-session", ReadOnly: true},
	}
	volumes := []corev1.Volume{
		{
			Name: "workspace",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: sessionPVCName(input.SessionKey),
				},
			},
		},
		{
			Name: codexAuthSecretVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: codexAuthSecretName,
					Items: []corev1.KeyToPath{
						{Key: "auth.json", Path: "auth.json"},
					},
				},
			},
		},
		{
			Name: sessionSecretVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: tokenSecretName,
					Items: []corev1.KeyToPath{
						{Key: "token", Path: "token"},
					},
				},
			},
		},
	}
	if strings.TrimSpace(input.GitHubSecretName) != "" {
		env = append(env, corev1.EnvVar{Name: "MATTERCODEX_GITHUB_ENABLED", Value: "true"})
		if strings.TrimSpace(input.RepositoryOwner) != "" && strings.TrimSpace(input.RepositoryName) != "" {
			env = append(env,
				corev1.EnvVar{Name: "MATTERCODEX_SESSION_REPOSITORY_ENABLED", Value: "true"},
				corev1.EnvVar{Name: "MATTERCODEX_REPO_PROVIDER", Value: defaultString(input.RepositoryProvider, "github")},
				corev1.EnvVar{Name: "MATTERCODEX_REPO_OWNER", Value: input.RepositoryOwner},
				corev1.EnvVar{Name: "MATTERCODEX_REPO_NAME", Value: input.RepositoryName},
				corev1.EnvVar{Name: "MATTERCODEX_BASE_BRANCH", Value: defaultString(input.RepositoryDefaultBranch, "main")},
			)
		}
		volumeMounts = append(volumeMounts, corev1.VolumeMount{Name: gitHubSecretVolume, MountPath: "/var/run/secrets/matter-codex-github", ReadOnly: true})
		volumes = append(volumes, corev1.Volume{
			Name: gitHubSecretVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: input.GitHubSecretName,
					Items: []corev1.KeyToPath{
						{Key: "github-token", Path: "github-token"},
						{Key: "github-username", Path: "github-username"},
						{Key: "github-email", Path: "github-email"},
					},
				},
			},
		})
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   sessionPodName(input.SessionKey),
			Labels: sessionLabels(input.SessionKey, input.Role),
		},
		Spec: corev1.PodSpec{
			ServiceAccountName:           runner.agentRunnerServiceAccountForAccess(kubernetesAccess),
			AutomountServiceAccountToken: boolPtr(true),
			SecurityContext:              runnerPodSecurityContext(),
			RestartPolicy:                corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:            "runner",
					Image:           runner.agentRunnerImage,
					Command:         runnerCommand(),
					Args:            []string{"session"},
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: runnerContainerSecurityContext(),
					Resources:       runner.runnerSessionResourceRequirements(),
					Env:             env,
					VolumeMounts:    append(volumeMounts, runnerWritableVolumeMounts()...),
				},
			},
			Volumes: append(volumes, runner.runnerWritableVolumes()...),
		},
	}
}

func (runner *Runner) codexAuthJob(accountName string, secretName string) *batchv1.Job {
	backoffLimit := int32(0)
	labels := codexAuthLabels(accountName)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   codexAuthJobName(accountName),
			Labels: labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &runner.jobTTLSecondsAfterFinish,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					ServiceAccountName:           runner.agentRunnerServiceAccount,
					AutomountServiceAccountToken: boolPtr(false),
					SecurityContext:              runnerPodSecurityContext(),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           runner.agentRunnerImage,
							Command:         runnerCommand(),
							Args:            []string{"codex-auth"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: runnerContainerSecurityContext(),
							Resources:       runner.runnerUtilityResourceRequirements(),
							Env: []corev1.EnvVar{
								{Name: "MATTERCODEX_OPENAI_ACCOUNT", Value: accountName},
								{Name: "MATTERCODEX_CODEX_AUTH_SECRET", Value: secretName},
							},
							VolumeMounts: append([]corev1.VolumeMount{
								{Name: "codex-home", MountPath: "/codex-home"},
							}, runnerWritableVolumeMounts()...),
						},
					},
					Volumes: append([]corev1.Volume{
						{
							Name: "codex-home",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					}, runner.runnerWritableVolumes()...),
				},
			},
		},
	}
}

func (runner *Runner) codexAuthSecretCheckJob(input runtimerepo.CodexAuthSecretCheckInput, jobName string) *batchv1.Job {
	backoffLimit := int32(0)
	labels := codexAuthCheckLabels(input.AccountName, jobName)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   jobName,
			Labels: labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &runner.authCheckJobTTLSecondsAfterFinish,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					ServiceAccountName:           runner.agentRunnerServiceAccount,
					AutomountServiceAccountToken: boolPtr(false),
					SecurityContext:              runnerPodSecurityContext(),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           runner.agentRunnerImage,
							Command:         runnerCommand(),
							Args:            []string{"codex-auth-secret-check"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: runnerContainerSecurityContext(),
							Resources:       runner.runnerUtilityResourceRequirements(),
							Env: []corev1.EnvVar{
								{Name: "MATTERCODEX_OPENAI_ACCOUNT", Value: input.AccountName},
								{Name: "MATTERCODEX_CODEX_AUTH_SECRET", Value: input.SecretName},
							},
							VolumeMounts: append([]corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
								{Name: codexAuthSecretVolume, MountPath: "/var/run/secrets/matter-codex-codex", ReadOnly: true},
							}, runnerWritableVolumeMounts()...),
						},
					},
					Volumes: append([]corev1.Volume{
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: codexAuthSecretVolume,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: input.SecretName,
									Items: []corev1.KeyToPath{
										{Key: "auth.json", Path: "auth.json"},
									},
								},
							},
						},
					}, runner.runnerWritableVolumes()...),
				},
			},
		},
	}
}

func (runner *Runner) logTail(ctx context.Context, podName string) string {
	limitBytes := int64(8192)
	stream, err := runner.client.CoreV1().Pods(runner.namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:  "runner",
		TailLines:  &runner.logTailLines,
		LimitBytes: &limitBytes,
	}).Stream(ctx)
	if err != nil {
		return ""
	}
	defer stream.Close()
	body, err := io.ReadAll(stream)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

func (runner *Runner) fillCodexAuthCheckPodStatus(ctx context.Context, result *runtimerepo.CodexAuthSecretCheckResult) {
	if result == nil || strings.TrimSpace(result.JobName) == "" {
		return
	}
	pod, ok, err := runner.latestPodByLabels(ctx, labels.Set{
		"app.kubernetes.io/component": "codex-auth-check",
		labelAuthCheckJob:             result.JobName,
	})
	if err != nil || !ok {
		return
	}
	result.PodName = pod.Name
	result.PodPhase = string(pod.Status.Phase)
	result.LogTail = runner.logTail(ctx, pod.Name)
}

func (runner *Runner) codexAuthJSONReady(ctx context.Context, podName string) bool {
	if runner.restConfig == nil {
		return false
	}
	_, err := runner.execPod(ctx, podName, []string{"matter-codex-agent-runner", "auth-ready-check"})
	return err == nil
}

func (runner *Runner) execPod(ctx context.Context, podName string, command []string) ([]byte, error) {
	if runner.restConfig == nil {
		return nil, fmt.Errorf("kubernetes rest config is required for pod exec")
	}
	request := runner.client.CoreV1().RESTClient().
		Post().
		Namespace(runner.namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "runner",
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(runner.restConfig, http.MethodPost, request.URL())
	if err != nil {
		return nil, fmt.Errorf("create pod exec: %w", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		return nil, fmt.Errorf("stream pod exec: %w: %s", err, safeKubernetesExecError(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (runner *Runner) createCodexAuthRevisionSecret(
	ctx context.Context,
	accountName string,
	secretName string,
	authJSON []byte,
) (runtimerepo.SecretIntegrity, error) {
	secretClient := runner.client.CoreV1().Secrets(runner.namespace)
	secret, err := secretClient.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.SecretIntegrity{}, fmt.Errorf("get Codex auth revision Secret: %w", err)
		}
		secret, err = secretClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: secretName,
				Labels: map[string]string{
					"app.kubernetes.io/name":      "matter-codex-agent-runner",
					"app.kubernetes.io/component": "codex-auth-secret",
					labelOpenAIAccount:            accountName,
				},
			},
			Immutable: boolPtr(true),
			Type:      corev1.SecretTypeOpaque,
			Data:      map[string][]byte{"auth.json": authJSON},
		}, metav1.CreateOptions{})
		if err != nil {
			return runtimerepo.SecretIntegrity{}, fmt.Errorf("create Codex auth revision Secret: %w", err)
		}
		return secretIntegrity(secret, "auth.json", authJSON), nil
	}
	existingAuthJSON := secret.Data["auth.json"]
	if secret.Immutable == nil || !*secret.Immutable || !bytes.Equal(existingAuthJSON, authJSON) {
		return runtimerepo.SecretIntegrity{}, fmt.Errorf("Codex auth revision Secret conflicts with existing immutable revision")
	}
	if secret.Labels[labelOpenAIAccount] != accountName {
		return runtimerepo.SecretIntegrity{}, fmt.Errorf("Codex auth revision Secret belongs to a different account")
	}
	return secretIntegrity(secret, "auth.json", existingAuthJSON), nil
}

func (runner *Runner) upsertGitHubTokenSecret(ctx context.Context, input runtimerepo.GitHubTokenSecretInput) (bool, error) {
	secretClient := runner.client.CoreV1().Secrets(runner.namespace)
	data := map[string][]byte{
		"github-token":    []byte(input.Token),
		"github-username": []byte(input.Username),
		"github-email":    []byte(input.Email),
	}
	labels := map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": "github-token-secret",
		labelGitHubAccount:            input.AccountName,
	}
	secret, err := secretClient.Get(ctx, input.SecretName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("get github token secret: %w", err)
		}
		_, err = secretClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   input.SecretName,
				Labels: labels,
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}, metav1.CreateOptions{})
		if err != nil {
			return false, fmt.Errorf("create github token secret: %w", err)
		}
		return true, nil
	}
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	if secret.Labels == nil {
		secret.Labels = make(map[string]string)
	}
	for key, value := range labels {
		secret.Labels[key] = value
	}
	for key, value := range data {
		secret.Data[key] = value
	}
	if _, err := secretClient.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("update github token secret: %w", err)
	}
	return false, nil
}

func (runner *Runner) upsertSessionTokenSecret(ctx context.Context, sessionKey string, secretName string, token string) (bool, error) {
	return runner.upsertSecret(ctx, secretName, map[string][]byte{"token": []byte(token)}, map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": sessionTokenComponent,
		labelSessionKey:               kubernetesLabelValue(sessionKey),
	})
}

func (runner *Runner) upsertSecret(ctx context.Context, name string, data map[string][]byte, labels map[string]string) (bool, error) {
	secretClient := runner.client.CoreV1().Secrets(runner.namespace)
	secret, err := secretClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("get secret %s: %w", name, err)
		}
		_, err = secretClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: labels,
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}, metav1.CreateOptions{})
		if err != nil {
			return false, fmt.Errorf("create secret %s: %w", name, err)
		}
		return true, nil
	}
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	if secret.Labels == nil {
		secret.Labels = make(map[string]string)
	}
	unchanged := true
	for key, value := range labels {
		if secret.Labels[key] != value {
			unchanged = false
		}
		secret.Labels[key] = value
	}
	for key, value := range data {
		if !bytes.Equal(secret.Data[key], value) {
			unchanged = false
		}
		secret.Data[key] = value
	}
	if unchanged {
		return false, nil
	}
	if _, err := secretClient.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("update secret %s: %w", name, err)
	}
	return false, nil
}

func kubernetesConfig(kubeconfigPath string) (*rest.Config, error) {
	if strings.TrimSpace(kubeconfigPath) != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("load kubeconfig: %w", err)
		}
		return cfg, nil
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster kubernetes config: %w", err)
	}
	return cfg, nil
}

func runtimeNamespace(configured string) (string, error) {
	if namespace := strings.TrimSpace(configured); namespace != "" {
		return namespace, nil
	}
	body, err := os.ReadFile(defaultNamespaceFile)
	if err != nil {
		return "", fmt.Errorf("read service account namespace: %w", err)
	}
	namespace := strings.TrimSpace(string(body))
	if namespace == "" {
		return "", fmt.Errorf("service account namespace is empty")
	}
	return namespace, nil
}

func runnerLabels(runID string, role string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": runnerComponent,
		labelRunID:                    runID,
		labelAgentRole:                role,
	}
}

func sessionLabels(sessionKey string, role string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": sessionComponent,
		labelSessionKey:               kubernetesLabelValue(sessionKey),
		labelAgentRole:                kubernetesLabelValue(role),
	}
}

func runnerLabelSelector() string {
	return labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": runnerComponent,
	}).String()
}

func sessionLabelSelector() string {
	return labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": sessionComponent,
	}).String()
}

func sessionTokenLabelSelector() string {
	return labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": sessionTokenComponent,
	}).String()
}

func jobCompletedOrCreatedAt(job batchv1.Job) time.Time {
	if job.Status.CompletionTime != nil && !job.Status.CompletionTime.IsZero() {
		return job.Status.CompletionTime.Time
	}
	return job.CreationTimestamp.Time
}

func podFinishedOrCreatedAt(pod corev1.Pod) time.Time {
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Terminated != nil && !status.State.Terminated.FinishedAt.IsZero() {
			return status.State.Terminated.FinishedAt.Time
		}
	}
	return pod.CreationTimestamp.Time
}

func olderThan(value time.Time, cutoff time.Time) bool {
	return !value.IsZero() && value.Before(cutoff)
}

func quotaExceeded(err error) bool {
	if !apierrors.IsForbidden(err) {
		return false
	}
	statusErr, ok := err.(*apierrors.StatusError)
	if !ok {
		return false
	}
	return strings.Contains(strings.ToLower(statusErr.ErrStatus.Message), "exceeded quota")
}

func runnerRetentionResourceExpired(runID string, createdAt time.Time, cutoff time.Time, jobByRunID map[string]batchv1.Job, matchedRunIDs map[string]struct{}) bool {
	if runID == "" {
		return false
	}
	if _, ok := matchedRunIDs[runID]; ok {
		return true
	}
	job, ok := jobByRunID[runID]
	if !ok {
		return olderThan(createdAt, cutoff)
	}
	if job.Status.Active > 0 {
		return false
	}
	if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
		return false
	}
	return olderThan(jobCompletedOrCreatedAt(job), cutoff)
}

func sortedRunIDs(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	runIDs := make([]string, 0, len(values))
	for runID := range values {
		if runID != "" {
			runIDs = append(runIDs, runID)
		}
	}
	sort.Strings(runIDs)
	return runIDs
}

func runnerJobName(runID string) string {
	return "mc-run-" + runID
}

func sessionPodName(sessionKey string) string {
	return kubernetesName("mc-session-", sessionKey)
}

func sessionPVCName(sessionKey string) string {
	return kubernetesName("mc-session-ws-", sessionKey)
}

func sessionSecretName(sessionKey string) string {
	return kubernetesName("mc-session-token-", sessionKey)
}

func codexAuthJobName(accountName string) string {
	return "mc-codex-auth-" + accountName
}

func codexAuthCheckJobName(accountName string) string {
	return kubernetesName("mc-codex-auth-check-", accountName+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
}

func codexAuthRevisionSecretName(currentSecretName string, authJSON []byte) string {
	baseName := kubernetesName("", codexAuthRevisionSuffixRE.ReplaceAllString(strings.TrimSpace(currentSecretName), ""))
	digest := sha256.Sum256(authJSON)
	suffix := "-rev-" + hex.EncodeToString(digest[:6])
	maximumBaseLength := 63 - len(suffix)
	if len(baseName) > maximumBaseLength {
		baseName = strings.TrimRight(baseName[:maximumBaseLength], "-")
	}
	return baseName + suffix
}

func workspacePVCName(runID string) string {
	return "mc-ws-" + runID
}

func promptConfigMapName(runID string) string {
	return "mc-prompt-" + runID
}

func kubernetesName(prefix string, value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		value = "unknown"
	}
	name := prefix + value
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	if name == "" {
		return prefix + "unknown"
	}
	return name
}

func kubernetesLabelValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9_.-]+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-_.")
	if value == "" {
		return "unknown"
	}
	if len(value) > 63 {
		value = strings.Trim(value[:63], "-_.")
	}
	if value == "" {
		return "unknown"
	}
	return value
}

func runtimeEnvVars(items []runtimerepo.RuntimeEnvVar) []corev1.EnvVar {
	names := make(map[string]struct{}, len(items))
	var env []corev1.EnvVar
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		secretName := strings.TrimSpace(item.SecretName)
		secretKey := defaultString(item.SecretKey, "value")
		if name == "" || secretName == "" || !runtimeEnvNameRE.MatchString(name) {
			continue
		}
		if _, exists := names[name]; exists {
			continue
		}
		names[name] = struct{}{}
		env = append(env, corev1.EnvVar{
			Name: name,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  secretKey,
				},
			},
		})
	}
	return env
}

func runtimeEnvAllowlistValue(items []runtimerepo.RuntimeEnvVar) string {
	names := make(map[string]struct{}, len(items))
	var values []string
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" || !runtimeEnvNameRE.MatchString(name) {
			continue
		}
		if _, exists := names[name]; exists {
			continue
		}
		names[name] = struct{}{}
		values = append(values, name)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func runtimeSensitiveEnvAllowlistValue(items []runtimerepo.RuntimeEnvVar) string {
	names := make(map[string]struct{}, len(items))
	var values []string
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if !item.Sensitive || name == "" || !runtimeEnvNameRE.MatchString(name) {
			continue
		}
		if _, exists := names[name]; exists {
			continue
		}
		names[name] = struct{}{}
		values = append(values, name)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func normalizeDeveloperRunInput(input runtimerepo.DeveloperRunInput) runtimerepo.DeveloperRunInput {
	input.RunID = strings.TrimSpace(input.RunID)
	input.Profile = defaultString(input.Profile, "developer")
	input.CodexAuthSecretName = strings.TrimSpace(input.CodexAuthSecretName)
	input.GitHubSecretName = strings.TrimSpace(input.GitHubSecretName)
	input.Provider = defaultString(input.Provider, "github")
	input.Owner = strings.TrimSpace(input.Owner)
	input.Name = strings.TrimSpace(input.Name)
	input.BaseBranch = defaultString(input.BaseBranch, "main")
	input.HeadBranch = strings.TrimSpace(input.HeadBranch)
	input.Title = strings.TrimSpace(input.Title)
	input.Task = strings.TrimSpace(input.Task)
	input.SandboxMode = defaultString(input.SandboxMode, "danger-full-access")
	return input
}

func normalizeReviewRunInput(input runtimerepo.ReviewRunInput) runtimerepo.ReviewRunInput {
	input.RunID = strings.TrimSpace(input.RunID)
	input.Profile = defaultString(input.Profile, "reviewer")
	input.CodexAuthSecretName = strings.TrimSpace(input.CodexAuthSecretName)
	input.GitHubSecretName = strings.TrimSpace(input.GitHubSecretName)
	input.Provider = defaultString(input.Provider, "github")
	input.Owner = strings.TrimSpace(input.Owner)
	input.Name = strings.TrimSpace(input.Name)
	input.SandboxMode = defaultString(input.SandboxMode, "danger-full-access")
	return input
}

func normalizeChatRunInput(input runtimerepo.ChatRunInput) runtimerepo.ChatRunInput {
	input.RunID = strings.TrimSpace(input.RunID)
	input.Profile = defaultString(input.Profile, "chat")
	input.CodexAuthSecretName = strings.TrimSpace(input.CodexAuthSecretName)
	input.GitHubSecretName = strings.TrimSpace(input.GitHubSecretName)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.SandboxMode = defaultString(input.SandboxMode, "danger-full-access")
	return input
}

func codexAuthLabels(accountName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": "codex-auth",
		labelOpenAIAccount:            accountName,
	}
}

func codexAuthCheckLabels(accountName string, jobName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": "codex-auth-check",
		labelOpenAIAccount:            kubernetesLabelValue(accountName),
		labelAuthCheckJob:             kubernetesLabelValue(jobName),
	}
}

func parseCodexDeviceAuth(logTail string) (string, string) {
	clean := stripANSI(logTail)
	deviceURL := ""
	if strings.Contains(clean, "https://auth.openai.com/codex/device") {
		deviceURL = "https://auth.openai.com/codex/device"
	}
	code := codexDeviceCodeRE.FindString(clean)
	return deviceURL, code
}

func stripANSI(value string) string {
	ansiRE := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	return ansiRE.ReplaceAllString(value, "")
}

func safeKubernetesExecError(value string) string {
	value = strings.TrimSpace(stripANSI(value))
	if value == "" {
		return "empty stderr"
	}
	lines := strings.Split(value, "\n")
	sort.Strings(lines)
	return lines[0]
}

func parseArtifacts(logTail string) map[string]string {
	artifacts := make(map[string]string)
	for _, line := range strings.Split(logTail, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "matter-codex artifact ") {
			continue
		}
		keyValue := strings.TrimPrefix(line, "matter-codex artifact ")
		key, value, ok := strings.Cut(keyValue, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			artifacts[key] = value
		}
	}
	if len(artifacts) == 0 {
		return nil
	}
	return artifacts
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func defaultInt32(value int32, fallback int32) int32 {
	if value <= 0 {
		return fallback
	}
	return value
}

func defaultInt64(value int64, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func parsePositiveResourceQuantity(value string, fallback string, field string) (resource.Quantity, error) {
	quantity, err := resource.ParseQuantity(defaultString(value, fallback))
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("parse %s: %w", field, err)
	}
	if quantity.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf("%s must be positive", field)
	}
	return quantity, nil
}

func runnerPodSecurityContext() *corev1.PodSecurityContext {
	fsGroupChangePolicy := corev1.FSGroupChangeOnRootMismatch
	return &corev1.PodSecurityContext{
		RunAsNonRoot:        boolPtr(true),
		RunAsUser:           int64Ptr(runnerUID),
		RunAsGroup:          int64Ptr(runnerGID),
		FSGroup:             int64Ptr(runnerGID),
		FSGroupChangePolicy: &fsGroupChangePolicy,
		SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}

func runnerContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsNonRoot:             boolPtr(true),
		RunAsUser:                int64Ptr(runnerUID),
		RunAsGroup:               int64Ptr(runnerGID),
		AllowPrivilegeEscalation: boolPtr(false),
		ReadOnlyRootFilesystem:   boolPtr(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

func (runner *Runner) runnerUtilityResourceRequirements() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(runnerUtilityCPURequest),
			corev1.ResourceMemory: resource.MustParse(runnerUtilityMemoryRequest),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: runner.utilityMemoryLimit,
		},
	}
}

func (runner *Runner) runnerSessionResourceRequirements() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    runner.sessionCPURequest,
			corev1.ResourceMemory: runner.sessionMemoryRequest,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: runner.sessionMemoryLimit,
		},
	}
}

func runnerWritableVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: runnerHomeVolume, MountPath: runnerHomePath},
		{Name: runnerTmpVolume, MountPath: runnerTmpPath},
		{Name: runnerDevShmVolume, MountPath: runnerDevShmPath},
	}
}

func (runner *Runner) runnerWritableVolumes() []corev1.Volume {
	return []corev1.Volume{
		{Name: runnerHomeVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: runnerTmpVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: runnerDevShmVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: &runner.devShmSizeLimit}}},
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
