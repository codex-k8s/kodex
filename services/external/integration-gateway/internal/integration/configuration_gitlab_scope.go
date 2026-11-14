package integration

import (
	"errors"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// validateNativeConfigurationInput preserves the separate owner-claimed configuration-source/writeback protocol for one exact PROJECT repository.
// validateNativeConfigurationInput сохраняет отдельный owner-claimed configuration-source/writeback protocol для одного exact PROJECT repository.
// Those native work contracts already pin RepositoryRef and never accept a model project selector; GROUP/INSTANCE connections and extra input selectors are refused.
// Эти native work contracts уже закрепляют RepositoryRef и не принимают model project selector; GROUP/INSTANCE connections и дополнительные input selectors отклоняются.
// This helper is not used by managed MCP invocations, whose complete project-ID/path input, grant and reusable consent checks remain mandatory.
// Helper не применяется к managed MCP invocations: их полные project-ID/path input, grant и reusable consent checks остаются обязательными.
func validateNativeConfigurationInput(definition integrationpackage.Package, capability integrationpackage.Capability, configuration map[string]string, repositoryRef string, raw []byte) ([]byte, error) {
	if capability.ResourceScope.Kind == "GITHUB_SCOPE" {
		if definition.Metadata.Key != "github" || definition.Spec.Adapter != "GITHUB" {
			return nil, errors.New("native GitHub configuration scope is invalid")
		}
		if _, err := integrationpackage.NativeGitHubRepository(configuration, repositoryRef); err != nil || configuration["service_identity"] == "" {
			return nil, errors.New("native GitHub configuration scope is invalid")
		}
		switch capability.Operation {
		case "github.repository.content.read", "github.branch.create", "github.repository.content.update", "github.pull_request.create":
		default:
			return nil, errors.New("native GitHub configuration operation is invalid")
		}
		fields := make([]integrationpackage.Field, 0, len(capability.InputFields))
		for _, field := range capability.InputFields {
			if field.Key != "repository_id" && field.Key != "repository_full_name" {
				fields = append(fields, field)
			}
		}
		capability.InputFields = fields
		return capability.ValidateInput(raw)
	}
	if capability.ResourceScope.Kind != "GITLAB_SCOPE" {
		return capability.ValidateInput(raw)
	}
	scope, err := integrationpackage.ParseGitLabResourceScope(configuration)
	if err != nil || definition.Metadata.Key != "gitlab" || definition.Spec.Adapter != "GITLAB" || scope.Kind != "PROJECT" || scope.ProjectPath != repositoryRef {
		return nil, errors.New("native configuration repository scope is invalid")
	}
	switch capability.Operation {
	case "gitlab.repository.file.read", "gitlab.branch.create", "gitlab.commit.create", "gitlab.merge_request.create":
	default:
		return nil, errors.New("native configuration operation is invalid")
	}
	fields := make([]integrationpackage.Field, 0, len(capability.InputFields))
	for _, field := range capability.InputFields {
		if field.Key != "project_id" && field.Key != "project_path" {
			fields = append(fields, field)
		}
	}
	capability.InputFields = fields
	return capability.ValidateInput(raw)
}
