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
	"time"

	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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
	Integration integrationgatewayv1.IntegrationManagementServiceClient
	issuer      *authorityclient.LocalConnection
	interaction *grpc.ClientConn
	integration *grpc.ClientConn
}

type operationSet map[string]string

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
	client.interaction, err = dial(ctx, config.InteractionTarget, config.InteractionTLSServerName, config, issuer, interactionOperations(), proofs)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	client.integration, err = dial(ctx, config.IntegrationTarget, config.IntegrationTLSServerName, config, issuer, integrationOperations(), proofs)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	client.Interaction = interactiongatewayv1.NewMattermostTeamServiceClient(client.interaction)
	client.Integration = integrationgatewayv1.NewIntegrationManagementServiceClient(client.integration)
	return client, nil
}

func dial(ctx context.Context, target, serverName string, config Config, issuer *authorityclient.LocalConnection, operations operationSet, proofs authorityclient.ProofProvider) (*grpc.ClientConn, error) {
	transport, err := transportCredentials(config.CAFile, config.ClientCertificateFile, config.ClientPrivateKeyFile, serverName)
	if err != nil {
		return nil, err
	}
	interceptors := []grpc.UnaryClientInterceptor{authorityclient.IssuerUnaryClientInterceptor(issuer.Issuer(), operations, proofs)}
	if config.UnaryClientInterceptor != nil {
		interceptors = append(interceptors, config.UnaryClientInterceptor)
	}
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
	interaction, interactionErr := client.Interaction.CheckReadiness(ctx, &interactiongatewayv1.MattermostTeamServiceCheckReadinessRequest{})
	integration, integrationErr := client.Integration.GetManagementDiagnostics(ctx, &integrationgatewayv1.GetManagementDiagnosticsRequest{})
	if interactionErr != nil || interaction == nil || !interaction.GetReady() || !interaction.GetAuthorityReady() ||
		integrationErr != nil || integration == nil || integration.GetStatus() != "READY" {
		return errors.New("protected owner RPC path is not ready")
	}
	return nil
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
		interactiongatewayv1.MattermostTeamService_ListMattermostTeams_FullMethodName:               "interaction.team.catalog.read",
		interactiongatewayv1.MattermostTeamService_CreateMattermostTeam_FullMethodName:              "interaction.team.create",
		interactiongatewayv1.MattermostTeamService_LinkMattermostTeam_FullMethodName:                "interaction.team.link",
		interactiongatewayv1.MattermostTeamService_GetMattermostTeamBinding_FullMethodName:          "interaction.team.binding.get",
		interactiongatewayv1.MattermostTeamService_RelinkMattermostTeam_FullMethodName:              "interaction.team.relink",
		interactiongatewayv1.MattermostTeamService_UnlinkMattermostTeam_FullMethodName:              "interaction.team.unlink",
		interactiongatewayv1.MattermostTeamService_GetMattermostTeamMappingOperation_FullMethodName: "interaction.team.mapping-operation.get",
		interactiongatewayv1.MattermostTeamService_GetMattermostTeamProviderReadback_FullMethodName: "interaction.team.provider.readback",
		interactiongatewayv1.MattermostTeamService_CheckReadiness_FullMethodName:                    "interaction.team.readiness",
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
