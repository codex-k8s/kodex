package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/security/workloadticket"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const runtimeConfigFile = "/var/run/config/mattercodex/runtime/runtime.json"

const (
	vaultTokenFile        = "/var/run/secrets/tokens/vault"
	workloadTicketKeyFile = "/var/run/config/mattercodex/runtime-workload-ticket/public-key.hex"
	s3KMSKeyARNFile       = "/var/run/secrets/mattercodex/runtime-credential-broker/s3/kms-key-arn"
	s3ArchiveRoleARNFile  = "/var/run/secrets/mattercodex/runtime-credential-broker/s3/archive-role-arn"
	s3RestoreRoleARNFile  = "/var/run/secrets/mattercodex/runtime-credential-broker/s3/restore-role-arn"
	maximumSTSTTL         = 15 * time.Minute
)

type runtimeSnapshot struct {
	Execution             entity.Execution `json:"execution"`
	Revision              entity.Revision  `json:"runtime_revision"`
	Runner                json.RawMessage  `json:"runner_input"`
	WorkloadTicket        string           `json:"workload_ticket"`
	ArchiveWorkloadTicket string           `json:"archive_workload_ticket"`
	RestoreWorkloadTicket string           `json:"restore_workload_ticket"`
	DesiredPod            *corev1.Pod      `json:"desired_pod"`
}

type credentialSnapshotEntry struct {
	ID, Purpose, ImmutableSecretRef, ProviderContentVersion, ContentSHA256 string
	Version                                                                uint64
}

type vaultResponse struct {
	LeaseID       string         `json:"lease_id"`
	LeaseDuration int64          `json:"lease_duration"`
	Auth          *vaultAuth     `json:"auth"`
	Data          map[string]any `json:"data"`
}

type vaultAuth struct {
	ClientToken string `json:"client_token"`
	Accessor    string `json:"accessor"`
}

func main() {
	base := context.Background()
	ctx, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if len(os.Args) != 2 || (os.Args[1] != "snapshot" && os.Args[1] != "s3-archive" && os.Args[1] != "s3-restore") {
		return errors.New("runtime credential broker mode is invalid")
	}
	executionID, namespace := os.Getenv("RUNTIME_EXECUTION_ID"), os.Getenv("RUNTIME_NAMESPACE")
	if uuid.Validate(executionID) != nil || namespace == "" {
		return errors.New("runtime credential broker identity is invalid")
	}
	raw, err := os.ReadFile(runtimeConfigFile)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return errors.New("read immutable runtime credential input")
	}
	var snapshot runtimeSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		snapshot.Execution.ID != executionID ||
		snapshot.Execution.Validate() != nil || snapshot.Revision.ValidateFor(snapshot.Execution) != nil ||
		!credentialSnapshotMatches(snapshot.Execution, snapshot.Revision) || verifyWorkloadTicket(snapshot, os.Args[1]) != nil {
		return errors.New("immutable runtime credential input is invalid")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("load credential broker Kubernetes configuration")
	}
	config.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.New("create credential broker Kubernetes client")
	}
	if os.Args[1] == "snapshot" {
		for index, credential := range snapshot.Revision.Credentials {
			if err := materializeCredential(ctx, client, namespace, snapshot.Execution, credential, index); err != nil {
				return err
			}
		}
		if err := materializeAccessProfile(ctx, client, namespace, snapshot.Execution, len(snapshot.Revision.Credentials)); err != nil {
			return err
		}
		if err := materializeWorkerJournalGrants(ctx, client, namespace, snapshot.Execution); err != nil {
			return err
		}
		return materializeRolePod(ctx, client, namespace, snapshot)
	}
	action := strings.TrimPrefix(os.Args[1], "s3-")
	return materializeS3Credential(ctx, client, namespace, snapshot.Execution, action)
}

func materializeWorkerJournalGrants(ctx context.Context, client kubernetes.Interface, namespace string, execution entity.Execution) error {
	workers := map[string]string{
		"runtime-archive":            "runtime-archive",
		"runtime-restore-verifier":   "runtime-restore-verifier",
		"runtime-rehydrate":          "runtime-restore-verifier",
		"runtime-cleanup-authorizer": "runtime-cleanup-authorizer",
	}
	for component, serviceAccount := range workers {
		name := workerJobName(component, execution)
		desired := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: executionLabels(execution, "worker-journal-authority")},
			Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"},
				ResourceNames: []string{journalName(execution.ID)}, Verbs: []string{"get", "update"}}}}
		roles := client.RbacV1().Roles(namespace)
		actual, err := roles.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			actual, err = roles.Create(ctx, desired, metav1.CreateOptions{})
		}
		if err != nil || !policyRulesEqual(actual.Rules, desired.Rules) {
			return errors.New("materialize exact worker journal role")
		}
		if err := applyRoleBinding(ctx, client, namespace, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: desired.Labels},
			Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: serviceAccount, Namespace: namespace}},
			RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}}); err != nil {
			return err
		}
	}
	return nil
}

