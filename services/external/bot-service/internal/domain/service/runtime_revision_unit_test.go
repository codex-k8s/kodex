package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

func TestRuntimeRevisionUnitPersistsTurnAndRecreatesIdleSessionForChangedDigest(t *testing.T) {
	base := chatRuntimeStore()
	base.openAIAccounts["main"] = entity.OpenAIAccount{
		Name: "main", SecretRef: "matter-codex-codex-auth-main", Status: "authorized",
		CredentialID: 17, UpdatedAt: time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC),
	}
	role := entity.AgentRole{
		ID: 1, ProjectID: 1, Name: "developer", RoleType: "worker", OpenAIAccountName: "main",
		KubernetesAccess: "read-only", SandboxMode: "danger-full-access", AdvancedSettings: "{}",
		Enabled: true, UpdatedAt: time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC),
	}
	base.agentRoles[role.ID] = role
	chat := entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-runtime", Slug: "runtime", Name: "Runtime"}
	base.chats[chat.ID] = chat
	store := &runtimeRevisionFakeStore{
		fakeAdminStore: base,
		revisions:      map[string]entity.RuntimeRevision{},
		states:         map[string]entity.AgentSessionRuntimeRevisionState{},
		archives:       map[int64][]entity.AgentSessionArchive{},
	}
	runner := &restoringRuntimeRunner{fakeRuntimeRunner: &fakeRuntimeRunner{}}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer: testLocalizer(t, texti18n.DefaultLocale), Store: store, RuntimeRunner: runner,
		StorageReady: true, RuntimeReady: true, DisableMonitor: true,
		BotServiceURL: "http://bot-service", AgentRunnerImage: "agent-runner@sha256:synthetic-safe-digest",
	})
	request := AgentTurnRequest{
		Project: base.projects[1], Chat: chat, Role: role, UserID: "user-runtime", UserName: "owner",
		UserMessage: "first runtime revision", PreparedPrompt: "safe prepared prompt", SourcePostID: "post-1",
		ReplyRootID: "post-1", SessionRootID: "post-1", SessionScope: agentSessionScopeThreadRole,
	}
	first, err := svc.EnqueueAgentTurn(context.Background(), request)
	if err != nil {
		t.Fatalf("first EnqueueAgentTurn() error = %v", err)
	}
	if len(store.revisions) != 1 || len(runner.sessionRuns) != 1 || len(base.sessionTurns) != 1 {
		t.Fatalf("first component state: revisions=%#v runs=%#v turns=%#v", store.revisions, runner.sessionRuns, base.sessionTurns)
	}
	firstRun := runner.sessionRuns[0]
	firstPod := runner.starts[0]
	firstState := store.states[first.SessionKey]
	if firstRun.RuntimeRevisionDigest == "" || firstRun.OpenAIAccountAlias != "main" || !firstRun.AllowPodRecreation || firstPod.Recreation.Action != runtimerepo.AgentSessionPodCreated {
		t.Fatalf("first runtime input/start = %#v / %#v", firstRun, firstPod)
	}
	if firstState.DesiredRuntimeRevisionID == 0 || firstState.DesiredRuntimeRevisionID != firstState.AppliedRuntimeRevisionID || base.sessionTurns[0].RuntimeRevisionID != firstState.AppliedRuntimeRevisionID {
		t.Fatalf("first revision bindings: state=%#v turn=%#v", firstState, base.sessionTurns[0])
	}
	for _, revision := range store.revisions {
		if strings.Contains(revision.Manifest, "synthetic-secret-value") {
			t.Fatal("runtime revision manifest contains a secret value")
		}
	}
	firstTurn := base.sessionTurns[0]
	firstTurn.Status = agentSessionTurnRunning
	base.sessionTurns[0] = firstTurn
	activeSession := withActiveTurn(base.agentSessions[first.SessionKey], firstTurn.ID, firstTurn.RunID)
	base.agentSessions[first.SessionKey] = activeSession
	archiveRaw := []byte("synthetic-confirmed-codex-archive")
	archivePayload := base64.StdEncoding.EncodeToString(archiveRaw)
	archiveDigest := sha256.Sum256(archiveRaw)
	archiveSHA := hex.EncodeToString(archiveDigest[:])
	sessionService := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer: testLocalizer(t, texti18n.DefaultLocale), Store: store, RuntimeRunner: runner,
		ThreadPublisher: &fakeThreadPublisher{}, StorageReady: true, RuntimeReady: true,
	})
	if err := sessionService.CompleteTurn(context.Background(), first.SessionKey, runner.botTokenSecrets[activeSession.TokenSecretRef], CompleteAgentSessionTurnCommand{
		TurnID: firstTurn.ID, RunID: firstTurn.RunID, Status: agentSessionTurnSucceeded,
		FinalMessage: "готово", CodexSessionID: "codex-session-safe-id",
		SessionArchiveGzipBase64: archivePayload, ArchiveSHA256: archiveSHA, ArchiveSizeBytes: int64(len(archiveRaw)),
	}); err != nil {
		t.Fatalf("CompleteTurn() archive component error = %v", err)
	}
	snapshot, err := sessionService.Snapshot(context.Background(), first.SessionKey, runner.botTokenSecrets[activeSession.TokenSecretRef])
	if err != nil {
		t.Fatalf("Snapshot() component error = %v", err)
	}
	if snapshot.ArchiveVersion != 1 || snapshot.ArchiveSHA256 != archiveSHA || snapshot.CodexSessionID != "codex-session-safe-id" {
		t.Fatalf("confirmed snapshot = %#v", snapshot)
	}
	runner.snapshot = snapshot
	firstPodUID := firstPod.PodUID
	runner.removePod()

	firstPVC := first.PVCName
	role.ConfigOverlay = "model_reasoning_effort = \"high\""
	role.UpdatedAt = role.UpdatedAt.Add(time.Minute)
	request.Role = role
	request.UserMessage = "next revision"
	request.PreparedPrompt = "safe continuation prompt"
	request.SourcePostID = "post-2"
	request.ReplyRootID = "post-1"
	second, err := svc.EnqueueAgentTurn(context.Background(), request)
	if err != nil {
		t.Fatalf("second EnqueueAgentTurn() error = %v", err)
	}
	if second.SessionKey != first.SessionKey || len(store.revisions) != 2 || len(runner.sessionRuns) != 2 || len(base.sessionTurns) != 2 {
		t.Fatalf("second component state: result=%#v revisions=%#v runs=%#v turns=%#v", second, store.revisions, runner.sessionRuns, base.sessionTurns)
	}
	secondRun := runner.sessionRuns[1]
	secondPod := runner.starts[1]
	secondState := store.states[second.SessionKey]
	if secondRun.RuntimeRevisionDigest == firstRun.RuntimeRevisionDigest || !secondRun.AllowPodRecreation || secondPod.Recreation.Action != runtimerepo.AgentSessionPodCreated || secondPod.PodUID == firstPodUID {
		t.Fatalf("changed runtime input/start = %#v / %#v", secondRun, secondPod)
	}
	if runner.restoredCodexSessionID != "codex-session-safe-id" || runner.restoredArchiveSHA256 != archiveSHA {
		t.Fatalf("restored component state: codex_session_id=%q archive_sha256=%q", runner.restoredCodexSessionID, runner.restoredArchiveSHA256)
	}
	if secondState.DesiredRuntimeRevisionID == firstState.DesiredRuntimeRevisionID || secondState.DesiredRuntimeRevisionID != secondState.AppliedRuntimeRevisionID || base.sessionTurns[1].RuntimeRevisionID != secondState.AppliedRuntimeRevisionID {
		t.Fatalf("changed revision bindings: state=%#v turns=%#v", secondState, base.sessionTurns)
	}
	if firstPVC != second.PVCName || secondRun.SessionKey != firstRun.SessionKey {
		t.Fatalf("session identity/PVC changed across revision: first=%#v second=%#v", first, second)
	}
	role.ConfigOverlay = "model_reasoning_effort = \"xhigh\""
	role.UpdatedAt = role.UpdatedAt.Add(time.Minute)
	request.Role = role
	request.UserMessage = "queued after pending revision"
	request.PreparedPrompt = "safe third prompt"
	request.SourcePostID = "post-3"
	third, err := svc.EnqueueAgentTurn(context.Background(), request)
	if err != nil {
		t.Fatalf("third EnqueueAgentTurn() error = %v", err)
	}
	thirdRun := runner.sessionRuns[2]
	thirdPod := runner.starts[2]
	thirdState := store.states[third.SessionKey]
	if thirdRun.RuntimeRevisionDigest != secondRun.RuntimeRevisionDigest || thirdState.AppliedRuntimeRevisionID != secondState.AppliedRuntimeRevisionID || thirdPod.Recreation.Action != runtimerepo.AgentSessionPodReused || thirdPod.PodUID != secondPod.PodUID {
		t.Fatalf("existing FIFO head revision was skipped: second=%#v third=%#v pod=%#v state=%#v", secondRun, thirdRun, thirdPod, thirdState)
	}
	if len(store.revisions) != 3 || len(base.sessionTurns) != 3 || base.sessionTurns[2].RuntimeRevisionID == base.sessionTurns[1].RuntimeRevisionID {
		t.Fatalf("third turn did not retain its own exact revision: revisions=%#v turns=%#v", store.revisions, base.sessionTurns)
	}
}

