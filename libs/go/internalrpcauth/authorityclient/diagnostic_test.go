package authorityclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const diagnosticSecret = "PRIVATE-credential-SQL-JWS-sentinel"

type rejectedDiagnosticIssuer struct {
	api.AuthorizationIssuerServiceClient
	err   error
	calls int
}

func (issuer *rejectedDiagnosticIssuer) IssueAuthorizationContext(context.Context, *api.IssueAuthorizationContextRequest, ...grpc.CallOption) (*api.IssueAuthorizationContextResponse, error) {
	issuer.calls++
	return nil, issuer.err
}

func (issuer *rejectedDiagnosticIssuer) IssueContinuationAuthorizationContext(context.Context, *api.IssueContinuationAuthorizationContextRequest, ...grpc.CallOption) (*api.IssueContinuationAuthorizationContextResponse, error) {
	issuer.calls++
	return nil, issuer.err
}

func TestAuthorityDiagnosticCoversStreamAndContinuationBoundaries(t *testing.T) {
	for _, bound := range []bool{false, true} {
		for _, issuerFailure := range []bool{false, true} {
			issuer := &rejectedDiagnosticIssuer{err: status.Error(codes.PermissionDenied, diagnosticSecret)}
			provider := &scriptedProofProvider{}
			wantStage := StageContextIssue
			if !issuerFailure {
				provider.errors = []error{NewProofFailure(StageGrantRead, errors.New(diagnosticSecret))}
				wantStage = StageGrantRead
			}
			var methods []string
			if bound {
				methods = []string{"/test"}
			}
			opened := false
			stream, err := IssuerStreamClientInterceptor(issuer, fakeOperationResolver{"/test": "test"}, provider, methods...)(t.Context(), &grpc.StreamDesc{ServerStreams: true}, nil, "/test",
				func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
					opened = true
					return nil, nil
				})
			if bound {
				if err != nil {
					t.Fatal(err)
				}
				err = stream.SendMsg(&emptypb.Empty{})
			}
			var failure *LocalAuthorityError
			if !errors.As(err, &failure) || failure.Diagnostic().Stage != wantStage || opened || provider.calls != 1 {
				t.Fatal("stream diagnostic or no-effect boundary mismatch")
			}
		}
	}
	issuer := &rejectedDiagnosticIssuer{err: status.Error(codes.PermissionDenied, diagnosticSecret)}
	ctx := context.WithValue(t.Context(), continuationSourceKey{}, continuationSource{compact: diagnosticSecret, verified: &api.VerifiedAuthorizationContext{Jti: "parent"}})
	ctx, err := BindContinuation(ctx, "test", "/test", "request", "correlation")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = ContinuationUnaryClientInterceptor(issuer, fakeOperationResolver{"/test": "test"})(ctx, "/test", &emptypb.Empty{}, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			called = true
			return nil
		})
	var failure *LocalAuthorityError
	if !errors.As(err, &failure) || failure.Diagnostic().Stage != StageContinuationIssue || called || issuer.calls != 1 || strings.Contains(err.Error(), diagnosticSecret) {
		t.Fatal("continuation failure changed authority boundary")
	}
}

func TestAuthorityDiagnosticDistinguishesProviderAndIssuerWithoutEffects(t *testing.T) {
	for _, stage := range []DiagnosticStage{StageProofOperation, StageGrantMissing, StageGrantRead, StageProofResolve, StageProofResponse, StageContextIssue} {
		t.Run(string(stage), func(t *testing.T) {
			cause := status.Error(codes.PermissionDenied, diagnosticSecret)
			issuer := &rejectedDiagnosticIssuer{err: cause}
			provider := &scriptedProofProvider{}
			if stage != StageContextIssue {
				provider.errors = []error{NewProofFailure(stage, cause)}
			}
			called := false
			err := IssuerUnaryClientInterceptor(issuer, fakeOperationResolver{"/test": "test"}, provider)(t.Context(), "/test", &emptypb.Empty{}, nil, nil,
				func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
					called = true
					return nil
				})
			var failure *LocalAuthorityError
			if !errors.As(err, &failure) || called || provider.calls != 1 || status.Code(err) != codes.Unauthenticated {
				t.Fatal("authority failure changed effects or classification")
			}
			if failure.Diagnostic().Stage != stage || failure.Diagnostic().Code != codes.PermissionDenied {
				t.Fatal("authority stage was lost")
			}
			if (stage == StageContextIssue && issuer.calls != 1) || (stage != StageContextIssue && issuer.calls != 0) {
				t.Fatal("issuer effect was repeated or reached without proof")
			}
			if strings.Contains(fmt.Sprintf("%+v", fmt.Errorf("startup: %w", err)), diagnosticSecret) || strings.Contains(failure.GRPCStatus().Message(), "authority_stage") {
				t.Fatal("private diagnostic escaped the local boundary")
			}
		})
	}
}

