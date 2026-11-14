package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestProjectAssistantPublicGitHubScope checks exact public owner/repository values and canonical
// digest while rejecting unknown/duplicate/credential fields, unsafe locators and
// unsupported-provider scope leakage.
// TestProjectAssistantPublicGitHubScope проверяет точные публичные owner и repository и
// канонический дайджест, отклоняя неизвестные, повторные и credential-поля, небезопасные locator и
// раскрытие области неподдерживаемого провайдера.
func TestProjectAssistantPublicGitHubScope(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition, matched := integrationpackage.ResolveShippedRevision(definitions["github"], "6.4.0", "bd6d504e4fc1d19d5c083f8585115c04ec75852b7879ad19ed54dc41cbe9a1da")
	if !matched {
		t.Fatal("frozen public repository scope fixture is unavailable")
	}
	capability, ok := definition.Capability("github.repository.content.read")
	if !ok {
		t.Fatal("missing capability")
	}
	configuration := map[string]string{"owner": "fixture-owner", "repository": "repository"}
	values, digest, err := projectAssistantPublicResourceScope(definition, capability, configuration)
	encoded, _ := json.Marshal(configuration)
	expected := sha256.Sum256(encoded)
	if err != nil || len(values) != 2 || values["owner"] != configuration["owner"] || values["repository"] != configuration["repository"] || digest != hex.EncodeToString(expected[:]) {
		t.Fatal("exact public scope lost values or canonical digest")
	}
	for _, scenario := range []string{"kind", "field", "duplicate", "missing", "path", "nul", "token", "credential"} {
		t.Run(scenario, func(t *testing.T) {
			def, cap := definition, capability
			cfg := map[string]string{"owner": configuration["owner"], "repository": configuration["repository"]}
			switch scenario {
			case "kind":
				cap.ResourceScope.Kind = "UNKNOWN"
			case "field":
				cap.ResourceScope.ConnectionFields = []string{"owner", "token"}
			case "duplicate":
				cap.ResourceScope.ConnectionFields = []string{"owner", "owner"}
			case "missing":
				delete(cfg, "repository")
			case "path":
				cfg["owner"] = "../PRIVATE_SENTINEL"
			case "nul":
				cfg["repository"] = "PRIVATE_SENTINEL\x00"
			case "token":
				cfg["token"] = "PRIVATE_SENTINEL"
			case "credential":
				cred := *def.Spec.Credential
				cred.SecretKey = "owner"
				def.Spec.Credential = &cred
			}
			values, digest, err := projectAssistantPublicResourceScope(def, cap, cfg)
			if err == nil || values != nil || digest != "" {
				t.Fatal("unknown/credential scope became public")
			}
		})
	}
	definition.Metadata.Key = "synthetic"
	if values, digest, err := projectAssistantPublicResourceScope(definition, capability, configuration); err != nil || values != nil || digest != "" {
		t.Fatal("scope leaked into unsupported provider")
	}
}

// TestProjectAssistantScopedGitHubPublicScope exposes only canonical successor scope metadata under exact field declarations; configuration and identity never become model-issued grants.
// TestProjectAssistantScopedGitHubPublicScope выдаёт только canonical successor scope metadata с exact field declarations; configuration и identity не становятся выданными моделью grants.
func TestProjectAssistantScopedGitHubPublicScope(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["github"]
	capability, found := definition.Capability("github.repository.content.read")
	if !found {
		t.Fatal("scoped GitHub read capability is absent")
	}
	for _, configuration := range []map[string]string{{"scope_kind": "ALL_ACCESSIBLE"}, {"scope_kind": "SELECTED", "repositories": `[{"id":7,"full_name":"owner/repository"}]`}} {
		values, digest, err := projectAssistantPublicResourceScope(definition, capability, configuration)
		encoded, _ := json.Marshal(configuration)
		expected := sha256.Sum256(encoded)
		if err != nil || values["scope_kind"] != configuration["scope_kind"] || digest != hex.EncodeToString(expected[:]) {
			t.Fatal("scoped GitHub public metadata changed", err)
		}
		configuration["token"] = "synthetic"
		if _, _, err := projectAssistantPublicResourceScope(definition, capability, configuration); err == nil {
			t.Fatal("private configuration field became public scope")
		}
	}
}
