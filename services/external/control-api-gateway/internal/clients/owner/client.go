// Package owner подключает control-api-gateway к exact owner RPC interaction
// и integration gateway с тем же application proof, что browser request.
package owner

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const maximumRPCMessageBytes = 4 << 20

type Config struct {
	InteractionTarget        string
	InteractionTLSServerName string
	IntegrationTarget        string
	IntegrationTLSServerName string
	CAFile                   string
	ClientCertificateFile    string
	ClientPrivateKeyFile     string
	ExpectedIssuerUID        uint32
	ExpectedIssuerGID        uint32
	DialTimeout              time.Duration
	UnaryClientInterceptor   grpc.UnaryClientInterceptor
}

type Client struct {
	Interaction interactiongatewayv1.MattermostTeamServiceClient
	Bot         interactiongatewayv1.AgentMattermostBotIdentityServiceClient
	Integration integrationgatewayv1.IntegrationManagementServiceClient
	issuer      *authorityclient.LocalConnection
	interaction *grpc.ClientConn
	integration *grpc.ClientConn
}

type operationSet map[string]string

type RPCSource string

const (
	RPCSourceInteraction RPCSource = "interaction"
	RPCSourceIntegration RPCSource = "integration"
)

type DownstreamError struct {
	Source           RPCSource
	Method           string
	NormalizedDetail *controlplanev1.ErrorDetail
	Err              error
}

func (err *DownstreamError) Error() string { return err.Err.Error() }
func (err *DownstreamError) Unwrap() error { return err.Err }

func (err *DownstreamError) Valid() bool {
	if err == nil || err.Err == nil || err.Method == "" {
		return false
	}
	switch err.Source {
	case RPCSourceInteraction:
		_, ok := interactionOperations()[err.Method]
		return ok
	case RPCSourceIntegration:
		_, ok := integrationOperations()[err.Method]
		return ok
	default:
		return false
	}
}

func (operations operationSet) OperationID(fullMethod string) (string, bool) {
	operation, ok := operations[fullMethod]
	return operation, ok
}

func Dial(ctx context.Context, config Config, proofs authorityclient.ProofProvider) (*Client, error) {
	if proofs == nil || config.InteractionTarget == "" || config.IntegrationTarget == "" ||
		config.InteractionTLSServerName == "" || config.IntegrationTLSServerName == "" ||
		net.ParseIP(config.InteractionTLSServerName) != nil || net.ParseIP(config.IntegrationTLSServerName) != nil ||
		!filepath.IsAbs(config.CAFile) || !filepath.IsAbs(config.ClientCertificateFile) ||
		!filepath.IsAbs(config.ClientPrivateKeyFile) || config.ExpectedIssuerUID == 0 ||
		config.ExpectedIssuerGID == 0 || config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second {
		return nil, errors.New("owner RPC client configuration is invalid")
	}
	issuer, err := authorityclient.DialLocal(ctx, authorityclient.LocalConfig{
		SocketPath: authorityclient.IssuerSocketPath, ExpectedServerUID: config.ExpectedIssuerUID,
		ExpectedServerGID: config.ExpectedIssuerGID, DialTimeout: config.DialTimeout,
	})
	if err != nil {
		return nil, err
	}
	client := &Client{issuer: issuer}
	client.interaction, err = dial(ctx, config.InteractionTarget, config.InteractionTLSServerName, config, issuer, interactionOperations(), proofs, RPCSourceInteraction)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	client.integration, err = dial(ctx, config.IntegrationTarget, config.IntegrationTLSServerName, config, issuer, integrationOperations(), proofs, RPCSourceIntegration)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	client.Interaction = interactiongatewayv1.NewMattermostTeamServiceClient(client.interaction)
	client.Bot = interactiongatewayv1.NewAgentMattermostBotIdentityServiceClient(client.interaction)
	client.Integration = integrationgatewayv1.NewIntegrationManagementServiceClient(client.integration)
	return client, nil
}

