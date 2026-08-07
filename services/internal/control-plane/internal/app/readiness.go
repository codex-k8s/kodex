package app

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/libs/go/cache"
	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	domainobjectstore "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/objectstore"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	authorityservice "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/authority"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
	transportgrpc "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/transport/grpc"
)

type readinessChecker struct {
	repository         domainrepo.Repository
	cache              cache.Store
	relay              *eventing.Relay
	proof              *authorityservice.Service
	verifier           internalrpcauthorityv1.AuthorizationVerifierServiceClient
	policyRevision     uint64
	instructionObjects domainobjectstore.Client
}

func (checker *readinessChecker) Check(
	ctx context.Context,
) (transportgrpc.ReadinessState, error) {
	state := transportgrpc.ReadinessState{}
	if err := checker.repository.Check(ctx); err != nil {
		return state, errors.New("postgresql runtime is not ready")
	}
	state.PostgresReady = true
	if err := checker.cache.Check(ctx); err != nil {
		return state, errors.New("redis cache is not ready")
	}
	state.RedisReady = true
	if err := checker.relay.Check(ctx); err != nil {
		return state, errors.New("outbox relay is not ready")
	}
	state.OutboxReady = true
	if checker.instructionObjects == nil || checker.instructionObjects.Check(ctx) != nil {
		return state, errors.New("instruction object store is not ready")
	}
	proofState, err := checker.proof.Check(ctx)
	if err != nil || proofState.PolicyRevision != checker.policyRevision {
		return state, errors.New("authority proof signer is not ready")
	}
	verified, err := checker.verifier.CheckReadiness(
		ctx,
		&internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest{},
	)
	if err != nil ||
		!verified.GetReady() ||
		!verified.GetReplayStoreReady() ||
		verified.GetPolicyRevision() != checker.policyRevision ||
		verified.GetSourceRevision() == 0 ||
		verified.GetKeySetRevision() == 0 ||
		verified.GetSignerGeneration() == 0 ||
		len(verified.GetSnapshotDigestSha256()) != 64 {
		return state, errors.New("internal RPC authority verifier is not ready")
	}
	state.AuthorityReady = true
	state.SchemaVersion = uint64(schema.CurrentVersion)
	return state, nil
}
