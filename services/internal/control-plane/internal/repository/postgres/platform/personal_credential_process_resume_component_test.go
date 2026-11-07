package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// personalProcessResumeState carries only synthetic opaque references and expected digests into
// a separate test process. Credential values and the loopback disposable DSN are never serialized.
// personalProcessResumeState передаёт отдельному test process лишь synthetic refs и ожидаемые
// digests. Значения credentials и loopback disposable DSN в файл не сериализуются.
type personalProcessResumeState struct {
	ParentPID        int
	ConversationRef  string
	SessionRef       string
	RunRef           string
	ConnectionRef    string
	CredentialRef    string
	ActorID          string
	NativeSessionID  string
	ExpectNative     bool
	OriginalReadRef  string
	OriginalReadHash string
	FirstMessage     string
	Attempt          string
}

// personalProcessRead performs the actual owner invocation/claim/Begin RPC/receipt cycle and
// checks that the persisted root and credential pin belong to the original synthetic user.
// Provider output and transport peer authentication remain explicit fixture observations.
// personalProcessRead проходит настоящий owner invocation/claim/Begin RPC/receipt и проверяет
// принадлежность сохранённого root и credential pin исходному synthetic user. Provider output
// и transport peer authentication остаются явными наблюдениями оснастки.
func personalProcessRead(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, worker value.Principal, lease map[string]any, state personalProcessResumeState) string {
	t.Helper()
	invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": stringMap(lease, "runRef"), "node_ref": stringMap(lease, "nodeRef"), "connection_ref": state.ConnectionRef, "capability_key": "gitlab.project.metadata.read", "idempotency_key": "personal-process-read-" + state.Attempt}, map[string]any{})
	if err != nil {
		t.Fatal("resolve process-resume personal read", err)
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	work, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-process-gateway", 1)
	if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") || work[0]["credential"] != nil {
		t.Fatal("process-resume claim lost exact personal intent", err)
	}
	request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: "personal-process-gateway"}
	response, err := personalRuntimeRPCClient(t, service).BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
	if err != nil || !response.GetAdmitted() || response.GetCredentialRevision().GetRef() != state.CredentialRef {
		t.Fatal("process-resume Begin substituted the initiator's key", err)
	}
	var initiator string
	if err := repository.pool.QueryRow(ctx, `SELECT root.initiated_by::text FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id AND invocation.organization_id=pin.organization_id
JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
WHERE invocation.ref=$1 AND root.initiated_by=pin.subject_id`, request.InvocationRef).Scan(&initiator); err != nil || initiator != state.ActorID {
		t.Fatal("process-resume root lineage changed", err)
	}
	summary := string(asJSON(map[string]any{"id": 6, "path_with_namespace": "fixture/project", "default_branch": "main", "visibility": "private", "archived": false}))
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "personal-process-complete-" + state.Attempt}, Payload: command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, Success: true, EffectKey: stringMap(work[0], "effectKey"), InputDigest: stringMap(work[0], "inputDigest"), ProviderEffectRef: "gitlab-project:6", ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}}); err != nil {
		t.Fatal("complete process-resume personal read", err)
	}
	return request.InvocationRef
}

