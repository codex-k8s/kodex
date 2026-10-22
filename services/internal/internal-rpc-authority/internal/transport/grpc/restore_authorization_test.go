package authoritygrpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/url"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// rejectingRestoreOperatorVerifier records only synthetic review arguments and rejects every token;
// it proves ordering before domain invocation, not real Kubernetes TokenReview or bearer validity.
// rejectingRestoreOperatorVerifier записывает только synthetic review аргументы и отклоняет все tokens;
// проверяется порядок до domain вызова, не реальный Kubernetes TokenReview и не bearer validity.
type rejectingRestoreOperatorVerifier struct {
	calls   int
	matched bool
}

// VerifyOperatorCredential checks the exact test token and configured audience, counts the review and
// returns a fixed private rejection so no test can reach a nil domain application after admission.
// VerifyOperatorCredential проверяет точные test token и настроенную audience, считает review и
// возвращает фиксированный private отказ, не допуская входа в nil domain application после admission.
func (verifier *rejectingRestoreOperatorVerifier) VerifyOperatorCredential(_ context.Context, token, audience string) (model.RestoreOperatorCredential, error) {
	verifier.calls++
	verifier.matched = token == "synthetic-operator-token" && audience == restoreOperatorApplicationAudience
	if !verifier.matched {
		return model.RestoreOperatorCredential{}, errors.New("synthetic review arguments changed")
	}
	return model.RestoreOperatorCredential{}, errors.New("synthetic private operator rejection")
}

// syntheticRestoreOperatorPeer builds only a TLSInfo context fixture with one leaf URI; no handshake,
// certificate verification, real token, network endpoint or operator grant is created.
// syntheticRestoreOperatorPeer строит только TLSInfo context fixture с одним leaf URI; handshake,
// проверка сертификата, реальный token, network endpoint и operator grant не создаются.
func syntheticRestoreOperatorPeer(t *testing.T, identity string) context.Context {
	t.Helper()
	uri, err := url.Parse(identity)
	if err != nil {
		t.Fatal("synthetic operator URI is invalid")
	}
	leaf := &x509.Certificate{URIs: []*url.URL{uri}}
	return peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{leaf}},
	}}})
}

// invokeRestoreCommandFixture invokes either specialized public RPC with identical synthetic command
// fields; the closed test switch never substitutes a generic production command dispatcher.
// invokeRestoreCommandFixture вызывает один из двух специализированных public RPC с одинаковыми
// synthetic command полями; закрытый test switch не заменяет production команды generic dispatcher.
func invokeRestoreCommandFixture(t *testing.T, server *RestoreControllerServer, method string, ctx context.Context, target *timestamppb.Timestamp) error {
	t.Helper()
	switch method {
	case "prepare":
		_, err := server.PrepareRestore(ctx, &internalrpcauthorityv1.PrepareRestoreRequest{
			RestoreId: "synthetic-restore", DatabaseClusterId: "synthetic-cluster", BackupManifestDigestSha256: "synthetic-digest",
			RecoveryTargetTime: target, IdempotencyKey: "synthetic-idempotency", CorrelationId: "synthetic-correlation",
		})
		return err
	case "complete":
		_, err := server.CompleteRestore(ctx, &internalrpcauthorityv1.CompleteRestoreRequest{
			RestoreId: "synthetic-restore", DatabaseClusterId: "synthetic-cluster", BackupManifestDigestSha256: "synthetic-digest",
			RecoveryTargetTime: target, IdempotencyKey: "synthetic-idempotency", CorrelationId: "synthetic-correlation",
		})
		return err
	default:
		t.Fatal("unknown synthetic restore method")
		return nil
	}
}

// TestRestoreCommandShapePrecedesOperatorAdmission proves both handlers reject missing/invalid recovery
// timestamps before reviewing credentials or invoking their absent domain application.
// TestRestoreCommandShapePrecedesOperatorAdmission доказывает отказ обоих handlers отсутствующему или
// неверному recovery timestamp до credential review и вызова отсутствующего domain application.
func TestRestoreCommandShapePrecedesOperatorAdmission(t *testing.T) {
	for _, method := range []string{"prepare", "complete"} {
		for _, target := range []*timestamppb.Timestamp{nil, {Seconds: 1 << 62}} {
			t.Run(method, func(t *testing.T) {
				verifier := &rejectingRestoreOperatorVerifier{}
				server := &RestoreControllerServer{credentialVerifier: verifier}
				err := invokeRestoreCommandFixture(t, server, method, t.Context(), target)
				if status.Code(err) != codes.InvalidArgument || verifier.calls != 0 {
					t.Fatal("malformed command crossed the operator admission boundary")
				}
			})
		}
	}
}

// TestRestoreCommandKeepsPeerAndBearerOrder proves both RPCs reject missing/wrong operator peers before
// token review, then reject absent or unverified bearer credentials without reaching domain commands.
// TestRestoreCommandKeepsPeerAndBearerOrder проверяет отказ обоих RPC отсутствующему/чужому operator
// peer до token review, затем отказ отсутствующему/непроверенному bearer без входа в domain команды.
func TestRestoreCommandKeepsPeerAndBearerOrder(t *testing.T) {
	for _, method := range []string{"prepare", "complete"} {
		for _, mode := range []string{"missing-peer", "wrong-peer", "missing-bearer", "nil-verifier", "rejected-bearer"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				verifier := &rejectingRestoreOperatorVerifier{}
				server := &RestoreControllerServer{credentialVerifier: verifier}
				ctx := t.Context()
				expected, calls := codes.Unauthenticated, 0
				switch mode {
				case "missing-peer":
					expected = codes.PermissionDenied
				case "wrong-peer":
					ctx = syntheticRestoreOperatorPeer(t, restoreControllerSPIFFEForTransport)
					expected = codes.PermissionDenied
				default:
					ctx = syntheticRestoreOperatorPeer(t, restoreOperatorSPIFFE)
				}
				if mode != "missing-bearer" {
					ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer synthetic-operator-token"))
				}
				if mode == "nil-verifier" {
					server.credentialVerifier = nil
				}
				if mode == "rejected-bearer" {
					calls = 1
				}
				err := invokeRestoreCommandFixture(t, server, method, ctx, timestamppb.New(time.Unix(1, 0)))
				if status.Code(err) != expected || verifier.calls != calls || calls == 1 && !verifier.matched {
					t.Fatal("restore command changed peer or bearer gate ordering")
				}
				details := status.Convert(err).Details()
				if len(details) != 1 {
					t.Fatal("restore rejection lost its typed detail")
				}
				value, ok := details[0].(*internalrpcauthorityv1.AuthorizationErrorDetail)
				if !ok || value.GetCorrelationId() != "synthetic-correlation" {
					t.Fatal("restore rejection lost the validated correlation identifier")
				}
			})
		}
	}
}
