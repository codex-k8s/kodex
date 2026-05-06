package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
)

var systemAccessActionNamespace = uuid.MustParse("e4f59d5e-2f8a-4f52-879b-beb77754aa4b")

type systemAccessActionDescriptor struct {
	key          string
	resourceType string
}

var systemAccessActions = map[string]systemAccessActionDescriptor{
	accessActionExplainAccess:                {key: accessActionExplainAccess, resourceType: accessResourceAccessDecisionAudit},
	accessActionSetUserStatus:                {key: accessActionSetUserStatus, resourceType: accessResourceUser},
	accessActionDisableAllowlistEntry:        {key: accessActionDisableAllowlistEntry, resourceType: accessResourceAllowlistEntry},
	accessActionListPendingAccess:            {key: accessActionListPendingAccess, resourceType: accessResourcePendingAccess},
	accessActionListMembershipGraph:          {key: accessActionListMembershipGraph, resourceType: accessResourceMembershipGraph},
	accessActionManageExternalProvider:       {key: accessActionManageExternalProvider, resourceType: accessResourceExternalProvider},
	accessActionManageExternalAccount:        {key: accessActionManageExternalAccount, resourceType: accessResourceExternalAccount},
	accessActionManageExternalAccountBinding: {key: accessActionManageExternalAccountBinding, resourceType: accessResourceExternalAccountBinding},
	"project.create":                         {key: "project.create", resourceType: "project"},
	"project.update":                         {key: "project.update", resourceType: "project"},
	"project.read":                           {key: "project.read", resourceType: "project"},
	"project.list":                           {key: "project.list", resourceType: "project"},
	"repository.attach":                      {key: "repository.attach", resourceType: "repository"},
	"repository.update":                      {key: "repository.update", resourceType: "repository"},
	"repository.detach":                      {key: "repository.detach", resourceType: "repository"},
	"repository.read":                        {key: "repository.read", resourceType: "repository"},
	"repository.list":                        {key: "repository.list", resourceType: "repository"},
	"project.policy.import":                  {key: "project.policy.import", resourceType: "services_policy"},
	"project.policy.read":                    {key: "project.policy.read", resourceType: "services_policy"},
	"project.policy.propose":                 {key: "project.policy.propose", resourceType: "services_policy"},
	"project.policy.override":                {key: "project.policy.override", resourceType: "policy_override"},
	"project.policy.override.read":           {key: "project.policy.override.read", resourceType: "policy_override"},
	"project.policy.override.cancel":         {key: "project.policy.override.cancel", resourceType: "policy_override"},
	"project.docs.update":                    {key: "project.docs.update", resourceType: "documentation_source"},
	"project.docs.read":                      {key: "project.docs.read", resourceType: "documentation_source"},
	"project.workspace.read":                 {key: "project.workspace.read", resourceType: "project"},
	"project.branch_rules.update":            {key: "project.branch_rules.update", resourceType: "branch_rules"},
	"project.branch_rules.read":              {key: "project.branch_rules.read", resourceType: "branch_rules"},
	"project.release_policy.update":          {key: "project.release_policy.update", resourceType: "release_policy"},
	"project.release_policy.read":            {key: "project.release_policy.read", resourceType: "release_policy"},
	"project.release_line.update":            {key: "project.release_line.update", resourceType: "release_line"},
	"project.release_line.read":              {key: "project.release_line.read", resourceType: "release_line"},
	"project.placement_policy.update":        {key: "project.placement_policy.update", resourceType: "placement_policy"},
	"project.placement_policy.read":          {key: "project.placement_policy.read", resourceType: "placement_policy"},
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
