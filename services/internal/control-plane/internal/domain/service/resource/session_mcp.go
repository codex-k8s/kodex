package resource

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// BindSessionMCP принимает только проверенный readback immutable Secret от
// interaction-gateway. Значение bearer никогда не пересекает control-plane.
func (service *Service) BindSessionMCP(ctx context.Context, input BindSessionMCPInput) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionBindSessionMCP); err != nil {
		return entity.Resource{}, err
	}
	if !service.interactionGatewayPrincipal(input.Principal) || value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.SessionID) != nil ||
		!validOpaqueRuntimeIdentifier(input.AgentSessionKey) || input.AgentSessionID <= 0 ||
		input.AgentSessionVersion == 0 || !validSHA256Text(input.AgentSessionBindingSHA256) ||
		!validSessionMCPSecretRef(input.ImmutableSecretRef) ||
		input.ProviderContentVersion == "" || len(input.ProviderContentVersion) > 256 ||
		!validSHA256Text(input.ContentSHA256) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		SessionID, AgentSessionKey, AgentSessionBindingSHA256, ImmutableSecretRef string
		ProviderContentVersion, ContentSHA256                                     string
		AgentSessionVersion                                                       uint64
		AgentSessionID                                                            int64
	}{input.SessionID, input.AgentSessionKey, input.AgentSessionBindingSHA256,
		input.ImmutableSecretRef, input.ProviderContentVersion, input.ContentSHA256,
		input.AgentSessionVersion, input.AgentSessionID})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"bind_session_mcp", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			session, readErr := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.SessionID)
			if readErr != nil {
				return entity.Resource{}, readErr
			}
			spec, ok := session.Spec.(entity.SessionSpec)
			if !ok || session.Kind != enum.KindSession || session.OwnerActorID != input.Principal.ActorID ||
				session.State != enum.StateActive || (spec.AgentSessionKey != "" &&
				(spec.AgentSessionKey != input.AgentSessionKey || spec.AgentSessionID != input.AgentSessionID ||
					input.AgentSessionVersion < spec.AgentSessionBindingVersion)) {
				return entity.Resource{}, errs.ErrStateConflict
			}
			bindingID := sessionMCPBindingID(session.ID)
			bindingSpec := entity.CredentialBindingSpec{Purpose: "mcp-token",
				SecretRef:    "k8s-secret://mattercodex-system/" + immutableSecretName(input.ImmutableSecretRef),
				PrincipalRef: "bot-agent-session:" + input.AgentSessionKey, Revision: input.AgentSessionVersion,
				ImmutableSecretRef: input.ImmutableSecretRef, ProviderContentVersion: input.ProviderContentVersion,
				ContentSHA256: input.ContentSHA256, Ownership: entity.ConfigurationOwnership{ManagedBy: "UI",
					SourceRef: "bot-agent-session:" + input.AgentSessionKey, SourceRevision: input.AgentSessionVersion}}
			now := service.now().UTC()
			binding, readErr := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, bindingID)
			if errors.Is(readErr, errs.ErrNotFound) {
				if spec.AgentSessionBindingVersion != 0 {
					return entity.Resource{}, errs.ErrStateConflict
				}
				binding, readErr = entity.New(bindingID, session.OrganizationID, session.ProjectID, session.ID,
					session.OwnerActorID, enum.KindCredentialBinding, "Session MCP token", bindingSpec, now)
				if readErr == nil {
					readErr = tx.Insert(ctx, binding)
				}
			} else if readErr == nil {
				current, castOK := binding.Spec.(entity.CredentialBindingSpec)
				if !castOK || binding.Kind != enum.KindCredentialBinding || binding.ParentID != session.ID ||
					current.Purpose != "mcp-token" || current.Revision > input.AgentSessionVersion {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if current.Revision == input.AgentSessionVersion {
					if spec.AgentSessionKey != input.AgentSessionKey || spec.AgentSessionID != input.AgentSessionID ||
						spec.AgentSessionBindingVersion != input.AgentSessionVersion ||
						spec.AgentSessionBindingSHA256 != input.AgentSessionBindingSHA256 ||
						current.SecretRef != bindingSpec.SecretRef || current.PrincipalRef != bindingSpec.PrincipalRef ||
						current.ImmutableSecretRef != input.ImmutableSecretRef ||
						current.ProviderContentVersion != input.ProviderContentVersion ||
						current.ContentSHA256 != input.ContentSHA256 {
						return entity.Resource{}, errs.ErrStateConflict
					}
					return session, nil
				}
				updated, updateErr := binding.Update(binding.Name, bindingSpec, now)
				if updateErr != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				readErr = tx.Update(ctx, updated, binding.Version)
				binding = updated
			}
			if readErr != nil {
				return entity.Resource{}, readErr
			}
			spec.AgentSessionKey, spec.AgentSessionID = input.AgentSessionKey, input.AgentSessionID
			spec.AgentSessionBindingVersion = input.AgentSessionVersion
			spec.AgentSessionBindingSHA256 = input.AgentSessionBindingSHA256
			updated, updateErr := session.Update(session.Name, spec, now)
			if updateErr != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if replaceErr := tx.Update(ctx, updated, session.Version); replaceErr != nil {
				return entity.Resource{}, replaceErr
			}
			if replaceErr := service.appendMutationRecords(ctx, tx, input.Principal, "bind_session_mcp_credential", binding); replaceErr != nil {
				return entity.Resource{}, replaceErr
			}
			if replaceErr := service.appendMutationRecords(ctx, tx, input.Principal, "bind_session_mcp", updated); replaceErr != nil {
				return entity.Resource{}, replaceErr
			}
			return updated, nil
		})
}

func sessionMCPBindingID(sessionID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:session-mcp-binding:"+sessionID)).String()
}

func immutableSecretName(reference string) string {
	parts := strings.Split(strings.TrimPrefix(reference, "k8s-immutable-secret://"), "/")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func validSessionMCPSecretRef(reference string) bool {
	parsed, err := url.Parse(reference)
	return err == nil && parsed.Scheme == "k8s-immutable-secret" && parsed.Host == "mattercodex-system" &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" &&
		immutableSecretName(reference) != ""
}