func dial(ctx context.Context, target, serverName string, config Config, issuer *authorityclient.LocalConnection, operations operationSet, proofs authorityclient.ProofProvider, source RPCSource) (*grpc.ClientConn, error) {
	transport, err := transportCredentials(config.CAFile, config.ClientCertificateFile, config.ClientPrivateKeyFile, serverName)
	if err != nil {
		return nil, err
	}
	interceptors := []grpc.UnaryClientInterceptor{authorityclient.IssuerUnaryClientInterceptor(issuer.Issuer(), operations, proofs)}
	if config.UnaryClientInterceptor != nil {
		interceptors = append(interceptors, config.UnaryClientInterceptor)
	}
	interceptors = append(interceptors, sourceErrorInterceptor(source))
	connection, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(transport),
		grpc.WithChainUnaryInterceptor(interceptors...),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maximumRPCMessageBytes), grpc.MaxCallSendMsgSize(maximumRPCMessageBytes)),
	)
	if err != nil {
		return nil, errors.New("create owner RPC connection")
	}
	if err := ctx.Err(); err != nil {
		_ = connection.Close()
		return nil, errors.New("owner RPC connection context canceled")
	}
	connection.Connect()
	return connection, nil
}

func sourceErrorInterceptor(source RPCSource) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		err := invoker(ctx, method, request, reply, connection, options...)
		if err == nil {
			return nil
		}
		return &DownstreamError{Source: source, Method: method, NormalizedDetail: normalizeBareError(source, method, err), Err: err}
	}
}

func normalizeBareError(source RPCSource, method string, err error) *controlplanev1.ErrorDetail {
	current, ok := status.FromError(err)
	if !ok || len(current.Details()) != 0 || source == RPCSourceInteraction && strings.HasPrefix(method, "/interactiongateway.v1.AgentMattermostBotIdentityService/") {
		return nil
	}
	type normalized struct {
		reason    controlplanev1.ErrorReason
		code      string
		retryable bool
	}
	value, known := map[codes.Code]normalized{
		codes.InvalidArgument:    {controlplanev1.ErrorReason_ERROR_REASON_INVALID_REQUEST, "INVALID_REQUEST", false},
		codes.Unauthenticated:    {controlplanev1.ErrorReason_ERROR_REASON_UNAUTHENTICATED, "UNAUTHENTICATED", false},
		codes.PermissionDenied:   {controlplanev1.ErrorReason_ERROR_REASON_PERMISSION_DENIED, "PERMISSION_DENIED", false},
		codes.NotFound:           {controlplanev1.ErrorReason_ERROR_REASON_NOT_FOUND, "NOT_FOUND", false},
		codes.FailedPrecondition: {controlplanev1.ErrorReason_ERROR_REASON_STATE_CONFLICT, "STATE_CONFLICT", false},
		codes.AlreadyExists:      {controlplanev1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT, "IDEMPOTENCY_CONFLICT", false},
		codes.Aborted:            {controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH, "VERSION_MISMATCH", true},
		codes.ResourceExhausted:  {controlplanev1.ErrorReason_ERROR_REASON_RATE_LIMITED, "RATE_LIMITED", true},
		codes.Unavailable:        {controlplanev1.ErrorReason_ERROR_REASON_UNAVAILABLE, "UNAVAILABLE", true},
		codes.DeadlineExceeded:   {controlplanev1.ErrorReason_ERROR_REASON_UNAVAILABLE, "DEADLINE_EXCEEDED", true},
		codes.Internal:           {controlplanev1.ErrorReason_ERROR_REASON_INTERNAL, "INTERNAL", false},
	}[current.Code()]
	if !known {
		return nil
	}
	return &controlplanev1.ErrorDetail{Reason: value.reason, Code: value.code, CorrelationId: uuid.NewString(), Retryable: value.retryable}
}

func transportCredentials(caFile, certificateFile, keyFile, serverName string) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(certificateFile, keyFile)
	if err != nil {
		return nil, errors.New("load owner RPC client identity")
	}
	raw, err := os.ReadFile(caFile)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return nil, errors.New("read owner RPC CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return nil, errors.New("parse owner RPC CA")
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots, Certificates: []tls.Certificate{certificate}}), nil
}

func (client *Client) Check(ctx context.Context) error {
	if client == nil || client.issuer == nil || client.interaction == nil || client.integration == nil {
		return errors.New("owner RPC transport path is not ready")
	}
	issuer, err := client.issuer.Issuer().CheckReadiness(
		ctx,
		&internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest{},
	)
	if err != nil || !issuer.GetReady() || issuer.GetSourceRevision() == 0 ||
		issuer.GetKeySetRevision() == 0 || issuer.GetPolicyRevision() == 0 ||
		issuer.GetSignerGeneration() == 0 {
		return errors.New("owner RPC authority issuer is not ready")
	}
	if err := waitForReady(ctx, client.interaction); err != nil {
		return errors.New("interaction owner RPC transport is not ready")
	}
	if err := waitForReady(ctx, client.integration); err != nil {
		return errors.New("integration owner RPC transport is not ready")
	}
	return nil
}