func materializeRolePod(ctx context.Context, client kubernetes.Interface, namespace string, snapshot runtimeSnapshot) error {
	pod := snapshot.DesiredPod
	execution, revision := snapshot.Execution, snapshot.Revision
	if pod == nil || pod.Name != podName(execution) || pod.Namespace != "" ||
		pod.Labels["app.kubernetes.io/component"] != "role-runtime" ||
		pod.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
		pod.Annotations["runtime.mattercodex.dev/version"] != strconv.FormatUint(execution.Version, 10) ||
		pod.Annotations["runtime.mattercodex.dev/fence"] != strconv.FormatUint(execution.Fence, 10) ||
		pod.Annotations["runtime.mattercodex.dev/revision-sha256"] != execution.RuntimeRevisionSHA256 ||
		pod.Annotations["runtime.mattercodex.dev/input-sha256"] != execution.ImmutableInputSHA256 ||
		pod.Annotations["runtime.mattercodex.dev/workload-ticket"] != snapshot.WorkloadTicket ||
		pod.Spec.ServiceAccountName != accessServiceAccountName(execution) || pod.Spec.AutomountServiceAccountToken == nil ||
		*pod.Spec.AutomountServiceAccountToken || pod.Spec.RestartPolicy != corev1.RestartPolicyNever ||
		len(pod.Spec.InitContainers) != 1 || len(pod.Spec.Containers) != 1 ||
		len(pod.Spec.InitContainers[0].Args) != 1 || pod.Spec.InitContainers[0].Args[0] != "runtime-init-workspace" ||
		len(pod.Spec.Containers[0].Args) != 1 || pod.Spec.Containers[0].Args[0] != "runtime-session" ||
		pod.Spec.InitContainers[0].Image != pod.Spec.Containers[0].Image ||
		!strings.HasSuffix(pod.Spec.Containers[0].Image, "@"+revision.ImageDigest) ||
		!restrictedContainer(pod.Spec.InitContainers[0].SecurityContext) ||
		!restrictedContainer(pod.Spec.Containers[0].SecurityContext) ||
		!exactRuntimeVolumes(pod.Spec.Volumes, execution, len(revision.Credentials)) {
		return errors.New("runtime role Pod ticketed spec is invalid")
	}
	pods := client.CoreV1().Pods(namespace)
	created, err := pods.Get(ctx, pod.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		created, err = pods.Create(ctx, pod, metav1.CreateOptions{})
	}
	if err != nil {
		return errors.New("materialize exact runtime role Pod")
	}
	if created.Annotations["runtime.mattercodex.dev/execution-id"] == execution.ID &&
		created.Annotations["runtime.mattercodex.dev/workload-ticket"] == snapshot.WorkloadTicket &&
		exactRolePodShape(created, pod, execution, len(revision.Credentials), false) {
		return nil
	}
	if created.Annotations["runtime.mattercodex.dev/effective-runtime-sha256"] != execution.EffectiveRuntimeSHA256 ||
		created.Annotations["runtime.mattercodex.dev/archive-gate"] != "OPEN" ||
		created.Status.Phase != corev1.PodRunning || !podReady(created) ||
		!exactRolePodShape(created, pod, execution, len(revision.Credentials), true) {
		return errors.New("materialize exact runtime role Pod")
	}
	return nil
}

func exactRolePodShape(actual, desired *corev1.Pod, execution entity.Execution, credentialCount int, warm bool) bool {
	if actual == nil || desired == nil || actual.DeletionTimestamp != nil ||
		actual.Spec.ServiceAccountName != desired.Spec.ServiceAccountName ||
		actual.Labels["runtime.mattercodex.dev/session"] != desired.Labels["runtime.mattercodex.dev/session"] ||
		actual.Labels["runtime.mattercodex.dev/role"] != desired.Labels["runtime.mattercodex.dev/role"] ||
		actual.Labels["runtime.mattercodex.dev/access-profile"] != desired.Labels["runtime.mattercodex.dev/access-profile"] ||
		actual.Spec.AutomountServiceAccountToken == nil || *actual.Spec.AutomountServiceAccountToken ||
		actual.Spec.RestartPolicy != corev1.RestartPolicyNever || len(actual.Spec.InitContainers) != 1 ||
		len(actual.Spec.Containers) != 1 || actual.Spec.InitContainers[0].Image != desired.Spec.InitContainers[0].Image ||
		actual.Spec.Containers[0].Image != desired.Spec.Containers[0].Image ||
		!jsonEqual(actual.Spec.InitContainers[0].Args, []string{"runtime-init-workspace"}) ||
		!jsonEqual(actual.Spec.Containers[0].Args, []string{"runtime-session"}) ||
		!restrictedContainer(actual.Spec.InitContainers[0].SecurityContext) ||
		!restrictedContainer(actual.Spec.Containers[0].SecurityContext) {
		return false
	}
	if warm {
		return compatibleWarmVolumes(actual.Spec.Volumes, execution, credentialCount)
	}
	return exactRuntimeVolumes(actual.Spec.Volumes, execution, credentialCount)
}