func TestAuthorityDiagnosticSanitizesDetailsAndUnknownEnums(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		detail := &api.AuthorizationErrorDetail{Reason: api.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_REPLAY_DETECTED, Stage: api.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_REPLAY, CorrelationId: diagnosticSecret}
		if invalid {
			detail.Reason = 999
			detail.Stage = 999
		}
		st, err := status.New(codes.Unauthenticated, diagnosticSecret).WithDetails(detail)
		if err != nil {
			t.Fatal(err)
		}
		for _, stage := range []DiagnosticStage{StageContextIssue, StageContinuationIssue, StageProofResolve} {
			failure := newLocalAuthorityError(st.Err(), "local-correlation", stage).(*LocalAuthorityError)
			diagnostic := failure.Diagnostic()
			wantDetail := !invalid && stage != StageProofResolve
			if (diagnostic.Reason != 0) != wantDetail || (diagnostic.AuthorityStage != 0) != wantDetail {
				t.Fatal("typed detail allowlist mismatch")
			}
			if strings.Contains(failure.Error(), diagnosticSecret) || len(failure.GRPCStatus().Details()) != 0 {
				t.Fatal("raw detail escaped")
			}
		}
	}
	value := Diagnostic{Stage: DiagnosticStage(diagnosticSecret), Code: codes.Code(999), Reason: 999, AuthorityStage: 999}
	if strings.Contains(value.String(), diagnosticSecret) || strings.Contains(value.String(), "999") {
		t.Fatal("unknown diagnostic value escaped")
	}
}

func TestDiagnosticProofFailurePreservesBoundedRetry(t *testing.T) {
	for _, code := range []codes.Code{codes.Canceled, codes.Unavailable, codes.DeadlineExceeded} {
		provider := &scriptedProofProvider{errors: []error{NewProofFailure(StageProofResolve, status.Error(code, diagnosticSecret))}}
		proof, _, err := authorityProofWithRetry(t.Context(), provider, "test", "/test")
		if err != nil || proof != "proof" || provider.calls != 2 || !provider.retryWasBounded {
			t.Fatal("diagnostic changed bounded retry")
		}
	}
}

func TestAuthorityDiagnosticRejectsUnknownAndAmbiguousDetails(t *testing.T) {
	for _, shape := range []string{"unknown_fields", "multiple", "foreign"} {
		detail := &api.AuthorizationErrorDetail{Reason: api.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_REPLAY_DETECTED, Stage: api.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_REPLAY}
		if shape == "unknown_fields" {
			detail.ProtoReflect().SetUnknown(append([]byte{0xa2, 0x06, byte(len(diagnosticSecret))}, []byte(diagnosticSecret)...))
		}
		st, err := status.New(codes.Unauthenticated, diagnosticSecret).WithDetails(detail)
		if err != nil {
			t.Fatal(err)
		}
		if shape == "multiple" {
			st, err = st.WithDetails(detail)
		}
		if shape == "foreign" {
			st, err = status.New(codes.Unauthenticated, diagnosticSecret).WithDetails(&emptypb.Empty{})
		}
		if err != nil {
			t.Fatal(err)
		}
		failure := newLocalAuthorityError(fmt.Errorf("%s: %w", diagnosticSecret, st.Err()), "local", StageContextIssue).(*LocalAuthorityError)
		if failure.Diagnostic().Reason != 0 || failure.Diagnostic().AuthorityStage != 0 || strings.Contains(failure.Error(), diagnosticSecret) {
			t.Fatal("untrusted details escaped")
		}
	}
}
