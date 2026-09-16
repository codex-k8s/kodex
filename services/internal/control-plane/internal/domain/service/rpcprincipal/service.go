// Package rpcprincipal разрешает доменного actor после допуска выбранного RPC-профиля.
package rpcprincipal

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
const gateway = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"

type Owner interface {
	ResolveProofAuthority(context.Context, platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error)
	ResolveServiceCredentialGeneration(context.Context, string) (uint64, error)
}

type CredentialVerifier interface {
	VerifyToken(context.Context, string) (oidcverifier.Principal, error)
}

type Service struct {
	owner       Owner
	credentials CredentialVerifier
	now         func() time.Time
}

// Input передаётся серверным caster: digest вычислен из фактического protobuf,
// ProjectRef является locator и повторно разрешается владельцем в tenant scope.
type Input struct {
	Admission                                      serviceidentity.Admission
	Authorization, ProjectRef, RequestDigestSHA256 string
}

func New(owner Owner, credentials CredentialVerifier) (*Service, error) {
	if owner == nil || credentials == nil {
		return nil, errors.New("RPC principal dependencies required")
	}
	return &Service{owner: owner, credentials: credentials, now: time.Now}, nil
}

func (service *Service) Resolve(ctx context.Context, input Input) (value.Principal, error) {
	admission := input.Admission
	if admission.ProjectRequired != (input.ProjectRef != "") {
		return value.Principal{}, errs.ErrForbidden
	}
	digest, digestErr := hex.DecodeString(input.RequestDigestSHA256)
	if admission.TargetSPIFFEID != target || admission.OperationID == "" || admission.Permission == "" || digestErr != nil || len(digest) != 32 || hex.EncodeToString(digest) != input.RequestDigestSHA256 {
		return value.Principal{}, errs.ErrForbidden
	}
	// Имя workload берётся только из результата Admit, не из business payload.
	// service-v1 подтверждает URI сертификатом; trusted-cluster использует
	// серверный caller key внутри принятой сетевой границы и тот же реестр методов.
	prefix := "spiffe://kodex.local/ns/kodex-system/sa/"
	if !strings.HasPrefix(admission.Peer.SPIFFEID, prefix) {
		return value.Principal{}, errs.ErrForbidden
	}
	workload := strings.TrimPrefix(admission.Peer.SPIFFEID, prefix)
	if workload == "" || strings.ContainsAny(workload, "/?#") {
		return value.Principal{}, errs.ErrForbidden
	}
	principalInput := platformrepo.ProofPrincipalInput{CallerWorkload: workload, Operation: admission.OperationID, ProjectRef: input.ProjectRef, RequestDigestSHA256: input.RequestDigestSHA256}
	var credential oidcverifier.Principal
	var generation uint64
	switch admission.ActorMode {
	case serviceidentity.UserActor:
		if admission.Peer.SPIFFEID != gateway {
			return value.Principal{}, errs.ErrForbidden
		}
		token := strings.TrimPrefix(input.Authorization, "Bearer ")
		if token == input.Authorization || token == "" || strings.TrimSpace(token) != token {
			return value.Principal{}, errs.ErrUnauthorized
		}
		verified, err := service.credentials.VerifyToken(ctx, token)
		if errors.Is(err, oidcverifier.ErrSigningKeysUnavailable) {
			return value.Principal{}, errs.ErrUnavailable
		}
		if err != nil {
			return value.Principal{}, errs.ErrUnauthorized
		}
		credential = verified
		principalInput.ExternalActorID, principalInput.ExternalTenantID = verified.Subject, verified.OrganizationID
		principalInput.ExternalDisplayName, principalInput.ExternalEmailHint = verified.DisplayName, verified.EmailHint
		principalInput.ExternalIssuer, principalInput.ExternalGroups = verified.Issuer, append([]string(nil), verified.Groups...)
		principalInput.ExternalSessionRevision, principalInput.OwnerClaim = verified.SessionRevision, verified.OwnerClaim
		principalInput.ExternalAuthenticatedAt, principalInput.ExternalACR, principalInput.ExternalAMR = verified.AuthenticatedAt, verified.ACR, append([]string(nil), verified.AMR...)
		generation = verified.SessionRevision
	case serviceidentity.ServiceActor:
		if admission.Peer.SPIFFEID == gateway || input.Authorization != "" || input.ProjectRef != "" {
			return value.Principal{}, errs.ErrForbidden
		}
		principalInput.ExternalActorID, principalInput.ExternalTenantID = "kodex-system-subject", "kodex-installation"
		var err error
		generation, err = service.owner.ResolveServiceCredentialGeneration(ctx, workload)
		if err != nil {
			return value.Principal{}, err
		}
	default:
		// Делегированный runtime/task путь подключается отдельно; он никогда
		// не понижается до обычной служебной операции.
		return value.Principal{}, errs.ErrForbidden
	}
	resolved, err := service.owner.ResolveProofAuthority(ctx, principalInput)
	if err != nil {
		return value.Principal{}, err
	}
	if resolved.RuntimeExecution != nil {
		return value.Principal{}, errs.ErrForbidden
	}
	principal := value.Principal{ActorID: resolved.ActorID, AuthorityTenant: resolved.OrganizationID, ProjectRef: resolved.ProjectID, Permission: admission.Permission, CallerWorkload: workload, CorrelationRef: uuid.NewString(), CredentialRevision: generation, CredentialAuthenticatedAt: credential.AuthenticatedAt, CredentialACR: credential.ACR, CredentialAMR: append([]string(nil), credential.AMR...)}
	if err = principal.Validate(); err != nil {
		return value.Principal{}, errs.ErrForbidden
	}
	return principal, nil
}