func waitForReady(ctx context.Context, connection *grpc.ClientConn) error {
	if ctx == nil || connection == nil {
		return errors.New("owner RPC connection is required")
	}
	connection.Connect()
	for {
		state := connection.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown {
			return errors.New("owner RPC connection is shut down")
		}
		if !connection.WaitForStateChange(ctx, state) {
			return errors.New("owner RPC connection readiness deadline exceeded")
		}
	}
}

func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	var result error
	if client.interaction != nil {
		result = errors.Join(result, client.interaction.Close())
	}
	if client.integration != nil {
		result = errors.Join(result, client.integration.Close())
	}
	if client.issuer != nil {
		result = errors.Join(result, client.issuer.Close())
	}
	return result
}

func interactionOperations() operationSet {
	return operationSet{
		interactiongatewayv1.MattermostTeamService_ListMattermostTeams_FullMethodName:                                       "interaction.team.catalog.read",
		interactiongatewayv1.MattermostTeamService_CreateMattermostTeam_FullMethodName:                                      "interaction.team.create",
		interactiongatewayv1.MattermostTeamService_LinkMattermostTeam_FullMethodName:                                        "interaction.team.link",
		interactiongatewayv1.MattermostTeamService_GetMattermostTeamBinding_FullMethodName:                                  "interaction.team.binding.get",
		interactiongatewayv1.MattermostTeamService_RelinkMattermostTeam_FullMethodName:                                      "interaction.team.relink",
		interactiongatewayv1.MattermostTeamService_UnlinkMattermostTeam_FullMethodName:                                      "interaction.team.unlink",
		interactiongatewayv1.MattermostTeamService_GetMattermostTeamMappingOperation_FullMethodName:                         "interaction.team.mapping-operation.get",
		interactiongatewayv1.MattermostTeamService_GetMattermostTeamProviderReadback_FullMethodName:                         "interaction.team.provider.readback",
		interactiongatewayv1.MattermostTeamService_CheckReadiness_FullMethodName:                                            "interaction.team.readiness",
		interactiongatewayv1.AgentMattermostBotIdentityService_ListAgentMattermostBotIdentities_FullMethodName:              "interaction.agent-bot.catalog.read",
		interactiongatewayv1.AgentMattermostBotIdentityService_CreateAndBindAgentMattermostBotIdentity_FullMethodName:       "interaction.agent-bot.create-and-bind",
		interactiongatewayv1.AgentMattermostBotIdentityService_BindAgentMattermostBotIdentity_FullMethodName:                "interaction.agent-bot.bind",
		interactiongatewayv1.AgentMattermostBotIdentityService_GetAgentMattermostBotIdentity_FullMethodName:                 "interaction.agent-bot.get",
		interactiongatewayv1.AgentMattermostBotIdentityService_RebindAgentMattermostBotIdentity_FullMethodName:              "interaction.agent-bot.rebind",
		interactiongatewayv1.AgentMattermostBotIdentityService_RevokeAgentMattermostBotIdentity_FullMethodName:              "interaction.agent-bot.revoke",
		interactiongatewayv1.AgentMattermostBotIdentityService_GetAgentMattermostBotIdentityOperation_FullMethodName:        "interaction.agent-bot.operation.get",
		interactiongatewayv1.AgentMattermostBotIdentityService_GetAgentMattermostBotIdentityProviderReadback_FullMethodName: "interaction.agent-bot.provider.readback",
		interactiongatewayv1.AgentMattermostBotIdentityService_CheckAgentMattermostBotIdentityReadiness_FullMethodName:      "interaction.agent-bot.readiness",
	}
}

