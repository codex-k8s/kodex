package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

func TestProjectAssistantPublicGitHubScope(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["github"]
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
