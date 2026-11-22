package platform

import (
	"context"
	_ "embed"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// emailAuthorizationFixtureState hashes only the fixture's owner rows before and after a successful SQL read whose driver Scan fails; no row content is printed or exported.
// emailAuthorizationFixtureState хеширует только owner rows фикстуры до и после успешного SQL read с отказом driver Scan; содержимое строк не печатается и не экспортируется.
//
//go:embed testdata/sql/email_authorization_fixture_state.sql
var emailAuthorizationFixtureState string

// emailAuthorizationCommitDelay keys only the successful native COMMIT observation selected after the exact stored authorization read; it neither changes the query nor fabricates a transaction result.
// emailAuthorizationCommitDelay адресует только успешный native COMMIT после exact stored authorization read; не меняет query и не подменяет результат транзакции.
type emailAuthorizationCommitDelay struct{}

// emailAuthorizationDeadlineTrace preserves the existing SQL-boundary tracer and delays one successful commit acknowledgement past a real finite access binding. Other owner transactions remain untouched.
// emailAuthorizationDeadlineTrace сохраняет существующий SQL-boundary tracer и задерживает один успешный commit ACK за срок реальной конечной access binding. Другие owner transactions не меняются.
type emailAuthorizationDeadlineTrace struct {
	base     pgx.QueryTracer
	armed    atomic.Bool
	read     atomic.Bool
	hits     atomic.Int32
	deadline atomic.Pointer[time.Time]
	scanWant atomic.Bool
	scanArm  atomic.Bool
	scanHits atomic.Int32
}

// emailAuthorizationBoolCodec keeps native PostgreSQL bool decoding and injects one client-side Scan failure without aborting the SQL transaction.
// emailAuthorizationBoolCodec сохраняет native PostgreSQL bool decoding и добавляет один client-side Scan failure без abort SQL transaction.
type emailAuthorizationBoolCodec struct {
	pgtype.BoolCodec
	trace *emailAuthorizationDeadlineTrace
}

// emailAuthorizationBoolScan delegates every ordinary result to the real driver's plan and faults only after the exact access-subject query has armed the observation.
// emailAuthorizationBoolScan передаёт обычные результаты настоящему driver plan и отказывает только после exact access-subject query, включившего наблюдение.
type emailAuthorizationBoolScan struct {
	plan  pgtype.ScanPlan
	trace *emailAuthorizationDeadlineTrace
}

// PlanScan always wraps the native plan so a prepared statement cannot bypass the later one-shot fault through a cached decoding plan.
// PlanScan всегда оборачивает native plan, чтобы prepared statement не обходил поздний one-shot fault через cached decoding plan.
func (codec emailAuthorizationBoolCodec) PlanScan(mapping *pgtype.Map, oid uint32, format int16, target any) pgtype.ScanPlan {
	plan := codec.BoolCodec.PlanScan(mapping, oid, format, target)
	if plan == nil {
		return nil
	}
	return emailAuthorizationBoolScan{plan: plan, trace: codec.trace}
}

// Scan faults once after real successful SELECT execution, preserving the transaction for an exact state readback and same-intent recovery.
// Scan отказывает один раз после настоящего успешного SELECT, сохраняя transaction для exact state readback и восстановления того же intent.
func (scan emailAuthorizationBoolScan) Scan(source []byte, target any) error {
	if scan.trace.scanArm.CompareAndSwap(true, false) {
		scan.trace.scanHits.Add(1)
		return errors.New("synthetic mailbox authority scan failure")
	}
	return scan.plan.Scan(source, target)
}

// configureCodec installs the fault observer only on new fixture connections; no process-wide driver registration or production codec changes are made.
// configureCodec устанавливает fault observer только в новых fixture connections; process-wide driver registration и production codecs не меняются.
func (trace *emailAuthorizationDeadlineTrace) configureCodec(_ context.Context, connection *pgx.Conn) error {
	connection.TypeMap().RegisterType(&pgtype.Type{Name: "bool", OID: pgtype.BoolOID, Codec: emailAuthorizationBoolCodec{trace: trace}})
	return nil
}

// TraceQueryStart arms the COMMIT delay only after the authorization ledger read, avoiding principal-resolution or unrelated owner commits.
// TraceQueryStart допускает задержку COMMIT только после чтения authorization ledger, исключая principal-resolution и несвязанные owner commits.
func (trace *emailAuthorizationDeadlineTrace) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.base.TraceQueryStart(ctx, connection, data)
	if data.SQL == queryAccessResolveSubject && trace.scanWant.CompareAndSwap(true, false) {
		trace.scanArm.Store(true)
	}
	if trace.armed.Load() && data.SQL == queryEmailAuthorizationGet {
		trace.read.Store(true)
	}
	if strings.EqualFold(strings.TrimSpace(data.SQL), "commit") && trace.read.Load() && trace.armed.CompareAndSwap(true, false) {
		return context.WithValue(ctx, emailAuthorizationCommitDelay{}, trace.deadline.Load())
	}
	return ctx
}

