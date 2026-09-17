package protectedrpc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const trustedCaller = "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"

func dialTrustedCluster(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil || ctx.Err() != nil || config.RPCProfile != transportprofile.TrustedCluster ||
		config.Policy.Target != "dns:///control-plane.kodex-system.svc:8443" ||
		config.Credential.Target != "dns:///secret-broker.kodex-system.svc:8443" ||
		config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second || config.Issuer != nil {
		return nil, errors.New("trusted STT dependency configuration rejected")
	}
	policy, err := newTrustedConnection(config.Policy.Target, false)
	if err != nil {
		return nil, err
	}
	credential, err := newTrustedConnection(config.Credential.Target, true)
	if err != nil {
		_ = policy.Close()
		return nil, err
	}
	return &Client{
		Authority:  sttv1.NewTranscriptionAuthorityServiceClient(policy),
		Policy:     sttv1.NewTranscriptionPolicyProjectionServiceClient(policy),
		Credential: sttv1.NewTranscriptionCredentialProjectionServiceClient(credential),
		policy:     policy, credential: credential, trustedCluster: true,
	}, nil
}

func newTrustedConnection(target string, credential bool) (*grpc.ClientConn, error) {
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(trustedUnary(credential)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)))
}

func trustedUnary(credential bool) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, response any, connection *grpc.ClientConn, next grpc.UnaryInvoker, options ...grpc.CallOption) error {
		allowed := method == sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName
		if !credential {
			allowed = method == sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName ||
				method == sttv1.TranscriptionAuthorityService_ResolveTranscriptionAuthority_FullMethodName ||
				method == sttv1.TranscriptionAuthorityService_ResolveTranscriptionCatalogAuthority_FullMethodName
		}
		md, _ := metadata.FromOutgoingContext(ctx)
		profiles, callers, credentials := md.Get(serviceidentity.ProfileMetadataKey), md.Get(serviceidentity.CallerMetadataKey), md.Get("authorization")
		if !allowed || len(profiles) != 1 || profiles[0] != transportprofile.TrustedCluster || len(callers) != 1 || callers[0] != trustedCaller ||
			len(credentials) != 1 || !validTrustedCredential(credentials[0]) {
			return status.Error(codes.PermissionDenied, "trusted STT dependency request rejected")
		}
		bound := metadata.NewOutgoingContext(ctx, metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
			serviceidentity.CallerMetadataKey, trustedCaller, "authorization", credentials[0]))
		return next(bound, method, request, response, connection, options...)
	}
}

func validTrustedCredential(credential string) bool {
	return len(credential) <= 16<<10 && strings.HasPrefix(credential, "Bearer ") &&
		len(strings.Fields(credential)) == 2 && strings.TrimSpace(credential) == credential
}

func bindTrustedDelegated(ctx context.Context, principal value.Principal, requestID, correlationID, method, operation string) (context.Context, error) {
	if ctx == nil || principal.RPCProfile != transportprofile.TrustedCluster || requestID == "" || requestID != principal.RequestID ||
		correlationID == "" || operation == "" || operationFor(method) != operation {
		return nil, errors.New("trusted STT delegated operation rejected")
	}
	resolved, err := authorization.Principal(ctx, sttv1.SpeechToTextService_Transcribe_FullMethodName)
	if err != nil || resolved != principal {
		return nil, errors.New("trusted STT delegated principal rejected")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	credentials := md.Get("authorization")
	if len(credentials) != 1 || !validTrustedCredential(credentials[0]) {
		return nil, errors.New("trusted STT delegated credential rejected")
	}
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
		serviceidentity.CallerMetadataKey, trustedCaller, "authorization", credentials[0])), nil
}

// Это только доступность внутренних соединений. User ACL, policy и credential
// проверяются отдельным CheckAvailability; readiness не доказывает STT outcome.
func (client *Client) checkTrustedConnections(ctx context.Context) error {
	if ctx == nil {
		return errors.New("trusted STT readiness context required")
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, connection := range []*grpc.ClientConn{client.policy, client.credential} {
		if connection == nil {
			return errors.New("trusted STT connection unavailable")
		}
		connection.Connect()
		for {
			state := connection.GetState()
			if state == connectivity.Ready {
				break
			}
			if state == connectivity.Shutdown || !connection.WaitForStateChange(bounded, state) {
				return errors.New("trusted STT connection is not ready")
			}
		}
	}
	return bounded.Err()
}
