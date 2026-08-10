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
	"net"
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
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	port "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/repository/s3credential"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/security/s3policy"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/security/workloadticket"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const runtimeConfigFile = "/var/run/config/mattercodex/runtime/runtime.json"

const (
	vaultTokenFile                 = "/var/run/secrets/tokens/vault"
	workloadTicketKeyFile          = "/var/run/config/mattercodex/runtime-workload-ticket/public-key.hex"
	brokerTLSCAFile                = "/var/run/config/mattercodex/runtime-credential-authority/ca.pem"
	brokerTLSCertFile              = "/var/run/config/mattercodex/runtime-credential-authority/tls.crt"
	brokerTLSKeyFile               = "/var/run/config/mattercodex/runtime-credential-authority/tls.key"
	s3KMSKeyARNFile                = "/var/run/secrets/mattercodex/runtime-credential-broker/s3/kms-key-arn"
	s3ArchiveRoleARNFile           = "/var/run/secrets/mattercodex/runtime-credential-broker/s3/archive-role-arn"
	s3RestoreRoleARNFile           = "/var/run/secrets/mattercodex/runtime-credential-broker/s3/restore-role-arn"
	s3MinioManagementAccessKeyFile = "/var/run/secrets/mattercodex/runtime-credential-broker/s3/minio-management-access-key-id"
	s3MinioManagementSecretKeyFile = "/var/run/secrets/mattercodex/runtime-credential-broker/s3/minio-management-secret-access-key"
	s3MinioIdentitySigningKeyFile  = "/var/run/secrets/mattercodex/runtime-credential-broker/s3/minio-identity-signing-key"
	maximumSTSTTL                  = 15 * time.Minute
	restoreEffectCAFile            = "/var/run/config/mattercodex/runtime-restore-effect/control-plane/ca.pem"
	restoreEffectCertFile          = "/var/run/secrets/mattercodex/runtime-restore-effect/workload-tls/tls.crt"
	restoreEffectKeyFile           = "/var/run/secrets/mattercodex/runtime-restore-effect/workload-tls/tls.key"
	restoreEffectGrantFile         = "/var/run/secrets/mattercodex/runtime-restore-effect/application-grant/application-grant.jws"
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
	if len(os.Args) != 2 {
		return errors.New("runtime credential broker mode is invalid")
	}
	if strings.HasPrefix(os.Args[1], "serve-") {
		return runAuthority(ctx, strings.TrimPrefix(os.Args[1], "serve-"))
	}
	if strings.HasPrefix(os.Args[1], "check-") {
		return checkAuthority(ctx, strings.TrimPrefix(os.Args[1], "check-"))
	}
	if strings.HasPrefix(os.Args[1], "ready-") {
		mode := strings.TrimPrefix(os.Args[1], "ready-")
		if err := checkAuthority(ctx, mode); err != nil {
			return err
		}
		if mode != "s3-restore" {
			return nil
		}
		client, err := dialRestoreEffectClient(ctx)
		if err != nil {
			return err
		}
		defer client.Close()
		return client.Check(ctx)
	}
	if os.Args[1] == "copy-credential" {
		return runCredentialCopy(ctx)
	}
	if os.Args[1] == "s3-readback" {
		return runS3CredentialReadback(ctx)
	}
	if os.Args[1] != "snapshot" && os.Args[1] != "s3-archive" && os.Args[1] != "s3-restore" {
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
	return requestMaterialization(ctx, raw, snapshot, os.Args[1])
}

func materialize(ctx context.Context, client kubernetes.Interface, namespace string, snapshot runtimeSnapshot, mode string) (map[string]string, error) {
	if mode == "snapshot" {
		for index, credential := range snapshot.Revision.Credentials {
			if err := materializeCredentialCopy(ctx, client, namespace, snapshot, credential, index); err != nil {
				return nil, err
			}
		}
		if err := materializeAccessProfile(ctx, client, namespace, snapshot.Execution, len(snapshot.Revision.Credentials)); err != nil {
			return nil, err
		}
		if err := materializeWorkerJournalGrants(ctx, client, namespace, snapshot.Execution); err != nil {
			return nil, err
		}
		return nil, materializeRolePod(ctx, client, namespace, snapshot)
	}
	action := strings.TrimPrefix(mode, "s3-")
	return materializeS3Credential(ctx, client, namespace, snapshot, action)
}

func runCredentialCopy(ctx context.Context) error {
	namespace, executionID := requiredEnv("RUNTIME_NAMESPACE"), requiredEnv("RUNTIME_EXECUTION_ID")
	index, err := strconv.Atoi(requiredEnv("RUNTIME_CREDENTIAL_INDEX"))
	if namespace == "" || uuid.Validate(executionID) != nil || err != nil || index < 0 || index > 63 {
		return errors.New("runtime credential copy identity is invalid")
	}
	raw, err := os.ReadFile(runtimeConfigFile)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return errors.New("read immutable runtime credential copy input")
	}
	var snapshot runtimeSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		snapshot.Execution.ID != executionID || index >= len(snapshot.Revision.Credentials) ||
		snapshot.Execution.Validate() != nil || snapshot.Revision.ValidateFor(snapshot.Execution) != nil ||
		!credentialSnapshotMatches(snapshot.Execution, snapshot.Revision) || verifyWorkloadTicket(snapshot, "snapshot") != nil {
		return errors.New("immutable runtime credential copy input is invalid")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("load runtime credential copy Kubernetes identity")
	}
	config.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.New("create runtime credential copy Kubernetes client")
	}
	credential := snapshot.Revision.Credentials[index]
	if err := materializeCredential(ctx, client, namespace, snapshot.Execution, credential, index); err != nil {
		return err
	}
	immutable := true
	receipt := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:   credentialCopyReceiptName(executionID, index),
		Labels: executionLabels(snapshot.Execution, "runtime-credential-copy-receipt"),
		Annotations: map[string]string{"runtime.mattercodex.dev/runtime-config-name": configName(snapshot.Execution),
			"runtime.mattercodex.dev/copy-service-account": credentialCopyName(executionID, index)},
	}, Immutable: &immutable, Data: map[string]string{
		"execution_id": executionID, "credential_index": strconv.Itoa(index),
		"resource_id": credential.ResourceID, "version": strconv.FormatUint(credential.Version, 10),
		"provider_content_version": credential.ProviderContentVersion,
		"content_sha256":           credential.ContentSHA256,
	}}
	created, err := client.CoreV1().ConfigMaps(namespace).Create(ctx, receipt, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = client.CoreV1().ConfigMaps(namespace).Get(ctx, receipt.Name, metav1.GetOptions{})
	}
	if err != nil || created.Immutable == nil || !*created.Immutable || !reflectStringMapEqual(created.Data, receipt.Data) {
		return errors.New("commit runtime credential copy receipt")
	}
	return nil
}