type runtimeRevisionFakeStore struct {
	*fakeAdminStore
	revisions       map[string]entity.RuntimeRevision
	states          map[string]entity.AgentSessionRuntimeRevisionState
	archives        map[int64][]entity.AgentSessionArchive
	secretRevisions map[string]entity.RuntimeSecretBindingRevision
}

func (store *runtimeRevisionFakeStore) UpsertAgentSession(ctx context.Context, input adminrepo.UpsertAgentSessionInput) (entity.AgentSession, bool, error) {
	session, created, err := store.fakeAdminStore.UpsertAgentSession(ctx, input)
	if err != nil {
		return entity.AgentSession{}, false, err
	}
	if session.OpenAIAccountName == "" {
		session.OpenAIAccountName = input.OpenAIAccountName
		store.agentSessions[session.SessionKey] = session
	}
	return session, created, nil
}

func (store *runtimeRevisionFakeStore) UpdateAgentSessionRuntime(ctx context.Context, input adminrepo.UpdateAgentSessionRuntimeInput) (entity.AgentSession, error) {
	session, err := store.fakeAdminStore.UpdateAgentSessionRuntime(ctx, input)
	if err != nil {
		return entity.AgentSession{}, err
	}
	state := store.states[input.SessionKey]
	state.SessionID = session.ID
	state.SessionKey = session.SessionKey
	if input.DesiredRuntimeRevisionID > 0 {
		state.DesiredRuntimeRevisionID = input.DesiredRuntimeRevisionID
	}
	if input.AppliedRuntimeRevisionID > 0 {
		state.AppliedRuntimeRevisionID = input.AppliedRuntimeRevisionID
	}
	store.states[input.SessionKey] = state
	return session, nil
}

