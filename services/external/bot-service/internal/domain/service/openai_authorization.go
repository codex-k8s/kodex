package service

import (
	"context"
	"fmt"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

type openAIAuthActor struct {
	UserID   string
	UserName string
}

func isFrozenOpenAIAccount(
	ctx context.Context,
	store adminrepo.Repository,
	accountName string,
) (bool, error) {
	repository, ok := store.(securityrepo.ClusterAdminAccountDependencyRepository)
	if !ok {
		return false, fmt.Errorf("cluster-admin account dependency guard is unavailable")
	}
	return repository.IsFrozenClusterAdminOpenAIAccount(ctx, strings.TrimSpace(accountName))
}

func completeOpenAIAccountAuthorization(
	ctx context.Context,
	store adminrepo.Repository,
	runner runtimerepo.Runner,
	account entity.OpenAIAccount,
	actor openAIAuthActor,
) (entity.OpenAIAccount, runtimerepo.CodexAuthCompleteResult, bool, error) {
	completed, err := runner.CompleteCodexAuthSession(ctx, runtimerepo.CodexAuthCompleteInput{
		AccountName: account.Name,
		SecretName:  account.SecretRef,
	})
	if err != nil {
		return entity.OpenAIAccount{}, runtimerepo.CodexAuthCompleteResult{}, false, err
	}

	frozen, err := isFrozenOpenAIAccount(ctx, store, account.Name)
	if err != nil {
		return entity.OpenAIAccount{}, completed, false, err
	}
	if !frozen {
		updated, updateErr := store.UpdateOpenAIAccountStatus(ctx, adminrepo.UpdateOpenAIAccountStatusInput{
			Name:      account.Name,
			SecretRef: completed.SecretName,
			Status:    "authorized",
		})
		return updated, completed, false, updateErr
	}

	rotationRepository, ok := store.(securityrepo.ClusterAdminOpenAIAccountRotationRepository)
	if !ok {
		return entity.OpenAIAccount{}, completed, true, fmt.Errorf("cluster-admin OpenAI account rotation guard is unavailable")
	}
	if strings.TrimSpace(completed.Integrity.ContentSHA256) == "" ||
		strings.TrimSpace(completed.Integrity.UID) == "" ||
		strings.TrimSpace(completed.Integrity.ResourceVersion) == "" {
		return entity.OpenAIAccount{}, completed, true, fmt.Errorf("verified immutable OpenAI credential revision metadata is incomplete")
	}
	updated, err := rotationRepository.RotateFrozenOpenAIAccount(ctx, securityrepo.RotateFrozenOpenAIAccountInput{
		AccountName:           account.Name,
		SecretRef:             completed.SecretName,
		SecretContentSHA256:   completed.Integrity.ContentSHA256,
		SecretResourceUID:     completed.Integrity.UID,
		SecretResourceVersion: completed.Integrity.ResourceVersion,
		ActorUserID:           strings.TrimSpace(actor.UserID),
		ActorUserName:         strings.TrimSpace(actor.UserName),
	})
	if err != nil {
		return entity.OpenAIAccount{}, completed, true, err
	}
	return updated, completed, true, nil
}
