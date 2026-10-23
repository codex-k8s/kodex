package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
)

// main runs the local authority readiness probe, prints the returned failure with a fixed English prefix and exits nonzero when the probe fails; it never starts an authority server.
// main запускает локальную проверку готовности authority, выводит возвращённую ошибку с фиксированным английским префиксом и завершается ненулевым кодом при отказе; сервер authority не запускается.
func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "local authority readiness failed: %v\n", err)
		os.Exit(1)
	}
}

// run parses the closed issuer/verifier role and optional expected server UID, selects the role's fixed Unix socket and default UID, and dials with the fixed group under a three-second root timeout and one-second dial budget. Issuer readiness requires ready, nonzero revision and a nonempty digest; verifier additionally requires replay-store readiness. This checks returned presence flags, not digest syntax or snapshot bytes, and closes the local connection on return.
// run разбирает закрытую роль issuer или verifier и необязательный ожидаемый UID сервера, выбирает фиксированный Unix-сокет роли и исходный UID и подключается с фиксированной группой при корневом сроке три секунды и бюджете dial одна секунда. Для issuer нужны ready, ненулевая ревизия и непустой дайджест; verifier дополнительно требует готовность replay-store. Проверяются возвращённые признаки присутствия, а не синтаксис дайджеста или байты снимка; локальное соединение закрывается при возврате.
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
