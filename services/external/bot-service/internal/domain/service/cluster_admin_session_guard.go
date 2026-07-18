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
	_, err := verifyClusterAdminSessionSecretIntegrityWithToken(ctx, store, runner, roleID, sessionKey, "")
	return err
}

func verifyClusterAdminSessionSecretIntegrityWithToken(ctx context.Context, store adminrepo.Repository, runner runtimerepo.Runner, roleID int64, sessionKey string, tokenSecretRef string) (runtimerepo.MattermostBotTokenSecret, error) {
	repository, ok := store.(securityrepo.ClusterAdminSecretIntegrityRepository)
	if !ok || runner == nil {
		return runtimerepo.MattermostBotTokenSecret{}, adminrepo.ErrClusterAdminAdmissionDenied
	}
	bindings, err := repository.ListClusterAdminSecretIntegrity(ctx, roleID, sessionKey)
	if err != nil {
		return runtimerepo.MattermostBotTokenSecret{}, err
	}
	if len(bindings) == 0 {
		return runtimerepo.MattermostBotTokenSecret{}, adminrepo.ErrClusterAdminAdmissionDenied
	}
	tokenSecretRef = strings.TrimSpace(tokenSecretRef)
	var tokenSecret runtimerepo.MattermostBotTokenSecret
	tokenBindingFound := tokenSecretRef == ""
	for _, binding := range bindings {
		if strings.TrimSpace(binding.ContentSHA256) == "" || strings.TrimSpace(binding.ResourceUID) == "" || strings.TrimSpace(binding.ResourceVersion) == "" {
			return runtimerepo.MattermostBotTokenSecret{}, adminrepo.ErrClusterAdminAdmissionDenied
		}
		var actual runtimerepo.SecretIntegrity
		if tokenSecretRef != "" && binding.Kind == "session" && strings.TrimSpace(binding.SecretRef) == tokenSecretRef && binding.SecretKey == "token" {
			if tokenBindingFound {
				return runtimerepo.MattermostBotTokenSecret{}, adminrepo.ErrClusterAdminAdmissionDenied
			}
			tokenBindingFound = true
			tokenSecret, err = runner.GetMattermostBotTokenSecret(ctx, tokenSecretRef)
			actual = tokenSecret.Integrity
		} else {
			actual, err = runner.InspectSecretIntegrity(ctx, runtimerepo.SecretIntegrityInput{
				SecretName: binding.SecretRef,
				SecretKey:  binding.SecretKey,
			})
		}
		if err != nil || actual.ContentSHA256 != binding.ContentSHA256 || actual.UID != binding.ResourceUID || actual.ResourceVersion != binding.ResourceVersion {
			return runtimerepo.MattermostBotTokenSecret{}, adminrepo.ErrClusterAdminAdmissionDenied
		}
	}
	if !tokenBindingFound || (tokenSecretRef != "" && strings.TrimSpace(tokenSecret.Token) == "") {
		return runtimerepo.MattermostBotTokenSecret{}, adminrepo.ErrClusterAdminAdmissionDenied
	}
	return tokenSecret, nil
}

func (svc *AgentSessionService) withCurrentSessionRuntimeGuard(ctx context.Context, session entity.AgentSession, operation string, sideEffect func(entity.AgentSession) error) error {
	return svc.withCurrentSessionGuard(ctx, session, operation, false, func(current entity.AgentSession, _ adminrepo.Repository) error {
		return sideEffect(current)
	})
}

func (svc *AgentSessionService) withCurrentSessionPersistenceGuard(ctx context.Context, session entity.AgentSession, operation string, sideEffect func(entity.AgentSession, adminrepo.Repository) error) error {
	return svc.withCurrentSessionGuard(ctx, session, operation, true, sideEffect)
}

