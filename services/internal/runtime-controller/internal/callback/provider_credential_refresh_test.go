package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type providerCredentialRefreshClient struct {
	controlplanev1.RuntimeWorkServiceClient
	requests []*controlplanev1.CommitProviderCredentialRefreshRequest
}

func (client *providerCredentialRefreshClient) CommitProviderCredentialRefresh(_ context.Context, request *controlplanev1.CommitProviderCredentialRefreshRequest, _ ...grpc.CallOption) (*controlplanev1.CommitProviderCredentialRefreshResponse, error) {
	client.requests = append(client.requests, request)
	return &controlplanev1.CommitProviderCredentialRefreshResponse{ProviderCredential: &controlplanev1.ProviderCredentialBinding{
		AccountRef: "pacc_abcdefgh", CredentialRevisionRef: "pcr_refreshed1", CredentialRevision: 2,
		SecretName: request.GetSecretName(), SecretUid: request.GetSecretUid(),
		SecretResourceVersion: request.GetSecretResourceVersion(), ContentSha256: request.GetContentSha256(),
	}}, nil
}

func TestProviderCredentialRefreshRouteCommitsOnlyMaterializedMetadata(t *testing.T) {
	manager, client, input, payload, ticket := providerCredentialRefreshRouteFixture(t)
	runtimeClient := &providerCredentialRefreshClient{}
	server := &Server{
		config:  Config{RequestTimeout: time.Second},
		manager: manager,
		control: &controlplaneclient.Client{Runtime: runtimeClient},
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal(payload) error = %v", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/provider-credential-refresh", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+ticket)
		bindTestExecutionHeaders(request, input, "provider-credential-refresh")
		response := httptest.NewRecorder()
		server.route(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("route attempt %d status = %d body=%q", attempt+1, response.Code, response.Body.String())
		}
	}
	if len(runtimeClient.requests) != 2 {
		t.Fatalf("control-plane commit count = %d, want 2", len(runtimeClient.requests))
	}
	first, second := runtimeClient.requests[0], runtimeClient.requests[1]
	if first.GetMutation().GetIdempotencyKey() == "" || first.GetMutation().GetIdempotencyKey() != second.GetMutation().GetIdempotencyKey() ||
		first.GetLeaseRef() != input.LeaseRef || first.GetFence() != input.LeaseFence || first.GetGeneration() != input.LeaseGeneration ||
		first.GetPreviousCredentialRevisionRef() != input.ProviderCredentialRef || first.GetPreviousContentSha256() != input.ProviderCredentialSHA256 ||
		first.GetSecretName() == "" || first.GetSecretUid() == "" || first.GetSecretResourceVersion() == "" ||
		first.GetContentSha256() == "" || first.GetContentSha256() == input.ProviderCredentialSHA256 {
		t.Fatalf("metadata-only refresh projection is invalid: %#v", first)
	}
	secret, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), first.GetSecretName(), metav1.GetOptions{})
	if err != nil || !bytes.Equal(secret.Data["auth.json"], payload.Authentication) {
		t.Fatalf("refreshed Secret was not read back exactly: err=%v", err)
	}

	unauthorized := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/provider-credential-refresh", bytes.NewReader(body))
	unauthorized.Header.Set("Authorization", "Bearer "+strings.Repeat("0", 64))
	unauthorizedResponse := httptest.NewRecorder()
	server.route(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusNotFound || len(runtimeClient.requests) != 2 {
		t.Fatalf("invalid execution ticket was not rejected: status=%d commits=%d", unauthorizedResponse.Code, len(runtimeClient.requests))
	}
}

