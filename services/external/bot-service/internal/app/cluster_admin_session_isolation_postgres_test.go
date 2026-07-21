//go:build postgres

package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	kubernetesintegration "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/kubernetes"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestPrepareClusterAdminSecretIntegrityIsolatesMissingSessionToken(t *testing.T) {
	for _, test := range []struct {
		name                string
		podDeleteError      error
		wantError           bool
		wantSessionStatus   string
		wantTurnStatus      string
		wantPodExists       bool
		wantBlockedSessions int
	}{
		{
			name:                "pod deletion commits blocked session",
			wantSessionStatus:   "blocked",
			wantTurnStatus:      "failed",
			wantBlockedSessions: 1,
		},
		{
			name:              "pod deletion failure rolls back database block",
			podDeleteError:    errors.New("synthetic pod deletion failure"),
			wantError:         true,
			wantSessionStatus: "running",
			wantTurnStatus:    "running",
			wantPodExists:     true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			dsn := testsupport.IsolatedSchemaDSN(t, "app_cluster_admin_session")
			if err := migrations.RunTo(ctx, dsn, 24); err != nil {
				t.Fatalf("migrate test schema through integrity staging: %v", err)
			}
			pool, err := pgxpool.New(ctx, dsn)
			if err != nil {
				t.Fatalf("open test database: %v", err)
			}
			defer pool.Close()

			const (
				namespace  = "mattermost"
				sessionKey = "isolation-test"
				podName    = "mc-session-isolation-test"
				pvcName    = "mc-session-ws-isolation-test"
				podUID     = "isolation-pod-uid"
				podVersion = "7"
			)
			sessionID := seedClusterAdminSessionWithMissingToken(t, ctx, pool, sessionKey, podName, pvcName)
			client := fake.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: podName, Namespace: namespace, UID: types.UID(podUID), ResourceVersion: podVersion,
						Labels: map[string]string{
							"app.kubernetes.io/name":       "matter-codex-agent-runner",
							"app.kubernetes.io/component":  "agent-session",
							"matter-codex.dev/session-key": sessionKey,
						},
					},
					Status: corev1.PodStatus{Phase: corev1.PodFailed},
				},
				&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: namespace}},
			)
			client.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
				deleteAction, ok := action.(k8stesting.DeleteAction)
				if !ok {
					return true, nil, errors.New("unexpected Pod delete action")
				}
				preconditions := deleteAction.GetDeleteOptions().Preconditions
				if preconditions == nil || preconditions.UID == nil || string(*preconditions.UID) != podUID ||
					preconditions.ResourceVersion == nil || *preconditions.ResourceVersion != podVersion {
					return true, nil, errors.New("Pod delete lacks exact UID/resourceVersion preconditions")
				}
				if test.podDeleteError != nil {
					return true, nil, test.podDeleteError
				}
				return false, nil, nil
			})
			runner, err := kubernetesintegration.NewRunnerWithClient(client, kubernetesintegration.Config{
				Namespace: namespace, WorkspaceStorageSize: "1Gi",
			})
			if err != nil {
				t.Fatalf("create Kubernetes runner: %v", err)
			}

			blockedSessions, prepareErr := prepareClusterAdminSecretIntegrity(ctx, dsn, runner)
			if test.wantError != (prepareErr != nil) {
				t.Fatalf("prepareClusterAdminSecretIntegrity() error = %v, wantError=%t", prepareErr, test.wantError)
			}
			if test.podDeleteError != nil && !errors.Is(prepareErr, test.podDeleteError) {
				t.Fatalf("prepareClusterAdminSecretIntegrity() error = %v, want %v", prepareErr, test.podDeleteError)
			}
			if blockedSessions != test.wantBlockedSessions {
				t.Fatalf("blocked sessions = %d, want %d", blockedSessions, test.wantBlockedSessions)
			}

			var sessionStatus string
			var activeTurnID sql.NullInt64
			if err := pool.QueryRow(ctx, `select status, active_turn_id from matter_codex_agent_sessions where id = $1`, sessionID).Scan(&sessionStatus, &activeTurnID); err != nil {
				t.Fatalf("read isolated session: %v", err)
			}
			if sessionStatus != test.wantSessionStatus {
				t.Fatalf("session status = %q, want %q", sessionStatus, test.wantSessionStatus)
			}
			if sessionStatus == "blocked" && activeTurnID.Valid {
				t.Fatalf("blocked session kept active turn %d", activeTurnID.Int64)
			}
			var turnStatus string
			var turnError string
			if err := pool.QueryRow(ctx, `select status, error_message from matter_codex_agent_session_turns where session_id = $1`, sessionID).Scan(&turnStatus, &turnError); err != nil {
				t.Fatalf("read isolated turn: %v", err)
			}
			if turnStatus != test.wantTurnStatus {
				t.Fatalf("turn status = %q, want %q", turnStatus, test.wantTurnStatus)
			}
			if turnStatus == "failed" && !strings.Contains(turnError, "session token secret is missing") {
				t.Fatalf("failed turn error = %q", turnError)
			}

			_, podErr := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
			podExists := podErr == nil
			if podErr != nil && !apierrors.IsNotFound(podErr) {
				t.Fatalf("get session pod: %v", podErr)
			}
			if podExists != test.wantPodExists {
				t.Fatalf("pod exists = %t, want %t", podExists, test.wantPodExists)
			}
			if _, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{}); err != nil {
				t.Fatalf("session PVC was not preserved: %v", err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "delete" && (action.GetResource().Resource == "persistentvolumeclaims" || action.GetResource().Resource == "secrets") {
					t.Fatalf("persistent session resource received delete action: %s", action.GetResource().Resource)
				}
			}
		})
	}
}

func seedClusterAdminSessionWithMissingToken(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	sessionKey string,
	podName string,
	pvcName string,
) int64 {
	t.Helper()
	var projectID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Isolation', 'isolation') returning id`).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	var roleID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_roles(project_id, name, role_type, kubernetes_access)
values ($1, 'sre', 'sre', 'cluster-admin') returning id`, projectID).Scan(&roleID); err != nil {
		t.Fatalf("insert cluster-admin role: %v", err)
	}
	var chatID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug)
values ($1, 'channel-isolation', 'Isolation', 'isolation') returning id`, projectID).Scan(&chatID); err != nil {
		t.Fatalf("insert chat: %v", err)
	}
	var sessionID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_sessions(
	session_key, project_id, chat_id, role_id, session_scope,
	mattermost_channel_id, mattermost_root_post_id, status, active_run_id,
	kubernetes_namespace, pod_name, pvc_name, token_secret_ref, ttl_seconds, expires_at
) values ($1, $2, $3, $4, 'thread', 'channel-isolation', 'root-isolation', 'running', 'run-isolation',
	'mattermost', $5, $6, 'missing-session-token', 3600, now() + interval '1 hour') returning id`,
		sessionKey, projectID, chatID, roleID, podName, pvcName).Scan(&sessionID); err != nil {
		t.Fatalf("insert running session: %v", err)
	}
	var turnID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_session_turns(
	session_id, run_id, mattermost_channel_id, mattermost_root_post_id,
	mattermost_post_id, message, status, started_at
) values ($1, 'run-isolation', 'channel-isolation', 'root-isolation', 'post-isolation', 'test', 'running', now()) returning id`, sessionID).Scan(&turnID); err != nil {
		t.Fatalf("insert running turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $1 where id = $2`, turnID, sessionID); err != nil {
		t.Fatalf("attach active turn: %v", err)
	}
	return sessionID
}
