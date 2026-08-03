// Package continuationgrant проверяет выдаваемую только control-plane узкую
// capability следующего перехода integration continuation.
package continuationgrant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/integrationgatewayauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

type Config struct {
	Issuer         string
	Audience       string
	WorkloadID     string
	CallerSPIFFEID string
	PublicJWKFile  string
	Generation     uint64
	MaximumTTL     time.Duration
}

type Verifier struct {
	config Config
	grant  *integrationgatewayauth.Verifier
}

func New(config Config) (*Verifier, error) {
	grant, err := integrationgatewayauth.NewVerifier(integrationgatewayauth.Config{
		Issuer: config.Issuer, Audience: config.Audience, WorkloadID: config.WorkloadID,
		CallerSPIFFEID: config.CallerSPIFFEID, Generation: config.Generation, MaximumTTL: config.MaximumTTL,
	}, config.PublicJWKFile)
	if err != nil {
		return nil, err
	}
	return &Verifier{config: config, grant: grant}, nil
}

func (verifier *Verifier) VerifyPeer(ctx context.Context) error {
	peerValue, ok := peer.FromContext(ctx)
	if !ok {
		return errs.ErrUnauthenticated
	}
	tlsInfo, ok := peerValue.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.PeerCertificates) != 1 ||
		len(tlsInfo.State.PeerCertificates[0].URIs) != 1 {
		return errs.ErrUnauthenticated
	}
	if tlsInfo.State.PeerCertificates[0].URIs[0].String() != verifier.config.CallerSPIFFEID {
		return errs.ErrPermissionDenied
	}
	return nil
}

func (verifier *Verifier) Authenticate(ctx context.Context) (authoritytype.ApplicationIdentity, error) {
	if err := verifier.VerifyPeer(ctx); err != nil {
		return authoritytype.ApplicationIdentity{}, err
	}
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	values := incoming.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	compact := strings.TrimPrefix(values[0], "Bearer ")
	claims, err := verifier.grant.Verify(ctx, compact)
	if err != nil || claims.Purpose != integrationgatewayauth.PurposeTransition {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	return authoritytype.ApplicationIdentity{
		ActorID: claims.Subject, OrganizationID: claims.OrganizationID, ProjectID: claims.ProjectID,
		SessionJTI: claims.JTI, SessionRevision: claims.ContinuationVersion,
		SubjectDigest: digest("WORKLOAD_SUBJECT:" + claims.Subject), CredentialDigest: digest(compact),
		CallerWorkload: claims.WorkloadID, CallerSPIFFEID: claims.CallerSPIFFEID,
		BoundSessionID: claims.SessionID, BoundTurnID: claims.TurnID, BoundAttempt: claims.Attempt,
		BoundInputSHA256: claims.InputSHA256, BoundGeneration: claims.GrantGeneration,
		BoundRuntimeRevisionID:      claims.RuntimeRevisionID,
		BoundRuntimeRevisionVersion: claims.RuntimeRevisionVersion,
		BoundRuntimeRevisionSHA256:  claims.RuntimeRevisionSHA256,
		BoundContinuationID:         claims.ContinuationID, BoundContinuationVersion: claims.ContinuationVersion,
		BoundContinuationFence: claims.ContinuationFence, BoundInvocationID: claims.InvocationID,
		AllowedOperationIDs: claims.AllowedOperationIDs,
	}, nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