func (store *runtimeRevisionFakeStore) EnsureRuntimeRevision(_ context.Context, input adminrepo.EnsureRuntimeRevisionInput) (entity.RuntimeRevision, error) {
	if existing, ok := store.revisions[input.Digest]; ok {
		if existing.Manifest != input.Manifest || existing.AccountAlias != input.AccountAlias || existing.AuthorizationRevision != input.AuthorizationRevision {
			return entity.RuntimeRevision{}, adminrepo.ErrRuntimeRevisionConflict
		}
		return existing, nil
	}
	revision := entity.RuntimeRevision{
		ID: int64(len(store.revisions) + 1), Digest: input.Digest, Manifest: input.Manifest,
		AccountAlias: input.AccountAlias, AuthorizationRevision: input.AuthorizationRevision, CreatedAt: time.Now().UTC(),
	}
	store.revisions[input.Digest] = revision
	return revision, nil
}

func (store *runtimeRevisionFakeStore) GetRuntimeRevision(_ context.Context, id int64) (entity.RuntimeRevision, error) {
	for _, revision := range store.revisions {
		if revision.ID == id {
			return revision, nil
		}
	}
	return entity.RuntimeRevision{}, adminrepo.ErrNotFound
}

func (store *runtimeRevisionFakeStore) GetAgentSessionRuntimeRevisionState(_ context.Context, sessionKey string) (entity.AgentSessionRuntimeRevisionState, error) {
	state, ok := store.states[sessionKey]
	if !ok {
		return entity.AgentSessionRuntimeRevisionState{}, adminrepo.ErrNotFound
	}
	return state, nil
}

