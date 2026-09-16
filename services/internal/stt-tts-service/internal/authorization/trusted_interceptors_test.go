package authorization

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type trustedStreamFixture struct {
	grpc.ServerStream
	ctx   context.Context
	reads int
}

func (stream *trustedStreamFixture) Context() context.Context { return stream.ctx }
func (stream *trustedStreamFixture) RecvMsg(any) error        { stream.reads++; return nil }

func TestTrustedStreamResolvesBeforeAudioAndPreservesCancellation(t *testing.T) {
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	method := sttv1.SpeechToTextService_Transcribe_FullMethodName
	authorizer, err := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, trustedSTTCaller, []serviceidentity.Binding{{
		CallerSPIFFEID: caller, FullMethod: method, OperationID: transcribeOperation, Permission: value.TransportPermissionTranscribe, ActorMode: serviceidentity.UserActor,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, accepted := range []bool{true, false} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		md := metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, caller)
		if accepted {
			md.Set("authorization", "Bearer synthetic-session")
		}
		ctx = metadata.NewIncomingContext(ctx, md)
		stream := &trustedStreamFixture{ctx: ctx}
		client := &trustedOwnerClient{snapshot: &sttv1.TrustedTranscriptionAuthority{
			RpcProfile: transportprofile.TrustedCluster, RequestId: "33333333-3333-4333-8333-333333333333",
			ActorId: "11111111-1111-4111-8111-111111111111", TenantId: "22222222-2222-4222-8222-222222222222",
			CredentialRevision: 9, Permission: value.PermissionTranscribe, ExpiresAt: timestamppb.New(time.Now().Add(5 * time.Second)),
		}}
		resolver, err := NewTrustedResolver(transportprofile.TrustedCluster, client)
		if err != nil {
			t.Fatal(err)
		}
		info := &grpc.StreamServerInfo{FullMethod: method, IsClientStream: true}
		called := false
		err = authorizer.StreamServerInterceptor()(nil, stream, info, func(server any, admitted grpc.ServerStream) error {
			return resolver.StreamServerInterceptor()(server, admitted, info, func(_ any, bound grpc.ServerStream) error {
				called = true
				if client.calls != 1 || stream.reads != 0 {
					t.Fatal("audio read before owner resolution")
				}
				if _, err := Principal(bound.Context(), method); err != nil {
					t.Fatal(err)
				}
				originalDeadline, _ := ctx.Deadline()
				actualDeadline, _ := bound.Context().Deadline()
				if !actualDeadline.Equal(originalDeadline) {
					t.Fatal("stream deadline changed")
				}
				if err := bound.RecvMsg(nil); err != nil {
					return err
				}
				cancel()
				if bound.Context().Err() != context.Canceled {
					t.Fatal("stream cancellation lost")
				}
				if _, err := Principal(bound.Context(), method); err == nil {
					t.Fatal("cancelled identity accepted")
				}
				return nil
			})
		})
		cancel()
		if accepted && (err != nil || !called || stream.reads != 1) {
			t.Fatal("authorized stream rejected")
		}
		if !accepted && (status.Code(err) != codes.PermissionDenied || called || stream.reads != 0 || client.calls != 0) {
			t.Fatal("unauthorized stream consumed audio")
		}
		if _, err := Principal(stream.Context(), method); err == nil {
			t.Fatal("original stream acquired authority")
		}
	}
}
