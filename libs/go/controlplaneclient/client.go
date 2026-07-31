// Package controlplaneclient собирает exact producer proof, local issuer и mTLS client.
package controlplaneclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const maximumCredentialBytes = 16 << 10

type Config struct {
	Target                string
	TLSServerName         string
	CAFile                string
	ClientCertificateFile string
	ClientPrivateKeyFile  string
	ApplicationGrantFile  string
	ExpectedIssuerUID     uint32
	ExpectedIssuerGID     uint32
	DialTimeout           time.Duration
	Operations            map[string]string
}

type Client struct {
	ControlPlane controlplanev1.ControlPlaneServiceClient
	resolver     internalrpcauthorityv1.AuthorityProofResolverServiceClient
	issuer       *authorityclient.LocalConnection
	raw          *grpc.ClientConn
	protected    *grpc.ClientConn
	operations   operationSet
	grantFile    string
}

type operationSet map[string]string

func (operations operationSet) OperationID(fullMethod string) (string, bool) {
	value, ok := operations[fullMethod]
	return value, ok
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.Target == "" || config.TLSServerName == "" ||
		!filepath.IsAbs(config.CAFile) ||
		!filepath.IsAbs(config.ClientCertificateFile) ||
		!filepath.IsAbs(config.ClientPrivateKeyFile) ||
		!filepath.IsAbs(config.ApplicationGrantFile) ||
		config.ExpectedIssuerUID == 0 || config.ExpectedIssuerGID == 0 ||
		config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 5*time.Second || len(config.Operations) == 0 {
		return nil, errors.New("control-plane client configuration is invalid")
	}
	operations := make(operationSet, len(config.Operations))
	for operationID, fullMethod := range config.Operations {
		if operationID == "" || fullMethod == "" {
			return nil, errors.New("control-plane client operation is invalid")
		}
		if _, duplicate := operations[fullMethod]; duplicate {
			return nil, errors.New("control-plane client operation is duplicated")
		}
		operations[fullMethod] = operationID
	}
	transport, err := transportCredentials(config)
	if err != nil {
		return nil, err
	}
	raw, err := grpc.NewClient(
		config.Target,
		grpc.WithTransportCredentials(transport),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1<<20),
			grpc.MaxCallSendMsgSize(1<<20),
		),
	)
	if err != nil {
		return nil, errors.New("create control-plane resolver connection")
	}
	issuer, err := authorityclient.DialLocal(ctx, authorityclient.LocalConfig{
		SocketPath:        authorityclient.IssuerSocketPath,
		ExpectedServerUID: config.ExpectedIssuerUID,
		ExpectedServerGID: config.ExpectedIssuerGID,
		DialTimeout:       config.DialTimeout,
	})
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	client := &Client{
		resolver:   internalrpcauthorityv1.NewAuthorityProofResolverServiceClient(raw),
		issuer:     issuer,
		raw:        raw,
		operations: operations,
		grantFile:  config.ApplicationGrantFile,
	}
	protected, err := grpc.NewClient(
		config.Target,
		grpc.WithTransportCredentials(transport),
		grpc.WithUnaryInterceptor(authorityclient.IssuerUnaryClientInterceptor(
			issuer.Issuer(),
			operations,
			client,
		)),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1<<20),
			grpc.MaxCallSendMsgSize(1<<20),
		),
	)
	if err != nil {
		_ = issuer.Close()
		_ = raw.Close()
		return nil, errors.New("create protected control-plane connection")
	}
	client.protected = protected
	client.ControlPlane = controlplanev1.NewControlPlaneServiceClient(protected)
	return client, nil
}

func (client *Client) AuthorityProof(
	ctx context.Context,
	operationID string,
	fullMethod string,
) (string, string, error) {
	expectedOperation, ok := client.operations[fullMethod]
	if !ok || expectedOperation != operationID {
		return "", "", errors.New("control-plane operation is not registered")
	}
	grant, err := readCredential(client.grantFile)
	if err != nil {
		return "", "", err
	}
	correlationID := uuid.NewString()
	requestContext := metadata.AppendToOutgoingContext(
		ctx,
		"authorization",
		"Bearer "+grant,
	)
	resolved, err := client.resolver.ResolveAuthorityProof(
		requestContext,
		&internalrpcauthorityv1.ResolveAuthorityProofRequest{
			OperationId:    operationID,
			IdempotencyKey: uuid.NewString(),
			CorrelationId:  correlationID,
		},
	)
	if err != nil {
		return "", "", err
	}
	if resolved.GetAuthorityProofCompactJws() == "" ||
		resolved.GetProofRevision() == 0 ||
		resolved.GetSignerGeneration() == 0 {
		return "", "", errors.New("control-plane authority proof is incomplete")
	}
	return resolved.GetAuthorityProofCompactJws(), correlationID, nil
}

func (client *Client) Check(ctx context.Context) error {
	if _, err := client.resolver.CheckReadiness(
		ctx,
		&internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessRequest{},
	); err != nil {
		return errors.New("control-plane proof resolver is not ready")
	}
	issued, err := client.issuer.Issuer().CheckReadiness(
		ctx,
		&internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest{},
	)
	if err != nil || !issued.GetReady() {
		return errors.New("local authority issuer is not ready")
	}
	checked, err := client.ControlPlane.CheckReadiness(
		ctx,
		&controlplanev1.CheckReadinessRequest{},
	)
	if err != nil || !checked.GetReady() || !checked.GetAuthorityReady() {
		return errors.New("protected control-plane path is not ready")
	}
	return nil
}

func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	var result error
	if client.protected != nil {
		result = errors.Join(result, client.protected.Close())
	}
	if client.raw != nil {
		result = errors.Join(result, client.raw.Close())
	}
	if client.issuer != nil {
		result = errors.Join(result, client.issuer.Close())
	}
	return result
}

func transportCredentials(config Config) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(
		config.ClientCertificateFile,
		config.ClientPrivateKeyFile,
	)
	if err != nil {
		return nil, errors.New("load control-plane client identity")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read control-plane CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse control-plane CA")
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS13,
		ServerName:   config.TLSServerName,
		RootCAs:      roots,
		Certificates: []tls.Certificate{certificate},
	}), nil
}

func readCredential(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > maximumCredentialBytes || info.Mode().Perm()&0o007 != 0 {
		return "", errors.New("application grant file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("read application grant")
	}
	grant := strings.TrimSpace(string(raw))
	if grant == "" || len(grant) > maximumCredentialBytes {
		return "", errors.New("application grant is invalid")
	}
	return grant, nil
}