func compatibleWarmVolumes(volumes []corev1.Volume, execution entity.Execution, credentialCount int) bool {
	if len(volumes) != credentialCount+4 {
		return false
	}
	seenSession, seenConfig, seenToken, credentials := false, false, false, 0
	for _, volume := range volumes {
		if volume.HostPath != nil || volume.NFS != nil {
			return false
		}
		switch volume.Name {
		case "session":
			seenSession = volume.PersistentVolumeClaim != nil &&
				volume.PersistentVolumeClaim.ClaimName == pvcName(execution) && !volume.PersistentVolumeClaim.ReadOnly
		case "runtime-config":
			seenConfig = volume.ConfigMap != nil && strings.HasPrefix(volume.ConfigMap.Name, "runtime-config-")
		case "kube-api-access":
			seenToken = volume.Projected != nil && projectedTokenHasAudience(volume.Projected, "https://kubernetes.default.svc")
		case "tmp":
			if volume.EmptyDir == nil || volume.EmptyDir.SizeLimit == nil {
				return false
			}
		default:
			if !strings.HasPrefix(volume.Name, "credential-") || volume.Secret == nil ||
				!strings.HasPrefix(volume.Secret.SecretName, "runtime-credential-") {
				return false
			}
			credentials++
		}
	}
	return seenSession && seenConfig && seenToken && credentials == credentialCount
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func restrictedContainer(security *corev1.SecurityContext) bool {
	return security != nil && security.AllowPrivilegeEscalation != nil && !*security.AllowPrivilegeEscalation &&
		security.ReadOnlyRootFilesystem != nil && *security.ReadOnlyRootFilesystem &&
		security.RunAsNonRoot != nil && *security.RunAsNonRoot && security.Capabilities != nil &&
		len(security.Capabilities.Drop) == 1 && security.Capabilities.Drop[0] == "ALL"
}

func exactRuntimeVolumes(volumes []corev1.Volume, execution entity.Execution, credentialCount int) bool {
	if len(volumes) != credentialCount+4 {
		return false
	}
	seen := make(map[string]bool, len(volumes))
	for _, volume := range volumes {
		if seen[volume.Name] || volume.HostPath != nil || volume.NFS != nil {
			return false
		}
		seen[volume.Name] = true
		switch volume.Name {
		case "session":
			if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName != pvcName(execution) || volume.PersistentVolumeClaim.ReadOnly {
				return false
			}
		case "runtime-config":
			if volume.ConfigMap == nil || volume.ConfigMap.Name != configName(execution) {
				return false
			}
		case "tmp":
			if volume.EmptyDir == nil || volume.EmptyDir.SizeLimit == nil {
				return false
			}
		case "kube-api-access":
			if volume.Projected == nil || !projectedTokenHasAudience(volume.Projected, "https://kubernetes.default.svc") {
				return false
			}
		default:
			if !strings.HasPrefix(volume.Name, "credential-") || volume.Secret == nil {
				return false
			}
			index, err := strconv.Atoi(strings.TrimPrefix(volume.Name, "credential-"))
			if err != nil || index < 0 || index >= credentialCount || volume.Secret.SecretName != executionCredentialSecretName(execution.ID, index) {
				return false
			}
		}
	}
	return seen["session"] && seen["runtime-config"] && seen["tmp"] && seen["kube-api-access"]
}

func projectedTokenHasAudience(projected *corev1.ProjectedVolumeSource, audience string) bool {
	for _, source := range projected.Sources {
		if source.ServiceAccountToken != nil && source.ServiceAccountToken.Audience == audience &&
			source.ServiceAccountToken.ExpirationSeconds != nil && *source.ServiceAccountToken.ExpirationSeconds <= 600 {
			return true
		}
	}
	return false
}

func jsonEqual(left, right any) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func materializeAccessProfile(ctx context.Context, client kubernetes.Interface, namespace string, execution entity.Execution, credentialCount int) error {
	name := accessServiceAccountName(execution)
	if string(execution.AccessProfile) == "CLUSTER_ADMIN" {
		serviceAccount, err := client.CoreV1().ServiceAccounts(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil || serviceAccount.Labels["runtime.mattercodex.dev/access-profile"] != "cluster_admin" {
			return errors.New("read prebound cluster admin admission identity")
		}
		binding, err := client.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
		if err != nil || binding.RoleRef.Kind != "ClusterRole" || binding.RoleRef.Name != "cluster-admin" ||
			len(binding.Subjects) != 1 || binding.Subjects[0].Kind != "ServiceAccount" ||
			binding.Subjects[0].Name != name || binding.Subjects[0].Namespace != namespace {
			return errors.New("read prebound cluster admin admission grant")
		}
	} else {
		annotations := map[string]string{
			"runtime.mattercodex.dev/execution-id":    execution.ID,
			"runtime.mattercodex.dev/organization-id": execution.OrganizationID,
			"runtime.mattercodex.dev/project-id":      execution.ProjectID,
			"runtime.mattercodex.dev/session-id":      execution.SessionID,
		}
		accounts := client.CoreV1().ServiceAccounts(namespace)
		account, err := accounts.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			account, err = accounts.Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name,
				Labels: executionLabels(execution, "runtime-access"), Annotations: annotations}, AutomountServiceAccountToken: boolPointer(false)}, metav1.CreateOptions{})
		} else if err == nil {
			if account.Annotations["runtime.mattercodex.dev/organization-id"] != execution.OrganizationID ||
				account.Annotations["runtime.mattercodex.dev/project-id"] != execution.ProjectID ||
				account.Annotations["runtime.mattercodex.dev/session-id"] != execution.SessionID {
				return errors.New("runtime access identity lineage mismatch")
			}
			updated := account.DeepCopy()
			updated.Annotations, updated.Labels = annotations, executionLabels(execution, "runtime-access")
			account, err = accounts.Update(ctx, updated, metav1.UpdateOptions{})
		}
		if err != nil || account.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID {
			return errors.New("materialize exact runtime access identity")
		}
		if string(execution.AccessProfile) == "PROJECT_READ_ONLY" {
			projectNamespace := projectNamespaceName(execution.ProjectID)
			ownerNamespace, err := client.CoreV1().Namespaces().Get(ctx, projectNamespace, metav1.GetOptions{})
			if err != nil || ownerNamespace.Annotations["mattercodex.dev/project-id"] != execution.ProjectID ||
				ownerNamespace.Annotations["mattercodex.dev/organization-id"] != execution.OrganizationID {
				return errors.New("read exact server-managed project namespace")
			}
			desired := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: executionLabels(execution, "runtime-access")},
				Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: name, Namespace: namespace}},
				RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "runtime-role-project-read"}}
			if err := applyRoleBinding(ctx, client, projectNamespace, desired); err != nil {
				return err
			}
		}
	}
	return materializeHandoff(ctx, client, namespace, execution, name, credentialCount)
}

