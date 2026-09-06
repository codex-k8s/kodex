package revision

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

type RoleImageConfiguration struct {
	RoleDefinitionRef string               `json:"roleDefinitionRef" yaml:"roleDefinitionRef" toml:"roleDefinitionRef"`
	Environment       RoleImageEnvironment `json:"environment" yaml:"environment" toml:"environment"`
}

type RoleImageEnvironment struct {
	EnvironmentKey    string   `json:"environmentKey" yaml:"environmentKey" toml:"environmentKey"`
	PackageKeys       []string `json:"packageKeys" yaml:"packageKeys" toml:"packageKeys"`
	ToolKeys          []string `json:"toolKeys" yaml:"toolKeys" toml:"toolKeys"`
	InstallationBlock string   `json:"installationBlock" yaml:"installationBlock" toml:"installationBlock"`
	Dockerfile        string   `json:"dockerfile" yaml:"dockerfile" toml:"dockerfile"`
}

func ParseRoleImage(format, content string) (string, string, entity.RoleEnvironmentSelection, error) {
	var value document
	if decodeStrict(format, content, &value) != nil || validateDocument(KindRoleImage, value) != nil || value.RoleImage == nil {
		return "", "", entity.RoleEnvironmentSelection{}, ErrInvalid
	}
	environment := value.RoleImage.Environment
	return value.Name, value.RoleImage.RoleDefinitionRef, entity.RoleEnvironmentSelection{EnvironmentKey: environment.EnvironmentKey, PackageKeys: environment.PackageKeys,
		ToolKeys: environment.ToolKeys, InstallationBlock: environment.InstallationBlock, Dockerfile: environment.Dockerfile}, nil
}
