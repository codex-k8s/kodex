package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/security/workloadticket"
	"github.com/jackc/pgx/v5/pgxpool"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

const (
	maximumAdmissionBody = 2 << 20
	admissionNamespace   = "mattercodex-system"
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

type admissionHandler struct {
	client             kubernetes.Interface
	database           *pgxpool.Pool
	publicKey          ed25519.PublicKey
	s3ArchivePublicKey ed25519.PublicKey
	s3RestorePublicKey ed25519.PublicKey
	now                func() time.Time
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
	keyRaw, err := os.ReadFile(requiredEnv("RUNTIME_ADMISSION_PUBLIC_KEY_FILE"))
	if err != nil {
		return errors.New("read runtime admission public key")
	}
	publicKey, err := workloadticket.DecodePublicKey(keyRaw)
	if err != nil {
		return err
	}
	archiveKey, err := readTicketPublicKey(requiredEnv("RUNTIME_ADMISSION_S3_ARCHIVE_PUBLIC_KEY_FILE"))
	if err != nil {
		return err
	}
	restoreKey, err := readTicketPublicKey(requiredEnv("RUNTIME_ADMISSION_S3_RESTORE_PUBLIC_KEY_FILE"))
	if err != nil {
		return err
	}
	dsnRaw, err := os.ReadFile(requiredEnv("RUNTIME_ADMISSION_POSTGRES_DSN_FILE"))
	if err != nil || len(dsnRaw) < 16 || len(dsnRaw) > 16<<10 {
		return errors.New("read runtime admission PostgreSQL credential")
	}
	database, err := pgxpool.New(ctx, strings.TrimSpace(string(dsnRaw)))
	if err != nil {
		return errors.New("open runtime admission PostgreSQL")
	}
	defer database.Close()
	if err := database.Ping(ctx); err != nil {
		return errors.New("check runtime admission PostgreSQL")
	}
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("load runtime admission Kubernetes identity")
	}
	restConfig.Timeout = 3 * time.Second
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return errors.New("open runtime admission Kubernetes client")
	}
	handler := &admissionHandler{
		client: client, database: database, publicKey: publicKey,
		s3ArchivePublicKey: archiveKey, s3RestorePublicKey: restoreKey, now: time.Now,
	}
	mux := http.NewServeMux()
	mux.Handle("/validate-runtime-pod", handler)
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || database.Ping(request.Context()) != nil {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{
		Addr: requiredEnv("RUNTIME_ADMISSION_LISTEN"), Handler: mux,
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 64 << 10,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13},
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServeTLS(
			requiredEnv("RUNTIME_ADMISSION_TLS_CERTIFICATE_FILE"),
			requiredEnv("RUNTIME_ADMISSION_TLS_PRIVATE_KEY_FILE"),
		)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func readTicketPublicKey(path string) (ed25519.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read runtime admission S3 public key")
	}
	return workloadticket.DecodePublicKey(raw)
}

func (handler *admissionHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maximumAdmissionBody))
	if err != nil {
		http.Error(response, "invalid admission body", http.StatusBadRequest)
		return
	}
	var review admissionv1.AdmissionReview
	if json.Unmarshal(body, &review) != nil || review.Request == nil {
		http.Error(response, "invalid admission review", http.StatusBadRequest)
		return
	}
	allowed, message := handler.admit(request.Context(), review.Request)
	review.Response = &admissionv1.AdmissionResponse{
		UID: review.Request.UID, Allowed: allowed,
		Result: &metav1.Status{Message: message},
	}
	review.Request = nil
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(review)
}