func materializeHandoff(ctx context.Context, client kubernetes.Interface, namespace string, execution entity.Execution, serviceAccount string, credentialCount int) error {
	name := "runtime-handoff-" + shortID(execution.SessionID)
	credentialNames := make([]string, 0, credentialCount)
	for index := 0; index < credentialCount; index++ {
		credentialNames = append(credentialNames, executionCredentialSecretName(execution.ID, index))
	}
	desired := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: executionLabels(execution, "runtime-handoff")}, Rules: []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"pods"}, ResourceNames: []string{podName(execution)}, Verbs: []string{"get", "patch"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{configName(execution)}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: credentialNames, Verbs: []string{"get"}},
	}}
	roles := client.RbacV1().Roles(namespace)
	actual, err := roles.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err = roles.Create(ctx, desired, metav1.CreateOptions{})
	} else if err == nil {
		updated := actual.DeepCopy()
		updated.Labels, updated.Rules = desired.Labels, desired.Rules
		actual, err = roles.Update(ctx, updated, metav1.UpdateOptions{})
	}
	if err != nil || !policyRulesEqual(actual.Rules, desired.Rules) {
		return errors.New("materialize exact runtime handoff role")
	}
	return applyRoleBinding(ctx, client, namespace, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: desired.Labels},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: serviceAccount, Namespace: namespace}},
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}})
}

func applyRoleBinding(ctx context.Context, client kubernetes.Interface, namespace string, desired *rbacv1.RoleBinding) error {
	bindings := client.RbacV1().RoleBindings(namespace)
	actual, err := bindings.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err = bindings.Create(ctx, desired, metav1.CreateOptions{})
	} else if err == nil {
		updated := actual.DeepCopy()
		updated.Labels, updated.Subjects, updated.RoleRef = desired.Labels, desired.Subjects, desired.RoleRef
		actual, err = bindings.Update(ctx, updated, metav1.UpdateOptions{})
	}
	if err != nil || len(actual.Subjects) != 1 || actual.Subjects[0] != desired.Subjects[0] || actual.RoleRef != desired.RoleRef {
		return errors.New("materialize exact runtime role binding")
	}
	return nil
}