func materializeCredentialCopy(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	snapshot runtimeSnapshot,
	credential entity.CredentialRef,
	index int,
) error {
	parsed, err := url.Parse(credential.Reference)
	if err != nil || parsed.Scheme != "k8s-immutable-secret" || parsed.Host != namespace ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil || !validDNSLabel(strings.TrimPrefix(parsed.Path, "/")) {
		return errors.New("runtime credential source reference is invalid")
	}
	execution := snapshot.Execution
	name := credentialCopyName(execution.ID, index)
	labels := executionLabels(execution, "runtime-credential-copy")
	resourceAnnotations := map[string]string{
		"runtime.mattercodex.dev/runtime-config-name":    configName(execution),
		"runtime.mattercodex.dev/workload-ticket-sha256": execution.WorkloadTicketSHA256,
		"runtime.mattercodex.dev/source-secret":          strings.TrimPrefix(parsed.Path, "/"),
		"runtime.mattercodex.dev/destination-secret":     executionCredentialSecretName(execution.ID, index),
		"runtime.mattercodex.dev/credential-index":       strconv.Itoa(index),
	}
	podAnnotations := make(map[string]string, len(resourceAnnotations)+2)
	for key, value := range resourceAnnotations {
		podAnnotations[key] = value
	}
	podAnnotations["runtime.mattercodex.dev/next-input-config"] = configName(execution)
	podAnnotations["runtime.mattercodex.dev/workload-ticket"] = snapshot.WorkloadTicket
	keyRaw, err := os.ReadFile(workloadTicketKeyFile)
	if err != nil || len(keyRaw) == 0 || len(keyRaw) > 4096 {
		return errors.New("read runtime workload ticket verification material")
	}
	trustName := "runtime-ticket-trust-" + stableHash(execution.ID, 20)
	immutable := true
	trust := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: trustName,
		Labels:      executionLabels(execution, "runtime-ticket-trust"),
		Annotations: map[string]string{"runtime.mattercodex.dev/runtime-config-name": configName(execution)}},
		Immutable: &immutable, Data: map[string]string{"public-key.hex": strings.TrimSpace(string(keyRaw))}}
	actualTrust, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, trustName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actualTrust, err = client.CoreV1().ConfigMaps(namespace).Create(ctx, trust, metav1.CreateOptions{})
	}
	if err != nil || actualTrust.Immutable == nil || !*actualTrust.Immutable ||
		actualTrust.Data["public-key.hex"] != trust.Data["public-key.hex"] {
		return errors.New("materialize exact runtime ticket trust snapshot")
	}
	serviceAccounts := client.CoreV1().ServiceAccounts(namespace)
	account := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels, Annotations: resourceAnnotations}, AutomountServiceAccountToken: boolPointer(false)}
	actualAccount, err := serviceAccounts.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actualAccount, err = serviceAccounts.Create(ctx, account, metav1.CreateOptions{})
	}
	if err != nil || actualAccount.Name != name || !reflectStringMapEqual(actualAccount.Annotations, resourceAnnotations) {
		return errors.New("materialize exact runtime credential copy identity")
	}
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels, Annotations: resourceAnnotations}, Rules: []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{strings.TrimPrefix(parsed.Path, "/"), executionCredentialSecretName(execution.ID, index)}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"create"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{configName(execution), credentialCopyReceiptName(execution.ID, index)}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"create"}},
	}}
	actualRole, err := client.RbacV1().Roles(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actualRole, err = client.RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{})
	}
	if err != nil || !policyRulesEqual(actualRole.Rules, role.Rules) || !reflectStringMapEqual(actualRole.Annotations, resourceAnnotations) {
		return errors.New("materialize exact runtime credential copy role")
	}
	binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels, Annotations: resourceAnnotations},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: name, Namespace: namespace}},
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}}
	actualBinding, err := client.RbacV1().RoleBindings(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actualBinding, err = client.RbacV1().RoleBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
	}
	if err != nil || len(actualBinding.Subjects) != 1 || actualBinding.Subjects[0] != binding.Subjects[0] || actualBinding.RoleRef != binding.RoleRef {
		return errors.New("materialize exact runtime credential copy binding")
	}
	tokenTTL := int64(600)
	defaultMode := int32(0o440)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels, Annotations: podAnnotations},
		Spec: corev1.PodSpec{
			ServiceAccountName: name, AutomountServiceAccountToken: boolPointer(false),
			RestartPolicy: corev1.RestartPolicyOnFailure, EnableServiceLinks: boolPointer(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: boolPointer(true), RunAsUser: int64Pointer(10001),
				RunAsGroup: int64Pointer(10001), FSGroup: int64Pointer(10001),
			},
			Containers: []corev1.Container{{
				Name: "copy", Image: requiredEnv("RUNTIME_CREDENTIAL_COPY_IMAGE"),
				Command: []string{"/usr/local/bin/runtime-credential-broker"}, Args: []string{"copy-credential"},
				Env: []corev1.EnvVar{
					{Name: "RUNTIME_NAMESPACE", Value: namespace},
					{Name: "RUNTIME_EXECUTION_ID", Value: execution.ID},
					{Name: "RUNTIME_CREDENTIAL_INDEX", Value: strconv.Itoa(index)},
				},
				SecurityContext: restrictedSecurityContext(),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "runtime-config", MountPath: runtimeConfigFile, SubPath: "runtime.json", ReadOnly: true},
					{Name: "ticket-trust", MountPath: "/var/run/config/mattercodex/runtime-workload-ticket", ReadOnly: true},
					{Name: "kube-api-access", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "runtime-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: configName(execution)}, DefaultMode: &defaultMode,
				}}},
				{Name: "ticket-trust", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: trustName}, DefaultMode: &defaultMode,
				}}},
				{Name: "kube-api-access", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
					DefaultMode: &defaultMode,
					Sources: []corev1.VolumeProjection{
						{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Audience: "https://kubernetes.default.svc", ExpirationSeconds: &tokenTTL, Path: "token"}},
						{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}, Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}},
						{DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{{Path: "namespace", FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.namespace"}}}}},
					},
				}}},
			},
		},
	}
	actualPod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actualPod, err = client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	}
	if err != nil || actualPod.Name != name || actualPod.Spec.ServiceAccountName != name || !reflectStringMapEqual(actualPod.Annotations, podAnnotations) {
		return errors.New("materialize exact runtime credential copy Pod")
	}
	return wait.PollUntilContextTimeout(ctx, 250*time.Millisecond, 30*time.Second, true, func(pollCtx context.Context) (bool, error) {
		receipt, getErr := client.CoreV1().ConfigMaps(namespace).Get(pollCtx, credentialCopyReceiptName(execution.ID, index), metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			return false, nil
		}
		if getErr != nil || receipt.Immutable == nil || !*receipt.Immutable || receipt.Data["execution_id"] != execution.ID ||
			receipt.Data["credential_index"] != strconv.Itoa(index) || receipt.Data["resource_id"] != credential.ResourceID ||
			receipt.Data["provider_content_version"] != credential.ProviderContentVersion || receipt.Data["content_sha256"] != credential.ContentSHA256 {
			return false, errors.New("runtime credential copy receipt mismatch")
		}
		return true, nil
	})
}

func credentialCopyName(executionID string, index int) string {
	return "runtime-credential-copy-" + stableHash(executionID+":"+strconv.Itoa(index), 20)
}

func credentialCopyReceiptName(executionID string, index int) string {
	return "runtime-credential-copy-receipt-" + stableHash(executionID+":"+strconv.Itoa(index), 20)
}

func reflectStringMapEqual(left, right map[string]string) bool {
	return jsonEqual(left, right)
}

func requestMaterialization(ctx context.Context, raw []byte, snapshot runtimeSnapshot, mode string) error {
	endpoint := requiredEnv("RUNTIME_CREDENTIAL_AUTHORITY_URL")
	serverName := requiredEnv("RUNTIME_CREDENTIAL_AUTHORITY_TLS_SERVER_NAME")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "/v1/materialize" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() != serverName {
		return errors.New("runtime credential authority endpoint is invalid")
	}
	client, err := exactMTLSClient(serverName, brokerTLSCAFile, brokerTLSCertFile, brokerTLSKeyFile)
	if err != nil {
		return err
	}
	ticket := snapshot.WorkloadTicket
	if mode == "s3-archive" {
		ticket = snapshot.ArchiveWorkloadTicket
	} else if mode == "s3-restore" {
		ticket = snapshot.RestoreWorkloadTicket
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return errors.New("create runtime credential authority request")
	}
	request.Header.Set("Authorization", "Bearer "+ticket)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return errors.New("call runtime credential authority")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusNoContent {
		return errors.New("runtime credential authority rejected request")
	}
	return nil
}