func TestProviderCredentialRefreshRouteRejectsRevisionMismatchBeforeMaterialization(t *testing.T) {
	manager, _, input, payload, ticket := providerCredentialRefreshRouteFixture(t)
	runtimeClient := &providerCredentialRefreshClient{}
	server := &Server{config: Config{RequestTimeout: time.Second}, manager: manager,
		control: &controlplaneclient.Client{Runtime: runtimeClient}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	payload.RuntimeRevisionDigest = strings.Repeat("f", 64)
	body, _ := json.Marshal(payload)
	request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/provider-credential-refresh", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+ticket)
	bindTestExecutionHeaders(request, input, "provider-credential-refresh")
	response := httptest.NewRecorder()
	server.route(response, request)
	if response.Code != http.StatusBadRequest || len(runtimeClient.requests) != 0 {
		t.Fatalf("revision mismatch status=%d commits=%d", response.Code, len(runtimeClient.requests))
	}
}

func bindTestExecutionHeaders(request *http.Request, input runtimecontract.RunnerInput, method string) {
	request.Header.Set("X-Kodex-Organization-Ref", input.OrganizationRef)
	request.Header.Set("X-Kodex-Project-Ref", input.ProjectRef)
	request.Header.Set("X-Kodex-Run-Ref", input.RunRef)
	request.Header.Set("X-Kodex-Node-Ref", input.NodeRef)
	request.Header.Set("X-Kodex-Session-Ref", input.SessionRef)
	request.Header.Set("X-Kodex-Turn-Ref", input.TurnRef)
	request.Header.Set("X-Kodex-Attempt", strconv.FormatInt(int64(input.Attempt), 10))
	request.Header.Set("X-Kodex-Runtime-Revision-Digest", input.RuntimeRevisionDigest)
	request.Header.Set("X-Kodex-Input-Digest", input.InputDigest)
	request.Header.Set("X-Kodex-Execution-Binding-Digest", input.ExecutionBindingDigest)
	request.Header.Set("X-Kodex-MCP-Binding-Digest", input.MCPBindingDigest)
	request.Header.Set("X-Kodex-Callback-Method", method)
}

func TestExecutionHeadersRejectCrossBoundaryAndStaleProjection(t *testing.T) {
	input := validWarmExecutionInput()
	request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/progress", nil)
	bindTestExecutionHeaders(request, input, "progress")
	if !executionHeadersMatch(request, input) {
		t.Fatal("exact execution headers were rejected")
	}
	for name, header := range map[string]string{
		"organization": "X-Kodex-Organization-Ref",
		"project":      "X-Kodex-Project-Ref",
		"session":      "X-Kodex-Session-Ref",
		"turn":         "X-Kodex-Turn-Ref",
		"attempt":      "X-Kodex-Attempt",
		"input digest": "X-Kodex-Input-Digest",
		"execution":    "X-Kodex-Execution-Binding-Digest",
		"MCP":          "X-Kodex-MCP-Binding-Digest",
		"method":       "X-Kodex-Callback-Method",
	} {
		t.Run(name, func(t *testing.T) {
			candidate := request.Clone(request.Context())
			candidate.Header = request.Header.Clone()
			candidate.Header.Set(header, "mismatch")
			if executionHeadersMatch(candidate, input) {
				t.Fatalf("mismatched header %q was accepted", header)
			}
		})
	}
}

func TestProviderCredentialRefreshReadbackRequiresExactNewRevision(t *testing.T) {
	input := validWarmExecutionInput()
	materialized := workload.ProviderSecretBinding{Name: "runtime-provider-refresh-123", UID: "secret-uid", ResourceVersion: "21", ContentSHA256: strings.Repeat("e", 64)}
	valid := &controlplanev1.ProviderCredentialBinding{
		AccountRef: input.ProviderAccountRef, CredentialRevisionRef: "pcr_refreshed1",
		CredentialRevision: input.ProviderCredentialRevision + 1,
		SecretName:         materialized.Name, SecretUid: materialized.UID,
		SecretResourceVersion: materialized.ResourceVersion, ContentSha256: materialized.ContentSHA256,
	}
	if !providerCredentialRefreshReadbackMatches(input, materialized, valid) {
		t.Fatal("exact provider credential refresh readback was rejected")
	}
	for index, mutate := range []func(*controlplanev1.ProviderCredentialBinding){
		func(value *controlplanev1.ProviderCredentialBinding) { value.AccountRef = "pacc_other123" },
		func(value *controlplanev1.ProviderCredentialBinding) {
			value.CredentialRevisionRef = input.ProviderCredentialRef
		},
		func(value *controlplanev1.ProviderCredentialBinding) { value.CredentialRevision++ },
		func(value *controlplanev1.ProviderCredentialBinding) { value.SecretResourceVersion = "22" },
		func(value *controlplanev1.ProviderCredentialBinding) { value.ContentSha256 = strings.Repeat("f", 64) },
	} {
		candidate := proto.Clone(valid).(*controlplanev1.ProviderCredentialBinding)
		mutate(candidate)
		if providerCredentialRefreshReadbackMatches(input, materialized, candidate) {
			t.Fatalf("mismatched readback case %d was accepted", index)
		}
	}
}

func providerCredentialRefreshRouteFixture(t *testing.T, configure ...func(*runtimecontract.RunnerInput)) (*workload.Manager, *fake.Clientset, runtimecontract.RunnerInput, runtimecontract.RunnerProviderCredentialRefreshRequest, string) {
	t.Helper()
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		created := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret)
		if strings.HasPrefix(created.Name, "runtime-provider-refresh-") {
			created.UID = "40000000-0000-4000-8000-000000000001"
			created.ResourceVersion = "21"
		}
		return false, nil, nil
	})
	const imageDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const contractDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	manager, err := workload.New(client, workload.Config{
		Environment: "test", ControlNamespace: "kodex-system", RuntimeNamespace: "kodex-runtime",
		ControllerPodUID: "controller-pod-uid", ControllerPodIP: "10.0.0.10",
		CallbackTLSServerName:  "runtime-controller-callback.kodex-system.svc.cluster.local",
		CallbackClientCASecret: "runtime-execution-client-tls", CallbackClientTLSSecret: "runtime-execution-client-tls",
		ProviderHTTPSProxy: "http://egress-gateway.kodex-system.svc:8080", KubernetesAPIServiceIP: "10.43.0.1",
		SessionPVCSize: "20Gi", RunnerServiceAccount: "agent-runner", PromotedRoleImageRepository: "registry.example/runner",
		DefaultRoleImageReference:   "registry.example/default@" + imageDigest,
		RoleRuntimeContractRevision: 1, RoleRuntimeContractSHA256: contractDigest,
	})
	if err != nil {
		t.Fatalf("workload.New() error = %v", err)
	}
	oldAuthentication := []byte(`{"auth_mode":"chatgpt","tokens":{"account_id":"account-1","access_token":"access-old","refresh_token":"refresh-old"}}`)
	oldDigest := sha256.Sum256(oldAuthentication)
	oldDigestHex := hex.EncodeToString(oldDigest[:])
	immutable := true
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "runtime-provider-openai-default-r1", Namespace: "kodex-runtime",
		UID: "10000000-0000-4000-8000-000000000001", ResourceVersion: "1"}, Immutable: &immutable,
		Data: map[string][]byte{"auth.json": oldAuthentication, "auth.sha256": []byte(oldDigestHex)}}
	if _, err := client.CoreV1().Secrets("kodex-runtime").Create(context.Background(), source, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create source Secret: %v", err)
	}
	input := validWarmExecutionInput()
	input.ImageReference = "registry.example/runner@" + imageDigest
	input.ImageManifestDigest = imageDigest
	input.EnvironmentImage = runtimecontract.RuntimeEnvironmentImage{Reference: input.ImageReference, Digest: imageDigest}
	input.RoleRuntimeContractSHA256 = contractDigest
	input.ProviderCredentialSHA256 = oldDigestHex
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	input.EnvironmentPolicy = policy
	access, err := runtimecontract.RuntimeKubernetesAccessForExecution(policy.KubernetesAccess,
		runtimecontract.RuntimeServiceAccountName(input.LeaseRef), runtimecontract.RuntimeTurnPodName(input.LeaseRef))
	if err != nil {
		t.Fatalf("RuntimeKubernetesAccessForExecution() error = %v", err)
	}
	input.EffectiveKubernetesAccess = access
	input.RuntimeEnvironmentDigest, _ = runtimecontract.RuntimeEnvironmentDigest(nil, nil, input.EnvironmentImage, nil, policy)
	snapshot := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema, OrganizationRef: input.OrganizationRef,
		ProjectRef: input.ProjectRef, AgentRef: input.AgentRef, Skills: []runtimecontract.RuntimeSkillBundle{}, Memories: []runtimecontract.RuntimeMemoryRecord{}}
	snapshot.Digest, _ = snapshot.ComputeDigest()
	input.ContextSnapshot = &snapshot
	for _, apply := range configure {
		apply(&input)
	}
	input.RuntimeRevisionDigest, _ = runtimecontract.RuntimeRevisionDigest(input, runtimecontract.RuntimeRevisionCredentialSource{
		SecretName: source.Name, SecretUID: string(source.UID), SecretResourceVersion: source.ResourceVersion,
	})
	input.ExecutionBindingDigest, input.MCPBindingDigest, _ = runtimecontract.RuntimeExecutionBindingDigests(input)
	binding := workload.ProviderSecretBinding{Name: source.Name, UID: string(source.UID), ResourceVersion: source.ResourceVersion, ContentSHA256: oldDigestHex}
	projection := workload.CredentialProjection{Namespace: "kodex-runtime", SecretName: "runtime-credentials-0123456789abcdef0123456789abcdef01234567",
		SecretUID: "40000000-0000-4000-8000-000000000001", SecretResourceVersion: "19", ContentSHA256: strings.Repeat("c", 64),
		ProviderAuthKey: "provider-auth.json", RuntimeSecretKeys: map[string]string{}}
	if err := manager.EnsureTurn(context.Background(), input, binding, projection); err != nil {
		t.Fatalf("EnsureTurn() error = %v", err)
	}
	ticketName := callbackTicketName(input.LeaseRef)
	ticketSecret, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), ticketName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read execution ticket: %v", err)
	}
	payload := runtimecontract.RunnerProviderCredentialRefreshRequest{
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, PreviousCredentialRevisionRef: input.ProviderCredentialRef,
		PreviousContentSHA256: input.ProviderCredentialSHA256,
		Authentication:        []byte(`{"auth_mode":"chatgpt","tokens":{"account_id":"account-1","access_token":"access-new","refresh_token":"refresh-new"}}`),
	}
	return manager, client, input, payload, string(ticketSecret.Data["token"])
}

func callbackTicketName(leaseRef string) string {
	digest := sha256.Sum256([]byte(leaseRef))
	return "runtime-ticket-" + hex.EncodeToString(digest[:8])
}