func (handler *admissionHandler) admit(ctx context.Context, request *admissionv1.AdmissionRequest) (bool, string) {
	if request.UID == "" || request.Operation != admissionv1.Create || request.Namespace != admissionNamespace ||
		request.Resource.Group != "" {
		return false, "runtime admission request scope is invalid"
	}
	if request.Resource.Resource == "secrets" {
		return handler.admitCredentialSecret(ctx, request)
	}
	if request.Resource.Resource != "pods" {
		return false, "runtime admission request scope is invalid"
	}
	var pod corev1.Pod
	if json.Unmarshal(request.Object.Raw, &pod) != nil || pod.Namespace != admissionNamespace {
		return false, "runtime Pod is invalid"
	}
	configName := pod.Annotations["runtime.mattercodex.dev/next-input-config"]
	configMap, err := handler.client.CoreV1().ConfigMaps(admissionNamespace).Get(ctx, configName, metav1.GetOptions{})
	if err != nil || configMap.Immutable == nil || !*configMap.Immutable || len(configMap.BinaryData["runtime.json"]) == 0 {
		return false, "immutable runtime input is unavailable"
	}
	var snapshot runtimeSnapshot
	decoder := json.NewDecoder(bytes.NewReader(configMap.BinaryData["runtime.json"]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		snapshot.Execution.Validate() != nil || snapshot.Revision.ValidateFor(snapshot.Execution) != nil {
		return false, "immutable runtime input is invalid"
	}
	component := pod.Labels["app.kubernetes.io/component"]
	ticket, publicKey, audience := snapshot.WorkloadTicket, handler.publicKey, "mattercodex-runtime-workload-admission"
	exact := component == "role-runtime" && exactRuntimePod(request.UserInfo.Username, &pod, snapshot)
	if component == "runtime-credential-copy" {
		exact = exactCredentialCopyPod(request.UserInfo.Username, &pod, snapshot)
	}
	if component == "runtime-s3-credential-readback" {
		action := pod.Annotations["runtime.mattercodex.dev/action"]
		ticket, publicKey, audience = snapshot.ArchiveWorkloadTicket, handler.s3ArchivePublicKey, "mattercodex-runtime-s3-archive"
		if action == "restore" {
			ticket, publicKey, audience = snapshot.RestoreWorkloadTicket, handler.s3RestorePublicKey, "mattercodex-runtime-s3-restore"
		}
		exact = exactS3ReadbackPod(request.UserInfo.Username, &pod, snapshot)
	}
	payload, err := workloadticket.VerifyForAudience(ticket, publicKey, snapshot.Execution, audience, handler.now())
	exact = exact && ticket == pod.Annotations["runtime.mattercodex.dev/workload-ticket"]
	if err != nil || !exact {
		return false, "runtime workload ticket or Pod spec is invalid"
	}
	specRaw, err := json.Marshal(struct {
		Name, Namespace     string
		Labels, Annotations map[string]string
		Spec                corev1.PodSpec
	}{pod.Name, pod.Namespace, pod.Labels, pod.Annotations, pod.Spec})
	if err != nil {
		return false, "runtime Pod digest is invalid"
	}
	specDigest := sha256.Sum256(specRaw)
	if !handler.recordAdmission(ctx, request, payload.TicketID, payload.ExecutionID, payload.ExpiresAt,
		"pod/"+pod.Name, hex.EncodeToString(specDigest[:])) {
		return false, "runtime workload ticket replay is denied"
	}
	return true, "exact runtime Pod admitted"
}

func exactS3ReadbackPod(username string, pod *corev1.Pod, snapshot runtimeSnapshot) bool {
	action := pod.Annotations["runtime.mattercodex.dev/action"]
	if action != "archive" && action != "restore" {
		return false
	}
	exchanger := "runtime-s3-" + action + "-exchanger"
	policySHA256 := pod.Annotations["runtime.mattercodex.dev/inline-policy-sha256"]
	if username != "system:serviceaccount:mattercodex-system:"+exchanger || !validSHA256Text(policySHA256) {
		return false
	}
	desired := desiredS3ReadbackPod(snapshot, action, policySHA256)
	clientgoscheme.Scheme.Default(desired)
	return pod.Name == desired.Name && pod.Namespace == desired.Namespace &&
		reflect.DeepEqual(pod.Labels, desired.Labels) && reflect.DeepEqual(pod.Annotations, desired.Annotations) &&
		reflect.DeepEqual(pod.Spec, desired.Spec)
}

func desiredS3ReadbackPod(snapshot runtimeSnapshot, action, policySHA256 string) *corev1.Pod {
	execution := snapshot.Execution
	name := "runtime-s3-readback-" + stableHash(execution.ID+":"+action, 20)
	trustName := "runtime-s3-readback-trust-" + stableHash(execution.ID+":"+action, 20)
	sourceExecutionID, ticket := execution.ID, snapshot.ArchiveWorkloadTicket
	if action == "restore" {
		ticket = snapshot.RestoreWorkloadTicket
		if execution.RestoreSourceExecutionID != "" {
			sourceExecutionID = execution.RestoreSourceExecutionID
		}
	}
	ticketDigest := sha256.Sum256([]byte(ticket))
	labels := map[string]string{
		"runtime.mattercodex.dev/managed": "true", "app.kubernetes.io/name": "runtime-controller",
		"app.kubernetes.io/component":            "runtime-s3-credential-readback",
		"runtime.mattercodex.dev/execution":      strings.ReplaceAll(execution.ID, "-", "")[:20],
		"runtime.mattercodex.dev/session":        strings.ReplaceAll(execution.SessionID, "-", "")[:20],
		"runtime.mattercodex.dev/role":           strings.ReplaceAll(execution.RoleID, "-", "")[:20],
		"runtime.mattercodex.dev/access-profile": strings.ToLower(string(execution.AccessProfile)),
	}
	annotations := map[string]string{
		"runtime.mattercodex.dev/execution-id": execution.ID, "runtime.mattercodex.dev/action": action,
		"runtime.mattercodex.dev/runtime-config-name":    "runtime-config-" + stableHash(execution.ID, 24),
		"runtime.mattercodex.dev/secret-name":            "runtime-s3-" + stableHash(execution.ID, 20) + "-" + action,
		"runtime.mattercodex.dev/source-execution-id":    sourceExecutionID,
		"runtime.mattercodex.dev/inline-policy-sha256":   policySHA256,
		"runtime.mattercodex.dev/next-input-config":      "runtime-config-" + stableHash(execution.ID, 24),
		"runtime.mattercodex.dev/workload-ticket":        ticket,
		"runtime.mattercodex.dev/workload-ticket-sha256": hex.EncodeToString(ticketDigest[:]),
	}
	defaultMode, tokenTTL := int32(0o440), int64(600)
	runAsUser, runAsGroup := int64(10001), int64(10001)
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: admissionNamespace, Labels: labels, Annotations: annotations}, Spec: corev1.PodSpec{
		ServiceAccountName: name, AutomountServiceAccountToken: boolPointer(false), RestartPolicy: corev1.RestartPolicyOnFailure,
		EnableServiceLinks: boolPointer(false), SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true), RunAsUser: &runAsUser, RunAsGroup: &runAsGroup, FSGroup: &runAsGroup},
		Containers: []corev1.Container{{Name: "readback", Image: requiredEnv("RUNTIME_S3_READBACK_IMAGE"),
			Command: []string{"/usr/local/bin/runtime-credential-broker"}, Args: []string{"s3-readback"},
			Env: []corev1.EnvVar{{Name: "RUNTIME_NAMESPACE", Value: admissionNamespace}, {Name: "RUNTIME_EXECUTION_ID", Value: execution.ID},
				{Name: "RUNTIME_S3_ACTION", Value: action}, {Name: "RUNTIME_S3_SOURCE_EXECUTION_ID", Value: sourceExecutionID}, {Name: "RUNTIME_S3_POLICY_SHA256", Value: policySHA256}},
			SecurityContext: &corev1.SecurityContext{RunAsNonRoot: boolPointer(true), RunAsUser: &runAsUser, RunAsGroup: &runAsGroup,
				AllowPrivilegeEscalation: boolPointer(false), ReadOnlyRootFilesystem: boolPointer(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
			VolumeMounts: []corev1.VolumeMount{{Name: "runtime-config", MountPath: "/var/run/config/mattercodex/runtime/runtime.json", SubPath: "runtime.json", ReadOnly: true},
				{Name: "ticket-trust", MountPath: "/var/run/config/mattercodex/runtime-workload-ticket", ReadOnly: true},
				{Name: "kube-api-access", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true}}}},
		Volumes: []corev1.Volume{
			{Name: "runtime-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "runtime-config-" + stableHash(execution.ID, 24)}, DefaultMode: &defaultMode}}},
			{Name: "ticket-trust", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: trustName}, DefaultMode: &defaultMode}}},
			{Name: "kube-api-access", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{DefaultMode: &defaultMode, Sources: []corev1.VolumeProjection{
				{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Audience: "https://kubernetes.default.svc", ExpirationSeconds: &tokenTTL, Path: "token"}},
				{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}, Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}},
			}}}},
		},
	}}
}