func integrationOperations() operationSet {
	return operationSet{
		integrationgatewayv1.IntegrationManagementService_ListProviders_FullMethodName:                 "integration.management.provider.catalog.list",
		integrationgatewayv1.IntegrationManagementService_GetProvider_FullMethodName:                   "integration.management.provider.catalog.get",
		integrationgatewayv1.IntegrationManagementService_StartProviderAuthorization_FullMethodName:    "integration.management.provider.authorization.start",
		integrationgatewayv1.IntegrationManagementService_GetProviderAuthorization_FullMethodName:      "integration.management.provider.authorization.get",
		integrationgatewayv1.IntegrationManagementService_RestartProviderAuthorization_FullMethodName:  "integration.management.provider.authorization.restart",
		integrationgatewayv1.IntegrationManagementService_CancelProviderAuthorization_FullMethodName:   "integration.management.provider.authorization.cancel",
		integrationgatewayv1.IntegrationManagementService_ListProviderConnections_FullMethodName:       "integration.management.provider.connection.list",
		integrationgatewayv1.IntegrationManagementService_GetProviderConnection_FullMethodName:         "integration.management.provider.connection.get",
		integrationgatewayv1.IntegrationManagementService_ReauthorizeProviderConnection_FullMethodName: "integration.management.provider.connection.reauthorize",
		integrationgatewayv1.IntegrationManagementService_RevokeProviderConnection_FullMethodName:      "integration.management.provider.connection.revoke",
		integrationgatewayv1.IntegrationManagementService_ManageProviderPool_FullMethodName:            "integration.management.provider.pool.manage",
		integrationgatewayv1.IntegrationManagementService_GetProviderPool_FullMethodName:               "integration.management.provider.pool.get",
		integrationgatewayv1.IntegrationManagementService_ListProviderPools_FullMethodName:             "integration.management.provider.pool.list",
		integrationgatewayv1.IntegrationManagementService_ListIntegrationDefinitions_FullMethodName:    "integration.management.definition.list",
		integrationgatewayv1.IntegrationManagementService_GetIntegrationDefinition_FullMethodName:      "integration.management.definition.get",
		integrationgatewayv1.IntegrationManagementService_ConfigureIntegration_FullMethodName:          "integration.management.configuration.manage",
		integrationgatewayv1.IntegrationManagementService_GetIntegrationConfiguration_FullMethodName:   "integration.management.configuration.get",
		integrationgatewayv1.IntegrationManagementService_ListIntegrationConfigurations_FullMethodName: "integration.management.configuration.list",
		integrationgatewayv1.IntegrationManagementService_TestIntegrationConnection_FullMethodName:     "integration.management.test.start",
		integrationgatewayv1.IntegrationManagementService_GetIntegrationTestReceipt_FullMethodName:     "integration.management.test.get",
		integrationgatewayv1.IntegrationManagementService_ListIntegrationApprovals_FullMethodName:      "integration.management.approval.list",
		integrationgatewayv1.IntegrationManagementService_GetIntegrationApproval_FullMethodName:        "integration.management.approval.get",
		integrationgatewayv1.IntegrationManagementService_DecideIntegrationApproval_FullMethodName:     "integration.management.approval.decide",
		integrationgatewayv1.IntegrationManagementService_GetManagementDiagnostics_FullMethodName:      "integration.management.diagnostics.get",
	}
}

// ProofOperations возвращает разрешённый resolver-профиль owner RPC.
func ProofOperations() map[string]string {
	result := make(map[string]string, len(interactionOperations())+len(integrationOperations()))
	for method, operationID := range interactionOperations() {
		result[operationID] = method
	}
	for method, operationID := range integrationOperations() {
		result[operationID] = method
	}
	return result
}

// ProjectRequiredOperations фиксирует project-scoped owner RPC. Глобальные
// health/readiness операции используют только проверенные tenant и actor.
func ProjectRequiredOperations() map[string]struct{} {
	result := make(map[string]struct{}, len(interactionOperations())+len(integrationOperations()))
	for _, operationID := range interactionOperations() {
		result[operationID] = struct{}{}
	}
	for _, operationID := range integrationOperations() {
		result[operationID] = struct{}{}
	}
	delete(result, "interaction.team.readiness")
	delete(result, "integration.management.diagnostics.get")
	return result
}

// MethodOperations возвращает закрытый telemetry registry full method → operation.
func MethodOperations() map[string]string {
	result := make(map[string]string, len(interactionOperations())+len(integrationOperations()))
	for method, operation := range interactionOperations() {
		result[method] = operation
	}
	for method, operation := range integrationOperations() {
		result[method] = operation
	}
	return result
}
