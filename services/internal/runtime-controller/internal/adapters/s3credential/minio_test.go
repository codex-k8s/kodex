package s3credential

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	port "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/repository/s3credential"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/security/s3policy"
	"github.com/google/uuid"
	"github.com/minio/madmin-go/v3"
	miniocredentials "github.com/minio/minio-go/v7/pkg/credentials"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var errTestNotFound = errors.New("not found")

type fakeAdmin struct {
	accounts map[string]accountInfo
	secrets  map[string]string
	adds     []addRequest
	sticky   bool
}

func (admin *fakeAdmin) AddServiceAccount(_ context.Context, request addRequest) (credential, error) {
	if _, exists := admin.accounts[request.AccessKey]; exists {
		return credential{}, errors.New("already exists")
	}
	admin.adds = append(admin.adds, request)
	admin.accounts[request.AccessKey] = accountInfo{ParentUser: "runtime-s3-archive-management", Status: "on",
		Policy: string(request.Policy), Name: request.Name, Description: request.Description, Expiration: request.Expiration}
	admin.secrets[request.AccessKey] = request.SecretKey
	return credential{AccessKey: request.AccessKey, SecretKey: request.SecretKey, Expiration: request.Expiration}, nil
}

func (admin *fakeAdmin) InfoServiceAccount(_ context.Context, accessKey string) (accountInfo, error) {
	info, ok := admin.accounts[accessKey]
	if !ok {
		return accountInfo{}, errTestNotFound
	}
	return info, nil
}

func (admin *fakeAdmin) DeleteServiceAccount(_ context.Context, accessKey string) error {
	if _, ok := admin.accounts[accessKey]; !ok {
		return errTestNotFound
	}
	if !admin.sticky {
		delete(admin.accounts, accessKey)
		delete(admin.secrets, accessKey)
	}
	return nil
}

func (*fakeAdmin) IsNotFound(err error) bool { return errors.Is(err, errTestNotFound) }
func (*fakeAdmin) Close()                    {}

type fakeStateStore struct {
	current   state
	version   uint64
	conflicts int
}

func (store *fakeStateStore) Load(context.Context, port.Action) (state, string, error) {
	raw, _ := json.Marshal(store.current)
	var copy state
	_ = json.Unmarshal(raw, &copy)
	return copy, string(rune(store.version)), nil
}

func (store *fakeStateStore) Save(_ context.Context, _ port.Action, resourceVersion string, next state) error {
	if store.conflicts > 0 {
		store.conflicts--
		return apierrors.NewConflict(corev1.Resource("secrets"), "exact-state", errors.New("test conflict"))
	}
	if resourceVersion != string(rune(store.version)) {
		return errors.New("conflict")
	}
	store.current = next
	store.version++
	return nil
}

func TestMinIOProviderCASConflictIsBounded(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	admin := &fakeAdmin{accounts: map[string]accountInfo{}, secrets: map[string]string{}}
	store := &fakeStateStore{current: state{Schema: stateSchema, Records: map[string]record{}}, version: 1, conflicts: 4}
	provider := testProvider(admin, store, now)
	if _, err := provider.Issue(t.Context(), testRequest(t, now, 1)); err == nil || len(admin.adds) != 0 {
		t.Fatal("CAS exhaustion did not fail before external credential creation")
	}
}

