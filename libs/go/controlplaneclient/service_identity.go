package controlplaneclient

import (
	"context"
	"errors"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// DialServiceIdentity создаёт отдельный явный профиль обычных RPC. Эти вызовы
// не читают platform grant и не обращаются к UDS issuer или proof resolver.
// Переходный client сохраняет прежние зависимости только для явного выпуска
// proof к другим target; ошибка service-v1 не включает legacy fallback.
func DialServiceIdentity(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, errors.New("control-plane client context unavailable")
	}
	if config.Target == "" || config.TLSServerName == "" || !filepath.IsAbs(config.CAFile) || !filepath.IsAbs(config.ClientCertificateFile) || !filepath.IsAbs(config.ClientPrivateKeyFile) || config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second || len(config.Operations) == 0 {
		return nil, errors.New("service identity client configuration rejected")
	}
	operations, err := validateOperations(config.Operations)
	if err != nil {
		return nil, err
	}
	projects, err := serviceProjectOperations(operations, config.ProofOperations, config.ProjectRequiredOperations)
	if err != nil {
		return nil, err
	}
	legacyConfig := config
	legacyConfig.ServiceIdentity = false
	client, err := Dial(ctx, legacyConfig)
	if err != nil {
		return nil, err
	}
	transport, err := transportCredentials(config.TLSServerName, config.CAFile, config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	interceptors := []grpc.UnaryClientInterceptor{serviceIdentityUnary(operations, projects)}
	if config.UnaryClientInterceptor != nil {
		interceptors = append(interceptors, config.UnaryClientInterceptor)
	}
	connection, err := grpc.NewClient(config.Target, grpc.WithTransportCredentials(transport), grpc.WithChainUnaryInterceptor(interceptors...), grpc.WithStreamInterceptor(serviceIdentityStream(operations, projects)), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maximumProtectedResponseBytes), grpc.MaxCallSendMsgSize(17<<20)))
	if err != nil {
		_ = client.Close()
		return nil, errors.New("create service identity control-plane connection")
	}
	if err = client.protected.Close(); err != nil {
		_ = connection.Close()
		_ = client.Close()
		return nil, errors.New("replace legacy control-plane connection")
	}
	client.serviceIdentity = true
	client.bindServices(connection)
	return client, nil
}

func serviceProjectOperations(operations operationSet, proofOperations map[string]string, required map[string]struct{}) (map[string]struct{}, error) {
	projects := make(map[string]struct{}, len(required))
	for operation := range required {
		found := false
		for _, registered := range operations {
			if operation == registered {
				found = true
				break
			}
		}
		if !found {
			_, found = proofOperations[operation]
		}
		if !found {
			return nil, errors.New("service project operation is not registered")
		}
		projects[operation] = struct{}{}
	}
	return projects, nil
}

func serviceIdentityStream(operations operationSet, projects map[string]struct{}) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, conn *grpc.ClientConn, method string, next grpc.Streamer, options ...grpc.CallOption) (grpc.ClientStream, error) {
		if method != cp.RuntimeWorkService_StreamExecutionArtifact_FullMethodName || desc == nil || desc.ClientStreams || !desc.ServerStreams {
			return nil, status.Error(codes.PermissionDenied, "service stream method rejected")
		}
		var stream grpc.ClientStream
		err := serviceIdentityUnary(operations, projects)(ctx, method, nil, nil, conn, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			var err error
			stream, err = next(ctx, desc, conn, method, options...)
			return err
		})
		return stream, err
	}
}

func serviceIdentityUnary(operations operationSet, projects map[string]struct{}) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, response any, connection *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
		operation, ok := operations[method]
		if !ok {
			return status.Error(codes.PermissionDenied, "service RPC operation is not registered")
		}
		md, _ := metadata.FromOutgoingContext(ctx)
		md = md.Copy()
		// Поля прежнего вызова не переносятся через переиспользованный context.
		md.Delete("x-kodex-authorization")
		md.Delete("authorization")
		md.Delete("x-kodex-project-ref")
		md.Set("x-kodex-rpc-profile", "service-v1")
		if credential, ok := ctx.Value(applicationGrantContextKey{}).(string); ok {
			md.Set("authorization", "Bearer "+credential)
		}
		if _, required := projects[operation]; required {
			project, _ := ctx.Value(projectReferenceContextKey{}).(string)
			if !validOpaqueReference(project, "prj") {
				return status.Error(codes.InvalidArgument, "service RPC project reference required")
			}
			md.Set("x-kodex-project-ref", project)
		}
		return invoke(metadata.NewOutgoingContext(ctx, md), method, request, response, connection, options...)
	}
}