func (store *runtimeRevisionFakeStore) ObserveRuntimeSecretBinding(_ context.Context, input adminrepo.ObserveRuntimeSecretBindingInput) (entity.RuntimeSecretBindingRevision, error) {
	if store.secretRevisions == nil {
		store.secretRevisions = make(map[string]entity.RuntimeSecretBindingRevision)
	}
	if existing, ok := store.secretRevisions[input.BindingKey]; ok && existing.SecretName == input.SecretName && existing.SecretKey == input.SecretKey && existing.IntegritySHA256 == input.IntegritySHA256 {
		return existing, nil
	}
	revision := int64(1)
	if existing, ok := store.secretRevisions[input.BindingKey]; ok {
		revision = existing.Revision + 1
	}
	observed := entity.RuntimeSecretBindingRevision{
		BindingKey: input.BindingKey, SecretName: input.SecretName, SecretKey: input.SecretKey,
		IntegritySHA256: input.IntegritySHA256, Revision: revision, UpdatedAt: time.Now().UTC(),
	}
	store.secretRevisions[input.BindingKey] = observed
	return observed, nil
}

func (store *runtimeRevisionFakeStore) SetAgentSessionDesiredRuntimeRevision(_ context.Context, input adminrepo.SetAgentSessionDesiredRuntimeRevisionInput) (entity.AgentSessionRuntimeRevisionState, error) {
	session, ok := store.agentSessions[input.SessionKey]
	if !ok {
		return entity.AgentSessionRuntimeRevisionState{}, adminrepo.ErrNotFound
	}
	state := store.states[input.SessionKey]
	state.SessionID = session.ID
	state.SessionKey = session.SessionKey
	state.DesiredRuntimeRevisionID = input.RuntimeRevisionID
	store.states[input.SessionKey] = state
	return state, nil
}

func (store *runtimeRevisionFakeStore) AcquireAgentSessionRuntimeLease(_ context.Context, input adminrepo.AcquireAgentSessionRuntimeLeaseInput) (entity.AgentSessionRuntimeRevisionState, error) {
	state, ok := store.states[input.SessionKey]
	if !ok || state.DesiredRuntimeRevisionID != input.DesiredRuntimeRevisionID || state.AppliedRuntimeRevisionID != input.ExpectedAppliedRuntimeRevisionID || state.AppliedPodUID != input.ExpectedPodUID || state.ReconcileLeaseToken != "" {
		return entity.AgentSessionRuntimeRevisionState{}, adminrepo.ErrRuntimeReconciliationConflict
	}
	state.ReconcileLeaseToken = input.LeaseToken
	state.ReconcileLeaseExpiresAt = time.Now().UTC().Add(time.Duration(input.LeaseSeconds) * time.Second)
	store.states[input.SessionKey] = state
	return state, nil
}

func (store *runtimeRevisionFakeStore) RefreshAgentSessionRuntimeLease(_ context.Context, input adminrepo.AcquireAgentSessionRuntimeLeaseInput) (entity.AgentSessionRuntimeRevisionState, error) {
	state, ok := store.states[input.SessionKey]
	if !ok || state.DesiredRuntimeRevisionID != input.DesiredRuntimeRevisionID || state.AppliedRuntimeRevisionID != input.ExpectedAppliedRuntimeRevisionID || state.AppliedPodUID != input.ExpectedPodUID || state.ReconcileLeaseToken != input.LeaseToken || time.Now().UTC().After(state.ReconcileLeaseExpiresAt) {
		return entity.AgentSessionRuntimeRevisionState{}, adminrepo.ErrRuntimeReconciliationConflict
	}
	state.ReconcileLeaseExpiresAt = time.Now().UTC().Add(time.Duration(input.LeaseSeconds) * time.Second)
	store.states[input.SessionKey] = state
	return state, nil
}