func exactMTLSClient(serverName, caFile, certificateFile, privateKeyFile string) (*http.Client, error) {
	if serverName == "" || net.ParseIP(serverName) != nil {
		return nil, errors.New("runtime credential authority TLS identity is invalid")
	}
	caRaw, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errors.New("read runtime credential authority CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse runtime credential authority CA")
	}
	certificate, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
	if err != nil {
		return nil, errors.New("load runtime credential authority client identity")
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots,
		Certificates: []tls.Certificate{certificate},
	}}, Timeout: 15 * time.Second}, nil
}

func runAuthority(ctx context.Context, mode string) error {
	if mode != "snapshot" && mode != "s3-archive" && mode != "s3-restore" {
		return errors.New("runtime credential authority mode is invalid")
	}
	if err := checkAuthorityFiles(mode); err != nil {
		return err
	}
	namespace := requiredEnv("RUNTIME_NAMESPACE")
	listen := requiredEnv("RUNTIME_CREDENTIAL_AUTHORITY_LISTEN")
	if namespace == "" || listen == "" {
		return errors.New("runtime credential authority configuration is invalid")
	}
	allowedClients := make(map[string]struct{})
	for _, identity := range strings.Split(requiredEnv("RUNTIME_CREDENTIAL_AUTHORITY_CLIENT_SPIFFE_IDS"), ",") {
		identity = strings.TrimSpace(identity)
		if !strings.HasPrefix(identity, "spiffe://mattercodex.internal/ns/"+namespace+"/sa/") {
			return errors.New("runtime credential authority client identity is invalid")
		}
		allowedClients[identity] = struct{}{}
	}
	if len(allowedClients) == 0 {
		return errors.New("runtime credential authority client identity is invalid")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("load credential authority Kubernetes configuration")
	}
	config.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.New("create credential authority Kubernetes client")
	}
	if err := checkS3Provider(ctx, client, namespace, mode); err != nil {
		return err
	}
	var restoreEffects *sharedclient.Client
	if mode == "s3-restore" {
		restoreEffects, err = dialRestoreEffectClient(ctx)
		if err != nil {
			return err
		}
		defer restoreEffects.Close()
		if err := restoreEffects.Check(ctx); err != nil {
			return errors.New("restore effect authority is unavailable")
		}
	}
	tlsConfig, err := exactMTLSServerConfig(brokerTLSCAFile, brokerTLSCertFile, brokerTLSKeyFile)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, request *http.Request) {
		probeCtx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
		defer cancel()
		if fileErr := checkAuthorityFiles(mode); fileErr != nil {
			http.Error(writer, "credential authority material is unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, probeErr := client.CoreV1().ConfigMaps(namespace).Get(probeCtx, "kube-root-ca.crt", metav1.GetOptions{}); probeErr != nil {
			http.Error(writer, "credential authority dependency is unavailable", http.StatusServiceUnavailable)
			return
		}
		if providerErr := checkS3Provider(probeCtx, client, namespace, mode); providerErr != nil {
			http.Error(writer, "credential authority provider is unavailable", http.StatusServiceUnavailable)
			return
		}
		if restoreEffects != nil {
			if effectErr := restoreEffects.Check(probeCtx); effectErr != nil {
				http.Error(writer, "restore effect authority is unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/materialize", authorityHandler(client, restoreEffects, namespace, mode, allowedClients))
	server := &http.Server{Addr: listen, Handler: mux, TLSConfig: tlsConfig,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second}
	errChannel := make(chan error, 1)
	go func() { errChannel <- server.ListenAndServeTLS("", "") }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			return errors.New("shutdown runtime credential authority")
		}
		return nil
	case serveErr := <-errChannel:
		if errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return errors.New("serve runtime credential authority")
	}
}

func checkAuthorityFiles(mode string) error {
	if mode != "snapshot" && mode != "s3-archive" && mode != "s3-restore" {
		return errors.New("runtime credential authority mode is invalid")
	}
	paths := []string{
		brokerTLSCAFile, brokerTLSCertFile, brokerTLSKeyFile, workloadTicketKeyFile,
		"/var/run/secrets/kubernetes.io/serviceaccount/token",
		"/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
	}
	if mode == "s3-archive" || mode == "s3-restore" {
		backend, err := configuredS3CredentialBackend()
		if err != nil {
			return err
		}
		paths = append(paths, requiredEnv("RUNTIME_S3_CA_FILE"), s3KMSKeyARNFile)
		if backend == s3CredentialBackendInternalMinIO {
			if requiredEnv("RUNTIME_S3_MINIO_ADMIN_ENDPOINT") == "" || requiredEnv("RUNTIME_S3_MINIO_ADMIN_TLS_SERVER_NAME") == "" ||
				requiredEnv("RUNTIME_S3_MINIO_PARENT_USER") == "" || requiredEnv("RUNTIME_S3_MINIO_KMS_KEY_ID") == "" {
				return errors.New("runtime MinIO identity provider configuration is invalid")
			}
			paths = append(paths, s3MinioManagementAccessKeyFile, s3MinioManagementSecretKeyFile, s3MinioIdentitySigningKeyFile)
		} else {
			paths = append(paths, vaultTokenFile, requiredEnv("RUNTIME_VAULT_CA_FILE"))
			if mode == "s3-archive" {
				paths = append(paths, s3ArchiveRoleARNFile)
			} else {
				paths = append(paths, s3RestoreRoleARNFile)
			}
		}
	}
	if mode == "s3-restore" {
		paths = append(paths, restoreEffectCAFile, restoreEffectCertFile,
			restoreEffectKeyFile, restoreEffectGrantFile)
	}
	for _, path := range paths {
		if path == "" {
			return errors.New("runtime credential authority file path is invalid")
		}
		raw, err := os.ReadFile(path)
		if err != nil || len(bytes.TrimSpace(raw)) == 0 || len(raw) > 1<<20 {
			return errors.New("read runtime credential authority material")
		}
	}
	if _, err := exactMTLSServerConfig(brokerTLSCAFile, brokerTLSCertFile, brokerTLSKeyFile); err != nil {
		return err
	}
	keyRaw, err := os.ReadFile(workloadTicketKeyFile)
	if err != nil {
		return errors.New("read runtime workload ticket verification material")
	}
	if _, err = workloadticket.DecodePublicKey(keyRaw); err != nil {
		return errors.New("read runtime workload ticket verification material")
	}
	return nil
}

func checkAuthority(ctx context.Context, mode string) error {
	if err := checkAuthorityFiles(mode); err != nil {
		return err
	}
	if mode != "s3-archive" && mode != "s3-restore" {
		return nil
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("load credential authority Kubernetes configuration")
	}
	config.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.New("create credential authority Kubernetes client")
	}
	return checkS3Provider(ctx, client, requiredEnv("RUNTIME_NAMESPACE"), mode)
}

func checkS3Provider(ctx context.Context, client kubernetes.Interface, namespace, mode string) error {
	if mode != "s3-archive" && mode != "s3-restore" {
		return nil
	}
	action := port.Action(strings.TrimPrefix(mode, "s3-"))
	provider, err := newS3CredentialProvider(client, namespace, action)
	if err != nil {
		return err
	}
	defer provider.Close()
	return provider.Ready(ctx, action)
}

func exactMTLSServerConfig(caFile, certificateFile, privateKeyFile string) (*tls.Config, error) {
	caRaw, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errors.New("read runtime credential authority client CA")
	}
	clients := x509.NewCertPool()
	if !clients.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse runtime credential authority client CA")
	}
	certificate, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
	if err != nil {
		return nil, errors.New("load runtime credential authority server identity")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs: clients, Certificates: []tls.Certificate{certificate}}, nil
}

