package httptransport

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/getkin/kin-openapi/openapi3"
)

// sshScopeValuesFixture derives the public nine-pin scope through the same target identity algorithm as CP.
// sshScopeValuesFixture выводит public scope с девятью pins тем же алгоритмом identity, что CP.
func sshScopeValuesFixture(t *testing.T) map[string]string {
	t.Helper()
	key := append([]byte{0, 0, 0, 11}, []byte("ssh-ed25519")...)
	key = append(key, 0, 0, 0, 32)
	key = append(key, bytes.Repeat([]byte{1}, 32)...)
	values := map[string]string{"project_ref": "prj_fixture_1838", "owner": "example", "repository": "project", "address": "192.0.2.10", "user": "deployer", "host_key": "ssh-ed25519 " + base64.StdEncoding.EncodeToString(key), "published_port": "8080", "runtime_image": "docker.io/library/python@sha256:" + strings.Repeat("a", 64), "ssh_secret_ref": "sec_fixture_1838"}
	target, err := sshcompose.TargetFromConfiguration(values)
	if err != nil {
		t.Fatal(err)
	}
	delete(values, "ssh_secret_ref")
	values["target_ref"] = target.Ref
	return values
}

// TestSSHScopeContractKeepsOtherAdaptersBounded proves exact SSH schema closure without widening other resource kinds.
// TestSSHScopeContractKeepsOtherAdaptersBounded доказывает exact SSH schema closure без расширения других resource kinds.
func TestSSHScopeContractKeepsOtherAdaptersBounded(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromFile("../../../../../../contracts/openapi/control-api-gateway/v1/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []bool{false, true} {
		for _, scenario := range []string{"ssh", "extra", "missing", "field-substitution", "numeric-port", "unpinned-image", "other-nine", "other-eight", "hidden"} {
			t.Run(strconv.FormatBool(candidate)+"/"+scenario, func(t *testing.T) {
				values := map[string]any{}
				for key, value := range sshScopeValuesFixture(t) {
					values[key] = value
				}
				kind, definition := "SSH_TARGET", "ssh-compose"
				switch scenario {
				case "extra":
					values["foreign"] = "value"
				case "missing":
					delete(values, "host_key")
				case "field-substitution":
					delete(values, "host_key")
					values["arbitrary_host"] = "value"
				case "numeric-port":
					values["published_port"] = float64(8080)
				case "unpinned-image":
					values["runtime_image"] = "python:latest"
				case "other-nine":
					kind, definition = "GITHUB_REPOSITORY", "github"
				case "other-eight":
					kind, definition = "GITHUB_REPOSITORY", "github"
					delete(values, "host_key")
				case "hidden":
					values = map[string]any{}
				}
				name := "IntegrationResourceScope"
				value := map[string]any{"kind": kind, "values": values, "digest": strings.Repeat("a", 64)}
				if candidate {
					name = "IntegrationGrantConnectionCandidate"
					value = map[string]any{"connectionRef": "int_fixture_1838", "name": "Fixture", "definitionKey": definition, "providerName": "Fixture", "resourceScope": values, "grantable": true, "usable": true, "reason": "READY", "pins": map[string]any{"contextDigest": strings.Repeat("b", 64)}}
				}
				err := spec.Components.Schemas[name].Value.VisitJSON(value)
				valid := scenario == "ssh" || scenario == "other-eight" || candidate && scenario == "hidden"
				if (err == nil) != valid {
					t.Fatalf("scope schema admission mismatch: %v", err)
				}
			})
		}
	}
}

// TestSSHScopeGateRoutesRejectMissingExtraAndSubstitutedPins checks every public owner gate consumer.
// TestSSHScopeGateRoutesRejectMissingExtraAndSubstitutedPins проверяет каждого public owner gate consumer.
func TestSSHScopeGateRoutesRejectMissingExtraAndSubstitutedPins(t *testing.T) {
	for _, route := range []string{"get", "list", "resolve"} {
		for _, scenario := range []string{"valid", "extra", "missing", "field-substitution", "address-substitution", "target-substitution", "kind-substitution"} {
			t.Run(route+"/"+scenario, func(t *testing.T) {
				gate := integrationGateFixture()
				values := sshScopeValuesFixture(t)
				gate.IntegrationIntent.ResourceScope = &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_SSH_TARGET, Values: values, Digest: strings.Repeat("a", 64)}
				switch scenario {
				case "extra":
					values["foreign"] = "value"
				case "missing":
					delete(values, "host_key")
				case "field-substitution":
					delete(values, "host_key")
					values["arbitrary_host"] = "value"
				case "address-substitution":
					values["address"] = "192.0.2.11"
				case "target-substitution":
					values["target_ref"] = "ssh_" + strings.Repeat("f", 32)
				case "kind-substitution":
					gate.IntegrationIntent.ResourceScope.Kind = cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_GITHUB_REPOSITORY
				}
				code, result := gateProjectionResponse(t, route, gate)
				if (code == 200) != (scenario == "valid") {
					t.Fatalf("SSH gate admission mismatch: %d", code)
				}
				if scenario == "valid" {
					raw, err := json.Marshal(result)
					if err != nil || !bytes.Contains(raw, []byte(values["target_ref"])) {
						t.Fatal("exact public target lost")
					}
				}
			})
		}
	}
}
