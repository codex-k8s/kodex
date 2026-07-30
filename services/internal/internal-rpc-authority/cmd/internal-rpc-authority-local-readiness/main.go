package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "local authority readiness failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	config := struct {
		Role        string `env:"INTERNAL_RPC_AUTHORITY_LOCAL_ROLE,required,notEmpty"`
		ExpectedUID uint32 `env:"INTERNAL_RPC_AUTHORITY_LOCAL_EXPECTED_SERVER_UID"`
	}{}
	if err := env.Parse(&config); err != nil {
		return errors.New("parse local authority readiness configuration")
	}
	role := config.Role
	socketPath := authorityclient.IssuerSocketPath
	expectedUID := uint64(29001)
	if role == "verifier" {
		socketPath = authorityclient.VerifierSocketPath
		expectedUID = 29002
	} else if role != "issuer" {
		return errors.New("local authority role is invalid")
	}
	if config.ExpectedUID != 0 {
		expectedUID = uint64(config.ExpectedUID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, err := authorityclient.DialLocal(
		ctx,
		authorityclient.LocalConfig{
			SocketPath:        socketPath,
			ExpectedServerUID: uint32(expectedUID),
			ExpectedServerGID: 29000,
			DialTimeout:       time.Second,
		},
	)
	if err != nil {
		return err
	}
	defer connection.Close()
	if role == "issuer" {
		response, callErr := connection.Issuer().CheckReadiness(
			ctx,
			&internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest{},
		)
		if callErr != nil {
			return callErr
		}
		if !response.GetReady() ||
			response.GetSourceRevision() == 0 ||
			response.GetSnapshotDigestSha256() == "" {
			return errors.New("local issuer readiness binding rejected")
		}
		return nil
	}
	response, err := connection.Verifier().CheckReadiness(
		ctx,
		&internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest{},
	)
	if err != nil {
		return err
	}
	if !response.GetReady() ||
		!response.GetReplayStoreReady() ||
		response.GetSourceRevision() == 0 ||
		response.GetSnapshotDigestSha256() == "" {
		return errors.New("local verifier readiness binding rejected")
	}
	return nil
}
