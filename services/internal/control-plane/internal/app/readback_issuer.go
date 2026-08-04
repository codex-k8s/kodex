package app

import (
	"context"

	readbackgrantauth "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/readbackgrant"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
)

type interactionReadbackIssuer struct{ signer *readbackgrantauth.Signer }

func (issuer interactionReadbackIssuer) Issue(ctx context.Context,
	claims resource.InteractionReadbackClaims) (resource.InteractionReadbackCredential, error) {
	compact, digest, state, err := issuer.signer.Sign(ctx, readbackgrantauth.Claims{
		Subject: claims.Subject, OrganizationID: claims.OrganizationID, ProjectID: claims.ProjectID,
		DeliveryID: claims.DeliveryID, JTI: claims.JTI, Readiness: claims.Readiness,
		IssuedAt: claims.IssuedAt.Unix(), NotBefore: claims.IssuedAt.Unix(), ExpiresAt: claims.ExpiresAt.Unix(),
	})
	if err != nil {
		return resource.InteractionReadbackCredential{}, err
	}
	return resource.InteractionReadbackCredential{Compact: compact, SHA256: digest,
		ProducerID: "control-plane.interaction-delivery-readback", Purpose: "INTERACTION_DELIVERY_READBACK_GRANT",
		WorkloadID: "control-plane", CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
		Operation: "interaction.delivery.read", Permission: "interaction.delivery.read",
		Generation: state.ServedGeneration, KeysetRevision: state.Revision,
		KeysetHighWatermark: state.HighWatermark, KeysetSHA256: state.KeysetSHA256}, nil
}

func (issuer interactionReadbackIssuer) Check(ctx context.Context) error {
	return issuer.signer.Check(ctx)
}
