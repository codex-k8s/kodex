package imageowner

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type terminalConnectionServer struct {
	cp.UnimplementedRoleImageServiceServer
	claim                  Claim
	code                   codes.Code
	failCalls, expireCalls atomic.Int32
	mu                     sync.Mutex
	failure                *cp.FailImageAdmissionRequest
}

func (server *terminalConnectionServer) receipt(code string) *cp.RoleImageAdmissionFailure {
	c := server.claim
	return &cp.RoleImageAdmissionFailure{ImageArtifactRef: c.ArtifactID, Version: c.Version + 1,
		RecipeRef: c.RecipeID, RecipeGeneration: c.RecipeGeneration, BuildRef: c.BuildID, BuildAttempt: c.BuildAttempt,
		ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: c.OrganizationRef,
		State: "FAILED", ErrorCode: code}
}

func (server *terminalConnectionServer) FailImageAdmission(_ context.Context, request *cp.FailImageAdmissionRequest) (*cp.FailImageAdmissionResponse, error) {
	server.failCalls.Add(1)
	server.mu.Lock()
	server.failure = proto.Clone(request).(*cp.FailImageAdmissionRequest)
	server.mu.Unlock()
	if server.code != codes.OK {
		return nil, status.Error(server.code, "closed fixture failure")
	}
	return &cp.FailImageAdmissionResponse{AdmissionFailure: server.receipt(request.ErrorCode)}, nil
}

func (server *terminalConnectionServer) ExpireImageAdmissionClaim(_ context.Context, _ *cp.ExpireImageAdmissionClaimRequest) (*cp.ExpireImageAdmissionClaimResponse, error) {
	server.expireCalls.Add(1)
	return &cp.ExpireImageAdmissionClaimResponse{AdmissionFailure: server.receipt("ADMISSION_LEASE_EXPIRED")}, nil
}

func (server *terminalConnectionServer) GetImageSupplyWorkAvailability(context.Context, *cp.GetImageSupplyWorkAvailabilityRequest) (*cp.GetImageSupplyWorkAvailabilityResponse, error) {
	return &cp.GetImageSupplyWorkAvailabilityResponse{}, nil
}

// Две реальные TCP-фазы: подтверждённый ECONNREFUSED до listener и затем
// тот же адрес. Fixture не выдаёт runtime authority и не вызывает живой owner.
func terminalLoopback(t *testing.T, claim Claim, code codes.Code) (*Client, *terminalConnectionServer, <-chan struct{}, func()) {
	t.Helper()
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve disposable loopback listener")
	}
	address := reserved.Addr().String()
	if err = reserved.Close(); err != nil {
		t.Fatal("close reserved loopback listener")
	}
	refused := make(chan struct{})
	var once sync.Once
	connection, err := grpc.NewClient("passthrough:///"+address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.Config{BaseDelay: 20 * time.Millisecond, Multiplier: 1, MaxDelay: 20 * time.Millisecond}, MinConnectTimeout: 100 * time.Millisecond}),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			conn, dialErr := (&net.Dialer{}).DialContext(ctx, "tcp", address)
			if errors.Is(dialErr, syscall.ECONNREFUSED) {
				once.Do(func() { close(refused) })
			}
			return conn, dialErr
		}))
	if err != nil {
		t.Fatal("construct disposable connection")
	}
	t.Cleanup(func() { _ = connection.Close() })
	server := &terminalConnectionServer{claim: claim, code: code}
	grpcServer := grpc.NewServer()
	cp.RegisterRoleImageServiceServer(grpcServer, server)
	var startOnce sync.Once
	start := func() {
		startOnce.Do(func() {
			listener, listenErr := net.Listen("tcp", address)
			if listenErr != nil {
				t.Fatal("open delayed disposable listener")
			}
			done := make(chan struct{})
			go func() { defer close(done); _ = grpcServer.Serve(listener) }()
			t.Cleanup(func() { grpcServer.Stop(); _ = listener.Close(); <-done })
		})
	}
	return &Client{shared: &sharedclient.Client{RoleImages: cp.NewRoleImageServiceClient(connection)}, rpcDeadline: 8 * time.Second}, server, refused, start
}

func awaitConnectionRefused(t *testing.T, refused <-chan struct{}) {
	t.Helper()
	select {
	case <-refused:
	case <-time.After(2 * time.Second):
		t.Fatal("fixture did not observe initial connection refusal")
	}
}

func awaitTerminalResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("terminal callback exceeded disposable budget")
		return nil
	}
}

