package sttclient

import (
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	stt "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func dialTrustedCluster(config Config) (*Client, error) {
	if config.RPCProfile != "trusted-cluster" || strings.TrimPrefix(config.Target, "dns:///") != "stt-tts-service.kodex-system.svc:8443" {
		return nil, errors.New("trusted STT target or profile rejected")
	}
	operations := controlplaneclient.STTGatewayOperations()
	unary, err := controlplaneclient.TrustedUnaryClientInterceptor(config.RPCProfile, "control-api-gateway", operations)
	if err != nil {
		return nil, err
	}
	stream, err := controlplaneclient.TrustedStreamClientInterceptor(config.RPCProfile, "control-api-gateway", operations)
	if err != nil {
		return nil, err
	}
	connection, err := grpc.NewClient(config.Target, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(unary), grpc.WithStreamInterceptor(stream),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(65<<10), grpc.MaxCallRecvMsgSize(1<<20)))
	if err != nil {
		return nil, errors.New("create trusted STT connection")
	}
	return &Client{Speech: stt.NewSpeechToTextServiceClient(connection), connection: connection}, nil
}