func (svc *AgentSessionService) withCurrentSessionsPersistenceGuard(ctx context.Context, child entity.AgentSession, source entity.AgentSession, operation string, sideEffect func(entity.AgentSession, entity.AgentSession, adminrepo.Repository) error) error {
	repository, ok := svc.cfg.Store.(adminrepo.ExactAgentSessionsRuntimeGuardRepository)
	if !ok {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return repository.WithExactAgentSessionsRuntimeGuard(ctx, []entity.AgentSession{child, source}, func(transactionalStore adminrepo.Repository) error {
		return svc.withCurrentSessionsPersistenceGuardUsingStore(ctx, transactionalStore, child, source, operation, sideEffect)
	})
}

func (svc *AgentSessionService) withCurrentSessionsPersistenceGuardUsingStore(ctx context.Context, store adminrepo.Repository, child entity.AgentSession, source entity.AgentSession, operation string, sideEffect func(entity.AgentSession, entity.AgentSession, adminrepo.Repository) error) error {
	if child.ID == source.ID || strings.TrimSpace(child.SessionKey) == strings.TrimSpace(source.SessionKey) {
		if child.ID != source.ID || strings.TrimSpace(child.SessionKey) != strings.TrimSpace(source.SessionKey) ||
			child.ProjectID != source.ProjectID || child.ChatID != source.ChatID || child.RoleID != source.RoleID ||
			strings.TrimSpace(child.MattermostChannelID) != strings.TrimSpace(source.MattermostChannelID) {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		return svc.withCurrentSessionGuardUsingStore(ctx, store, child, operation+".deduplicated", true, func(current entity.AgentSession, guardedStore adminrepo.Repository) error {
			return sideEffect(current, current, guardedStore)
		})
	}
	childFirst := child.ID < source.ID || (child.ID == source.ID && child.SessionKey < source.SessionKey)
	first := child
	firstSuffix := ".child"
	second := source
	secondSuffix := ".source"
	if !childFirst {
		first, second = source, child
		firstSuffix, secondSuffix = ".source", ".child"
	}
	return svc.withCurrentSessionGuardUsingStore(ctx, store, first, operation+firstSuffix, true, func(currentFirst entity.AgentSession, firstStore adminrepo.Repository) error {
		return svc.withCurrentSessionGuardUsingStore(ctx, firstStore, second, operation+secondSuffix, true, func(currentSecond entity.AgentSession, secondStore adminrepo.Repository) error {
			if childFirst {
				return sideEffect(currentFirst, currentSecond, secondStore)
			}
			return sideEffect(currentSecond, currentFirst, secondStore)
		})
	})
}

func (svc *AgentSessionService) withCurrentSessionsPublishGuard(ctx context.Context, child entity.AgentSession, source entity.AgentSession, operation string, sideEffect func(entity.AgentSession, entity.AgentSession) error) error {
	repository, ok := svc.cfg.Store.(adminrepo.ExactAgentSessionsRuntimeGuardRepository)
	if !ok {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return repository.WithExactAgentSessionsRuntimeGuard(ctx, []entity.AgentSession{child, source}, func(lockedStore adminrepo.Repository) error {
		return svc.withCurrentSessionsPersistenceGuardUsingStore(ctx, lockedStore, child, source, operation+".dependencies", func(currentChild entity.AgentSession, currentSource entity.AgentSession, dependencyStore adminrepo.Repository) error {
			fenceStore, ok := dependencyStore.(adminrepo.ExactAgentSessionsPublishFenceRepository)
			if !ok {
				return adminrepo.ErrClusterAdminAdmissionDenied
			}
			if err := fenceStore.LockExactAgentSessionsPublishFence(ctx, []entity.AgentSession{currentChild, currentSource}); err != nil {
				return err
			}
			return svc.withCurrentSessionsPersistenceGuardUsingStore(ctx, dependencyStore, currentChild, currentSource, operation+".fenced_recheck", func(recheckedChild entity.AgentSession, recheckedSource entity.AgentSession, _ adminrepo.Repository) error {
				return sideEffect(recheckedChild, recheckedSource)
			})
		})
	})
}

func (svc *AgentSessionService) withCurrentSessionGuard(ctx context.Context, expected entity.AgentSession, operation string, persistence bool, sideEffect func(entity.AgentSession, adminrepo.Repository) error) error {
	return svc.withCurrentSessionGuardUsingStore(ctx, svc.cfg.Store, expected, operation, persistence, sideEffect)
}

func (svc *AgentSessionService) withCurrentSessionGuardUsingStore(ctx context.Context, store adminrepo.Repository, expected entity.AgentSession, operation string, persistence bool, sideEffect func(entity.AgentSession, adminrepo.Repository) error) error {
	current, err := store.GetAgentSession(ctx, expected.SessionKey)
	if err != nil {
		return err
	}
	if current.ID != expected.ID || current.RoleID != expected.RoleID || current.ProjectID != expected.ProjectID || current.ChatID != expected.ChatID || current.MattermostChannelID != expected.MattermostChannelID {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	role := entity.AgentRole{ID: current.RoleID, ProjectID: current.ProjectID}
	if _, ok := store.(securityrepo.ClusterAdminSessionSubjectRepository); !ok {
		role, err = store.GetAgentRole(ctx, current.RoleID)
		if err != nil {
			return err
		}
	}
	required, err := clusterAdminSessionGuardRequired(ctx, store, role, current.SessionKey)
	if err != nil {
		return err
	}
	if !required {
		return sideEffect(current, store)
	}
	role, err = store.GetAgentRole(ctx, current.RoleID)
	if err != nil || role.ProjectID != current.ProjectID {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	chat, err := store.GetChat(ctx, current.ChatID)
	if err != nil || chat.ProjectID != current.ProjectID || strings.TrimSpace(chat.MattermostChannelID) != strings.TrimSpace(current.MattermostChannelID) {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	input := securityrepo.ClusterAdminBindingInput{
		RoleID: current.RoleID, ProjectID: current.ProjectID, ChatID: current.ChatID, ChatSlug: chat.Slug,
		MattermostChannelID: current.MattermostChannelID, SessionKey: current.SessionKey,
		Operation: operation, ActorUser: "runtime",
	}
	if persistence {
		repository, ok := store.(securityrepo.ClusterAdminPersistenceGuardRepository)
		if !ok {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		return repository.WithExistingClusterAdminPersistenceGuard(ctx, input, func(guardedStore adminrepo.Repository) error {
			if err := verifyClusterAdminSessionSecretIntegrity(ctx, guardedStore, svc.cfg.RuntimeRunner, current.RoleID, current.SessionKey); err != nil {
				return err
			}
			return sideEffect(current, guardedStore)
		})
	}
	repository, ok := store.(securityrepo.ClusterAdminRuntimeGuardRepository)
	if !ok {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return repository.WithExistingClusterAdminRuntimeGuard(ctx, input, func() error {
		if err := verifyClusterAdminSessionSecretIntegrity(ctx, store, svc.cfg.RuntimeRunner, current.RoleID, current.SessionKey); err != nil {
			return err
		}
		return sideEffect(current, store)
	})
}
