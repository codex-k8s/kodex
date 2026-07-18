package service

import (
	"context"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

func clusterAdminSessionGuardRequired(ctx context.Context, store adminrepo.Repository, role entity.AgentRole, sessionKey string) (bool, error) {
	if strings.TrimSpace(sessionKey) != "" {
		if repository, ok := store.(securityrepo.ClusterAdminSessionSubjectRepository); ok {
			required, err := repository.RequiresClusterAdminSessionGuard(ctx, role.ID, sessionKey)
			if err != nil {
				return false, err
			}
			return required, nil
		}
	}
	return strings.EqualFold(strings.TrimSpace(role.KubernetesAccess), "cluster-admin"), nil
}

func verifyClusterAdminSessionSecretIntegrity(ctx context.Context, store adminrepo.Repository, runner runtimerepo.Runner, roleID int64, sessionKey string) error {
	repository, ok := store.(securityrepo.ClusterAdminSecretIntegrityRepository)
	if !ok || runner == nil {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	bindings, err := repository.ListClusterAdminSecretIntegrity(ctx, roleID, sessionKey)
	if err != nil {
		return err
	}
	if len(bindings) == 0 {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	for _, binding := range bindings {
		if strings.TrimSpace(binding.ContentSHA256) == "" || strings.TrimSpace(binding.ResourceUID) == "" || strings.TrimSpace(binding.ResourceVersion) == "" {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		actual, err := runner.InspectSecretIntegrity(ctx, runtimerepo.SecretIntegrityInput{
			SecretName: binding.SecretRef,
			SecretKey:  binding.SecretKey,
		})
		if err != nil || actual.ContentSHA256 != binding.ContentSHA256 || actual.UID != binding.ResourceUID || actual.ResourceVersion != binding.ResourceVersion {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
	}
	return nil
}

func (svc *AgentSessionService) withCurrentSessionRuntimeGuard(ctx context.Context, session entity.AgentSession, operation string, sideEffect func(entity.AgentSession) error) error {
	return svc.withCurrentSessionGuard(ctx, session, operation, false, func(current entity.AgentSession, _ adminrepo.Repository) error {
		return sideEffect(current)
	})
}

func (svc *AgentSessionService) withCurrentSessionPersistenceGuard(ctx context.Context, session entity.AgentSession, operation string, sideEffect func(entity.AgentSession, adminrepo.Repository) error) error {
	return svc.withCurrentSessionGuard(ctx, session, operation, true, sideEffect)
}

func (svc *AgentSessionService) withCurrentSessionGuard(ctx context.Context, expected entity.AgentSession, operation string, persistence bool, sideEffect func(entity.AgentSession, adminrepo.Repository) error) error {
	current, err := svc.cfg.Store.GetAgentSession(ctx, expected.SessionKey)
	if err != nil {
		return err
	}
	if current.ID != expected.ID || current.RoleID != expected.RoleID || current.ProjectID != expected.ProjectID || current.ChatID != expected.ChatID || current.MattermostChannelID != expected.MattermostChannelID {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	role := entity.AgentRole{ID: current.RoleID, ProjectID: current.ProjectID}
	if _, ok := svc.cfg.Store.(securityrepo.ClusterAdminSessionSubjectRepository); !ok {
		role, err = svc.cfg.Store.GetAgentRole(ctx, current.RoleID)
		if err != nil {
			return err
		}
	}
	required, err := clusterAdminSessionGuardRequired(ctx, svc.cfg.Store, role, current.SessionKey)
	if err != nil {
		return err
	}
	if !required {
		return sideEffect(current, svc.cfg.Store)
	}
	role, err = svc.cfg.Store.GetAgentRole(ctx, current.RoleID)
	if err != nil || role.ProjectID != current.ProjectID {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	chat, err := svc.cfg.Store.GetChat(ctx, current.ChatID)
	if err != nil || chat.ProjectID != current.ProjectID || strings.TrimSpace(chat.MattermostChannelID) != strings.TrimSpace(current.MattermostChannelID) {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	input := securityrepo.ClusterAdminBindingInput{
		RoleID: current.RoleID, ProjectID: current.ProjectID, ChatID: current.ChatID, ChatSlug: chat.Slug,
		MattermostChannelID: current.MattermostChannelID, SessionKey: current.SessionKey,
		Operation: operation, ActorUser: "runtime",
	}
	if persistence {
		repository, ok := svc.cfg.Store.(securityrepo.ClusterAdminPersistenceGuardRepository)
		if !ok {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		return repository.WithExistingClusterAdminPersistenceGuard(ctx, input, func(guardedStore adminrepo.Repository) error {
			if err := verifyClusterAdminSessionSecretIntegrity(ctx, svc.cfg.Store, svc.cfg.RuntimeRunner, current.RoleID, current.SessionKey); err != nil {
				return err
			}
			return sideEffect(current, guardedStore)
		})
	}
	repository, ok := svc.cfg.Store.(securityrepo.ClusterAdminRuntimeGuardRepository)
	if !ok {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return repository.WithExistingClusterAdminRuntimeGuard(ctx, input, func() error {
		if err := verifyClusterAdminSessionSecretIntegrity(ctx, svc.cfg.Store, svc.cfg.RuntimeRunner, current.RoleID, current.SessionKey); err != nil {
			return err
		}
		return sideEffect(current, svc.cfg.Store)
	})
}
