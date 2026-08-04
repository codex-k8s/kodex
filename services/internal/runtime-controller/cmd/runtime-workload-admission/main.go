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
	"os"
	"os/signal"
	"reflect"
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
	roleImageRepository  = "mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/agent-runtime"
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
	client    kubernetes.Interface
	database  *pgxpool.Pool
	publicKey ed25519.PublicKey
	now       func() time.Time
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
	handler := &admissionHandler{client: client, database: database, publicKey: publicKey, now: time.Now}
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
		request.Resource.Group != "" || request.Resource.Resource != "pods" {
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
		snapshot.Execution.Validate() != nil || snapshot.Revision.ValidateFor(snapshot.Execution) != nil ||
		snapshot.WorkloadTicket != pod.Annotations["runtime.mattercodex.dev/workload-ticket"] {
		return false, "immutable runtime input is invalid"
	}
	payload, err := workloadticket.Verify(snapshot.WorkloadTicket, handler.publicKey, snapshot.Execution, handler.now())
	if err != nil || !exactRuntimePod(request.UserInfo.Username, &pod, snapshot) {
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
	var admitted bool
	err = handler.database.QueryRow(ctx, `
		INSERT INTO runtime_controller.runtime_workload_ticket_admissions AS admission (
			request_uid, ticket_id, pod_sha256, execution_id, expires_at
		) VALUES ($1, decode($2, 'hex'), decode($3, 'hex'), $4, $5)
		ON CONFLICT (ticket_id) DO UPDATE
		SET ticket_id = EXCLUDED.ticket_id
		WHERE admission.request_uid = EXCLUDED.request_uid
			AND admission.pod_sha256 = EXCLUDED.pod_sha256
		RETURNING true
	`, request.UID, payload.TicketID, hex.EncodeToString(specDigest[:]), payload.ExecutionID, payload.ExpiresAt).Scan(&admitted)
	if err != nil || !admitted {
		return false, "runtime workload ticket replay is denied"
	}
	return true, "exact runtime Pod admitted"
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
	expectedBroker, expectedAccount := "", ""
	switch execution.AccessProfile {
	case "NONE":
		expectedBroker = "system:serviceaccount:mattercodex-system:runtime-credential-broker"
		expectedAccount = "runtime-access-" + stableHash(execution.OrganizationID+":"+execution.ProjectID+":"+execution.SessionID+":"+execution.RoleID, 24)
	case "PROJECT_READ_ONLY":
		expectedBroker = "system:serviceaccount:mattercodex-system:runtime-project-read-broker"
		expectedAccount = "runtime-access-" + stableHash(execution.OrganizationID+":"+execution.ProjectID+":"+execution.SessionID+":"+execution.RoleID, 24)
	case "CLUSTER_ADMIN":
		expectedBroker = "system:serviceaccount:mattercodex-system:runtime-cluster-admin-broker"
		expectedAccount = "runtime-role-cluster-admin"
	default:
		return false
	}
	expectedPod := "runtime-role-" + stableHash(execution.RoleID+":"+execution.ThreadID+":"+execution.SessionID, 24)
	expectedImage := roleImageRepository + "@" + revision.ImageDigest
	if username != expectedBroker || pod.Name != expectedPod || pod.Spec.ServiceAccountName != expectedAccount ||
		pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken ||
		pod.Spec.HostNetwork || pod.Spec.HostPID || pod.Spec.HostIPC || pod.Spec.RestartPolicy != corev1.RestartPolicyNever ||
		len(pod.Spec.InitContainers) != 1 || len(pod.Spec.Containers) != 1 ||
		pod.Spec.InitContainers[0].Image != expectedImage || pod.Spec.Containers[0].Image != expectedImage ||
		!stringSliceEqual(pod.Spec.InitContainers[0].Args, []string{"runtime-init-workspace"}) ||
		!stringSliceEqual(pod.Spec.Containers[0].Args, []string{"runtime-session"}) ||
		pod.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
		pod.Annotations["runtime.mattercodex.dev/revision-sha256"] != execution.RuntimeRevisionSHA256 ||
		pod.Annotations["runtime.mattercodex.dev/input-sha256"] != execution.ImmutableInputSHA256 ||
		pod.Annotations["runtime.mattercodex.dev/credential-snapshot-sha256"] != execution.CredentialSnapshotSHA256 {
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

func stableHash(value string, length int) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])[:length]
}

func stringSliceEqual(left, right []string) bool {
	return len(left) == len(right) && strings.Join(left, "\x00") == strings.Join(right, "\x00")
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic("required runtime admission environment is missing: " + name)
	}
	return value
}