func validSHA256Text(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (handler *admissionHandler) admitCredentialSecret(
	ctx context.Context,
	request *admissionv1.AdmissionRequest,
) (bool, string) {
	var secret corev1.Secret
	if json.Unmarshal(request.Object.Raw, &secret) != nil || secret.Namespace != admissionNamespace {
		return false, "runtime credential snapshot is invalid"
	}
	if secret.Labels["app.kubernetes.io/component"] == "runtime-s3-credential" {
		return handler.admitS3CredentialSecret(ctx, request, &secret)
	}
	if secret.Labels["app.kubernetes.io/component"] != "runtime-credential-snapshot" {
		return false, "runtime credential snapshot is invalid"
	}
	configName := secret.Annotations["runtime.mattercodex.dev/runtime-config-name"]
	configMap, err := handler.client.CoreV1().ConfigMaps(admissionNamespace).Get(ctx, configName, metav1.GetOptions{})
	if err != nil || configMap.Immutable == nil || !*configMap.Immutable || len(configMap.BinaryData["runtime.json"]) == 0 {
		return false, "immutable runtime input is unavailable"
	}
	var snapshot runtimeSnapshot
	decoder := json.NewDecoder(bytes.NewReader(configMap.BinaryData["runtime.json"]))
	decoder.DisallowUnknownFields()
	index, indexErr := strconv.Atoi(secret.Annotations["runtime.mattercodex.dev/credential-index"])
	if decoder.Decode(&snapshot) != nil || decoder.Decode(&struct{}{}) != io.EOF || indexErr != nil ||
		snapshot.Execution.Validate() != nil || snapshot.Revision.ValidateFor(snapshot.Execution) != nil ||
		index < 0 || index >= len(snapshot.Revision.Credentials) {
		return false, "immutable runtime input is invalid"
	}
	payload, err := workloadticket.Verify(snapshot.WorkloadTicket, handler.publicKey, snapshot.Execution, handler.now())
	credential := snapshot.Revision.Credentials[index]
	expectedCopy := "runtime-credential-copy-" + stableHash(snapshot.Execution.ID+":"+strconv.Itoa(index), 20)
	expectedSecret := "runtime-credential-" + strings.ReplaceAll(snapshot.Execution.ID, "-", "")[:20] + "-" + strconv.Itoa(index)
	if err != nil || request.UserInfo.Username != "system:serviceaccount:mattercodex-system:"+expectedCopy ||
		secret.Name != expectedSecret || secret.Immutable == nil || !*secret.Immutable ||
		secret.Annotations["runtime.mattercodex.dev/copy-service-account"] != expectedCopy ||
		secret.Annotations["runtime.mattercodex.dev/execution-id"] != snapshot.Execution.ID ||
		secret.Annotations["runtime.mattercodex.dev/runtime-revision-id"] != snapshot.Execution.RuntimeRevisionID ||
		secret.Annotations["runtime.mattercodex.dev/provider-content-version"] != credential.ProviderContentVersion ||
		secret.Annotations["runtime.mattercodex.dev/content-sha256"] != credential.ContentSHA256 ||
		secretDataSHA256(secret.Data) != credential.ContentSHA256 {
		return false, "runtime credential snapshot authority mismatch"
	}
	raw, err := json.Marshal(secret)
	if err != nil {
		return false, "runtime credential snapshot digest is invalid"
	}
	digest := sha256.Sum256(raw)
	if !handler.recordAdmission(ctx, request, payload.TicketID, payload.ExecutionID,
		payload.ExpiresAt, "secret/"+secret.Name, hex.EncodeToString(digest[:])) {
		return false, "runtime workload ticket replay is denied"
	}
	return true, "exact runtime credential snapshot admitted"
}

func (handler *admissionHandler) admitS3CredentialSecret(
	ctx context.Context,
	request *admissionv1.AdmissionRequest,
	secret *corev1.Secret,
) (bool, string) {
	configName := secret.Annotations["runtime.mattercodex.dev/runtime-config-name"]
	configMap, err := handler.client.CoreV1().ConfigMaps(admissionNamespace).Get(ctx, configName, metav1.GetOptions{})
	if err != nil || configMap.Immutable == nil || !*configMap.Immutable || len(configMap.BinaryData["runtime.json"]) == 0 {
		return false, "immutable runtime input is unavailable"
	}
	var snapshot runtimeSnapshot
	decoder := json.NewDecoder(bytes.NewReader(configMap.BinaryData["runtime.json"]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		snapshot.Execution.Validate() != nil || snapshot.Revision.ValidateFor(snapshot.Execution) != nil {
		return false, "immutable runtime input is invalid"
	}
	execution, action := snapshot.Execution, secret.Annotations["runtime.mattercodex.dev/action"]
	ticket, publicKey, audience := snapshot.ArchiveWorkloadTicket, handler.s3ArchivePublicKey, "mattercodex-runtime-s3-archive"
	if action == "restore" {
		ticket, publicKey, audience = snapshot.RestoreWorkloadTicket, handler.s3RestorePublicKey, "mattercodex-runtime-s3-restore"
	} else if action != "archive" {
		return false, "runtime S3 credential action is invalid"
	}
	payload, err := workloadticket.VerifyForAudience(ticket, publicKey, execution, audience, handler.now())
	sourceExecutionID := execution.ID
	if action == "restore" && execution.RestoreSourceExecutionID != "" {
		sourceExecutionID = execution.RestoreSourceExecutionID
	}
	ticketDigest := sha256.Sum256([]byte(ticket))
	expiresAt, expiryErr := time.Parse(time.RFC3339, secret.Annotations["runtime.mattercodex.dev/expires-at"])
	expectedActor := "system:serviceaccount:mattercodex-system:runtime-s3-" + action + "-exchanger"
	if err != nil || expiryErr != nil || !expiresAt.After(handler.now().UTC().Add(time.Minute)) ||
		expiresAt.After(handler.now().UTC().Add(16*time.Minute)) || request.UserInfo.Username != expectedActor ||
		secret.Name != "runtime-s3-"+stableHash(execution.ID, 20)+"-"+action ||
		secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque || len(secret.Data) != 3 ||
		len(secret.StringData) != 0 || len(secret.OwnerReferences) != 0 || len(secret.Finalizers) != 0 ||
		len(secret.Labels) != 2 || secret.Labels["runtime.mattercodex.dev/managed"] != "true" ||
		secret.Labels["app.kubernetes.io/component"] != "runtime-s3-credential" ||
		secret.Annotations["runtime.mattercodex.dev/runtime-config-name"] != "runtime-config-"+stableHash(execution.ID, 24) ||
		secret.Annotations["runtime.mattercodex.dev/workload-ticket"] != ticket ||
		secret.Annotations["runtime.mattercodex.dev/workload-ticket-sha256"] != hex.EncodeToString(ticketDigest[:]) ||
		secret.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
		secret.Annotations["runtime.mattercodex.dev/organization-id"] != execution.OrganizationID ||
		secret.Annotations["runtime.mattercodex.dev/project-id"] != execution.ProjectID ||
		secret.Annotations["runtime.mattercodex.dev/session-id"] != execution.SessionID ||
		secret.Annotations["runtime.mattercodex.dev/source-execution-id"] != sourceExecutionID ||
		strings.TrimSpace(secret.Annotations["runtime.mattercodex.dev/bucket"]) == "" ||
		secret.Annotations["runtime.mattercodex.dev/sts-session-name"] != "mcx-"+strings.ReplaceAll(execution.ID, "-", "")[:20]+"-"+action ||
		!strings.HasPrefix(secret.Annotations["runtime.mattercodex.dev/assumed-role-arn"], "arn:") ||
		!validSHA256Text(secret.Annotations["runtime.mattercodex.dev/inline-policy-sha256"]) ||
		!validSHA256Text(secret.Annotations["runtime.mattercodex.dev/readback-sha256"]) ||
		len(secret.Data["access-key-id"]) == 0 || len(secret.Data["secret-access-key"]) == 0 || len(secret.Data["session-token"]) == 0 {
		return false, "runtime S3 credential authority mismatch"
	}
	raw, err := json.Marshal(secret)
	if err != nil {
		return false, "runtime S3 credential digest is invalid"
	}
	digest := sha256.Sum256(raw)
	if !handler.recordAdmission(ctx, request, payload.TicketID, payload.ExecutionID, payload.ExpiresAt,
		"secret/"+secret.Name, hex.EncodeToString(digest[:])) {
		return false, "runtime workload ticket replay is denied"
	}
	return true, "exact runtime S3 credential admitted"
}

func (handler *admissionHandler) recordAdmission(
	ctx context.Context,
	request *admissionv1.AdmissionRequest,
	ticketID, executionID string,
	expiresAt time.Time,
	resourceKey, resourceSHA256 string,
) bool {
	var admitted bool
	err := handler.database.QueryRow(ctx, `
		INSERT INTO runtime_controller.runtime_workload_ticket_admissions AS admission (
			request_uid, ticket_id, pod_sha256, execution_id, expires_at, resource_key
		) VALUES ($1, decode($2, 'hex'), decode($3, 'hex'), $4, $5, $6)
		ON CONFLICT (ticket_id, resource_key) DO UPDATE
		SET ticket_id = EXCLUDED.ticket_id
		WHERE admission.request_uid = EXCLUDED.request_uid
			AND admission.pod_sha256 = EXCLUDED.pod_sha256
		RETURNING true
	`, request.UID, ticketID, resourceSHA256, executionID, expiresAt, resourceKey).Scan(&admitted)
	return err == nil && admitted
}

func exactCredentialCopyPod(username string, pod *corev1.Pod, snapshot runtimeSnapshot) bool {
	execution := snapshot.Execution
	index, err := strconv.Atoi(pod.Annotations["runtime.mattercodex.dev/credential-index"])
	if err != nil || index < 0 || index >= len(snapshot.Revision.Credentials) {
		return false
	}
	credential := snapshot.Revision.Credentials[index]
	parsed, err := url.Parse(credential.Reference)
	if err != nil || parsed.Scheme != "k8s-immutable-secret" || parsed.Host != admissionNamespace ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return false
	}
	name := "runtime-credential-copy-" + stableHash(execution.ID+":"+strconv.Itoa(index), 20)
	trustName := "runtime-ticket-trust-" + stableHash(execution.ID, 20)
	if username != "system:serviceaccount:mattercodex-system:runtime-workload-materializer" ||
		pod.Name != name || pod.Spec.ServiceAccountName != name ||
		pod.Annotations["runtime.mattercodex.dev/runtime-config-name"] != "runtime-config-"+stableHash(execution.ID, 24) ||
		pod.Annotations["runtime.mattercodex.dev/workload-ticket-sha256"] != execution.WorkloadTicketSHA256 ||
		pod.Annotations["runtime.mattercodex.dev/source-secret"] != strings.TrimPrefix(parsed.Path, "/") ||
		pod.Annotations["runtime.mattercodex.dev/destination-secret"] !=
			"runtime-credential-"+strings.ReplaceAll(execution.ID, "-", "")[:20]+"-"+strconv.Itoa(index) ||
		pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken ||
		pod.Spec.RestartPolicy != corev1.RestartPolicyOnFailure || len(pod.Spec.InitContainers) != 0 ||
		len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Image != requiredEnv("RUNTIME_CREDENTIAL_COPY_IMAGE") ||
		!stringSliceEqual(pod.Spec.Containers[0].Command, []string{"/usr/local/bin/runtime-credential-broker"}) ||
		!stringSliceEqual(pod.Spec.Containers[0].Args, []string{"copy-credential"}) ||
		!exactCredentialCopyEnv(pod.Spec.Containers[0].Env, execution.ID, index) ||
		!restrictedCredentialCopyContainer(pod.Spec.Containers[0].SecurityContext) || len(pod.Spec.Volumes) != 3 {
		return false
	}
	configOK, trustOK, tokenOK := false, false, false
	for _, volume := range pod.Spec.Volumes {
		switch volume.Name {
		case "runtime-config":
			configOK = volume.ConfigMap != nil && volume.ConfigMap.Name == "runtime-config-"+stableHash(execution.ID, 24)
		case "ticket-trust":
			trustOK = volume.ConfigMap != nil && volume.ConfigMap.Name == trustName
		case "kube-api-access":
			if volume.Projected != nil {
				for _, source := range volume.Projected.Sources {
					if source.ServiceAccountToken != nil && source.ServiceAccountToken.Audience == "https://kubernetes.default.svc" &&
						source.ServiceAccountToken.ExpirationSeconds != nil && *source.ServiceAccountToken.ExpirationSeconds <= 600 {
						tokenOK = true
					}
				}
			}
		}
	}
	return configOK && trustOK && tokenOK
}

func exactCredentialCopyEnv(values []corev1.EnvVar, executionID string, index int) bool {
	return len(values) == 3 && values[0].Name == "RUNTIME_NAMESPACE" && values[0].Value == admissionNamespace &&
		values[1].Name == "RUNTIME_EXECUTION_ID" && values[1].Value == executionID &&
		values[2].Name == "RUNTIME_CREDENTIAL_INDEX" && values[2].Value == strconv.Itoa(index)
}

func restrictedCredentialCopyContainer(security *corev1.SecurityContext) bool {
	return security != nil && security.RunAsNonRoot != nil && *security.RunAsNonRoot &&
		security.AllowPrivilegeEscalation != nil && !*security.AllowPrivilegeEscalation &&
		security.ReadOnlyRootFilesystem != nil && *security.ReadOnlyRootFilesystem &&
		security.Capabilities != nil && len(security.Capabilities.Drop) == 1 && security.Capabilities.Drop[0] == "ALL"
}

func exactRuntimePod(username string, pod *corev1.Pod, snapshot runtimeSnapshot) bool {
	execution, revision := snapshot.Execution, snapshot.Revision
	if snapshot.DesiredPod == nil {
		return false
	}
	desired := snapshot.DesiredPod.DeepCopy()
	desired.Namespace = admissionNamespace
	clientgoscheme.Scheme.Default(desired)
	if pod.Name != desired.Name || pod.Namespace != desired.Namespace ||
		!reflect.DeepEqual(pod.Labels, desired.Labels) || !reflect.DeepEqual(pod.Annotations, desired.Annotations) ||
		!reflect.DeepEqual(pod.Spec, desired.Spec) {
		return false
	}
	expectedBroker := "system:serviceaccount:mattercodex-system:runtime-workload-materializer"
	if execution.AccessProfile != "NONE" {
		return false
	}
	expectedAccount := "runtime-access-" + stableHash(execution.OrganizationID+":"+execution.ProjectID+":"+
		execution.SessionID+":"+execution.RoleID+":"+execution.ID, 24)
	expectedPod := "runtime-role-" + stableHash(execution.RoleID+":"+execution.ThreadID+":"+execution.SessionID+":"+execution.ID, 24)
	expectedImage := revision.ImageReference
	if username != expectedBroker || pod.Name != expectedPod || pod.Spec.ServiceAccountName != expectedAccount ||
		pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken ||
		pod.Spec.HostNetwork || pod.Spec.HostPID || pod.Spec.HostIPC || pod.Spec.RestartPolicy != corev1.RestartPolicyNever ||
		len(pod.Spec.InitContainers) != 2 || len(pod.Spec.Containers) != 3 ||
		pod.Spec.InitContainers[0].Name != "internal-rpc-authority-socket-init" ||
		pod.Spec.InitContainers[1].Name != "workspace-init" ||
		pod.Spec.InitContainers[1].Image != expectedImage ||
		!stringSliceEqual(pod.Spec.InitContainers[1].Args, []string{"runtime-init-workspace"}) ||
		pod.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
		pod.Annotations["runtime.mattercodex.dev/revision-sha256"] != execution.RuntimeRevisionSHA256 ||
		pod.Annotations["runtime.mattercodex.dev/input-sha256"] != execution.ImmutableInputSHA256 ||
		pod.Annotations["runtime.mattercodex.dev/credential-snapshot-sha256"] != execution.CredentialSnapshotSHA256 {
		return false
	}
	containers := make(map[string]corev1.Container, len(pod.Spec.Containers))
	for _, container := range pod.Spec.Containers {
		if _, duplicate := containers[container.Name]; duplicate {
			return false
		}
		containers[container.Name] = container
	}
	role, roleOK := containers["role-runtime"]
	provider, providerOK := containers["provider-runtime"]
	issuer, issuerOK := containers["internal-rpc-authority-issuer"]
	if !roleOK || !providerOK || !issuerOK || role.Image != expectedImage || provider.Image != expectedImage ||
		!stringSliceEqual(role.Args, []string{"runtime-session"}) ||
		!stringSliceEqual(provider.Args, []string{"runtime-provider"}) ||
		!stringSliceEqual(issuer.Command, []string{"/usr/local/bin/internal-rpc-authority-issuer"}) ||
		!exactRunAsUser(role.SecurityContext, 10001) || !exactRunAsUser(provider.SecurityContext, 10002) ||
		!exactRunAsUser(issuer.SecurityContext, 29001) ||
		!exactVolumeMountNames(provider.VolumeMounts, "provider-socket", "provider-tmp", "session") ||
		containerHasMount(provider, "kube-api-access") || containerHasAuthorityMount(provider) {
		return false
	}
	for _, container := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
		if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil ||
			*container.SecurityContext.AllowPrivilegeEscalation || container.SecurityContext.RunAsNonRoot == nil ||
			!*container.SecurityContext.RunAsNonRoot || container.SecurityContext.ReadOnlyRootFilesystem == nil ||
			!*container.SecurityContext.ReadOnlyRootFilesystem {
			return false
		}
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.HostPath != nil || volume.NFS != nil {
			return false
		}
	}
	return true
}

func exactRunAsUser(security *corev1.SecurityContext, expected int64) bool {
	return security != nil && security.RunAsUser != nil && *security.RunAsUser == expected
}

func exactVolumeMountNames(mounts []corev1.VolumeMount, expected ...string) bool {
	if len(mounts) != len(expected) {
		return false
	}
	names := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		names = append(names, mount.Name)
	}
	sort.Strings(names)
	sort.Strings(expected)
	return strings.Join(names, "\x00") == strings.Join(expected, "\x00")
}

func containerHasMount(container corev1.Container, name string) bool {
	for _, mount := range container.VolumeMounts {
		if mount.Name == name {
			return true
		}
	}
	return false
}

func containerHasAuthorityMount(container corev1.Container) bool {
	for _, mount := range container.VolumeMounts {
		if strings.HasPrefix(mount.Name, "authority-") || mount.Name == "handoff-key" ||
			mount.Name == "runtime-controller-tls" || mount.Name == "mcp-upstream" {
			return true
		}
	}
	return false
}

func stableHash(value string, length int) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])[:length]
}

func boolPointer(value bool) *bool { return &value }

func stringSliceEqual(left, right []string) bool {
	return len(left) == len(right) && strings.Join(left, "\x00") == strings.Join(right, "\x00")
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

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic("required runtime admission environment is missing: " + name)
	}
	return value
}