func (store *runtimeRevisionFakeStore) MarkAgentSessionRuntimeApplied(_ context.Context, input adminrepo.MarkAgentSessionRuntimeAppliedInput) (entity.AgentSessionRuntimeRevisionState, error) {
	state, ok := store.states[input.SessionKey]
	if !ok || state.DesiredRuntimeRevisionID != input.RuntimeRevisionID || state.AppliedRuntimeRevisionID != input.ExpectedAppliedRuntimeRevisionID || state.AppliedPodUID != input.ExpectedPodUID || state.ReconcileLeaseToken != input.LeaseToken {
		return entity.AgentSessionRuntimeRevisionState{}, adminrepo.ErrRuntimeReconciliationConflict
	}
	state.AppliedRuntimeRevisionID = input.RuntimeRevisionID
	state.AppliedPodUID = input.AppliedPodUID
	state.ReconcileLeaseToken = ""
	state.ReconcileLeaseExpiresAt = time.Time{}
	store.states[input.SessionKey] = state
	return state, nil
}

func (store *runtimeRevisionFakeStore) ReleaseAgentSessionRuntimeLease(_ context.Context, input adminrepo.ReleaseAgentSessionRuntimeLeaseInput) error {
	state, ok := store.states[input.SessionKey]
	if !ok || state.ReconcileLeaseToken != input.LeaseToken {
		return adminrepo.ErrRuntimeReconciliationConflict
	}
	state.ReconcileLeaseToken = ""
	state.ReconcileLeaseExpiresAt = time.Time{}
	store.states[input.SessionKey] = state
	return nil
}

func (store *runtimeRevisionFakeStore) GetNextQueuedAgentSessionRuntimeRevision(_ context.Context, sessionID int64) (entity.RuntimeRevision, error) {
	for _, turn := range store.sessionTurns {
		if turn.SessionID != sessionID || turn.Status != agentSessionTurnQueued || turn.RuntimeRevisionID == 0 {
			continue
		}
		return store.GetRuntimeRevision(context.Background(), turn.RuntimeRevisionID)
	}
	return entity.RuntimeRevision{}, adminrepo.ErrNotFound
}

func (store *runtimeRevisionFakeStore) GetLatestAgentSessionArchive(_ context.Context, sessionID int64) (entity.AgentSessionArchive, error) {
	archives := store.archives[sessionID]
	if len(archives) == 0 {
		return entity.AgentSessionArchive{}, adminrepo.ErrNotFound
	}
	return archives[len(archives)-1], nil
}