func policyRulesEqual(left, right []rbacv1.PolicyRule) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func materializeS3Credential(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	execution entity.Execution,
	action string,
) error {
	if action != "archive" && action != "restore" {
		return errors.New("runtime S3 credential action is invalid")
	}
	policy, sourceExecutionID, err := exactS3Policy(execution, action)
	if err != nil {
		return err
	}
	policyRaw, err := json.Marshal(policy)
	if err != nil {
		return errors.New("encode exact runtime S3 policy")
	}
	policyDigest := sha256.Sum256(policyRaw)
	policySHA256 := hex.EncodeToString(policyDigest[:])
	vaultClient, vaultAddress, err := newVaultClient()
	if err != nil {
		return err
	}
	kubernetesJWT, err := os.ReadFile(vaultTokenFile)
	if err != nil || len(kubernetesJWT) < 20 || len(kubernetesJWT) > 1<<20 {
		return errors.New("read runtime S3 broker identity")
	}
	vaultRole := "runtime-s3-" + action + "-broker"
	login := vaultResponse{}
	if err := vaultRequest(ctx, vaultClient, http.MethodPost, vaultAddress+"/v1/auth/kubernetes/login", "", map[string]any{
		"role": vaultRole, "jwt": string(kubernetesJWT),
	}, &login); err != nil || login.Auth == nil || login.Auth.ClientToken == "" || login.Auth.Accessor == "" {
		return errors.New("authenticate exact runtime S3 broker identity")
	}
	tags := map[string]string{
		"organization_id":     execution.OrganizationID,
		"project_id":          execution.ProjectID,
		"session_id":          execution.SessionID,
		"execution_id":        execution.ID,
		"source_execution_id": sourceExecutionID,
	}
	if action == "restore" {
		_, archiveReference := restoreArchiveSource(execution)
		tags["archive_version_id"] = exactVersionID(archiveReference)
	}
	bootstrap := vaultResponse{}
	if err := vaultRequest(ctx, vaultClient, http.MethodPost, vaultAddress+"/v1/aws/sts/runtime-"+action+"-broker",
		login.Auth.ClientToken, map[string]any{
			"ttl": "15m", "role_session_name": "mcx-broker-" + shortID(execution.ID) + "-" + action,
		}, &bootstrap); err != nil {
		return errors.New("issue runtime S3 broker credential")
	}
	bootstrapAccessKey, accessOK := bootstrap.Data["access_key"].(string)
	bootstrapSecretKey, secretOK := bootstrap.Data["secret_key"].(string)
	bootstrapToken, tokenOK := bootstrap.Data["security_token"].(string)
	if !accessOK || !secretOK || !tokenOK || bootstrapAccessKey == "" || bootstrapSecretKey == "" || bootstrapToken == "" ||
		bootstrap.LeaseID == "" || bootstrap.LeaseDuration < 60 || bootstrap.LeaseDuration > int64(maximumSTSTTL/time.Second) {
		return errors.New("runtime S3 broker credential response is invalid")
	}
	stsClient, roleARN, err := newS3STSClient(action, bootstrapAccessKey, bootstrapSecretKey, bootstrapToken)
	if err != nil {
		return err
	}
	tagValues := make([]ststypes.Tag, 0, len(tags))
	tagNames := make([]string, 0, len(tags))
	for name := range tags {
		tagNames = append(tagNames, name)
	}
	sort.Strings(tagNames)
	for _, name := range tagNames {
		tagValues = append(tagValues, ststypes.Tag{Key: aws.String(name), Value: aws.String(tags[name])})
	}
	sessionName := "mcx-" + shortID(execution.ID) + "-" + action
	duration := int32(maximumSTSTTL / time.Second)
	assumed, err := stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn: aws.String(roleARN), RoleSessionName: aws.String(sessionName), DurationSeconds: &duration,
		Policy: aws.String(string(policyRaw)), Tags: tagValues,
	})
	if err != nil || assumed.Credentials == nil || assumed.AssumedRoleUser == nil {
		return errors.New("assume exact runtime S3 execution role")
	}
	accessKeyID := aws.ToString(assumed.Credentials.AccessKeyId)
	secretAccessKey := aws.ToString(assumed.Credentials.SecretAccessKey)
	sessionToken := aws.ToString(assumed.Credentials.SessionToken)
	expiresAt := aws.ToTime(assumed.Credentials.Expiration).UTC()
	if !accessOK || !secretOK || !tokenOK || accessKeyID == "" || secretAccessKey == "" || sessionToken == "" ||
		expiresAt.Before(time.Now().UTC().Add(time.Minute)) || expiresAt.After(time.Now().UTC().Add(maximumSTSTTL+time.Minute)) {
		return errors.New("runtime S3 credential response is invalid")
	}
	readbackRaw, err := json.Marshal(struct {
		BootstrapLeaseID, LoginAccessor, AssumedRoleARN, PolicySHA256 string
		ExecutionID, Action, SourceExecutionID, ExpiresAt             string
	}{bootstrap.LeaseID, login.Auth.Accessor, aws.ToString(assumed.AssumedRoleUser.Arn), policySHA256,
		execution.ID, action, sourceExecutionID,
		expiresAt.Format(time.RFC3339)})
	if err != nil {
		return errors.New("encode runtime S3 credential readback")
	}
	readbackDigest := sha256.Sum256(readbackRaw)
	immutable := true
	destination := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: s3CredentialSecretName(execution.ID, action),
		Labels: map[string]string{
			"runtime.mattercodex.dev/managed": "true", "app.kubernetes.io/component": "runtime-s3-credential",
		},
		Annotations: map[string]string{
			"runtime.mattercodex.dev/execution-id":           execution.ID,
			"runtime.mattercodex.dev/organization-id":        execution.OrganizationID,
			"runtime.mattercodex.dev/project-id":             execution.ProjectID,
			"runtime.mattercodex.dev/session-id":             execution.SessionID,
			"runtime.mattercodex.dev/source-execution-id":    sourceExecutionID,
			"runtime.mattercodex.dev/action":                 action,
			"runtime.mattercodex.dev/bucket":                 requiredEnv("RUNTIME_S3_BUCKET"),
			"runtime.mattercodex.dev/workload-ticket-sha256": execution.WorkloadTicketSHA256,
			"runtime.mattercodex.dev/sts-session-name":       sessionName,
			"runtime.mattercodex.dev/assumed-role-arn":       aws.ToString(assumed.AssumedRoleUser.Arn),
			"runtime.mattercodex.dev/inline-policy-sha256":   policySHA256,
			"runtime.mattercodex.dev/readback-sha256":        hex.EncodeToString(readbackDigest[:]),
			"runtime.mattercodex.dev/expires-at":             expiresAt.Format(time.RFC3339),
		},
	}, Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{
		"access-key-id": []byte(accessKeyID), "secret-access-key": []byte(secretAccessKey), "session-token": []byte(sessionToken),
	}}
	if destination.Annotations["runtime.mattercodex.dev/bucket"] == "" {
		return errors.New("runtime S3 bucket is invalid")
	}
	secrets := client.CoreV1().Secrets(namespace)
	created, err := secrets.Create(ctx, destination, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = secrets.Get(ctx, destination.Name, metav1.GetOptions{})
	}
	if err != nil || !credentialSnapshotSecretMatches(created, destination) || secretDataSHA256(created.Data) != secretDataSHA256(destination.Data) {
		return errors.New("materialize immutable runtime S3 credential snapshot")
	}
	return nil
}