// personalProcessReceiptHash reads one successful invocation without changing its lease or
// receipt. Only a digest leaves the isolated fixture; immutable history must survive rotation.
// personalProcessReceiptHash читает один успешный invocation без изменения lease либо receipt.
// За пределы isolated fixture выходит лишь digest; история должна переживать rotation.
func personalProcessReceiptHash(t *testing.T, ctx context.Context, repository *Repository, ref string) string {
	t.Helper()
	var raw []byte
	if err := repository.pool.QueryRow(ctx, `SELECT to_jsonb(invocation)::text FROM control_plane.integration_invocations invocation WHERE ref=$1 AND state='SUCCEEDED'`, ref).Scan(&raw); err != nil {
		t.Fatal("read preserved successful personal invocation", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// completePersonalProcessTurn publishes a synthetic Codex storage tuple through ordinary owner
// completion. It proves transaction/identity behavior, not actual native rollout or object bytes.
// completePersonalProcessTurn публикует synthetic Codex storage tuple штатным owner completion.
// Проверяются transaction/identity, а не реальные байты native rollout либо объекта.
func completePersonalProcessTurn(t *testing.T, ctx context.Context, service *platformservice.Service, worker value.Principal, lease map[string]any, state personalProcessResumeState) {
	t.Helper()
	result, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "personal-process-turn-" + state.Attempt}, Payload: command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, ResultSummary: "Synthetic personal process result", Usage: turnUsageFixture(), CodexSessionID: state.NativeSessionID, ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/11/07/rollout-2026-11-07T00-00-00-" + state.NativeSessionID + ".jsonl", ArchiveSHA256: strings.Repeat("a", 64), ArchiveSizeBytes: 128}})
	if err != nil || result.Run == nil || result.Run.State != "SUCCEEDED" {
		t.Fatal("complete process-resume turn", err)
	}
}

// runPersonalResumeChild launches this exact race-enabled test binary with an explicit
// environment and no inherited host credentials. The only DSN targets the caller's disposable
// loopback application database; state is a private regular file under a private directory.
// runPersonalResumeChild запускает тот же race-enabled test binary с явным окружением без
// host credentials. Единственный DSN указывает на disposable loopback application DB caller;
// state хранится в private regular file внутри private directory.
func runPersonalResumeChild(t *testing.T, ctx context.Context, repository *Repository, state personalProcessResumeState) {
	t.Helper()
	directory := t.TempDir()
	if os.Chmod(directory, 0o700) != nil {
		t.Fatal("protect process-resume state directory")
	}
	path := filepath.Join(directory, "state.json")
	raw, err := json.Marshal(state)
	if err != nil || os.WriteFile(path, raw, 0o600) != nil {
		t.Fatal("write synthetic process-resume state")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("locate exact component test executable")
	}
	childContext, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	child := exec.CommandContext(childContext, binary, "-test.run=^TestPersonalCredentialProcessResumeChild$", "-test.timeout=40s", "-test.v")
	child.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + directory, "KODEX_PERSONAL_RESUME_STATE_FILE=" + path, "KODEX_PERSONAL_RESUME_DSN=" + repository.pool.Config().ConnConfig.ConnString()}
	output, err := child.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("PERSONAL_PROCESS_RESUME_PASS")) {
		t.Fatalf("separate owner process failed synthetic %s rejoin: %v\n%s", state.Attempt, err, output)
	}
	t.Log("separate owner process completed synthetic rejoin", state.Attempt)
}