func (store *runtimeRevisionFakeStore) CompleteAgentSessionTurnWithArchive(_ context.Context, input adminrepo.CompleteAgentSessionTurnWithArchiveInput) (entity.AgentSessionCompletion, error) {
	session, ok := store.agentSessions[input.SessionKey]
	if !ok {
		return entity.AgentSessionCompletion{}, adminrepo.ErrNotFound
	}
	turnIndex := -1
	for index := range store.sessionTurns {
		if store.sessionTurns[index].ID == input.TurnID && store.sessionTurns[index].SessionID == session.ID {
			turnIndex = index
			break
		}
	}
	if turnIndex < 0 {
		return entity.AgentSessionCompletion{}, adminrepo.ErrNotFound
	}
	turn := store.sessionTurns[turnIndex]
	if agentSessionTurnTerminal(turn.Status) {
		archive, _ := store.GetLatestAgentSessionArchive(context.Background(), session.ID)
		return entity.AgentSessionCompletion{Turn: turn, Session: session, Archive: archive, AlreadyCompleted: true}, nil
	}
	raw, err := base64.StdEncoding.DecodeString(input.SessionArchiveGzipBase64)
	if err != nil {
		return entity.AgentSessionCompletion{}, err
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != input.ArchiveSHA256 || int64(len(raw)) != input.ArchiveSizeBytes {
		return entity.AgentSessionCompletion{}, fmt.Errorf("archive metadata mismatch")
	}
	archive := entity.AgentSessionArchive{
		ID: int64(len(store.archives[session.ID]) + 1), SessionID: session.ID,
		Version: int64(len(store.archives[session.ID]) + 1), CodexSessionID: input.CodexSessionID,
		PayloadGzipBase64: input.SessionArchiveGzipBase64, SHA256: input.ArchiveSHA256,
		SizeBytes: input.ArchiveSizeBytes, CreatedAt: time.Now().UTC(),
	}
	store.archives[session.ID] = append(store.archives[session.ID], archive)
	turn.Status = input.TurnStatus
	turn.FinalMessage = input.FinalMessage
	turn.ErrorMessage = input.ErrorMessage
	store.sessionTurns[turnIndex] = turn
	session.CodexSessionID = input.CodexSessionID
	session.SessionArchiveGzipBase64 = input.SessionArchiveGzipBase64
	session.Status = input.SessionStatus
	session.ActiveTurnID = 0
	session.ActiveRunID = ""
	store.agentSessions[session.SessionKey] = session
	return entity.AgentSessionCompletion{Turn: turn, Session: session, Archive: archive}, nil
}

type restoringRuntimeRunner struct {
	*fakeRuntimeRunner
	snapshot               AgentSessionSnapshot
	restoredCodexSessionID string
	restoredArchiveSHA256  string
	currentPodUID          string
	currentRevisionDigest  string
	podSequence            int
	starts                 []runtimerepo.StartedAgentSession
}

func (runner *restoringRuntimeRunner) StartAgentSession(ctx context.Context, input runtimerepo.AgentSessionPodInput) (runtimerepo.StartedAgentSession, error) {
	if runner.snapshot.ArchiveVersion > 0 {
		raw, err := base64.StdEncoding.DecodeString(runner.snapshot.SessionArchiveGzipBase64)
		if err != nil {
			return runtimerepo.StartedAgentSession{}, err
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != runner.snapshot.ArchiveSHA256 || int64(len(raw)) != runner.snapshot.ArchiveSizeBytes {
			return runtimerepo.StartedAgentSession{}, fmt.Errorf("restore metadata mismatch")
		}
		runner.restoredCodexSessionID = runner.snapshot.CodexSessionID
		runner.restoredArchiveSHA256 = runner.snapshot.ArchiveSHA256
	}
	started, err := runner.fakeRuntimeRunner.StartAgentSession(ctx, input)
	if err != nil {
		return started, err
	}
	previousPodUID := runner.currentPodUID
	action := runtimerepo.AgentSessionPodReused
	switch {
	case runner.currentPodUID == "":
		if input.RequirePodReuse {
			action = runtimerepo.AgentSessionPodReuseRequired
			started.Created = false
			break
		}
		runner.podSequence++
		runner.currentPodUID = fmt.Sprintf("safe-runtime-pod-%d", runner.podSequence)
		runner.currentRevisionDigest = input.RuntimeRevisionDigest
		action = runtimerepo.AgentSessionPodCreated
	case runner.currentRevisionDigest == input.RuntimeRevisionDigest:
		started.Created = false
	case input.RequirePodReuse:
		action = runtimerepo.AgentSessionPodReuseRequired
		started.Created = false
	case !input.AllowPodRecreation:
		action = runtimerepo.AgentSessionPodRecreationDeferred
		started.Created = false
	default:
		runner.podSequence++
		runner.currentPodUID = fmt.Sprintf("safe-runtime-pod-%d", runner.podSequence)
		runner.currentRevisionDigest = input.RuntimeRevisionDigest
		action = runtimerepo.AgentSessionPodRecreated
	}
	started.PodUID = runner.currentPodUID
	started.RuntimeRevisionDigest = input.RuntimeRevisionDigest
	started.Recreation = runtimerepo.AgentSessionPodRecreation{
		Action: action, PreviousPodUID: previousPodUID, CurrentPodUID: runner.currentPodUID,
		RevisionDigest: input.RuntimeRevisionDigest,
	}
	runner.starts = append(runner.starts, started)
	return started, nil
}

func (runner *restoringRuntimeRunner) removePod() {
	runner.currentPodUID = ""
	runner.currentRevisionDigest = ""
}

var _ adminrepo.RuntimeRevisionRepository = (*runtimeRevisionFakeStore)(nil)
var _ adminrepo.AgentSessionArchiveRepository = (*runtimeRevisionFakeStore)(nil)
var _ runtimerepo.Runner = (*restoringRuntimeRunner)(nil)
