package component

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/authority"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"sync/atomic"
)

// ownerGRPCFixture records the latest synthetic authorization request and can
// revoke subsequent authorization/report calls; it is not a full owner service.
// ownerGRPCFixture хранит последний искусственный запрос полномочий и умеет
// отозвать последующие запросы/отчёты; это не полная реализация сервиса владельца.
type ownerGRPCFixture struct {
	cp.UnimplementedRuntimeWorkServiceServer
	revoked atomic.Bool
	mu      sync.Mutex
	last    *cp.ResolveEmailAuthorizationRequest
}

// ResolveEmailAuthorization stores the request under a mutex and returns a
// short-lived fixture decision bound to its input and execution, unless revoked.
// ResolveEmailAuthorization сохраняет запрос под мьютексом и, если нет отзыва,
// возвращает краткоживущее тестовое решение с привязкой к вводу и выполнению.
func (s *ownerGRPCFixture) ResolveEmailAuthorization(_ context.Context, r *cp.ResolveEmailAuthorizationRequest) (*cp.ResolveEmailAuthorizationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = r
	if s.revoked.Load() {
		return nil, status.Error(codes.PermissionDenied, "revoked")
	}
	scope := &cp.EmailAuthorizationScope{MailboxRef: r.MailboxRef, Sender: r.Sender, Operations: []cp.EmailOperation{r.Operation}, Folders: []string{"INBOX"}, Recipients: []string{"recipient@example.test", "copy@example.test"}}
	return &cp.ResolveEmailAuthorizationResponse{Allowed: true, ActorRef: "actor", AgentRef: "agent", OrganizationRef: "tenant", ConnectionRef: "connection", MailboxRef: r.MailboxRef, GrantRef: "grant", Operation: r.Operation, SemanticInputDigest: r.SemanticInputDigest, EffectKey: r.EffectKey, ConfigurationRevision: r.ConfigurationRevision, CredentialGeneration: 1, Policy: cp.EmailApprovalPolicy_EMAIL_APPROVAL_POLICY_ALLOW, GateApproved: true, UserScope: scope, AgentScope: scope, ConnectionScope: scope, ResourceScope: scope, ExpiresAt: timestamppb.New(time.Now().Add(30 * time.Second)), Binding: r.Binding}, nil
}

// ReportEmailEffectReceipt checks the latest fixture request's input digest
// and invocation before echoing a synthetic owner receipt; revocation always denies.
// ReportEmailEffectReceipt проверяет хеш ввода и вызов последнего тестового запроса
// перед выдачей искусственной квитанции владельца; отзыв всегда запрещает отчёт.
func (s *ownerGRPCFixture) ReportEmailEffectReceipt(_ context.Context, r *cp.ReportEmailEffectReceiptRequest) (*cp.ReportEmailEffectReceiptResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revoked.Load() || s.last == nil || r.SemanticInputDigest != s.last.SemanticInputDigest || r.Binding.GetInvocationRef() != s.last.Binding.GetInvocationRef() {
		return nil, status.Error(codes.PermissionDenied, "receipt denied")
	}
	return &cp.ReportEmailEffectReceiptResponse{Receipt: &cp.EmailEffectReceipt{Ref: "receipt_fixture01", Version: 1, InvocationRef: r.Binding.GetInvocationRef(), ExternalReceiptRef: r.ExternalReceiptRef, ExternalReceiptDigest: r.ExternalReceiptDigest, SemanticInputDigest: r.SemanticInputDigest, EffectKey: s.last.EffectKey, MailboxRef: s.last.MailboxRef, ConnectionRef: "connection", ConfigurationRevision: s.last.ConfigurationRevision, Outcome: r.Outcome}}, nil
}

// executionFixture creates a synthetic invocation and one-minute generation-one
// lease; its dummy fence is not a credential for the running platform.
// executionFixture создаёт искусственный вызов и минутную аренду первого поколения;
// её условный ограничитель не является реквизитом действующей платформы.
func executionFixture() *api.ExecutionBinding {
	ref := "inv_fixture01"
	return &api.ExecutionBinding{InvocationRef: &ref, Lease: api.ExecutionLease{Ref: "lease_fixture01", Fence: "fixture-fence", Generation: 1, ExpiresAt: time.Now().Add(time.Minute)}}
}

// TestOwnerGRPCClient drives the real authority adapter through a loopback TLS 1.3
// gRPC fixture and verifies that revocation stops the next send before credential reads.
// TestOwnerGRPCClient вызывает настоящий адаптер полномочий через локальный gRPC
// с TLS 1.3 и проверяет остановку следующей отправки отзывом до чтения реквизитов.
func TestOwnerGRPCClient(t *testing.T) {
	f := newFixture(t, "implicit")
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(f.ca)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{f.cert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13})))
	owner := &ownerGRPCFixture{}
	cp.RegisterRuntimeWorkServiceServer(server, owner)
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-done })
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{ServerName: "mail.example.test", RootCAs: roots, Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS13})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s, sec, _ := service(t, f, "implicit", nil)
	client := &authority.Client{API: cp.NewRuntimeWorkServiceClient(conn)}
	s.Authority, s.Effects = client, client
	binding := executionFixture()
	ctx := api.WithExecutionBinding(t.Context(), binding)
	result, err := s.Execute(executionContext(ctx), httptransport.CallerSPIFFE, binding.Lease.Fence, send(api.OperationSend, "owner-grpc"))
	if err != nil || result.Status != "accepted" {
		t.Fatalf("gRPC owner send: %v", err)
	}
	before := sec.reads.Load()
	owner.revoked.Store(true)
	if _, err = s.Execute(executionContext(ctx), httptransport.CallerSPIFFE, binding.Lease.Fence, send(api.OperationSend, "owner-revoked")); !errors.Is(err, errs.Denied) || sec.reads.Load() != before {
		t.Fatal("revocation did not precede projection")
	}
}