// TestPersonalCredentialProcessResumeChild is invoked only by the parent native fixture. It
// starts a fresh owner pool/repository/bootstrap, claims durable queued work and repeats current
// personal admission through the real typed Begin RPC. A personal integration key is not a
// native-runtime dependency: rotation preserves compatible context but must select the new pin
// through fresh owner admission. Model/runtime dependencies retain their separate resume guards.
// TestPersonalCredentialProcessResumeChild вызывается лишь родительской native fixture. Новый
// owner pool/repository/bootstrap захватывает durable queued work и повторяет personal admission
// через настоящий typed Begin RPC. Личный ключ интеграции не входит в native-runtime dependency:
// rotation сохраняет совместимый контекст, но новый pin выбирается свежим owner admission.
// Зависимости модели и runtime сохраняют свои отдельные resume guards.
func TestPersonalCredentialProcessResumeChild(t *testing.T) {
	path, dsn := os.Getenv("KODEX_PERSONAL_RESUME_STATE_FILE"), os.Getenv("KODEX_PERSONAL_RESUME_DSN")
	if path == "" && dsn == "" {
		t.Skip("internal process-resume child requires its parent disposable fixture")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil || config.ConnConfig.User == "postgres" || config.ConnConfig.Port < 1024 || config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost" || !regexp.MustCompile(`^kodex_assistant_test_[a-f0-9]{16}$`).MatchString(config.ConnConfig.Database) {
		t.Fatal("child requires an isolated loopback application database")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > 16384 {
		t.Fatal("child synthetic state is not private and bounded")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read child synthetic state")
	}
	var state personalProcessResumeState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || state.ParentPID <= 0 || state.ParentPID == os.Getpid() || state.ParentPID != os.Getppid() {
		t.Fatal("resume did not cross an actual OS process boundary")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open child isolated pool")
	}
	defer pool.Close()
	repository := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner, worker := resolveSystemGrantComponentPrincipals(t, ctx, repository)
	if owner.ActorID != state.ActorID || personalProcessReceiptHash(t, ctx, repository, state.OriginalReadRef) != state.OriginalReadHash {
		t.Fatal("process reconstruction changed original identity or successful receipt")
	}
	lease := claimComponentExecution(t, ctx, service, worker, state.RunRef, "personal-process-child-"+state.Attempt)
	if stringMap(lease, "sessionRef") != state.SessionRef || (stringMap(lease, "codexSessionID") == state.NativeSessionID) != state.ExpectNative || !state.ExpectNative && stringMap(lease, "codexSessionID") != "" {
		var previousJSON, currentJSON []byte
		if err := pool.QueryRow(ctx, `SELECT revision.safe_snapshot FROM control_plane.session_storage storage JOIN control_plane.sessions session ON session.id=storage.session_id JOIN control_plane.runtime_revisions revision ON revision.id=storage.runtime_revision_id WHERE session.ref=$1`, state.SessionRef).Scan(&previousJSON); err != nil {
			t.Fatal("read prior synthetic compatibility snapshot", err)
		}
		if err := pool.QueryRow(ctx, "SELECT safe_snapshot FROM control_plane.runtime_revisions WHERE ref=$1", stringMap(lease, "runtimeRevisionRef")).Scan(&currentJSON); err != nil {
			t.Fatal("read current synthetic compatibility snapshot", err)
		}
		var previous, current map[string]any
		if json.Unmarshal(previousJSON, &previous) != nil || json.Unmarshal(currentJSON, &current) != nil {
			t.Fatal("decode synthetic compatibility snapshots")
		}
		_, previousMCPError := runtimeSessionMCPDependencies(previous)
		_, currentMCPError := runtimeSessionMCPDependencies(current)
		t.Log("synthetic compatibility", runtimeSessionResumeCompatible(previous, current), "MCP validity", previousMCPError == nil, currentMCPError == nil, "session match", stringMap(lease, "sessionRef") == state.SessionRef, "native match", stringMap(lease, "codexSessionID") == state.NativeSessionID)
		for key, value := range previous {
			before, _ := json.Marshal(value)
			after, _ := json.Marshal(current[key])
			if !bytes.Equal(before, after) {
				t.Log("changed synthetic snapshot field", key)
			}
		}
		t.Fatal("process reconstruction reused a foreign session or changed native compatibility")
	}
	found := false
	for _, message := range runtimeRevisionSessionContext(lease["sessionContext"]) {
		found = found || message.Role == "USER" && message.Content == state.FirstMessage
	}
	if !found {
		t.Fatal("process reconstruction lost canonical original user history")
	}
	personalProcessRead(t, ctx, repository, service, worker, lease, state)
	completePersonalProcessTurn(t, ctx, service, worker, lease, state)
	if personalProcessReceiptHash(t, ctx, repository, state.OriginalReadRef) != state.OriginalReadHash {
		t.Fatal("resumed turn rewrote a completed external receipt")
	}
	t.Log("PERSONAL_PROCESS_RESUME_PASS")
}

// assertPersonalArchiveProcessResume drives owner snapshot/delete/restore/GC with a personal
// SYSTEM assistant, then makes two separate processes claim the same durable conversation.
// Revoke blocks the next turn before state changes, replacement uses a new pin with the same
// compatible native context, and original successful receipt/history remain. Object/provider tuples are
// synthetic; this is not an installed Pod restart, real S3/PVC transfer or live model proof.
// assertPersonalArchiveProcessResume исполняет owner snapshot/delete/restore/GC для personal
// SYSTEM assistant и даёт двум отдельным процессам захватить ту же durable conversation.
// Revoke блокирует turn до записи; replacement использует новый pin и совместимую native session,
// сохраняя прежний успешный receipt/history. Object/rollout/provider tuples синтетические;
// installed Pod restart, настоящий S3/PVC transfer либо живая модель здесь не проверяются.
func assertPersonalArchiveProcessResume(t *testing.T, ctx context.Context, repository *Repository, service, workers *platformservice.Service, owner, worker value.Principal, connectionRef string) {
	t.Helper()
	execute := newComponentCommandExecutor(t, ctx, service, "personal-process-parent-")
	replace := func(key string) entity.PersonalIntegrationCredential {
		t.Helper()
		self := owner
		self.Permission = "platform.personal-integrations.get"
		binding, err := service.GetPersonalIntegrationCredential(ctx, self, connectionRef)
		if err != nil {
			t.Fatal(err)
		}
		self.Permission = "platform.personal-integrations.configure"
		if _, err := service.ConfigurePersonalIntegrationCredential(ctx, self, value.Mutation{IdempotencyKey: "personal-process-" + key, ExpectedVersion: &binding.Version}, connectionRef, binding.ConnectionVersion, []byte("synthetic-process-key-"+key)); err != nil {
			t.Fatal(err)
		}
		return verifyPersonalAssistantKey(t, ctx, repository, service, workers, owner, connectionRef, "personal-process-"+key)
	}
	binding := replace("initial")
	conversation := execute(command.CreateAssistantConversation, owner, "conversation", nil, command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
	state := personalProcessResumeState{ParentPID: os.Getpid(), ConversationRef: conversation.Ref, SessionRef: conversation.SessionRef, ConnectionRef: connectionRef, CredentialRef: binding.Credential.Ref, ActorID: owner.ActorID, NativeSessionID: "00000000-0000-4000-8000-000000000073", FirstMessage: "Synthetic personal archive context retained across owner processes", Attempt: "initial"}
	queued := execute(command.AddAssistantTurn, owner, "first-turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: state.FirstMessage, DeliveryMode: "QUEUE"}).Conversation
	lease := claimComponentExecution(t, ctx, workers, worker, queued.Turns[0].RunRef, "personal-process-first")
	state.OriginalReadRef = personalProcessRead(t, ctx, repository, workers, worker, lease, state)
	completePersonalProcessTurn(t, ctx, workers, worker, lease, state)
	state.OriginalReadHash = personalProcessReceiptHash(t, ctx, repository, state.OriginalReadRef)
	restore := prepareAssistantSessionArchiveRoundTrip(t, ctx, repository, service, worker, owner, conversation.Ref, conversation.SessionRef, "personal-process")
	execute(command.AddAssistantTurn, owner, "after-archive", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic resumed read without repeating the original message", DeliveryMode: "QUEUE"})
	restore()
	var pending int64
	if err := repository.pool.QueryRow(ctx, `SELECT count(*),COALESCE(min(ref),'') FROM control_plane.runs WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1) AND state IN ('QUEUED','RUNNING')`, state.SessionRef).Scan(&pending, &state.RunRef); err != nil || pending != 1 {
		t.Fatal("read exact restored queued run", err)
	}
	state.ExpectNative, state.Attempt = true, "restored"
	runPersonalResumeChild(t, ctx, repository, state)
	self := owner
	self.Permission = "platform.personal-integrations.revoke"
	execute(command.RevokePersonalIntegrationCredential, self, "revoke", &binding.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connectionRef})
	var before, after int64
	if err := repository.pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.session_turns").Scan(&before); err != nil {
		t.Fatal(err)
	}
	_, err := service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-process-refused-after-restart"}, Payload: command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic revoked key cannot resume", DeliveryMode: "QUEUE"}})
	var problem *platformservice.IntegrationCredentialPreflightError
	if !errors.As(err, &problem) || len(problem.Issues) != 1 || problem.Issues[0].Problem != entity.IntegrationCredentialRevoked {
		t.Fatal("process-resumed conversation bypassed current revocation", err)
	}
	if err := repository.pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.session_turns").Scan(&after); err != nil || before != after {
		t.Fatal("revoked continuation created a turn", err)
	}
	binding = replace("replacement")
	if binding.Credential.Ref == state.CredentialRef {
		t.Fatal("personal replacement reused an old immutable credential revision")
	}
	state.CredentialRef, state.ExpectNative, state.Attempt = binding.Credential.Ref, true, "replacement"
	execute(command.AddAssistantTurn, owner, "after-replacement", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic replacement continues the original conversation", DeliveryMode: "QUEUE"})
	if err := repository.pool.QueryRow(ctx, `SELECT count(*),COALESCE(min(ref),'') FROM control_plane.runs WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1) AND state IN ('QUEUED','RUNNING')`, state.SessionRef).Scan(&pending, &state.RunRef); err != nil || pending != 1 {
		t.Fatal(err)
	}
	runPersonalResumeChild(t, ctx, repository, state)
	execute(command.RevokePersonalIntegrationCredential, self, "cleanup-revoke", &binding.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connectionRef})
}