// TraceQueryEnd waits only after the database has actually committed; caller cancellation remains bounded and is not presented as access expiry evidence.
// TraceQueryEnd ждёт только после фактического commit базы; cancellation caller остаётся ограниченным и не выдаётся за доказательство access expiry.
func (trace *emailAuthorizationDeadlineTrace) TraceQueryEnd(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryEndData) {
	trace.base.TraceQueryEnd(ctx, connection, data)
	until, _ := ctx.Value(emailAuthorizationCommitDelay{}).(*time.Time)
	if until == nil || data.Err != nil {
		return
	}
	trace.hits.Add(1)
	timer := time.NewTimer(time.Until(*until) + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// personalEmailFiniteAuthorizationFixture changes the sole exact self mailbox binding through the real access command, crosses its expiry after successful COMMIT, restores access, and requires a new test ref rather than reviving the stored intent. It uses no SMTP/IMAP effect or live account.
// personalEmailFiniteAuthorizationFixture меняет единственную exact self mailbox binding настоящей access command, пересекает expiry после успешного COMMIT, восстанавливает access и требует новый test ref вместо оживления stored intent. SMTP/IMAP effect и live account не используются.
func personalEmailFiniteAuthorizationFixture(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, admin, owner, gateway, email value.Principal, connection entity.IntegrationConnection, mailbox api.Mailbox, trace *emailAuthorizationDeadlineTrace) entity.IntegrationConnection {
	t.Helper()
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal("resolve finite mailbox actor", err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal("resolve finite mailbox actor scope", err)
	}
	admin.Permission = "platform.access.bindings.list"
	bindings, next, err := service.ListAccessBindings(ctx, admin, query.AccessBindingFilter{Page: query.Page{Size: 100}, SubjectKind: "USER", SubjectRef: current.actorRef})
	if err != nil || next != "" {
		t.Fatal("read exact finite mailbox binding candidates", err)
	}
	var selected []entity.AccessBinding
	for _, binding := range bindings {
		if binding.State == "ACTIVE" && binding.Scope.Kind == "RESOURCE_INSTANCE" && binding.Scope.ResourceKind == "INTEGRATION" && binding.Scope.ResourceRef == connection.Ref && slices.Contains(binding.RoleVersion.PermissionKeys, "integration.manage") {
			selected = append(selected, binding)
		}
	}
	if len(selected) != 1 {
		t.Fatal("finite mailbox fixture requires one exact self binding")
	}
	original := selected[0]
	begin := func(key string) (query.EmailAuthorization, command.IntegrationConnectionTestInput) {
		t.Helper()
		owner.Permission = "platform.command.integrations.test"
		fresh, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
		if err != nil {
			t.Fatal("read current finite mailbox connection", err)
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &fresh.Version}, Payload: command.ConnectionInput{Ref: connection.Ref}}); err != nil {
			t.Fatal("queue finite mailbox health intent", err)
		}
		claims, err := service.ClaimIntegrationConnectionTests(ctx, gateway, key, 32)
		if err != nil || len(claims) != 1 {
			t.Fatal("claim exact finite mailbox health intent", err)
		}
		claim := claims[0]
		if stringMap(claim, "connectionRef") != connection.Ref {
			t.Fatal("finite mailbox claim belongs to another connection")
		}
		binding := entity.EmailExecutionBinding{ConnectionTestRef: stringMap(claim, "testRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), ExpiresAt: claim["expiresAt"].(time.Time)}
		semantic, err := api.CommandForIntegration("email.delivery.health.read", mailbox.Id, mailbox.Sender, "", []byte(`{}`))
		if err != nil {
			t.Fatal("derive finite mailbox health semantic input", err)
		}
		input := query.EmailAuthorization{Binding: binding, MailboxRef: mailbox.Id, ConfigurationRevision: mailbox.Revision, Operation: "health", Sender: mailbox.Sender, Folder: mailbox.Folder, SemanticInputDigest: api.Digest(semantic)}
		return input, command.IntegrationConnectionTestInput{TestRef: binding.ConnectionTestRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation}
	}
	input, completion := begin("email-finite-original-health")
	readState := func() string {
		t.Helper()
		var digest string
		if err := repository.pool.QueryRow(ctx, emailAuthorizationFixtureState, pgx.StrictNamedArgs{"organization_id": current.organizationID, "test_ref": input.Binding.ConnectionTestRef}).Scan(&digest); err != nil {
			t.Fatal("read isolated mailbox owner state fingerprint", err)
		}
		return digest
	}
	beforeFault := readState()
	trace.scanWant.Store(true)
	failed, failure := service.ResolveEmailAuthorization(ctx, email, input)
	if !errors.Is(failure, errs.ErrUnavailable) || !reflect.DeepEqual(failed, entity.EmailAuthorization{}) || trace.scanHits.Load() != 1 || ctx.Err() != nil {
		t.Fatal("mailbox successful SELECT Scan failure became an authorization denial", failure)
	}
	if readState() != beforeFault {
		t.Fatal("mailbox authority infrastructure failure changed owner rows")
	}
	until := time.Now().UTC().Add(3 * time.Second)
	conditions := original.Conditions
	conditions.ValidUntil = &until
	admin.Permission = "platform.access.bindings.change"
	changed, err := service.Execute(ctx, command.Command{Kind: command.ChangeAccessBinding, Principal: admin, Mutation: value.Mutation{IdempotencyKey: "email-finite-access", ExpectedVersion: &original.Version}, Payload: command.AccessBindingInput{BindingRef: original.Ref, SubjectKind: "USER", SubjectRef: current.actorRef, RoleVersionRef: original.RoleVersion.Ref, Scope: original.Scope, Conditions: conditions}})
	if err != nil || changed.AccessBinding == nil || changed.AccessBinding.Conditions.ValidUntil == nil {
		t.Fatal("set finite mailbox access through owner command", err)
	}
	trace.deadline.Store(changed.AccessBinding.Conditions.ValidUntil)
	trace.read.Store(false)
	trace.armed.Store(true)
	decision, err := service.ResolveEmailAuthorization(ctx, email, input)
	if !errors.Is(err, errs.ErrForbidden) || !reflect.DeepEqual(decision, entity.EmailAuthorization{}) || ctx.Err() != nil || trace.hits.Load() != 1 {
		t.Fatal("expired mailbox access escaped successful COMMIT without a live caller context", err)
	}
	restored, err := service.Execute(ctx, command.Command{Kind: command.ChangeAccessBinding, Principal: admin, Mutation: value.Mutation{IdempotencyKey: "email-restore-finite-access", ExpectedVersion: &changed.AccessBinding.Version}, Payload: command.AccessBindingInput{BindingRef: original.Ref, SubjectKind: "USER", SubjectRef: current.actorRef, RoleVersionRef: original.RoleVersion.Ref, Scope: original.Scope, Conditions: original.Conditions}})
	if err != nil || restored.AccessBinding == nil {
		t.Fatal("restore test mailbox access", err)
	}
	if decision, err := service.ResolveEmailAuthorization(ctx, email, input); !errors.Is(err, errs.ErrForbidden) || decision.Allowed {
		t.Fatal("restored mailbox access renewed the original expired intent", err)
	}
	completion.SafeErrorCode = "INTEGRATION_REQUEST_REJECTED"
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "email-finite-original-close"}, Payload: completion}); err != nil {
		t.Fatal("close original finite mailbox test without a remote effect", err)
	}
	fresh, nextCompletion := begin("email-finite-fresh-health")
	if fresh.Binding.ConnectionTestRef == input.Binding.ConnectionTestRef {
		t.Fatal("new mailbox health attempt reused an expired test ref")
	}
	if decision, err := service.ResolveEmailAuthorization(ctx, email, fresh); err != nil || !decision.Allowed {
		t.Fatal("restored access did not admit a new mailbox health intent", err)
	}
	nextCompletion.Success = true
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "email-finite-fresh-close"}, Payload: nextCompletion}); err != nil {
		t.Fatal("complete fresh synthetic mailbox health", err)
	}
	connection, err = service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil || connection.State != "CONNECTED" {
		t.Fatal("finite mailbox lifecycle did not restore connected metadata", err)
	}
	return connection
}
