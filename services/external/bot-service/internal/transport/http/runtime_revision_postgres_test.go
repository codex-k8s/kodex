//go:build postgres

package http

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/sessionarchive"
	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/kubernetes"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestProductionBoundaryNMinusOneCompletionSnapshotAndRestore(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "runtime_http_production_boundary")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrate production boundary schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := postgresrepo.NewRepository(pool)
	project, _, err := repository.UpsertProject(ctx, adminrepo.UpsertProjectInput{Name: "Runtime HTTP", Slug: "runtime-http", AdvancedSettings: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	role, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: project.ID, Name: "developer", RoleType: "worker", PromptMode: "template",
		OpenAIAccountName: "primary", KubernetesAccess: "read-only", SandboxMode: "danger-full-access",
		AdvancedSettings: "{}", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	chat, _, err := repository.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID: project.ID, MattermostChannelID: "channel-runtime-http", Name: "Runtime HTTP chat",
		Slug: "runtime-http-chat", ChatType: "custom", Settings: "{}", RoleIDs: []int64{role.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := repository.UpsertAgentSession(ctx, adminrepo.UpsertAgentSessionInput{
		SessionKey: "runtime-http-session", ProjectID: project.ID, ChatID: chat.ID, RoleID: role.ID,
		SessionScope: "thread", MattermostChannelID: chat.MattermostChannelID, MattermostRootPostID: "root-runtime-http",
		OpenAIAccountName: "primary", TTLSeconds: 3600, Capabilities: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}

	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		pod := action.(ktesting.CreateAction).GetObject().(*corev1.Pod)
		pod.UID = types.UID("pod-runtime-http-1")
		return false, nil, nil
	})
	runner, err := kubernetes.NewRunnerWithClient(client, kubernetes.Config{
		Namespace: "matter-codex-test", AgentRunnerImage: "agent-runner@sha256:synthetic",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatal(err)
	}
	const sessionToken = "synthetic-session-token-with-safe-length"
	started, err := runner.StartAgentSession(ctx, runtimerepo.AgentSessionPodInput{
		SessionKey: session.SessionKey, Role: role.Name, OpenAIAccountAlias: "primary",
		KubernetesAccess: role.KubernetesAccess, BotServiceURL: "http://bot-service",
		InternalToken: sessionToken, CodexAuthSecretName: "synthetic-codex-auth",
		AllowPodRecreation: true,
	})
	if err != nil {
		t.Fatalf("start through Kubernetes adapter: %v", err)
	}
	if started.PodUID != "pod-runtime-http-1" {
		t.Fatalf("confirmed Kubernetes pod UID = %q", started.PodUID)
	}
	if _, err := repository.UpdateAgentSessionRuntime(ctx, adminrepo.UpdateAgentSessionRuntimeInput{
		SessionKey: session.SessionKey, Status: "idle", KubernetesNamespace: started.Namespace,
		PodName: started.PodName, PVCName: started.PVCName, TokenSecretRef: started.SecretName, ExtendTTLSeconds: 3600,
	}); err != nil {
		t.Fatal(err)
	}
	service := statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
		Store: repository, RuntimeRunner: runner, ThreadPublisher: productionBoundaryPublisher{},
		StorageReady: true, RuntimeReady: true,
	})
	router := NewRouter(RouterConfig{SessionService: service})

	firstTurn, err := repository.CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
		SessionID: session.ID, RunID: "legacy-http-run-1", MattermostChannelID: chat.MattermostChannelID,
		MattermostRootPostID: "root-runtime-http", MattermostPostID: "post-runtime-http-1",
		UserID: "runtime-user", UserName: "developer", Message: "legacy HTTP completion",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := productionBoundaryRequest(t, router, "POST", "/internal/agent-sessions/"+session.SessionKey+"/turns/claim", sessionToken, nil)
	if claim.Code != 200 {
		t.Fatalf("N-1 HTTP claim status=%d body=%s", claim.Code, claim.Body.String())
	}
	var claimBody statusservice.AgentSessionTurnClaim
	if err := json.Unmarshal(claim.Body.Bytes(), &claimBody); err != nil || claimBody.TurnID != firstTurn.ID || claimBody.RuntimeRevisionID != 0 {
		t.Fatalf("N-1 claim=%#v error=%v", claimBody, err)
	}
	archive := productionBoundaryArchive(t, "confirmed production boundary")
	legacyCompletion := map[string]any{
		"turn_id": claimBody.TurnID, "run_id": claimBody.RunID, "status": "succeeded",
		"final_message": "готово", "codex_session_id": "legacy-http-codex",
		"session_archive_gzip_base64": archive, "artifacts": map[string]string{},
	}
	completed := productionBoundaryRequest(t, router, "POST", "/internal/agent-sessions/"+session.SessionKey+"/turns/complete", sessionToken, legacyCompletion)
	if completed.Code != 200 {
		t.Fatalf("N-1 HTTP completion status=%d body=%s", completed.Code, completed.Body.String())
	}
	snapshotResponse := productionBoundaryRequest(t, router, "GET", "/internal/agent-sessions/"+session.SessionKey+"/snapshot", sessionToken, nil)
	if snapshotResponse.Code != 200 {
		t.Fatalf("snapshot status=%d body=%s", snapshotResponse.Code, snapshotResponse.Body.String())
	}
	var snapshot statusservice.AgentSessionSnapshot
	if err := json.Unmarshal(snapshotResponse.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.ArchiveVersion != 1 || snapshot.ArchiveSHA256 == "" || snapshot.ArchiveSizeBytes <= 0 {
		t.Fatalf("confirmed snapshot=%#v", snapshot)
	}
	restoreRoot := t.TempDir()
	if err := sessionarchive.RestoreEncoded(snapshot.SessionArchiveGzipBase64, restoreRoot, snapshot.ArchiveSHA256, snapshot.ArchiveSizeBytes); err != nil {
		t.Fatalf("production archive restore: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(restoreRoot, "sessions", "state.json"))
	if err != nil || string(restored) != "confirmed production boundary" {
		t.Fatalf("restored archive=%q error=%v", restored, err)
	}

	secondTurn, err := repository.CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
		SessionID: session.ID, RunID: "legacy-http-run-2", MattermostChannelID: chat.MattermostChannelID,
		MattermostRootPostID: "root-runtime-http", MattermostPostID: "post-runtime-http-2",
		UserID: "runtime-user", UserName: "developer", Message: "invalid legacy HTTP completion",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim = productionBoundaryRequest(t, router, "POST", "/internal/agent-sessions/"+session.SessionKey+"/turns/claim", sessionToken, nil)
	if claim.Code != 200 || json.Unmarshal(claim.Body.Bytes(), &claimBody) != nil || claimBody.TurnID != secondTurn.ID {
		t.Fatalf("second claim status=%d body=%s", claim.Code, claim.Body.String())
	}
	for name, invalidArchive := range map[string]string{
		"пустой successful archive": "",
		"повреждённый archive":      base64.StdEncoding.EncodeToString([]byte("not-gzip-ustar")),
	} {
		t.Run(name, func(t *testing.T) {
			payload := map[string]any{
				"turn_id": claimBody.TurnID, "run_id": claimBody.RunID, "status": "succeeded",
				"codex_session_id": "invalid-codex", "session_archive_gzip_base64": invalidArchive,
				"artifacts": map[string]string{},
			}
			response := productionBoundaryRequest(t, router, "POST", "/internal/agent-sessions/"+session.SessionKey+"/turns/complete", sessionToken, payload)
			if response.Code != 502 {
				t.Fatalf("invalid completion status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	snapshotResponse = productionBoundaryRequest(t, router, "GET", "/internal/agent-sessions/"+session.SessionKey+"/snapshot", sessionToken, nil)
	if snapshotResponse.Code != 200 || json.Unmarshal(snapshotResponse.Body.Bytes(), &snapshot) != nil || snapshot.ArchiveVersion != 1 || snapshot.SessionArchiveGzipBase64 != archive {
		t.Fatalf("invalid archive replaced confirmed latest: status=%d snapshot=%#v", snapshotResponse.Code, snapshot)
	}
	if _, err := client.CoreV1().Pods(started.Namespace).Get(ctx, started.PodName, metav1.GetOptions{}); err != nil {
		t.Fatalf("production Kubernetes adapter pod disappeared: %v", err)
	}
}

func productionBoundaryRequest(t *testing.T, router *Router, method string, path string, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = *bytes.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, &reader)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func productionBoundaryArchive(t *testing.T, value string) string {
	t.Helper()
	var raw bytes.Buffer
	gzipWriter := gzip.NewWriter(&raw)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "sessions", Typeflag: tar.TypeDir, Mode: 0o700, Format: tar.FormatUSTAR}); err != nil {
		t.Fatal(err)
	}
	body := []byte(value)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "sessions/state.json", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(body)), Format: tar.FormatUSTAR}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw.Bytes())
}

type productionBoundaryPublisher struct{}

func (productionBoundaryPublisher) PostThreadMessage(_ context.Context, input statusservice.MattermostThreadPostInput) (statusservice.MattermostPostRef, error) {
	return statusservice.MattermostPostRef{ChannelID: input.ChannelID, PostID: "result-post"}, nil
}
func (productionBoundaryPublisher) PostThreadMessageWithToken(ctx context.Context, _ string, input statusservice.MattermostThreadPostInput) (statusservice.MattermostPostRef, error) {
	return productionBoundaryPublisher{}.PostThreadMessage(ctx, input)
}
func (productionBoundaryPublisher) UpdateThreadMessage(_ context.Context, input statusservice.MattermostThreadUpdateInput) (statusservice.MattermostPostRef, error) {
	return statusservice.MattermostPostRef{ChannelID: input.ChannelID, PostID: input.PostID}, nil
}
func (productionBoundaryPublisher) UpdateThreadMessageWithToken(ctx context.Context, _ string, input statusservice.MattermostThreadUpdateInput) (statusservice.MattermostPostRef, error) {
	return productionBoundaryPublisher{}.UpdateThreadMessage(ctx, input)
}
func (productionBoundaryPublisher) PostThreadCard(_ context.Context, card statusservice.MattermostCard) (statusservice.MattermostPostRef, error) {
	return statusservice.MattermostPostRef{ChannelID: card.ChannelID, PostID: "status-card"}, nil
}
func (productionBoundaryPublisher) UpdateThreadCard(_ context.Context, card statusservice.MattermostCard) (statusservice.MattermostPostRef, error) {
	return statusservice.MattermostPostRef{ChannelID: card.ChannelID, PostID: card.PostID}, nil
}
func (productionBoundaryPublisher) AddPostReactionWithToken(context.Context, string, statusservice.MattermostPostReactionInput) error {
	return nil
}
