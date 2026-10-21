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

// DialServiceIdentity replaces the ordinary RPC connection with an exact TLS service-v1 client after validating operations.
// DialServiceIdentity заменяет соединение обычных RPC на точный TLS-клиент service-v1 после проверки операций.
// Other explicitly requested proof paths retain their separate dependencies; a failed service-v1 call is never retried via them.
// Иные явно запрошенные proof-пути сохраняют отдельные зависимости; ошибка service-v1 не повторяется через них.
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

// serviceProjectOperations requires each project-scoped operation to exist in either the ordinary or proof registry.
// serviceProjectOperations требует наличия каждой проектной операции в обычном реестре либо реестре proof.
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

// serviceIdentityStream fixes the service-v1 profile for the closed set of permitted upload and download streams.
// serviceIdentityStream закрепляет профиль service-v1 для закрытого набора разрешённых потоков загрузки и скачивания.
func serviceIdentityStream(operations operationSet, projects map[string]struct{}) grpc.StreamClientInterceptor {
	return serviceStream(operations, projects, "service-v1", "")
}

// serviceStream rejects unsupported stream methods or directions before applying the same operation and metadata checks as unary RPC.
// serviceStream отвергает неизвестные методы и направления потоков до общих проверок операции и метаданных unary RPC.
func serviceStream(operations operationSet, projects map[string]struct{}, profile, caller string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, conn *grpc.ClientConn, method string, next grpc.Streamer, options ...grpc.CallOption) (grpc.ClientStream, error) {
		serverStream := (method == cp.RuntimeWorkService_StreamExecutionArtifact_FullMethodName ||
			method == cp.PlatformCommandService_DownloadArtifact_FullMethodName ||
			method == cp.PlatformCommandService_DownloadArtifactRevision_FullMethodName) && desc != nil && !desc.ClientStreams && desc.ServerStreams
		clientStream := (method == cp.PlatformCommandService_UploadArtifact_FullMethodName ||
			method == cp.PlatformCommandService_UploadOrganizationArtifact_FullMethodName ||
			method == cp.PlatformCommandService_UploadAgentAvatar_FullMethodName) && desc != nil && desc.ClientStreams && !desc.ServerStreams
		if !serverStream && !clientStream {
			return nil, status.Error(codes.PermissionDenied, "service stream method rejected")
		}
		var stream grpc.ClientStream
		err := serviceUnary(operations, projects, profile, caller)(ctx, method, nil, nil, conn, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			var err error
			stream, err = next(ctx, desc, conn, method, options...)
			return err
		})
		return stream, err
	}
}

// serviceIdentityUnary binds ordinary unary calls to the service-v1 profile without a synthetic trusted caller.
// serviceIdentityUnary привязывает обычные unary-вызовы к профилю service-v1 без искусственного trusted caller.
func serviceIdentityUnary(operations operationSet, projects map[string]struct{}) grpc.UnaryClientInterceptor {
	return serviceUnary(operations, projects, "service-v1", "")
}

// serviceUnary replaces stale outgoing authority headers and forwards only a registered operation with explicit grant and project pins.
// serviceUnary заменяет прежние заголовки полномочий и отправляет лишь зарегистрированную операцию с явными grant и project pins.
func serviceUnary(operations operationSet, projects map[string]struct{}, profile, caller string) grpc.UnaryClientInterceptor {
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
		md.Delete("x-kodex-trusted-caller")
		md.Set("x-kodex-rpc-profile", profile)
		if caller != "" {
			md.Set("x-kodex-trusted-caller", caller)
		}
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