func TestMinIOProviderIssueCheckRestartAndRevoke(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	admin := &fakeAdmin{accounts: map[string]accountInfo{}, secrets: map[string]string{}}
	store := &fakeStateStore{current: state{Schema: stateSchema, Records: map[string]record{}}, version: 1}
	provider := testProvider(admin, store, now)
	request := testRequest(t, now, 1)

	issued, err := provider.Issue(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if issued.SessionToken != "" || issued.ExpiresAt.Sub(now) != credentialTTL || len(admin.adds) != 1 ||
		admin.adds[0].Expiration.Sub(now) != 15*time.Minute {
		t.Fatal("MinIO child credential does not have exact direct-production lifetime")
	}
	policyText := string(admin.adds[0].Policy)
	for _, forbidden := range []string{`"kms:Decrypt"`, `"s3:GetBucketPublicAccessBlock"`, `"Resource":"*","Effect":"Allow"`} {
		if strings.Contains(policyText, forbidden) {
			t.Fatalf("MinIO policy was broadened by %s", forbidden)
		}
	}
	for _, required := range []string{`"s3:GetBucketPolicyStatus"`, `"s3:x-amz-server-side-encryption-aws-kms-key-id":"mattercodex-runtime"`} {
		if !strings.Contains(policyText, required) {
			t.Fatalf("MinIO policy misses %s", required)
		}
	}
	if err := provider.Check(t.Context(), request); err != nil {
		t.Fatalf("functional readback: %v", err)
	}
	restarted := testProvider(admin, store, now)
	rejoined, err := restarted.Issue(t.Context(), request)
	if err != nil || rejoined.AccessKeyID != issued.AccessKeyID || rejoined.SecretAccessKey != issued.SecretAccessKey || len(admin.adds) != 1 {
		t.Fatalf("restart did not rejoin exact child credential: issued=%+v err=%v", rejoined, err)
	}
	if err := restarted.Revoke(t.Context(), request, port.Issue{}); err != nil {
		t.Fatalf("revoke exact child: %v", err)
	}
	if err := restarted.Revoke(t.Context(), request, port.Issue{}); err != nil {
		t.Fatalf("idempotent revoke: %v", err)
	}
	if _, ok := admin.accounts[issued.AccessKeyID]; ok {
		t.Fatal("revoked MinIO child remains readable")
	}
}

func TestMinIOProviderRejectsPolicyConflictRollbackAndReadbackMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	admin := &fakeAdmin{accounts: map[string]accountInfo{}, secrets: map[string]string{}}
	store := &fakeStateStore{current: state{Schema: stateSchema, Records: map[string]record{}}, version: 1}
	provider := testProvider(admin, store, now)
	request := testRequest(t, now, 1)
	issued, err := provider.Issue(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	mutations := map[string]func([]byte) []byte{
		"action": func(raw []byte) []byte { return bytes.Replace(raw, []byte(`"s3:GetObject"`), []byte(`"s3:*"`), 1) },
		"resource": func(raw []byte) []byte {
			return bytes.ReplaceAll(raw, []byte("mattercodex-runtime"), []byte("another-bucket"))
		},
		"kms": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte("mattercodex-runtime"), []byte("another-key"), 1)
		},
		"tag": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`{`), []byte(`{"Tag":"caller",`), 1)
		},
	}
	for name, mutate := range mutations {
		tampered := request
		tampered.PolicyRaw = mutate(append([]byte(nil), request.PolicyRaw...))
		if _, err := provider.Issue(t.Context(), tampered); err == nil {
			t.Fatalf("caller %s policy broadening was accepted", name)
		}
	}
	conflict := request
	conflict.Execution.ImmutableInputSHA256 = strings.Repeat("c", 64)
	if _, err := provider.Issue(t.Context(), conflict); err == nil {
		t.Fatal("duplicate execution tuple with another input digest was accepted")
	}
	next := testRequest(t, now, 2)
	next.Execution.ID = request.Execution.ID
	next.Execution.OrganizationID = request.Execution.OrganizationID
	next.Execution.ProjectID = request.Execution.ProjectID
	next.Execution.ProcessID = request.Execution.ProcessID
	next.Execution.SessionID = request.Execution.SessionID
	next.Execution.RuntimeRevisionID = request.Execution.RuntimeRevisionID
	next.Execution.ProviderBindingID = request.Execution.ProviderBindingID
	next = rebuildRequest(t, next, now)
	if _, err := provider.Issue(t.Context(), next); err != nil {
		t.Fatalf("forward retry generation: %v", err)
	}
	if _, err := provider.Issue(t.Context(), request); err == nil {
		t.Fatal("attempt rollback was accepted")
	}
	info := admin.accounts[provider.accessKey(recordID(next))]
	info.ParentUser = "wrong-parent"
	admin.accounts[provider.accessKey(recordID(next))] = info
	if err := provider.Check(t.Context(), next); err == nil {
		t.Fatal("wrong parent readback was accepted")
	}
	admin.sticky = true
	if err := provider.Revoke(t.Context(), next, issued); err == nil {
		t.Fatal("revoke without deletion readback was accepted")
	}
}

func TestMinIOProviderReadinessUsesBoundedCreateInfoDelete(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	admin := &fakeAdmin{accounts: map[string]accountInfo{}, secrets: map[string]string{}}
	store := &fakeStateStore{current: state{Schema: stateSchema, Records: map[string]record{}}, version: 1}
	provider := testProvider(admin, store, now)
	if err := provider.Ready(t.Context(), port.ActionRestore); err == nil {
		t.Fatal("archive provider accepted restore action")
	}
	if err := provider.Ready(t.Context(), port.ActionArchive); err != nil {
		t.Fatal(err)
	}
	if len(admin.adds) != 1 || admin.adds[0].Expiration.Sub(now) != probeTTL || len(admin.accounts) != 0 ||
		!strings.Contains(string(admin.adds[0].Policy), `"Effect":"Deny"`) {
		t.Fatal("readiness did not prove bounded add/info/delete")
	}
}