func authorityHandler(
	client kubernetes.Interface,
	restoreEffects *sharedclient.Client,
	namespace, mode string,
	allowedClients map[string]struct{},
) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.TLS == nil || len(request.TLS.PeerCertificates) != 1 ||
			!allowedPeer(request.TLS.PeerCertificates[0], allowedClients) {
			http.Error(writer, "credential authority caller is forbidden", http.StatusForbidden)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(request.Body, (1<<20)+1))
		if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
			http.Error(writer, "credential authority request is invalid", http.StatusBadRequest)
			return
		}
		var snapshot runtimeSnapshot
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&snapshot) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
			snapshot.Execution.Validate() != nil || snapshot.Revision.ValidateFor(snapshot.Execution) != nil ||
			!credentialSnapshotMatches(snapshot.Execution, snapshot.Revision) || verifyWorkloadTicket(snapshot, mode) != nil {
			http.Error(writer, "credential authority request is invalid", http.StatusBadRequest)
			return
		}
		ticket := snapshot.WorkloadTicket
		if mode == "s3-archive" {
			ticket = snapshot.ArchiveWorkloadTicket
		} else if mode == "s3-restore" {
			ticket = snapshot.RestoreWorkloadTicket
		}
		if request.Header.Get("Authorization") != "Bearer "+ticket {
			http.Error(writer, "credential authority bearer is invalid", http.StatusForbidden)
			return
		}
		if mode == "s3-restore" {
			if restoreEffects == nil || authorizeRestoreCredentialEffect(
				request.Context(), restoreEffects, snapshot.Execution,
			) != nil {
				http.Error(writer, "restore effect authorization was rejected", http.StatusConflict)
				return
			}
		}
		requestDigest := sha256.Sum256(append([]byte(mode+"\x00"), raw...))
		digest := hex.EncodeToString(requestDigest[:])
		if rejoined, receiptErr := rejoinAuthorityReceipt(request.Context(), client, namespace, snapshot.Execution, mode, digest); receiptErr != nil {
			http.Error(writer, "credential authority receipt is invalid", http.StatusConflict)
			return
		} else if rejoined {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		proof, err := materialize(request.Context(), client, namespace, snapshot, mode)
		if err != nil {
			http.Error(writer, "credential materialization failed", http.StatusServiceUnavailable)
			return
		}
		if err := recordAuthorityReceipt(request.Context(), client, namespace, snapshot.Execution, mode, digest, proof); err != nil {
			http.Error(writer, "credential authority receipt was not committed", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}

func dialRestoreEffectClient(ctx context.Context) (*sharedclient.Client, error) {
	target := requiredEnv("RUNTIME_RESTORE_EFFECT_CONTROL_PLANE_TARGET")
	serverName := requiredEnv("RUNTIME_RESTORE_EFFECT_CONTROL_PLANE_TLS_SERVER_NAME")
	if target == "" || serverName == "" {
		return nil, errors.New("restore effect control-plane configuration is invalid")
	}
	return sharedclient.Dial(ctx, sharedclient.Config{
		Target: target, TLSServerName: serverName, CAFile: restoreEffectCAFile,
		ClientCertificateFile: restoreEffectCertFile, ClientPrivateKeyFile: restoreEffectKeyFile,
		ApplicationGrantFile: restoreEffectGrantFile, ExpectedIssuerUID: 29001,
		ExpectedIssuerGID: 29000, DialTimeout: 2 * time.Second,
		Operations: sharedclient.RuntimeRestoreEffectOperations(),
	})
}

func authorizeRestoreCredentialEffect(
	ctx context.Context,
	client *sharedclient.Client,
	execution entity.Execution,
) error {
	if string(execution.State) != "ADMITTED" || execution.RestoreOperationID == "" ||
		execution.RestoreOperationGeneration == 0 || !validSHA256(execution.RestoreSourceAuthoritySHA256) {
		return errors.New("restore effect tuple is invalid")
	}
	key := uuid.NewSHA1(uuid.NameSpaceOID, []byte("runtime-restore-s3-credential:"+
		execution.RestoreOperationID+":"+strconv.FormatUint(execution.RestoreOperationGeneration, 10))).String()
	response, err := client.ControlPlane.AuthorizeRuntimeRestoreEffect(ctx,
		&controlplanev1.AuthorizeRuntimeRestoreEffectRequest{
			IdempotencyKey: key, ExecutionId: execution.ID,
			ExpectedVersion: execution.Version, ExpectedFence: execution.Fence,
			RestoreOperationId:           execution.RestoreOperationID,
			RestoreOperationGeneration:   execution.RestoreOperationGeneration,
			RestoreSourceAuthoritySha256: execution.RestoreSourceAuthoritySHA256,
			Effect:                       controlplanev1.RuntimeRestoreEffect_RUNTIME_RESTORE_EFFECT_S3_CREDENTIAL,
		})
	if err != nil {
		return err
	}
	current := response.GetExecution()
	if current == nil || current.GetExecutionId() != execution.ID || current.GetVersion() != execution.Version ||
		current.GetFence() != execution.Fence ||
		current.GetState() != controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_ADMITTED {
		return errors.New("restore effect readback mismatch")
	}
	return nil
}

func allowedPeer(certificate *x509.Certificate, allowed map[string]struct{}) bool {
	if certificate == nil || len(certificate.URIs) != 1 {
		return false
	}
	_, ok := allowed[certificate.URIs[0].String()]
	return ok
}

func authorityReceiptName(executionID, mode string) string {
	return "runtime-authority-receipt-" + stableHash(executionID+":"+mode, 24)
}

func rejoinAuthorityReceipt(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	execution entity.Execution,
	mode, digest string,
) (bool, error) {
	receipt, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, authorityReceiptName(execution.ID, mode), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil || receipt.Immutable == nil || !*receipt.Immutable || receipt.Data["request_sha256"] != digest ||
		receipt.Data["execution_id"] != execution.ID || receipt.Data["mode"] != mode ||
		receipt.Data["version"] != strconv.FormatUint(execution.Version, 10) ||
		receipt.Data["fence"] != strconv.FormatUint(execution.Fence, 10) {
		return false, errors.New("runtime credential authority receipt mismatch")
	}
	if mode == "s3-archive" || mode == "s3-restore" {
		action := strings.TrimPrefix(mode, "s3-")
		sourceExecutionID := execution.ID
		if action == "restore" && execution.RestoreSourceExecutionID != "" {
			sourceExecutionID = execution.RestoreSourceExecutionID
		}
		if receipt.Data["s3_execution_id"] != execution.ID ||
			receipt.Data["s3_organization_id"] != execution.OrganizationID ||
			receipt.Data["s3_project_id"] != execution.ProjectID ||
			receipt.Data["s3_session_id"] != execution.SessionID ||
			receipt.Data["s3_source_execution_id"] != sourceExecutionID ||
			receipt.Data["s3_action"] != action || receipt.Data["s3_secret_name"] != s3CredentialSecretName(execution.ID, action) ||
			receipt.Data["s3_secret_uid"] == "" || receipt.Data["s3_secret_resource_version"] == "" ||
			!validSHA256(receipt.Data["s3_policy_sha256"]) || !validSHA256(receipt.Data["s3_readback_sha256"]) ||
			!validSHA256(receipt.Data["s3_secret_data_sha256"]) || !validSHA256(receipt.Data["s3_content_sha256"]) {
			return false, errors.New("runtime credential authority S3 proof mismatch")
		}
		expiresAt, parseErr := time.Parse(time.RFC3339, receipt.Data["s3_expires_at"])
		if parseErr != nil || !expiresAt.After(time.Now().UTC().Add(time.Minute)) ||
			expiresAt.After(time.Now().UTC().Add(maximumSTSTTL+time.Minute)) {
			return false, errors.New("runtime credential authority S3 proof expired")
		}
	}
	return true, nil
}

func recordAuthorityReceipt(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	execution entity.Execution,
	mode, digest string,
	proof map[string]string,
) error {
	immutable := true
	desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: authorityReceiptName(execution.ID, mode),
		Labels: map[string]string{
			"runtime.mattercodex.dev/managed":   "true",
			"runtime.mattercodex.dev/execution": shortID(execution.ID),
			"app.kubernetes.io/component":       "runtime-credential-authority-receipt",
		},
	}, Immutable: &immutable, Data: map[string]string{
		"request_sha256": digest, "execution_id": execution.ID, "mode": mode,
		"version": strconv.FormatUint(execution.Version, 10), "fence": strconv.FormatUint(execution.Fence, 10),
	}}
	for key, value := range proof {
		if !strings.HasPrefix(key, "s3_") || value == "" {
			return errors.New("runtime credential authority proof is invalid")
		}
		desired.Data[key] = value
	}
	created, err := client.CoreV1().ConfigMaps(namespace).Create(ctx, desired, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		returnValue, readErr := rejoinAuthorityReceipt(ctx, client, namespace, execution, mode, digest)
		if readErr == nil && returnValue {
			return nil
		}
		return errors.New("runtime credential authority receipt conflict")
	}
	if err != nil || created.Immutable == nil || !*created.Immutable || created.Data["request_sha256"] != digest {
		return errors.New("commit runtime credential authority receipt")
	}
	return nil
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
		len(pod.Spec.InitContainers) != 2 || len(pod.Spec.Containers) != 3 ||
		pod.Spec.InitContainers[0].Name != "internal-rpc-authority-socket-init" ||
		pod.Spec.InitContainers[1].Name != "workspace-init" ||
		pod.Spec.Containers[0].Name != "role-runtime" || pod.Spec.Containers[1].Name != "provider-runtime" ||
		pod.Spec.Containers[2].Name != "internal-rpc-authority-issuer" ||
		len(pod.Spec.InitContainers[1].Args) != 1 || pod.Spec.InitContainers[1].Args[0] != "runtime-init-workspace" ||
		len(pod.Spec.Containers[0].Args) != 1 || pod.Spec.Containers[0].Args[0] != "runtime-session" ||
		len(pod.Spec.Containers[1].Args) != 1 || pod.Spec.Containers[1].Args[0] != "runtime-provider" ||
		pod.Spec.InitContainers[1].Image != pod.Spec.Containers[0].Image || pod.Spec.Containers[1].Image != revision.ImageReference ||
		pod.Spec.Containers[0].Image != revision.ImageReference ||
		!restrictedContainer(pod.Spec.InitContainers[0].SecurityContext) || !restrictedContainer(pod.Spec.InitContainers[1].SecurityContext) ||
		!restrictedContainer(pod.Spec.Containers[0].SecurityContext) ||
		!restrictedContainer(pod.Spec.Containers[1].SecurityContext) || !restrictedContainer(pod.Spec.Containers[2].SecurityContext) ||
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
		actual.Spec.RestartPolicy != corev1.RestartPolicyNever || len(actual.Spec.InitContainers) != 2 ||
		len(actual.Spec.Containers) != 3 ||
		actual.Spec.InitContainers[0].Name != "internal-rpc-authority-socket-init" ||
		actual.Spec.InitContainers[1].Name != "workspace-init" ||
		actual.Spec.Containers[0].Name != "role-runtime" || actual.Spec.Containers[1].Name != "provider-runtime" ||
		actual.Spec.Containers[2].Name != "internal-rpc-authority-issuer" ||
		actual.Spec.InitContainers[0].Image != desired.Spec.InitContainers[0].Image ||
		actual.Spec.InitContainers[1].Image != desired.Spec.InitContainers[1].Image ||
		actual.Spec.Containers[0].Image != desired.Spec.Containers[0].Image ||
		actual.Spec.Containers[1].Image != desired.Spec.Containers[1].Image ||
		actual.Spec.Containers[2].Image != desired.Spec.Containers[2].Image ||
		!jsonEqual(actual.Spec.InitContainers[1].Args, []string{"runtime-init-workspace"}) ||
		!jsonEqual(actual.Spec.Containers[0].Args, []string{"runtime-session"}) ||
		!jsonEqual(actual.Spec.Containers[1].Args, []string{"runtime-provider"}) ||
		!restrictedContainer(actual.Spec.InitContainers[0].SecurityContext) ||
		!restrictedContainer(actual.Spec.InitContainers[1].SecurityContext) ||
		!restrictedContainer(actual.Spec.Containers[0].SecurityContext) ||
		!restrictedContainer(actual.Spec.Containers[1].SecurityContext) ||
		!restrictedContainer(actual.Spec.Containers[2].SecurityContext) {
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
	if len(volumes) != credentialCount+20 {
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
			if strings.HasPrefix(volume.Name, "authority-") {
				continue
			}
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
	if string(execution.AccessProfile) != "NONE" {
		return errors.New("direct Kubernetes access is not allowed for agent-runner")
	}
	annotations := map[string]string{
		"runtime.mattercodex.dev/execution-id":    execution.ID,
		"runtime.mattercodex.dev/organization-id": execution.OrganizationID,
		"runtime.mattercodex.dev/project-id":      execution.ProjectID,
		"runtime.mattercodex.dev/session-id":      execution.SessionID,
	}
	accounts := client.CoreV1().ServiceAccounts(namespace)
	account, err := accounts.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		account, err = accounts.Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name: name, Labels: executionLabels(execution, "runtime-access"), Annotations: annotations,
		}, AutomountServiceAccountToken: boolPointer(false)}, metav1.CreateOptions{})
	}
	if err != nil || account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken ||
		account.Labels["app.kubernetes.io/component"] != "runtime-access" {
		return errors.New("materialize exact runtime access identity")
	}
	for key, value := range annotations {
		if account.Annotations[key] != value {
			return errors.New("materialize exact runtime access identity")
		}
	}
	return materializeHandoff(ctx, client, namespace, execution, name, credentialCount)
}

