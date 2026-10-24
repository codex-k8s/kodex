// Package rpcprincipal разрешает доменного actor после допуска выбранного RPC-профиля.
package rpcprincipal

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// target pins the control-plane SPIFFE target required by accepted RPC admission before domain
// principal resolution.
// target фиксирует SPIFFE target control-plane, обязательный для принятого RPC admission до
// разрешения доменного principal.
const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"

// gateway pins the control API gateway peer permitted to carry ordinary user credentials, apart
// from the closed trusted-STT delegation path.
// gateway фиксирует peer control API gateway, допускающий обычные пользовательские credentials,
// помимо закрытого пути делегирования trusted STT.
const gateway = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"

// Owner resolves external proof subjects and service credential generations from authoritative
// domain state rather than caller-provided actor identifiers.
// Owner разрешает внешних субъектов proof и поколения service credentials по авторитетному
// доменному состоянию вместо actor identifiers caller.
type Owner interface {
	// ResolveProofAuthority resolves the admitted profile, caller and verified external identity into
	// domain actor, organization and optional project authority.
	// ResolveProofAuthority разрешает допущенные profile, caller и проверенную внешнюю identity в
	// доменные полномочия actor, организации и необязательного проекта.
	ResolveProofAuthority(context.Context, platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error)
	// ResolveServiceCredentialGeneration reads the authoritative credential generation of the admitted
	// service workload without using a user bearer token.
	// ResolveServiceCredentialGeneration читает авторитетное поколение credentials допущенного service
	// workload без использования пользовательского bearer token.
	ResolveServiceCredentialGeneration(context.Context, string) (uint64, error)
}

// CredentialVerifier verifies a user bearer credential before its external subject, tenant and
// authentication evidence are passed to the owner.
// CredentialVerifier проверяет пользовательский bearer credential до передачи его внешних subject,
// tenant и свидетельств аутентификации владельцу.
type CredentialVerifier interface {
	// VerifyToken returns verified external identity and authentication claims or a verifier error;
	// the resolver maps unavailable signing keys separately from invalid credentials.
	// VerifyToken возвращает проверенную внешнюю identity и claims аутентификации либо ошибку
	// verifier; resolver отдельно отображает недоступные signing keys и неверные credentials.
	VerifyToken(context.Context, string) (oidcverifier.Principal, error)
}

// Service combines admitted transport identity, verified user credentials and authoritative owner
// resolution into a validated domain principal.
// Service объединяет допущенную transport identity, проверенные пользовательские credentials и
// авторитетное разрешение владельца в проверенный доменный principal.
type Service struct {
	owner       Owner
	credentials CredentialVerifier
	now         func() time.Time
}

// Input carries server-cast admission and the actual protobuf digest; ProjectRef is a locator
// re-resolved inside owner tenant scope and authorization is not a source of domain actor IDs.
// Input несёт admission из серверного caster и digest фактического protobuf; ProjectRef повторно
// разрешается владельцем внутри tenant scope, а authorization не задаёт доменные actor IDs.
type Input struct {
	Admission                                      serviceidentity.Admission
	Authorization, ProjectRef, RequestDigestSHA256 string
}

// New requires nonnil owner and credential-verification ports and initializes the resolver with
// the process wall-clock function.
// New требует непустые порты владельца и проверки credentials и создаёт resolver с функцией
// системных часов процесса.
func New(owner Owner, credentials CredentialVerifier) (*Service, error) {
	if owner == nil || credentials == nil {
		return nil, errors.New("RPC principal dependencies required")
	}
	return &Service{owner: owner, credentials: credentials, now: time.Now}, nil
}

// Resolve validates admitted target, request digest and project presence, verifies user or owner
// service generation, and resolves exact domain authority while refusing task-mode downgrade and
// runtime execution authority.
// Resolve проверяет допущенный target, digest запроса и наличие проекта, проверяет user credential либо
// поколение service владельца и разрешает точные доменные полномочия с отказом понижению task mode
// и runtime execution authority.
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
	principalInput := platformrepo.ProofPrincipalInput{RPCProfile: admission.RPCProfile, CallerWorkload: workload, Operation: admission.OperationID, ProjectRef: input.ProjectRef, RequestDigestSHA256: input.RequestDigestSHA256}
	var credential oidcverifier.Principal
	var generation uint64
	switch admission.ActorMode {
	case serviceidentity.UserActor:
		_, trustedSTT := platformrepo.TrustedSTTAuthorityPermission(workload, admission.OperationID)
		trustedSTT = trustedSTT && admission.RPCProfile == transportprofile.TrustedCluster &&
			admission.Permission == admission.OperationID && input.ProjectRef == "" && !admission.ProjectRequired
		if admission.Peer.SPIFFEID != gateway && !trustedSTT {
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