func TestKubernetesStateStoreUsesOnlyExactAggregateSecret(t *testing.T) {
	t.Parallel()
	initial := []byte(`{"schema":"mattercodex.runtime-s3-minio-identity-state/v1","generation":0,"records":{}}`)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: stateSecretName(port.ActionArchive), Namespace: "runtime",
		ResourceVersion: "7", Annotations: map[string]string{"runtime.mattercodex.dev/state-kind": "minio-service-account-records",
			"runtime.mattercodex.dev/action": "archive"}}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"state.json": initial}}
	client := fake.NewSimpleClientset(secret)
	store := &kubernetesStateStore{secrets: client.CoreV1().Secrets("runtime")}
	loaded, _, err := store.Load(t.Context(), port.ActionArchive)
	if err != nil || loaded.Schema != stateSchema || len(loaded.Records) != 0 {
		t.Fatalf("load exact aggregate Secret: %+v %v", loaded, err)
	}
	if _, _, err := store.Load(t.Context(), port.ActionRestore); err == nil {
		t.Fatal("archive identity could read restore aggregate Secret")
	}
	tampered, err := client.CoreV1().Secrets("runtime").Get(t.Context(), stateSecretName(port.ActionArchive), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tampered.Data["caller-key"] = []byte("not-authority")
	if _, err = client.CoreV1().Secrets("runtime").Update(t.Context(), tampered, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(t.Context(), port.ActionArchive); err == nil {
		t.Fatal("aggregate Secret with an extra key was accepted")
	}
}

func TestMinIOProviderRejectsTTLAndGenerationStateTampering(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	admin := &fakeAdmin{accounts: map[string]accountInfo{}, secrets: map[string]string{}}
	store := &fakeStateStore{current: state{Schema: stateSchema, Records: map[string]record{}}, version: 1}
	provider := testProvider(admin, store, now)
	request := testRequest(t, now, 1)
	if _, err := provider.Issue(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	id := recordID(request)
	original := store.current.Records[id]
	tampered := original
	tampered.ExpiresAt = tampered.ExpiresAt.Add(time.Minute)
	tampered.Signature = provider.sign(tampered)
	store.current.Records[id] = tampered
	if err := provider.Check(t.Context(), request); err == nil {
		t.Fatal("credential TTL other than 900 seconds was accepted")
	}
	store.current.Records[id] = original
	store.current.Generation = original.Generation - 1
	if err := provider.Check(t.Context(), request); err == nil {
		t.Fatal("durable state generation rollback was accepted")
	}
}

func TestMinIOAdminTransportUsesExactEncryptedOperations(t *testing.T) {
	t.Parallel()
	const managementSecret = "management-secret-value"
	expiresAt := time.Now().UTC().Add(15 * time.Minute).Truncate(time.Second)
	requests := make([]string, 0, 3)
	roundTripper := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: request}
		switch request.Method + " " + request.URL.Path {
		case http.MethodPut + " /minio/admin/v3/add-service-account":
			decrypted, err := madmin.DecryptData(managementSecret, request.Body)
			if err != nil {
				t.Fatal(err)
			}
			var actual madmin.AddServiceAccountReq
			if json.Unmarshal(decrypted, &actual) != nil || actual.TargetUser != "" || actual.AccessKey != "MCXEXACTACCESSKEY01" ||
				actual.SecretKey != "exact-child-secret" || actual.Name != "mcx-archive-exact" || actual.Description != "exact-description" ||
				actual.Expiration == nil || !actual.Expiration.Equal(expiresAt) || !bytes.Equal(actual.Policy, []byte(`{"Version":"2012-10-17"}`)) {
				t.Fatal("MinIO add-service-account request was broadened")
			}
			raw, _ := json.Marshal(madmin.AddServiceAccountResp{Credentials: madmin.Credentials{AccessKey: actual.AccessKey, SecretKey: actual.SecretKey, Expiration: expiresAt}})
			encrypted, _ := madmin.EncryptData(managementSecret, raw)
			response.Body = io.NopCloser(bytes.NewReader(encrypted))
		case http.MethodGet + " /minio/admin/v3/info-service-account":
			if request.URL.Query().Get("accessKey") != "MCXEXACTACCESSKEY01" || len(request.URL.Query()) != 1 {
				t.Fatal("MinIO info-service-account query was broadened")
			}
			raw, _ := json.Marshal(madmin.InfoServiceAccountResp{ParentUser: "runtime-s3-archive-management", AccountStatus: "on",
				Policy: `{"Version":"2012-10-17"}`, Name: "mcx-archive-exact", Description: "exact-description", Expiration: &expiresAt})
			encrypted, _ := madmin.EncryptData(managementSecret, raw)
			response.Body = io.NopCloser(bytes.NewReader(encrypted))
		case http.MethodDelete + " /minio/admin/v3/delete-service-account":
			if request.URL.Query().Get("accessKey") != "MCXEXACTACCESSKEY01" || len(request.URL.Query()) != 1 {
				t.Fatal("MinIO delete-service-account query was broadened")
			}
			response.StatusCode = http.StatusNoContent
			response.Body = http.NoBody
		default:
			t.Fatalf("unexpected MinIO admin operation: %s %s", request.Method, request.URL.String())
		}
		return response, nil
	})
	client, err := madmin.NewWithOptions("minio.mattercodex-system.svc:9000", &madmin.Options{
		Creds: miniocredentials.NewStaticV4("management-access", managementSecret, ""), Secure: true, Transport: roundTripper,
	})
	if err != nil {
		t.Fatal(err)
	}
	transport := &madminClient{client: client, transport: &http.Transport{}}
	policy := []byte(`{"Version":"2012-10-17"}`)
	created, err := transport.AddServiceAccount(t.Context(), addRequest{AccessKey: "MCXEXACTACCESSKEY01", SecretKey: "exact-child-secret",
		Name: "mcx-archive-exact", Description: "exact-description", Policy: policy, Expiration: expiresAt})
	if err != nil || created.AccessKey != "MCXEXACTACCESSKEY01" || created.SecretKey != "exact-child-secret" {
		t.Fatalf("add service account: %+v %v", created, err)
	}
	if _, err := transport.InfoServiceAccount(t.Context(), created.AccessKey); err != nil {
		t.Fatal(err)
	}
	if err := transport.DeleteServiceAccount(t.Context(), created.AccessKey); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 || !strings.HasPrefix(requests[0], "PUT /minio/admin/v3/add-service-account") ||
		!strings.HasPrefix(requests[1], "GET /minio/admin/v3/info-service-account?") ||
		!strings.HasPrefix(requests[2], "DELETE /minio/admin/v3/delete-service-account?") {
		t.Fatalf("unexpected MinIO admin request sequence: %v", requests)
	}
}

