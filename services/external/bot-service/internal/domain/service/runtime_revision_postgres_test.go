//go:build postgres

package service

import (
	"context"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/kubernetes"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestLegacyQueuedTurnRepairsRuntimeThenClaimsAfterUpgrade(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "runtime_upgrade_repair_claim")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 30); err != nil {
		t.Fatalf("prepare N-1 schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	repository := postgresrepo.NewRepository(pool)
	project, _, err := repository.UpsertProject(ctx, adminrepo.UpsertProjectInput{Name: "Runtime upgrade", Slug: "runtime-upgrade-repair", AdvancedSettings: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.UpsertOpenAIAccount(ctx, adminrepo.UpsertOpenAIAccountInput{
		Name: "primary", CredentialName: "primary", SecretRef: "synthetic-codex-auth", Status: "authorized",
	}); err != nil {
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
		ProjectID: project.ID, MattermostChannelID: "channel-upgrade-repair", Name: "Upgrade repair",
		Slug: "upgrade-repair", ChatType: "custom", Settings: "{}", RoleIDs: []int64{role.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := repository.UpsertAgentSession(ctx, adminrepo.UpsertAgentSessionInput{
		SessionKey: "legacy-upgrade-session", ProjectID: project.ID, ChatID: chat.ID, RoleID: role.ID,
		SessionScope: "thread", MattermostChannelID: chat.MattermostChannelID, MattermostRootPostID: "legacy-root",
		OpenAIAccountName: "primary", TTLSeconds: 3600, Capabilities: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "synthetic-codex-auth", Namespace: "matter-codex-test", UID: "codex-auth-uid", ResourceVersion: "1"},
		Data:       map[string][]byte{"auth.json": []byte(`{"synthetic":"authorized"}`)},
	})
	client.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		pod := action.(ktesting.CreateAction).GetObject().(*corev1.Pod)
		pod.UID = types.UID("legacy-runtime-pod-uid")
		pod.Status.Phase = corev1.PodRunning
		return false, nil, nil
	})
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job)
		job.Status.Succeeded = 1
		return false, nil, nil
	})
	runner, err := kubernetes.NewRunnerWithClient(client, kubernetes.Config{
		Namespace: "matter-codex-test", AgentRunnerImage: "agent-runner@sha256:synthetic",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatal(err)
	}
	const sessionToken = "synthetic-upgrade-session-token"
	started, err := runner.StartAgentSession(ctx, runtimerepo.AgentSessionPodInput{
		SessionKey: session.SessionKey, Role: role.Name, OpenAIAccountAlias: "primary",
		KubernetesAccess: "read-only", BotServiceURL: "http://bot-service", InternalToken: sessionToken,
		CodexAuthSecretName: "synthetic-codex-auth", AllowPodRecreation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateAgentSessionRuntime(ctx, adminrepo.UpdateAgentSessionRuntimeInput{
		SessionKey: session.SessionKey, Status: "idle", KubernetesNamespace: started.Namespace,
		PodName: started.PodName, PVCName: started.PVCName, TokenSecretRef: started.SecretName, ExtendTTLSeconds: 3600,
	}); err != nil {
		t.Fatal(err)
	}
	var legacyTurnID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_session_turns(
	session_id, run_id, mattermost_channel_id, mattermost_root_post_id,
	mattermost_post_id, user_id, user_name, message
) values ($1, 'legacy-upgrade-run', $2, 'legacy-root', 'legacy-post', 'legacy-user', 'developer', 'legacy queued prompt')
returning id
`, session.ID, chat.MattermostChannelID).Scan(&legacyTurnID); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository = postgresrepo.NewRepository(pool)
	chatService := NewChatRunService(ChatRunServiceConfig{
		Store: repository, RuntimeRunner: runner, StorageReady: true, RuntimeReady: true, DisableMonitor: true,
		BotServiceURL: "http://bot-service", AgentRunnerImage: "agent-runner@sha256:synthetic",
	})
	repair, err := chatService.RepairAgentSessions(ctx, 10)
	if err != nil {
		t.Fatalf("RepairAgentSessions() error = %v", err)
	}
	if repair.QueuedSessionsEnsured != 1 || repair.Failed != 0 {
		t.Fatalf("repair result = %#v", repair)
	}
	state, err := repository.GetAgentSessionRuntimeRevisionState(ctx, session.SessionKey)
	if err != nil || state.DesiredRuntimeRevisionID != 0 || state.AppliedRuntimeRevisionID != 0 || state.AppliedPodUID != "legacy-runtime-pod-uid" {
		t.Fatalf("legacy runtime state = %#v, error=%v", state, err)
	}
	sessionService := NewAgentSessionService(AgentSessionServiceConfig{
		Store: repository, RuntimeRunner: runner, ThreadPublisher: &fakeThreadPublisher{},
		StorageReady: true, RuntimeReady: true,
	})
	claim, err := sessionService.ClaimNextTurn(ctx, session.SessionKey, sessionToken)
	if err != nil {
		t.Fatalf("ClaimNextTurn() after upgrade repair error = %v", err)
	}
	if !claim.HasTurn || claim.TurnID != legacyTurnID || claim.RunID != "legacy-upgrade-run" || claim.RuntimeRevisionID != 0 {
		t.Fatalf("legacy FIFO claim = %#v", claim)
	}
}
