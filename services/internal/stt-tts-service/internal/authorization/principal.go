// Package authorization преобразует только проверенный internal RPC context.
package authorization

import (
	"context"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/google/uuid"
)

const (
	expectedAudience          = "urn:kodex:internal-rpc:stt-tts-service"
	expectedWorkloadID        = "stt-tts-service"
	expectedCaller            = "control-api-gateway"
	transcribeOperation       = "platform.stt.transcribe"
	modelCatalogOperation     = "platform.stt.model-catalog.get"
	emptyCatalogRequestSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	maximumAuthorityRevision  = uint64(1<<53 - 1)
)

func Principal(ctx context.Context, fullMethod string) (value.Principal, error) {
	operation, permission, binding, domainPermission := transcribeOperation, value.TransportPermissionTranscribe, internalrpcauth.RequestBindingStream, value.PermissionTranscribe
	switch fullMethod {
	case sttv1.SpeechToTextService_Transcribe_FullMethodName:
	case sttv1.SpeechToTextService_GetModelCatalog_FullMethodName:
		operation, permission, binding, domainPermission = modelCatalogOperation, value.PermissionManageConfiguration, internalrpcauth.RequestBindingUnary, value.PermissionManageConfiguration
	default:
		return value.Principal{}, errors.New("verified STT method is invalid")
	}
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if !ok ||
		verified.GetContractVersion() != 1 || verified.GetAudience() != expectedAudience ||
		verified.GetAuthorityAbiVersion() != internalrpcauth.AuthorityABIVersion ||
		verified.GetRequestBindingMode() != binding ||
		verified.GetTargetWorkloadId() != expectedWorkloadID || verified.GetCallerWorkloadId() != expectedCaller ||
		verified.GetFullMethod() != fullMethod || verified.GetOperationId() != operation ||
		verified.GetPermission() != permission || verified.GetAuthority() == nil ||
		verified.GetExpiresAt() == nil || !verified.GetExpiresAt().IsValid() ||
		verified.GetSourceRevision() == 0 || verified.GetSourceRevision() > maximumAuthorityRevision ||
		!validSHA256(verified.GetSourceDigestSha256()) || uuid.Validate(verified.GetJti()) != nil {
		return value.Principal{}, errors.New("verified STT authorization context is invalid")
	}
	authority := verified.GetAuthority()
	if fullMethod == sttv1.SpeechToTextService_GetModelCatalog_FullMethodName &&
		(authority.GetProject() != nil || verified.GetContinuation() != nil ||
			verified.GetRequestDigestSha256() != emptyCatalogRequestSHA256) {
		return value.Principal{}, errors.New("verified STT catalog authority is invalid")
	}
	actor, err := identity(authority.GetActor())
	if err != nil {
		return value.Principal{}, errors.New("verified STT actor is invalid")
	}
	tenant, err := identity(authority.GetTenant())
	if err != nil {
		return value.Principal{}, errors.New("verified STT tenant is invalid")
	}
	var project verifiedIdentity
	if authority.GetProject() != nil {
		project, err = identity(authority.GetProject())
		if err != nil {
			return value.Principal{}, errors.New("verified STT project is invalid")
		}
	}
	return value.Principal{
		ActorID: actor.id, TenantID: tenant.id, ProjectID: project.id,
		Actor: actor.provenance, Tenant: tenant.provenance, Project: project.provenance,
		RequestID: verified.GetJti(), Permission: domainPermission,
		AuthorityRevision: verified.GetSourceRevision(), AuthorityDigestSHA256: verified.GetSourceDigestSha256(),
		ExpiresAt: verified.GetExpiresAt().AsTime().UTC(),
	}, nil
}

type verifiedIdentity struct {
	id         string
	provenance value.AuthorityProvenance
}

func identity(input *internalrpcauthorityv1.AuthorityIdentity) (verifiedIdentity, error) {
	if input == nil || input.GetProvenance() == nil {
		return verifiedIdentity{}, errors.New("authority identity is incomplete")
	}
	provenance := input.GetProvenance()
	id := strings.TrimSpace(input.GetId())
	reference := strings.TrimSpace(provenance.GetReference())
	_, knownSource := internalrpcauthorityv1.AuthoritySource_name[int32(provenance.GetSource())]
	if id == "" || reference == "" || !knownSource || provenance.GetSource() == internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_UNSPECIFIED ||
		provenance.GetRevision() == 0 || provenance.GetRevision() > maximumAuthorityRevision || !validSHA256(provenance.GetDigestSha256()) {
		return verifiedIdentity{}, errors.New("authority identity provenance is invalid")
	}
	return verifiedIdentity{id: id, provenance: value.AuthorityProvenance{
		Source: int32(provenance.GetSource()), Reference: reference, Revision: provenance.GetRevision(), DigestSHA256: provenance.GetDigestSha256(),
	}}, nil
}

func validSHA256(input string) bool {
	if len(input) != 64 {
		return false
	}
	for _, character := range input {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}