func TestAdmissionTerminalWaitPreservesFirstRequestAfterConnectionRefused(t *testing.T) {
	claim, _ := admissionRiskFixture(t, false)
	client, server, refused, start := terminalLoopback(t, claim, codes.OK)
	result := make(chan error, 1)
	go func() { result <- client.Fail(t.Context(), "same-failure-key", claim, "ADMISSION_WORKER_FAILED") }()
	awaitConnectionRefused(t, refused)
	if server.failCalls.Load() != 0 {
		t.Fatal("owner effect happened before a connection was ready")
	}
	start()
	if awaitTerminalResult(t, result) != nil {
		t.Fatal("bounded terminal callback did not survive connection refusal")
	}
	server.mu.Lock()
	request := server.failure
	server.mu.Unlock()
	if server.failCalls.Load() != 1 || server.expireCalls.Load() != 0 || request == nil ||
		request.IdempotencyKey != "same-failure-key" || request.ClaimToken != claim.ClaimToken ||
		request.ExpectedVersion != claim.Version || request.ExpectedFence != claim.Fence || request.ExpectedAuthorityGeneration != claim.AuthorityGeneration ||
		request.ExpectedAdmissionAttemptRef != claim.AdmissionAttemptRef || request.ExpectedAdmissionAttempt != claim.AdmissionAttempt ||
		request.ImageArtifactRef != claim.ArtifactID || request.ManifestDigest != claim.ManifestDigest || request.ImmutableBuildSha256 != claim.ImmutableBuildSHA256 ||
		request.ProvenanceSha256 != claim.ProvenanceSHA256 || request.PolicyRevision != claim.PolicyRevision || request.PolicySha256 != claim.PolicySHA256 ||
		request.BuildRef != claim.BuildID || request.ExpectedBuildAttempt != claim.BuildAttempt || request.RecipeGeneration != claim.RecipeGeneration || request.SpecSha256 != claim.SpecSHA256 {
		t.Fatal("connection wait changed exact first owner request or repeated its effect")
	}
}

func TestAdmissionTerminalWaitCancelAndDeadlineBeforeReadyHaveNoEffect(t *testing.T) {
	for _, operation := range []string{"fail", "expire"} {
		for _, cancelled := range []bool{false, true} {
			name := operation + "/deadline"
			if cancelled {
				name = operation + "/cancel"
			}
			t.Run(name, func(t *testing.T) {
				claim, _ := admissionRiskFixture(t, false)
				client, server, refused, start := terminalLoopback(t, claim, codes.OK)
				budget := 80 * time.Millisecond
				if cancelled {
					budget = time.Second
				}
				ctx, cancel := context.WithTimeout(t.Context(), budget)
				defer cancel()
				result := make(chan error, 1)
				go func() {
					if operation == "expire" {
						result <- client.Expire(ctx, "same-expiry-key", claim)
					} else {
						result <- client.Fail(ctx, "same-failure-key", claim, "ADMISSION_WORKER_FAILED")
					}
				}()
				awaitConnectionRefused(t, refused)
				if cancelled {
					cancel()
				}
				expected := codes.DeadlineExceeded
				if cancelled {
					expected = codes.Canceled
				}
				if status.Code(awaitTerminalResult(t, result)) != expected {
					t.Fatal("connection wait ignored caller deadline or cancellation")
				}
				start()
				barrierCtx, cancelBarrier := context.WithTimeout(t.Context(), time.Second)
				defer cancelBarrier()
				if _, err := client.shared.RoleImages.GetImageSupplyWorkAvailability(barrierCtx, &cp.GetImageSupplyWorkAvailabilityRequest{}, grpc.WaitForReady(true)); err != nil {
					t.Fatal("disposable listener did not accept the read-only barrier")
				}
				if server.failCalls.Load() != 0 || server.expireCalls.Load() != 0 {
					t.Fatal("cancelled pending callback produced an owner effect")
				}
			})
		}
	}
}

func TestAdmissionTerminalWaitDoesNotRetryServerUnavailable(t *testing.T) {
	claim, _ := admissionRiskFixture(t, false)
	client, server, _, start := terminalLoopback(t, claim, codes.Unavailable)
	start()
	if status.Code(client.Fail(t.Context(), "same-failure-key", claim, "ADMISSION_WORKER_FAILED")) != codes.Unavailable || server.failCalls.Load() != 1 || server.expireCalls.Load() != 0 {
		t.Fatal("handled unavailable callback was retried or became an expiry")
	}
}

func TestAdmissionTerminalWaitPermissionDeniedUsesFreshDedicatedExpiry(t *testing.T) {
	claim, _ := admissionRiskFixture(t, false)
	client, server, _, start := terminalLoopback(t, claim, codes.PermissionDenied)
	start()
	if client.Fail(t.Context(), "same-failure-key", claim, "ADMISSION_WORKER_FAILED") != nil || server.failCalls.Load() != 1 || server.expireCalls.Load() != 1 {
		t.Fatal("permission denial did not immediately use the dedicated owner expiry")
	}
}
