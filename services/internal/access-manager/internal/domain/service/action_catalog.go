package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
)

var systemAccessActionNamespace = uuid.MustParse("e4f59d5e-2f8a-4f52-879b-beb77754aa4b")

type systemAccessActionDescriptor struct {
	key          string
	resourceType string
}

var systemAccessActions = systemAccessActionCatalog(systemAccessActionDescriptors())

func systemAccessActionDescriptors() []systemAccessActionDescriptor {
	descriptors := []systemAccessActionDescriptor{
		{key: accessActionExplainAccess, resourceType: accessResourceAccessDecisionAudit},
		{key: accessActionSetUserStatus, resourceType: accessResourceUser},
		{key: accessActionDisableAllowlistEntry, resourceType: accessResourceAllowlistEntry},
		{key: accessActionListPendingAccess, resourceType: accessResourcePendingAccess},
		{key: accessActionListMembershipGraph, resourceType: accessResourceMembershipGraph},
		{key: accessActionManageExternalProvider, resourceType: accessResourceExternalProvider},
		{key: accessActionManageExternalAccount, resourceType: accessResourceExternalAccount},
		{key: accessActionManageExternalAccountBinding, resourceType: accessResourceExternalAccountBinding},
	}
	for _, action := range accesscatalog.ProjectCatalogActions() {
		descriptors = append(descriptors, systemAccessActionDescriptor{key: action.Key, resourceType: action.ResourceType})
	}
	for _, action := range accesscatalog.PackageHubActions() {
		descriptors = append(descriptors, systemAccessActionDescriptor{key: action.Key, resourceType: action.ResourceType})
	}
	return descriptors
}

func systemAccessActionCatalog(descriptors []systemAccessActionDescriptor) map[string]systemAccessActionDescriptor {
	catalog := make(map[string]systemAccessActionDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		catalog[descriptor.key] = descriptor
	}
	return catalog
}

func (s *Service) accessActionByKey(ctx context.Context, key string) (entity.AccessAction, error) {
	key = strings.TrimSpace(key)
	if action, ok := systemAccessActionByKey(key); ok {
		return action, nil
	}
	return s.repository.GetAccessActionByKey(ctx, key)
}

func systemAccessActionByKey(key string) (entity.AccessAction, bool) {
	descriptor, ok := systemAccessActions[strings.TrimSpace(key)]
	if !ok {
		return entity.AccessAction{}, false
	}
	now := time.Unix(0, 0).UTC()
	action := entity.AccessAction{
		Base: entity.Base{
			ID:        uuid.NewSHA1(systemAccessActionNamespace, []byte(descriptor.key)),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		Key:          descriptor.key,
		DisplayName:  systemAccessActionMessageKey(descriptor.key, "name"),
		Description:  systemAccessActionMessageKey(descriptor.key, "description"),
		ResourceType: descriptor.resourceType,
		Status:       enum.AccessActionStatusActive,
	}
	return action, true
}

func rejectSystemAccessActionMutation(key string) error {
	if _, ok := systemAccessActionByKey(key); ok {
		return errs.ErrPreconditionFailed
	}
	return nil
}

func systemAccessActionMessageKey(actionKey string, suffix string) string {
	key := strings.NewReplacer(".", "_", "-", "_").Replace(actionKey)
	return "access.actions." + key + "." + suffix
}