func TestMinIOAdminConfigRejectsNonExactTLSBoundary(t *testing.T) {
	t.Parallel()
	for _, config := range []Config{
		{Endpoint: "http://minio.mattercodex-system.svc:9000", TLSServerName: "minio.mattercodex-system.svc.cluster.local"},
		{Endpoint: "https://minio.mattercodex-system.svc:9000/admin", TLSServerName: "minio.mattercodex-system.svc.cluster.local"},
		{Endpoint: "https://minio.mattercodex-system.svc:9000"},
	} {
		if _, err := newAdmin(config); err == nil {
			t.Fatalf("non-exact MinIO TLS boundary was accepted: %+v", config)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testProvider(admin admin, store stateStore, now time.Time) *Provider {
	return &Provider{config: Config{Action: port.ActionArchive, ParentUser: "runtime-s3-archive-management",
		ParentProfile: "runtime-s3-archive-minio-management", Bucket: "mattercodex-runtime", Region: "mattercodex-1",
		KMSKeyARN: "arn:aws:kms:mattercodex-1:local:key/mattercodex-runtime", KMSKeyID: "mattercodex-runtime"},
		admin: admin, state: store, signingKey: []byte(strings.Repeat("k", 32)), now: func() time.Time { return now }}
}

func testRequest(t *testing.T, now time.Time, attempt uint32) port.Request {
	t.Helper()
	execution := validExecution(now)
	execution.Attempt = attempt
	execution.GrantGeneration = uint64(attempt)
	request := port.Request{Execution: execution, Action: port.ActionArchive, SourceExecutionID: execution.ID}
	return rebuildRequest(t, request, now)
}

func rebuildRequest(t *testing.T, request port.Request, now time.Time) port.Request {
	t.Helper()
	result, err := s3policy.Build(request.Execution, request.Action, s3policy.Config{Bucket: "mattercodex-runtime", Region: "mattercodex-1",
		KMSKeyARN: "arn:aws:kms:mattercodex-1:local:key/mattercodex-runtime", KMSKeyID: "mattercodex-runtime"}, s3policy.DialectMinIO, now)
	if err != nil {
		t.Fatal(err)
	}
	request.SourceExecutionID = result.SourceExecutionID
	request.PolicyRaw = result.Raw
	return request
}

func validExecution(now time.Time) entity.Execution {
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
		ArchiveRetentionSeconds: 7776000, PVCCleanupEligibleAt: now.Add(24 * time.Hour),
		ArchiveRetainUntil: now.Add(90 * 24 * time.Hour), CapacityObservationExpiresAt: now.Add(time.Hour), RescheduleAfter: now.Add(time.Minute),
		RestoreAssignmentState: "NONE", WorkloadTicketSHA256: strings.Repeat("9", 64),
		ProviderBindingID: uuid.NewString(), ProviderBindingVersion: 1, ProviderBindingSHA256: strings.Repeat("7", 64),
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