func exactS3Policy(execution entity.Execution, action string) (map[string]any, string, error) {
	bucket, kmsKeyARN := requiredEnv("RUNTIME_S3_BUCKET"), requiredEnvOrFile("RUNTIME_S3_KMS_KEY_ARN", s3KMSKeyARNFile)
	region := requiredEnv("RUNTIME_S3_REGION")
	if bucket == "" || region == "" || kmsKeyARN == "" || !strings.HasPrefix(kmsKeyARN, "arn:") {
		return nil, "", errors.New("runtime S3 policy configuration is invalid")
	}
	sourceExecutionID := execution.ID
	archiveObject := strings.Join([]string{"runtime", execution.OrganizationID, execution.ProjectID, execution.SessionID, execution.ID, "archive.tar.gz"}, "/")
	archiveARN := "arn:aws:s3:::" + bucket + "/" + archiveObject
	writeARN := archiveARN
	statements := []any{
		map[string]any{"Effect": "Allow", "Action": []string{"s3:GetBucketVersioning", "s3:GetObjectLockConfiguration", "s3:GetEncryptionConfiguration", "s3:GetBucketPublicAccessBlock"}, "Resource": "arn:aws:s3:::" + bucket},
	}
	if action == "restore" {
		var archiveReference string
		sourceExecutionID, archiveReference = restoreArchiveSource(execution)
		if sourceExecutionID == "" || archiveReference == "" {
			return nil, "", errors.New("runtime restore source is invalid")
		}
		archiveObject = strings.Join([]string{"runtime", execution.OrganizationID, execution.ProjectID, execution.SessionID, sourceExecutionID, "archive.tar.gz"}, "/")
		proofObject := strings.Join([]string{"runtime-restore-proof", execution.OrganizationID, execution.ProjectID, execution.SessionID, sourceExecutionID, "restore-proof.json"}, "/")
		archiveARN, writeARN = "arn:aws:s3:::"+bucket+"/"+archiveObject, "arn:aws:s3:::"+bucket+"/"+proofObject
		versionID := exactVersionID(archiveReference)
		if versionID == "" {
			return nil, "", errors.New("runtime restore archive version is invalid")
		}
		statements = append(statements,
			map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObjectVersion", "s3:GetObjectRetention"}, "Resource": archiveARN,
				"Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "true"}, "StringEquals": map[string]string{"s3:VersionId": versionID}}},
			map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObject", "s3:GetObjectVersion", "s3:GetObjectRetention"}, "Resource": writeARN,
				"Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "true"}}},
		)
	} else if action == "archive" {
		statements = append(statements, map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObject", "s3:GetObjectVersion", "s3:GetObjectRetention"}, "Resource": archiveARN,
			"Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "true"}}})
	} else {
		return nil, "", errors.New("runtime S3 credential action is invalid")
	}
	statements = append(statements,
		map[string]any{"Effect": "Allow", "Action": []string{"s3:PutObject", "s3:PutObjectRetention"}, "Resource": writeARN,
			"Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "true"}, "StringEquals": map[string]string{"s3:x-amz-server-side-encryption": "aws:kms", "s3:object-lock-mode": "COMPLIANCE"}}},
		map[string]any{"Effect": "Allow", "Action": []string{"kms:Decrypt", "kms:Encrypt", "kms:GenerateDataKey"}, "Resource": kmsKeyARN,
			"Condition": map[string]any{"StringEquals": map[string]any{
				"kms:ViaService":                   "s3." + region + ".amazonaws.com",
				"kms:EncryptionContext:aws:s3:arn": []string{archiveARN, writeARN},
			}}},
		map[string]any{"Effect": "Deny", "Action": []string{"s3:PutObject", "s3:PutObjectRetention"}, "Resource": writeARN,
			"Condition": map[string]any{"NumericLessThan": map[string]string{"s3:object-lock-remaining-retention-days": "90"}}},
		map[string]any{"Effect": "Deny", "Action": []string{"s3:ListBucket", "s3:DeleteObject", "s3:DeleteObjectVersion", "s3:BypassGovernanceRetention"}, "Resource": []string{"arn:aws:s3:::" + bucket, "arn:aws:s3:::" + bucket + "/*"}},
		map[string]any{"Effect": "Deny", "Action": "s3:*", "Resource": []string{"arn:aws:s3:::" + bucket, "arn:aws:s3:::" + bucket + "/*"}, "Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "false"}}},
	)
	return map[string]any{"Version": "2012-10-17", "Statement": statements}, sourceExecutionID, nil
}

func restoreArchiveSource(execution entity.Execution) (string, string) {
	if execution.RestoreSourceExecutionID != "" || execution.RestoreSourceArchiveReference != "" {
		return execution.RestoreSourceExecutionID, execution.RestoreSourceArchiveReference
	}
	return execution.ID, execution.ArchiveReference
}

func exactVersionID(reference string) string {
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "s3" || parsed.Query().Get("versionId") == "" || len(parsed.Query()) != 1 {
		return ""
	}
	return parsed.Query().Get("versionId")
}

func newS3STSClient(action, accessKey, secretKey, sessionToken string) (*sts.Client, string, error) {
	endpoint := requiredEnv("RUNTIME_S3_ENDPOINT")
	serverName := requiredEnv("RUNTIME_S3_TLS_SERVER_NAME")
	region := requiredEnv("RUNTIME_S3_REGION")
	caFile := requiredEnv("RUNTIME_S3_CA_FILE")
	roleFile := s3RestoreRoleARNFile
	if action == "archive" {
		roleFile = s3ArchiveRoleARNFile
	}
	roleARN := requiredEnvOrFile("RUNTIME_S3_"+strings.ToUpper(action)+"_ROLE_ARN", roleFile)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" ||
		parsed.Fragment != "" || serverName == "" || parsed.Hostname() != serverName || region == "" || caFile == "" ||
		!strings.HasPrefix(roleARN, "arn:") ||
		!strings.HasSuffix(roleARN, ":role/mattercodex-runtime-"+action+"-execution") {
		return nil, "", errors.New("runtime S3 STS endpoint or role is invalid")
	}
	caRaw, err := os.ReadFile(caFile)
	if err != nil {
		return nil, "", errors.New("read runtime S3 STS CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, "", errors.New("parse runtime S3 STS CA")
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots,
	}}, Timeout: 10 * time.Second}
	config := aws.Config{Region: region, HTTPClient: httpClient,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken))}
	return sts.NewFromConfig(config, func(options *sts.Options) { options.BaseEndpoint = aws.String(endpoint) }), roleARN, nil
}

