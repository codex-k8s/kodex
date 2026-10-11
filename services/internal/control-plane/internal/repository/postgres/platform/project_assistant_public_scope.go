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

var projectGitHubScopeIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// Только проверенная public configuration и exact published package.
// Scope остаётся описанием назначения подключения, не grant на исполнение.
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
