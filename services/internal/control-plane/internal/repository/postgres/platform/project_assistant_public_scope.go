package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// projectGitHubScopeIdentifier bounds public GitHub owner/repository identifiers to 1–100 ASCII
// letters, digits, underscore, dot or hyphen. The caller separately rejects dot-only traversal
// names and secret-key collisions.
// projectGitHubScopeIdentifier ограничивает публичные owner/repository GitHub длиной 1–100
// символов из ASCII букв, цифр, подчёркивания, точки и дефиса. Вызывающий код отдельно отклоняет
// имена перехода из точек и совпадения с ключом секрета.
var projectGitHubScopeIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// projectAssistantPublicResourceScope returns no scope for non-GitHub packages; GitHub requires
// the exact adapter, capability prefix, repository scope fields and validated public
// configuration. It rejects duplicate/unknown fields, invalid owner/repository names and
// credential-key collisions, then hashes canonical JSON values. The scope describes connection
// purpose and grants no execution authority.
// projectAssistantPublicResourceScope не возвращает область для пакетов кроме GitHub; GitHub
// требует точные adapter, префикс capability, поля области репозитория и проверенную публичную
// конфигурацию. Отклоняет повторные или неизвестные поля, неверные owner/repository и совпадения с
// credential key, затем хеширует канонический JSON значений. Область описывает назначение
// подключения и не даёт права исполнения.
func projectAssistantPublicResourceScope(definition integrationpackage.Package, capability integrationpackage.Capability, configuration map[string]string) (map[string]string, string, error) {
	if definition.Metadata.Key != "github" {
		return nil, "", nil
	}
	if definition.Spec.Adapter != "GITHUB" || !strings.HasPrefix(capability.Key, "github.") || capability.ResourceScope.Kind != "GITHUB_REPOSITORY" ||
		len(capability.ResourceScope.ConnectionFields) != 2 || definition.ValidateConfiguration(configuration) != nil {
		return nil, "", errs.ErrUnavailable
	}
	fields := map[string]bool{}
	for _, key := range capability.ResourceScope.ConnectionFields {
		if key != "owner" && key != "repository" || fields[key] {
			return nil, "", errs.ErrUnavailable
		}
		fields[key] = true
	}
	values, err := capability.ResourceScopeValues(configuration)
	if err != nil || len(values) != 2 {
		return nil, "", errs.ErrUnavailable
	}
	for _, key := range []string{"owner", "repository"} {
		value := values[key]
		if !projectGitHubScopeIdentifier.MatchString(value) || value == "." || value == ".." ||
			definition.Spec.Credential != nil && definition.Spec.Credential.SecretKey == key {
			return nil, "", errs.ErrUnavailable
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	digest := sha256.Sum256(encoded)
	return values, hex.EncodeToString(digest[:]), nil
}