func newVaultClient() (*http.Client, string, error) {
	address, serverName, caFile := requiredEnv("RUNTIME_VAULT_ADDRESS"), requiredEnv("RUNTIME_VAULT_TLS_SERVER_NAME"), requiredEnv("RUNTIME_VAULT_CA_FILE")
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || serverName == "" || parsed.Hostname() != serverName {
		return nil, "", errors.New("runtime Vault endpoint is invalid")
	}
	caRaw, err := os.ReadFile(caFile)
	if err != nil {
		return nil, "", errors.New("read runtime Vault CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, "", errors.New("parse runtime Vault CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots}}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}, strings.TrimSuffix(address, "/"), nil
}

func vaultRequest(ctx context.Context, client *http.Client, method, endpoint, token string, body any, target *vaultResponse) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("X-Vault-Token", token)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return errors.New("vault request was rejected")
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if decoder.Decode(target) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("vault response is invalid")
	}
	return nil
}

func requiredEnv(name string) string { return strings.TrimSpace(os.Getenv(name)) }

func requiredEnvOrFile(name, path string) string {
	if value := requiredEnv(name); value != "" {
		return value
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 4096 {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func materializeCredential(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	execution entity.Execution,
	credential entity.CredentialRef,
	index int,
) error {
	parsed, err := url.Parse(credential.Reference)
	if err != nil || parsed.Scheme != "k8s-immutable-secret" || parsed.Host != namespace ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return errors.New("runtime credential source reference is invalid")
	}
	sourceName := strings.TrimPrefix(parsed.Path, "/")
	if !validDNSLabel(sourceName) {
		return errors.New("runtime credential source name is invalid")
	}
	source, err := client.CoreV1().Secrets(namespace).Get(ctx, sourceName, metav1.GetOptions{})
	if err != nil || source.Immutable == nil || !*source.Immutable ||
		credential.ProviderContentVersion != string(source.UID)+":"+source.ResourceVersion ||
		credential.ContentSHA256 != secretDataSHA256(source.Data) {
		return errors.New("runtime credential source readback failed")
	}
	immutable := true
	destination := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: executionCredentialSecretName(execution.ID, index),
		Labels: map[string]string{
			"runtime.mattercodex.dev/managed": "true",
			"app.kubernetes.io/component":     "runtime-credential-snapshot",
		},
		Annotations: map[string]string{
			"runtime.mattercodex.dev/execution-id":             execution.ID,
			"runtime.mattercodex.dev/runtime-revision-id":      execution.RuntimeRevisionID,
			"runtime.mattercodex.dev/runtime-revision-version": strconv.FormatUint(execution.RuntimeRevisionVersion, 10),
			"runtime.mattercodex.dev/credential-resource-id":   credential.ResourceID,
			"runtime.mattercodex.dev/credential-version":       strconv.FormatUint(credential.Version, 10),
			"runtime.mattercodex.dev/provider-content-version": credential.ProviderContentVersion,
			"runtime.mattercodex.dev/content-sha256":           credential.ContentSHA256,
			"runtime.mattercodex.dev/purpose":                  credential.Purpose,
			"runtime.mattercodex.dev/source-secret":            sourceName,
			"runtime.mattercodex.dev/source-secret-uid":        string(source.UID),
			"runtime.mattercodex.dev/source-resource-version":  source.ResourceVersion,
			"runtime.mattercodex.dev/snapshot-sha256":          execution.CredentialSnapshotSHA256,
		},
	}, Immutable: &immutable, Type: source.Type, Data: source.Data}
	secrets := client.CoreV1().Secrets(namespace)
	created, err := secrets.Create(ctx, destination, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = secrets.Get(ctx, destination.Name, metav1.GetOptions{})
	}
	if err != nil || !credentialSnapshotSecretMatches(created, destination) {
		return errors.New("materialize immutable runtime credential snapshot")
	}
	return nil
}

