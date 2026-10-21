package imageowner

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
	"time"
)

// failureRPCStub records terminal/claim call options, requests and deadlines while
// returning controlled errors and a synthetic expiry receipt; it has no network.
// failureRPCStub сохраняет options, запросы и сроки terminal/claim, возвращая
// заданные ошибки и искусственную expiry-квитанцию; сетевых вызовов здесь нет.
type failureRPCStub struct {
	cp.RoleImageServiceClient
	code                                        codes.Code
	failCalls, expireCalls                      int
	expiry                                      *cp.ExpireImageAdmissionClaimRequest
	failure                                     *cp.FailImageAdmissionRequest
	failureOptions, expiryOptions, claimOptions []grpc.CallOption
	failureDeadline, expiryDeadline             time.Time
}

// FailImageAdmission captures one callback's exact request/options/deadline and
// returns the configured status, allowing assertions against unbounded retries.
// FailImageAdmission сохраняет точные request/options/deadline одного callback
// и возвращает заданный код для проверки отсутствия неограниченных повторов.
func (s *failureRPCStub) FailImageAdmission(ctx context.Context, in *cp.FailImageAdmissionRequest, options ...grpc.CallOption) (*cp.FailImageAdmissionResponse, error) {
	s.failCalls++
	s.failure = in
	s.failureOptions = options
	s.failureDeadline, _ = ctx.Deadline()
	return nil, status.Error(s.code, "closed fixture failure")
}

// ExpireImageAdmissionClaim records the distinct expiry request and fresh deadline,
// then fabricates the fixture organization's version+1 FAILED receipt with the
// requested build/recipe pins and fixed ADMISSION_LEASE_EXPIRED code.
// ExpireImageAdmissionClaim сохраняет отдельный expiry-запрос и новый срок, затем
// создаёт искусственную FAILED-квитанцию version+1 организации fixture с заданными
// build/recipe и постоянным кодом ADMISSION_LEASE_EXPIRED.
func (s *failureRPCStub) ExpireImageAdmissionClaim(ctx context.Context, in *cp.ExpireImageAdmissionClaimRequest, options ...grpc.CallOption) (*cp.ExpireImageAdmissionClaimResponse, error) {
	s.expireCalls++
	s.expiry = in
	s.expiryOptions = options
	s.expiryDeadline, _ = ctx.Deadline()
	return &cp.ExpireImageAdmissionClaimResponse{AdmissionFailure: &cp.RoleImageAdmissionFailure{ImageArtifactRef: in.ImageArtifactRef, Version: in.ExpectedVersion + 1, RecipeRef: "imgrec_12345678", RecipeGeneration: in.RecipeGeneration, BuildRef: in.BuildRef, BuildAttempt: in.ExpectedBuildAttempt, ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: "org_12345678", State: "FAILED", ErrorCode: "ADMISSION_LEASE_EXPIRED"}}, nil
}