func materializeHandoff(ctx context.Context, client kubernetes.Interface, namespace string, execution entity.Execution, serviceAccount string, credentialCount int) error {
	name := "runtime-handoff-" + shortID(execution.ID)
	_ = credentialCount
	desired := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: executionLabels(execution, "runtime-handoff")}, Rules: []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{configName(execution)}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{"runtime-handoff-" + shortID(execution.ID)}, Verbs: []string{"get", "update"}},
	}}
	roles := client.RbacV1().Roles(namespace)
	actual, err := roles.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err = roles.Create(ctx, desired, metav1.CreateOptions{})
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
	snapshot runtimeSnapshot,
	action string,
) (map[string]string, error) {
	execution := snapshot.Execution
	if action != "archive" && action != "restore" {
		return nil, errors.New("runtime S3 credential action is invalid")
	}
	backend, err := configuredS3CredentialBackend()
	if err != nil {
		return nil, err
	}
	secrets := client.CoreV1().Secrets(namespace)
	policy, sourceExecutionID, err := exactS3PolicyForBackend(execution, action, backend)
	if err != nil {
		return nil, err
	}
	policyRaw, err := json.Marshal(policy)
	if err != nil {
		return nil, errors.New("encode exact runtime S3 policy")
	}
	policyDigest := sha256.Sum256(policyRaw)
	policySHA256 := hex.EncodeToString(policyDigest[:])
	provider, err := newS3CredentialProvider(client, namespace, port.Action(action))
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	if err = provider.Ready(ctx, port.Action(action)); err != nil {
		return nil, err
	}
	providerRequest := port.Request{
		Execution: execution, Action: port.Action(action), SourceExecutionID: sourceExecutionID, PolicyRaw: policyRaw,
	}
	issued, err := provider.Issue(ctx, providerRequest)
	if err != nil {
		return nil, err
	}
	if err = provider.Check(ctx, providerRequest); err != nil {
		return nil, err
	}
	materialized := false
	defer func() {
		if materialized {
			return
		}
		revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = provider.Revoke(revokeCtx, providerRequest, issued)
	}()
	accessKeyID, secretAccessKey, sessionToken, expiresAt := issued.AccessKeyID, issued.SecretAccessKey, issued.SessionToken, issued.ExpiresAt
	sessionName := issued.SessionName
	readbackRaw, err := json.Marshal(struct {
		BootstrapLeaseID, LoginAccessor, AssumedRoleARN, PolicySHA256 string
		ExecutionID, Action, SourceExecutionID, ExpiresAt             string
	}{issued.BootstrapLeaseID, issued.LoginAccessor, issued.AssumedRoleARN, policySHA256,
		execution.ID, action, sourceExecutionID,
		expiresAt.Format(time.RFC3339)})
	if err != nil {
		return nil, errors.New("encode runtime S3 credential readback")
	}
	readbackDigest := sha256.Sum256(readbackRaw)
	actionTicket := snapshot.ArchiveWorkloadTicket
	if action == "restore" {
		actionTicket = snapshot.RestoreWorkloadTicket
	}
	actionTicketDigest := sha256.Sum256([]byte(actionTicket))
	immutable := true
	destination := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: s3CredentialSecretName(execution.ID, action),
		Labels: map[string]string{
			"runtime.mattercodex.dev/managed": "true", "app.kubernetes.io/component": "runtime-s3-credential",
		},
		Annotations: map[string]string{
			"runtime.mattercodex.dev/runtime-config-name":    configName(execution),
			"runtime.mattercodex.dev/workload-ticket":        actionTicket,
			"runtime.mattercodex.dev/execution-id":           execution.ID,
			"runtime.mattercodex.dev/organization-id":        execution.OrganizationID,
			"runtime.mattercodex.dev/project-id":             execution.ProjectID,
			"runtime.mattercodex.dev/session-id":             execution.SessionID,
			"runtime.mattercodex.dev/source-execution-id":    sourceExecutionID,
			"runtime.mattercodex.dev/action":                 action,
			"runtime.mattercodex.dev/bucket":                 requiredEnv("RUNTIME_S3_BUCKET"),
			"runtime.mattercodex.dev/workload-ticket-sha256": hex.EncodeToString(actionTicketDigest[:]),
			"runtime.mattercodex.dev/sts-session-name":       sessionName,
			"runtime.mattercodex.dev/assumed-role-arn":       issued.AssumedRoleARN,
			"runtime.mattercodex.dev/inline-policy-sha256":   policySHA256,
			"runtime.mattercodex.dev/credential-backend":     string(backend),
			"runtime.mattercodex.dev/readback-sha256":        hex.EncodeToString(readbackDigest[:]),
			"runtime.mattercodex.dev/expires-at":             expiresAt.Format(time.RFC3339),
		},
	}, Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{
		"access-key-id": []byte(accessKeyID), "secret-access-key": []byte(secretAccessKey), "session-token": []byte(sessionToken),
	}}
	if destination.Annotations["runtime.mattercodex.dev/bucket"] == "" {
		return nil, errors.New("runtime S3 bucket is invalid")
	}
	expiresAt, err = time.Parse(time.RFC3339, destination.Annotations["runtime.mattercodex.dev/expires-at"])
	readbackSHA256 := destination.Annotations["runtime.mattercodex.dev/readback-sha256"]
	if err != nil || !expiresAt.After(time.Now().UTC().Add(time.Minute)) ||
		!validSHA256(readbackSHA256) ||
		destination.Annotations["runtime.mattercodex.dev/inline-policy-sha256"] != policySHA256 {
		return nil, errors.New("runtime S3 credential lifetime is invalid")
	}
	created, err := secrets.Create(ctx, destination, metav1.CreateOptions{})
	// Admission one-time receipt отклоняет новый API request UID после потерянного
	// успешного ответа. В этом случае только scoped readback отличает persisted
	// exact Secret от настоящего запрета или mismatch.
	if apierrors.IsAlreadyExists(err) || apierrors.IsForbidden(err) {
		proof, readbackErr := materializeS3CredentialReadback(
			ctx, client, namespace, snapshot, action, sourceExecutionID, policySHA256,
		)
		materialized = readbackErr == nil
		return proof, readbackErr
	}
	if err != nil || !credentialSnapshotSecretMatches(created, destination) || secretDataSHA256(created.Data) != secretDataSHA256(destination.Data) {
		return nil, errors.New("materialize immutable runtime S3 credential snapshot")
	}
	proof, proofErr := s3CredentialProof(execution, action, sourceExecutionID, policySHA256, actionTicket, created)
	materialized = proofErr == nil
	return proof, proofErr
}

