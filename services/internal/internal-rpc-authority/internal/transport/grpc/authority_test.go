package authoritygrpc

import (
	"context"
	"testing"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIssuerRejectsDuplicateProtoWithCanonicalDetail(t *testing.T) {
	request := &internalrpcauthorityv1.IssueAuthorizationContextRequest{}
	duplicateOperationID := []byte{0x0a, 0x01, 'a', 0x0a, 0x01, 'b'}
	if err := grpcserver.StrictProtoCodec().Unmarshal(duplicateOperationID, request); err != nil {
		t.Fatalf("decode duplicate request: %v", err)
	}

	_, err := NewIssuerServer(nil).IssueAuthorizationContext(context.Background(), request)
	assertMalformedAuthorizationError(t, err)
}

func TestVerifierRejectsUnknownProtoWithCanonicalDetail(t *testing.T) {
	request := &internalrpcauthorityv1.VerifyAuthorizationContextRequest{}
	unknownField := []byte{0x78, 0x01}
	if err := grpcserver.StrictProtoCodec().Unmarshal(unknownField, request); err != nil {
		t.Fatalf("decode unknown request: %v", err)
	}

	_, err := NewVerifierServer(nil).VerifyAuthorizationContext(context.Background(), request)
	assertMalformedAuthorizationError(t, err)
}

func TestCredentialLifecycleRejectsMalformedProto(t *testing.T) {
	request := &internalrpcauthorityv1.ReconcileDatabaseCredentialsRequest{}
	brokenWire := []byte{0x0a, 0xff}
	if err := grpcserver.StrictProtoCodec().Unmarshal(brokenWire, request); err != nil {
		t.Fatalf("decode broken request: %v", err)
	}

	_, err := NewDatabaseCredentialLifecycleServer(nil).ReconcileDatabaseCredentials(
		context.Background(),
		request,
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status code = %s, want %s", status.Code(err), codes.InvalidArgument)
	}
}

func assertMalformedAuthorizationError(t *testing.T, err error) {
	t.Helper()
	value, ok := status.FromError(err)
	if !ok || value.Code() != codes.InvalidArgument {
		t.Fatalf("status = %v, want InvalidArgument", err)
	}
	details := value.Details()
	if len(details) != 1 {
		t.Fatalf("detail count = %d", len(details))
	}
	detail, ok := details[0].(*internalrpcauthorityv1.AuthorizationErrorDetail)
	if !ok ||
		detail.GetReason() != internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_MALFORMED_REQUEST ||
		detail.GetRetryable() {
		t.Fatalf("malformed detail = %#v", details[0])
	}
}