func verifyWorkloadTicket(snapshot runtimeSnapshot, mode string) error {
	keyRaw, keyErr := os.ReadFile(workloadTicketKeyFile)
	publicKey, decodeKeyErr := workloadticket.DecodePublicKey(keyRaw)
	if keyErr != nil || decodeKeyErr != nil {
		return errors.New("read runtime workload ticket verification material")
	}
	ticket, audience := snapshot.WorkloadTicket, "mattercodex-runtime-workload-admission"
	if mode == "s3-archive" {
		ticket, audience = snapshot.ArchiveWorkloadTicket, "mattercodex-runtime-s3-archive"
	} else if mode == "s3-restore" {
		ticket, audience = snapshot.RestoreWorkloadTicket, "mattercodex-runtime-s3-restore"
	}
	_, err := workloadticket.VerifyForAudience(ticket, publicKey, snapshot.Execution, audience, time.Now())
	return err
}

func credentialSnapshotMatches(execution entity.Execution, revision entity.Revision) bool {
	entries := make([]credentialSnapshotEntry, 0, len(revision.Credentials))
	for _, credential := range revision.Credentials {
		entries = append(entries, credentialSnapshotEntry{
			ID: credential.ResourceID, Purpose: credential.Purpose,
			ImmutableSecretRef: credential.Reference, ProviderContentVersion: credential.ProviderContentVersion,
			ContentSHA256: credential.ContentSHA256, Version: credential.Version,
		})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].ID < entries[right].ID })
	raw, err := json.Marshal(struct {
		ExecutionID string
		Credentials []credentialSnapshotEntry
	}{execution.ID, entries})
	if err != nil {
		return false
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]) == execution.CredentialSnapshotSHA256
}

func credentialSnapshotSecretMatches(actual, expected *corev1.Secret) bool {
	if actual == nil || expected == nil || actual.Immutable == nil || !*actual.Immutable ||
		actual.Type != expected.Type || secretDataSHA256(actual.Data) != secretDataSHA256(expected.Data) ||
		len(actual.Annotations) != len(expected.Annotations) {
		return false
	}
	for key, value := range expected.Annotations {
		if actual.Annotations[key] != value {
			return false
		}
	}
	return true
}

func secretDataSHA256(data map[string][]byte) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	digest := sha256.New()
	for _, key := range keys {
		_, _ = digest.Write([]byte(key))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(data[key])
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func executionCredentialSecretName(executionID string, index int) string {
	parsed := strings.ReplaceAll(executionID, "-", "")
	return "runtime-credential-" + parsed[:20] + "-" + strconv.Itoa(index)
}

func s3CredentialSecretName(executionID, action string) string {
	return "runtime-s3-" + shortID(executionID) + "-" + action
}

func shortID(value string) string {
	return strings.ReplaceAll(value, "-", "")[:20]
}

func stableHash(value string, length int) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])[:length]
}

func accessServiceAccountName(execution entity.Execution) string {
	if string(execution.AccessProfile) == "CLUSTER_ADMIN" {
		return "runtime-role-cluster-admin"
	}
	return "runtime-access-" + stableHash(execution.OrganizationID+":"+execution.ProjectID+":"+execution.SessionID+":"+execution.RoleID, 24)
}

func projectNamespaceName(projectID string) string {
	return "mattercodex-project-" + stableHash(projectID, 20)
}

func podName(execution entity.Execution) string {
	return "runtime-role-" + stableHash(execution.RoleID+":"+execution.ThreadID+":"+execution.SessionID, 24)
}

func pvcName(execution entity.Execution) string {
	return "runtime-session-" + stableHash(execution.OrganizationID+":"+execution.ProjectID+":"+execution.SessionID, 20)
}

func configName(execution entity.Execution) string {
	return "runtime-config-" + shortID(execution.ID) + "-v" + strconv.FormatUint(execution.RuntimeRevisionVersion, 10)
}

func journalName(executionID string) string { return "runtime-journal-" + shortID(executionID) }

func workerJobName(component string, execution entity.Execution) string {
	return "runtime-" + strings.TrimPrefix(component, "runtime-") + "-" + shortID(execution.ID) + "-f" + strconv.FormatUint(execution.Fence, 10)
}

func executionLabels(execution entity.Execution, component string) map[string]string {
	return map[string]string{
		"runtime.mattercodex.dev/managed":        "true",
		"app.kubernetes.io/name":                 "runtime-controller",
		"app.kubernetes.io/component":            component,
		"runtime.mattercodex.dev/execution":      shortID(execution.ID),
		"runtime.mattercodex.dev/session":        shortID(execution.SessionID),
		"runtime.mattercodex.dev/role":           shortID(execution.RoleID),
		"runtime.mattercodex.dev/access-profile": strings.ToLower(string(execution.AccessProfile)),
	}
}

func boolPointer(value bool) *bool { return &value }

func validDNSLabel(value string) bool {
	if len(value) < 1 || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, symbol := range value {
		if (symbol < 'a' || symbol > 'z') && (symbol < '0' || symbol > '9') && symbol != '-' {
			return false
		}
	}
	return true
}