func s3CredentialProof(
	execution entity.Execution,
	action, sourceExecutionID, policySHA256, workloadTicket string,
	created *corev1.Secret,
) (map[string]string, error) {
	backend, backendErr := configuredS3CredentialBackend()
	workloadTicketDigest := sha256.Sum256([]byte(workloadTicket))
	if backendErr != nil || created == nil || created.UID == "" || created.ResourceVersion == "" ||
		created.Immutable == nil || !*created.Immutable || created.Type != corev1.SecretTypeOpaque ||
		created.Name != s3CredentialSecretName(execution.ID, action) ||
		created.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
		created.Annotations["runtime.mattercodex.dev/organization-id"] != execution.OrganizationID ||
		created.Annotations["runtime.mattercodex.dev/project-id"] != execution.ProjectID ||
		created.Annotations["runtime.mattercodex.dev/session-id"] != execution.SessionID ||
		created.Annotations["runtime.mattercodex.dev/source-execution-id"] != sourceExecutionID ||
		created.Annotations["runtime.mattercodex.dev/action"] != action ||
		created.Annotations["runtime.mattercodex.dev/credential-backend"] != string(backend) ||
		created.Annotations["runtime.mattercodex.dev/workload-ticket"] != workloadTicket ||
		created.Annotations["runtime.mattercodex.dev/workload-ticket-sha256"] != hex.EncodeToString(workloadTicketDigest[:]) ||
		created.Annotations["runtime.mattercodex.dev/inline-policy-sha256"] != policySHA256 ||
		len(created.Data) != 3 {
		return nil, errors.New("runtime S3 credential readback identity is invalid")
	}
	expiresAt, err := time.Parse(time.RFC3339, created.Annotations["runtime.mattercodex.dev/expires-at"])
	readbackSHA256 := created.Annotations["runtime.mattercodex.dev/readback-sha256"]
	if err != nil || !expiresAt.After(time.Now().UTC().Add(time.Minute)) ||
		!validSHA256(readbackSHA256) {
		return nil, errors.New("runtime S3 credential readback lifetime is invalid")
	}
	contentRaw, err := json.Marshal(struct {
		Name, UID, ResourceVersion, ExecutionID, OrganizationID, ProjectID string
		SessionID, SourceExecutionID, Action, PolicySHA256, ReadbackSHA256 string
		SecretDataSHA256, ExpiresAt                                        string
	}{created.Name, string(created.UID), created.ResourceVersion, execution.ID,
		execution.OrganizationID, execution.ProjectID, execution.SessionID,
		sourceExecutionID, action, policySHA256, readbackSHA256,
		secretDataSHA256(created.Data), expiresAt.Format(time.RFC3339)})
	if err != nil {
		return nil, errors.New("encode runtime S3 credential owner receipt")
	}
	contentDigest := sha256.Sum256(contentRaw)
	return map[string]string{
		"s3_secret_name": created.Name, "s3_secret_uid": string(created.UID),
		"s3_secret_resource_version": created.ResourceVersion,
		"s3_execution_id":            execution.ID, "s3_organization_id": execution.OrganizationID,
		"s3_project_id": execution.ProjectID, "s3_session_id": execution.SessionID,
		"s3_source_execution_id": sourceExecutionID, "s3_action": action,
		"s3_policy_sha256":      policySHA256,
		"s3_readback_sha256":    readbackSHA256,
		"s3_secret_data_sha256": secretDataSHA256(created.Data),
		"s3_expires_at":         expiresAt.Format(time.RFC3339),
		"s3_content_sha256":     hex.EncodeToString(contentDigest[:]),
	}, nil
}

