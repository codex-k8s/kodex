package revision

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

// RoleImageConfiguration holds the role-definition locator and typed environment selection
// supplied by a structured role-image revision.
// RoleImageConfiguration содержит locator определения роли и типизированный выбор окружения из
// структурированной ревизии образа роли.
type RoleImageConfiguration struct {
	RoleDefinitionRef string               `json:"roleDefinitionRef" yaml:"roleDefinitionRef" toml:"roleDefinitionRef"`
	Environment       RoleImageEnvironment `json:"environment" yaml:"environment" toml:"environment"`
}

// RoleImageEnvironment describes catalog selection and optional installation or Dockerfile text
// without granting authority over server-owned supply-chain pins.
// RoleImageEnvironment описывает выбор каталога и необязательный installation либо Dockerfile
// текст без выдачи полномочий на supply-chain pins сервера.
type RoleImageEnvironment struct {
	EnvironmentKey    string   `json:"environmentKey" yaml:"environmentKey" toml:"environmentKey"`
	PackageKeys       []string `json:"packageKeys" yaml:"packageKeys" toml:"packageKeys"`
	ToolKeys          []string `json:"toolKeys" yaml:"toolKeys" toml:"toolKeys"`
	InstallationBlock string   `json:"installationBlock" yaml:"installationBlock" toml:"installationBlock"`
	Dockerfile        string   `json:"dockerfile" yaml:"dockerfile" toml:"dockerfile"`
}

// ParseRoleImage decodes known fields in the requested format and validates a typed role-image
// document before returning its name, role locator and environment selection.
// ParseRoleImage декодирует известные поля запрошенного формата и проверяет типизированный
// документ образа роли до возврата имени, locator роли и выбора окружения.
func ParseRoleImage(format, content string) (string, string, entity.RoleEnvironmentSelection, error) {
	var value document
	if decodeStrict(format, content, &value) != nil || validateDocument(KindRoleImage, value) != nil || value.RoleImage == nil {
		return "", "", entity.RoleEnvironmentSelection{}, ErrInvalid
	}
	environment := value.RoleImage.Environment
	return value.Name, value.RoleImage.RoleDefinitionRef, entity.RoleEnvironmentSelection{EnvironmentKey: environment.EnvironmentKey, PackageKeys: environment.PackageKeys,
		ToolKeys: environment.ToolKeys, InstallationBlock: environment.InstallationBlock, Dockerfile: environment.Dockerfile}, nil
}