// TestAdmissionFailFallbackUsesDedicatedFreshExpiryAndExactTuple requires exactly
// one fail callback, fallback only for PermissionDenied, and a distinct expiry key,
// later deadline and unchanged claim/attempt pins with WaitForReady on both calls.
// TestAdmissionFailFallbackUsesDedicatedFreshExpiryAndExactTuple требует ровно один
// fail callback, fallback только для PermissionDenied, отдельные expiry key и более
// поздний срок, неизменные claim/attempt и WaitForReady у обоих вызовов.
func TestAdmissionFailFallbackUsesDedicatedFreshExpiryAndExactTuple(t *testing.T) {
	claim := Claim{ArtifactID: "imgart_12345678", Version: 2, Fence: 3, AuthorityGeneration: 4, ClaimToken: "private-fixture-token", RecipeID: "imgrec_12345678", RecipeGeneration: 5, BuildID: "imgbld_12345678", BuildAttempt: 1, ScopeKind: "ORGANIZATION", OrganizationRef: "org_12345678", ManifestDigest: "manifest-fixture", ImmutableBuildSHA256: "build-fixture", ProvenanceSHA256: "provenance-fixture", PolicyRevision: 1, PolicySHA256: "policy-fixture", SpecSHA256: "spec-fixture"}
	claim.AdmissionAttemptRef, claim.AdmissionAttempt, claim.ExpiresAt = "imgadm_12345678", 2, time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unavailable, codes.Aborted, codes.InvalidArgument} {
		t.Run(code.String(), func(t *testing.T) {
			stub := &failureRPCStub{code: code}
			client := &Client{shared: &sharedclient.Client{RoleImages: stub}, rpcDeadline: time.Second}
			err := client.Fail(t.Context(), "failure-key", claim, "ADMISSION_WORKER_FAILED")
			if code == codes.PermissionDenied {
				if err != nil || stub.expireCalls != 1 || stub.expiry.ExpectedAuthorityGeneration != claim.AuthorityGeneration || stub.expiry.ExpectedFence != claim.Fence || stub.expiry.ExpectedVersion != claim.Version || stub.expiry.IdempotencyKey != "failure-key-expiry" || stub.expiry.ManifestDigest != claim.ManifestDigest || stub.expiry.BuildRef != claim.BuildID || stub.expiry.SpecSha256 != claim.SpecSHA256 {
					t.Fatal("dedicated expiry lost exact tuple or receipt")
				}
			} else if err == nil || stub.expireCalls != 0 {
				t.Fatal("unknown callback outcome incorrectly became owner expiry")
			}
			if stub.failCalls != 1 {
				t.Fatal("callback retry was unbounded")
			}
			assertTerminalWaitForReady(t, stub.failureOptions)
			if code == codes.PermissionDenied {
				assertTerminalWaitForReady(t, stub.expiryOptions)
				if !stub.expiryDeadline.After(stub.failureDeadline) {
					t.Fatal("dedicated expiry did not receive a fresh bounded context")
				}
			}
			if stub.failure.ExpectedAdmissionAttemptRef != claim.AdmissionAttemptRef || stub.failure.ExpectedAdmissionAttempt != claim.AdmissionAttempt ||
				stub.expiry != nil && (stub.expiry.ExpectedAdmissionAttemptRef != claim.AdmissionAttemptRef || stub.expiry.ExpectedAdmissionAttempt != claim.AdmissionAttempt) {
				t.Fatal("terminal callback lost exact admission attempt")
			}
		})
	}
}

// assertTerminalWaitForReady requires the single explicit non-fail-fast call option
// used by terminal callbacks; unrelated extra options fail the assertion as well.
// assertTerminalWaitForReady требует единственный явный non-fail-fast option
// terminal callback; лишние посторонние options также приводят к ошибке проверки.
func assertTerminalWaitForReady(t *testing.T, options []grpc.CallOption) {
	t.Helper()
	if len(options) != 1 {
		t.Fatal("terminal callback call options are incomplete")
	}
	option, ok := options[0].(grpc.FailFastCallOption)
	if !ok || option.FailFast {
		t.Fatal("terminal callback does not wait for a ready connection")
	}
}

// ClaimImageAdmission captures fresh-claim options and returns Unavailable without
// producing work, exposing accidental reuse of terminal connection-wait settings.
// ClaimImageAdmission сохраняет options нового claim и возвращает Unavailable
// без работы для выявления случайного переноса terminal connection-wait настроек.
func (s *failureRPCStub) ClaimImageAdmission(_ context.Context, _ *cp.ClaimImageAdmissionRequest, options ...grpc.CallOption) (*cp.ClaimImageAdmissionResponse, error) {
	s.claimOptions = options
	return nil, status.Error(codes.Unavailable, "closed fixture failure")
}

// TestAdmissionConnectionWaitDoesNotExtendToClaim ensures a fresh claim does not
// inherit terminal WaitForReady and keeps the stub's call option list empty.
// TestAdmissionConnectionWaitDoesNotExtendToClaim проверяет отсутствие terminal
// WaitForReady у нового claim: список options заглушки должен остаться пустым.
func TestAdmissionConnectionWaitDoesNotExtendToClaim(t *testing.T) {
	stub := &failureRPCStub{}
	client := &Client{shared: &sharedclient.Client{RoleImages: stub}, rpcDeadline: time.Second}
	_, _ = client.Claim(t.Context(), "claim-key")
	if len(stub.claimOptions) != 0 {
		t.Fatal("terminal connection wait escaped into a fresh claim")
	}
}