func s3ReadbackName(executionID, action string) string {
	return "runtime-s3-readback-" + stableHash(executionID+":"+action, 20)
}

func s3ReadbackReceiptName(executionID, action string) string {
	return "runtime-s3-readback-receipt-" + stableHash(executionID+":"+action, 20)
}

func runS3CredentialReadback(ctx context.Context) error {
	namespace, executionID, action := requiredEnv("RUNTIME_NAMESPACE"), requiredEnv("RUNTIME_EXECUTION_ID"), requiredEnv("RUNTIME_S3_ACTION")
	if namespace == "" || uuid.Validate(executionID) != nil || (action != "archive" && action != "restore") {
		return errors.New("runtime S3 readback identity is invalid")
	}
	raw, err := os.ReadFile(runtimeConfigFile)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return errors.New("read immutable runtime S3 readback input")
	}
	var snapshot runtimeSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		snapshot.Execution.ID != executionID || snapshot.Execution.Validate() != nil ||
		snapshot.Revision.ValidateFor(snapshot.Execution) != nil || verifyWorkloadTicket(snapshot, "s3-"+action) != nil {
		return errors.New("immutable runtime S3 readback input is invalid")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("load runtime S3 readback Kubernetes identity")
	}
	config.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.New("create runtime S3 readback Kubernetes client")
	}
	sourceExecutionID, policySHA256 := requiredEnv("RUNTIME_S3_SOURCE_EXECUTION_ID"), requiredEnv("RUNTIME_S3_POLICY_SHA256")
	if uuid.Validate(sourceExecutionID) != nil || !validSHA256(policySHA256) {
		return errors.New("runtime S3 readback tuple is invalid")
	}
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, s3CredentialSecretName(executionID, action), metav1.GetOptions{})
	if err != nil {
		return errors.New("read exact runtime S3 credential snapshot")
	}
	ticket := snapshot.ArchiveWorkloadTicket
	if action == "restore" {
		ticket = snapshot.RestoreWorkloadTicket
	}
	proof, err := s3CredentialProof(snapshot.Execution, action, sourceExecutionID, policySHA256, ticket, secret)
	if err != nil {
		return err
	}
	immutable := true
	receipt := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:   s3ReadbackReceiptName(executionID, action),
		Labels: executionLabels(snapshot.Execution, "runtime-s3-credential-readback"),
		Annotations: map[string]string{
			"runtime.mattercodex.dev/execution-id":             executionID,
			"runtime.mattercodex.dev/action":                   action,
			"runtime.mattercodex.dev/readback-service-account": s3ReadbackName(executionID, action),
		},
	}, Immutable: &immutable, Data: proof}
	created, err := client.CoreV1().ConfigMaps(namespace).Create(ctx, receipt, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = client.CoreV1().ConfigMaps(namespace).Get(ctx, receipt.Name, metav1.GetOptions{})
	}
	if err != nil || created.Immutable == nil || !*created.Immutable ||
		created.Data["s3_content_sha256"] != proof["s3_content_sha256"] ||
		created.Data["s3_secret_uid"] != proof["s3_secret_uid"] ||
		created.Data["s3_secret_resource_version"] != proof["s3_secret_resource_version"] {
		return errors.New("commit runtime S3 credential readback receipt")
	}
	return nil
}

func materializeS3CredentialReadback(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	snapshot runtimeSnapshot,
	action, sourceExecutionID, policySHA256 string,
) (map[string]string, error) {
	execution := snapshot.Execution
	name := s3ReadbackName(execution.ID, action)
	labels := executionLabels(execution, "runtime-s3-credential-readback")
	ticket := snapshot.ArchiveWorkloadTicket
	if action == "restore" {
		ticket = snapshot.RestoreWorkloadTicket
	}
	ticketDigest := sha256.Sum256([]byte(ticket))
	resourceAnnotations := map[string]string{
		"runtime.mattercodex.dev/execution-id": execution.ID, "runtime.mattercodex.dev/action": action,
		"runtime.mattercodex.dev/runtime-config-name":  configName(execution),
		"runtime.mattercodex.dev/secret-name":          s3CredentialSecretName(execution.ID, action),
		"runtime.mattercodex.dev/source-execution-id":  sourceExecutionID,
		"runtime.mattercodex.dev/inline-policy-sha256": policySHA256,
	}
	podAnnotations := make(map[string]string, len(resourceAnnotations)+3)
	for key, value := range resourceAnnotations {
		podAnnotations[key] = value
	}
	podAnnotations["runtime.mattercodex.dev/next-input-config"] = configName(execution)
	podAnnotations["runtime.mattercodex.dev/workload-ticket"] = ticket
	podAnnotations["runtime.mattercodex.dev/workload-ticket-sha256"] = hex.EncodeToString(ticketDigest[:])
	keyRaw, err := os.ReadFile(workloadTicketKeyFile)
	if err != nil || len(keyRaw) == 0 || len(keyRaw) > 4096 {
		return nil, errors.New("read runtime S3 workload ticket verification material")
	}
	trustName := "runtime-s3-readback-trust-" + stableHash(execution.ID+":"+action, 20)
	immutable := true
	trust := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: trustName, Labels: labels,
		Annotations: resourceAnnotations}, Immutable: &immutable,
		Data: map[string]string{"public-key.hex": strings.TrimSpace(string(keyRaw))}}
	actualTrust, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, trustName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actualTrust, err = client.CoreV1().ConfigMaps(namespace).Create(ctx, trust, metav1.CreateOptions{})
	}
	if err != nil || actualTrust.Immutable == nil || !*actualTrust.Immutable || actualTrust.Data["public-key.hex"] != trust.Data["public-key.hex"] {
		return nil, errors.New("materialize runtime S3 readback trust")
	}
	account := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels, Annotations: resourceAnnotations}, AutomountServiceAccountToken: boolPointer(false)}
	actualAccount, err := client.CoreV1().ServiceAccounts(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actualAccount, err = client.CoreV1().ServiceAccounts(namespace).Create(ctx, account, metav1.CreateOptions{})
	}
	if err != nil || !reflectStringMapEqual(actualAccount.Annotations, resourceAnnotations) {
		return nil, errors.New("materialize runtime S3 readback identity")
	}
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels, Annotations: resourceAnnotations}, Rules: []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{s3CredentialSecretName(execution.ID, action)}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{configName(execution), trustName, s3ReadbackReceiptName(execution.ID, action)}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"create"}},
	}}
	actualRole, err := client.RbacV1().Roles(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actualRole, err = client.RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{})
	}
	if err != nil || !policyRulesEqual(actualRole.Rules, role.Rules) {
		return nil, errors.New("materialize runtime S3 readback role")
	}
	binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels, Annotations: resourceAnnotations},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: name, Namespace: namespace}},
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}}
	actualBinding, err := client.RbacV1().RoleBindings(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actualBinding, err = client.RbacV1().RoleBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
	}
	if err != nil || len(actualBinding.Subjects) != 1 || actualBinding.Subjects[0] != binding.Subjects[0] || actualBinding.RoleRef != binding.RoleRef {
		return nil, errors.New("materialize runtime S3 readback binding")
	}
	tokenTTL, defaultMode := int64(600), int32(0o440)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels, Annotations: podAnnotations}, Spec: corev1.PodSpec{
		ServiceAccountName: name, AutomountServiceAccountToken: boolPointer(false), RestartPolicy: corev1.RestartPolicyOnFailure,
		EnableServiceLinks: boolPointer(false), SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true), RunAsUser: int64Pointer(10001), RunAsGroup: int64Pointer(10001), FSGroup: int64Pointer(10001)},
		Containers: []corev1.Container{{Name: "readback", Image: requiredEnv("RUNTIME_S3_READBACK_IMAGE"), Command: []string{"/usr/local/bin/runtime-credential-broker"}, Args: []string{"s3-readback"},
			Env: []corev1.EnvVar{{Name: "RUNTIME_NAMESPACE", Value: namespace}, {Name: "RUNTIME_EXECUTION_ID", Value: execution.ID},
				{Name: "RUNTIME_S3_ACTION", Value: action}, {Name: "RUNTIME_S3_SOURCE_EXECUTION_ID", Value: sourceExecutionID}, {Name: "RUNTIME_S3_POLICY_SHA256", Value: policySHA256}},
			SecurityContext: restrictedSecurityContext(), VolumeMounts: []corev1.VolumeMount{{Name: "runtime-config", MountPath: runtimeConfigFile, SubPath: "runtime.json", ReadOnly: true}, {Name: "ticket-trust", MountPath: "/var/run/config/mattercodex/runtime-workload-ticket", ReadOnly: true}, {Name: "kube-api-access", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true}}}},
		Volumes: []corev1.Volume{
			{Name: "runtime-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configName(execution)}, DefaultMode: &defaultMode}}},
			{Name: "ticket-trust", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: trustName}, DefaultMode: &defaultMode}}},
			{Name: "kube-api-access", VolumeSource: projectedKubeAPIVolume(&defaultMode, &tokenTTL)},
		},
	}}
	actualPod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actualPod, err = client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	}
	if err != nil || actualPod.Spec.ServiceAccountName != name || !reflectStringMapEqual(actualPod.Annotations, podAnnotations) {
		return nil, errors.New("materialize runtime S3 readback Pod")
	}
	var proof map[string]string
	err = wait.PollUntilContextTimeout(ctx, 250*time.Millisecond, 30*time.Second, true, func(pollCtx context.Context) (bool, error) {
		receipt, getErr := client.CoreV1().ConfigMaps(namespace).Get(pollCtx, s3ReadbackReceiptName(execution.ID, action), metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			return false, nil
		}
		if getErr != nil || receipt.Immutable == nil || !*receipt.Immutable ||
			receipt.Data["s3_execution_id"] != execution.ID || receipt.Data["s3_action"] != action ||
			receipt.Data["s3_source_execution_id"] != sourceExecutionID || receipt.Data["s3_policy_sha256"] != policySHA256 ||
			!validSHA256(receipt.Data["s3_content_sha256"]) || receipt.Data["s3_secret_uid"] == "" || receipt.Data["s3_secret_resource_version"] == "" {
			return false, errors.New("runtime S3 readback receipt mismatch")
		}
		proof = receipt.Data
		return true, nil
	})
	return proof, err
}

func exactS3Policy(execution entity.Execution, action string) (map[string]any, string, error) {
	backend, err := configuredS3CredentialBackend()
	if err != nil {
		return nil, "", err
	}
	return exactS3PolicyForBackend(execution, action, backend)
}

func exactS3PolicyForBackend(execution entity.Execution, action string, backend s3CredentialBackend) (map[string]any, string, error) {
	dialect := s3policy.DialectAWS
	if backend == s3CredentialBackendInternalMinIO {
		dialect = s3policy.DialectMinIO
	}
	result, err := s3policy.Build(execution, port.Action(action), s3policy.Config{
		Bucket: requiredEnv("RUNTIME_S3_BUCKET"), Region: requiredEnv("RUNTIME_S3_REGION"),
		KMSKeyARN: requiredEnvOrFile("RUNTIME_S3_KMS_KEY_ARN", s3KMSKeyARNFile),
		KMSKeyID:  requiredEnv("RUNTIME_S3_MINIO_KMS_KEY_ID"),
	}, dialect, time.Now())
	if err != nil {
		return nil, "", err
	}
	var policy map[string]any
	if json.Unmarshal(result.Raw, &policy) != nil {
		return nil, "", errors.New("decode exact runtime S3 policy")
	}
	return policy, result.SourceExecutionID, nil
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
	if target == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return err
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
			"runtime.mattercodex.dev/runtime-config-name":      configName(execution),
			"runtime.mattercodex.dev/credential-index":         strconv.Itoa(index),
			"runtime.mattercodex.dev/runtime-revision-id":      execution.RuntimeRevisionID,
			"runtime.mattercodex.dev/runtime-revision-version": strconv.FormatUint(execution.RuntimeRevisionVersion, 10),
			"runtime.mattercodex.dev/credential-resource-id":   credential.ResourceID,
			"runtime.mattercodex.dev/credential-version":       strconv.FormatUint(credential.Version, 10),
			"runtime.mattercodex.dev/provider-content-version": credential.ProviderContentVersion,
			"runtime.mattercodex.dev/content-sha256":           credential.ContentSHA256,
			"runtime.mattercodex.dev/purpose":                  credential.Purpose,
			"runtime.mattercodex.dev/source-secret":            sourceName,
			"runtime.mattercodex.dev/destination-secret":       executionCredentialSecretName(execution.ID, index),
			"runtime.mattercodex.dev/source-secret-uid":        string(source.UID),
			"runtime.mattercodex.dev/source-resource-version":  source.ResourceVersion,
			"runtime.mattercodex.dev/snapshot-sha256":          execution.CredentialSnapshotSHA256,
			"runtime.mattercodex.dev/copy-service-account":     credentialCopyName(execution.ID, index),
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

func validSHA256(value string) bool {
	return len(value) == 64 && value == strings.ToLower(value) &&
		strings.Trim(value, "0123456789abcdef") == ""
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
	return "runtime-access-" + stableHash(execution.OrganizationID+":"+execution.ProjectID+":"+
		execution.SessionID+":"+execution.RoleID+":"+execution.ID, 24)
}

func projectNamespaceName(projectID string) string {
	return "mattercodex-project-" + stableHash(projectID, 20)
}

func podName(execution entity.Execution) string {
	return "runtime-role-" + stableHash(execution.RoleID+":"+execution.ThreadID+":"+execution.SessionID+":"+execution.ID, 24)
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

func int64Pointer(value int64) *int64 { return &value }

func restrictedSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsNonRoot: boolPointer(true), RunAsUser: int64Pointer(10001), RunAsGroup: int64Pointer(10001),
		AllowPrivilegeEscalation: boolPointer(false), ReadOnlyRootFilesystem: boolPointer(true),
		Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

func projectedKubeAPIVolume(defaultMode *int32, tokenTTL *int64) corev1.VolumeSource {
	return corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
		DefaultMode: defaultMode,
		Sources: []corev1.VolumeProjection{
			{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
				Audience: "https://kubernetes.default.svc", ExpirationSeconds: tokenTTL, Path: "token",
			}},
			{ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"},
				Items:                []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
			}},
		},
	}}
}

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
