package workspacepolicy

import (
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
)

// workspaceHistorySecurity constructs only the closed fixture security fields consumed by the real policy, without issuing execution authority.
// workspaceHistorySecurity создаёт только closed fixture security fields реальной policy без выдачи execution authority.
func workspaceHistorySecurity(uid, group int64, seccomp string) map[string]any {
	return map[string]any{"runAsUser": uid, "runAsGroup": group, "runAsNonRoot": true,
		"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true,
		"capabilities": map[string]any{"drop": []any{"ALL"}}, "seccompProfile": map[string]any{"type": seccomp}}
}

// workspaceHistoryBoundaryFixture supplies the complete session-mount subset used by the actual deployed V2 boundary expression.
// This fixture tests that expression only; full Pod materialization and owner grants require separate producer/native acceptance.
// workspaceHistoryBoundaryFixture задаёт полный session-mount subset реального deployed V2 boundary expression.
// Fixture проверяет только это expression; full Pod materialization/owner grants требуют отдельной producer/native acceptance.
func workspaceHistoryBoundaryFixture() map[string]any {
	prefix := "workspaces/wks_fixture000/g1"
	container := func(name string, writer bool) map[string]any {
		mounts := []any{
			map[string]any{"name": "session", "mountPath": "/workspace/.kodex/state", "subPath": "provider-state"},
			map[string]any{"name": "session", "mountPath": "/workspace/work", "subPath": prefix + "/tree"},
			map[string]any{"name": "session", "mountPath": "/var/run/config/kodex/workspace/mount.json", "subPath": prefix + "/mount.json", "readOnly": true},
		}
		if writer {
			mounts = append(mounts, map[string]any{"name": "session", "mountPath": "/var/run/config/kodex/workspace/writer.json", "subPath": prefix + "/writer.json", "readOnly": true})
		}
		return map[string]any{"name": name, "volumeMounts": mounts}
	}
	role, provider, init := container("role-runtime", true), container("provider-runtime", false), container("workspace-init", true)
	role["terminationMessagePath"], role["terminationMessagePolicy"] = "/dev/termination-log", "File"
	role["securityContext"] = workspaceHistorySecurity(10001, 29000, "RuntimeDefault")
	provider["securityContext"] = workspaceHistorySecurity(10002, 29000, "Unconfined")
	init["securityContext"] = workspaceHistorySecurity(10001, 29000, "RuntimeDefault")
	prepare := map[string]any{"name": "workspace-prepare", "volumeMounts": []any{
		map[string]any{"name": "session", "mountPath": "/var/run/config/kodex/session-workspace"}},
		"securityContext": workspaceHistorySecurity(10001, 29000, "RuntimeDefault")}
	relay := map[string]any{"name": "provider-credential-relay", "securityContext": workspaceHistorySecurity(10003, 10003, "RuntimeDefault")}
	return map[string]any{"object": map[string]any{"spec": map[string]any{"initContainers": []any{prepare, init}}},
		"params": map[string]any{"data": map[string]any{"providerAppArmorProfile": ""}},
		"variables": map[string]any{"workspaceStorageV2": true, "workspaceStoragePrefix": prefix,
			"allContainers":  []any{prepare, init, role, provider, relay},
			"roleContainers": []any{role}, "providerContainers": []any{provider}, "relayContainers": []any{relay}}}
}

// TestWorkspaceWriterPrimaryGroupCELKeepsLegacyAndV2Separate checks the actual conditional security expression without widening any UID or relay group.
// TestWorkspaceWriterPrimaryGroupCELKeepsLegacyAndV2Separate проверяет actual conditional security expression без расширения UID либо relay group.
func TestWorkspaceWriterPrimaryGroupCELKeepsLegacyAndV2Separate(t *testing.T) {
	policy := readRuntimePolicy(t, "runtime-role-pod-exact-secret-projection")
	var expression string
	for _, validation := range policy.Spec.Validations {
		if strings.Contains(validation.Expression, "runAsGroup == (variables.workspaceStorageV2") {
			expression = validation.Expression
		}
	}
	if expression == "" {
		t.Fatal("conditional V2 writer group predicate absent")
	}
	environment, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("variables", cel.DynType), cel.Variable("params", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}
	program := runtimeCompile(t, environment, expression)
	for _, profile := range []string{"V2", "legacy"} {
		fixture := workspaceHistoryBoundaryFixture()
		variables := fixture["variables"].(map[string]any)
		if profile == "legacy" {
			variables["workspaceStorageV2"] = false
			for _, item := range variables["allContainers"].([]any) {
				security := item.(map[string]any)["securityContext"].(map[string]any)
				security["runAsGroup"] = security["runAsUser"]
			}
		}
		if !runtimeAccepted(program, fixture) {
			t.Fatal("closed writer group profile rejected")
		}
		for _, name := range []string{"roleContainers", "providerContainers", "workspace-init", "relayContainers"} {
			t.Run(profile+"/"+name, func(t *testing.T) {
				input := runtimeClone(t, fixture)
				security := runtimePodContainer(input, name)["securityContext"].(map[string]any)
				security["runAsGroup"] = 0
				if runtimeAccepted(program, input) {
					t.Fatal("unpublished writer primary group accepted")
				}
			})
		}
	}
}

// TestWorkspaceHistoryCELRefusesFullPVCAndPrivateGenerationAliases exercises the real policy expression against the former accepted full-root mount.
// TestWorkspaceHistoryCELRefusesFullPVCAndPrivateGenerationAliases проверяет actual policy expression на прежнем full-root mount.
func TestWorkspaceHistoryCELRefusesFullPVCAndPrivateGenerationAliases(t *testing.T) {
	policy := readRuntimePolicy(t, "runtime-role-pod-exact-secret-projection")
	var expression string
	for _, validation := range policy.Spec.Validations {
		if strings.Contains(validation.Expression, "'/workspace/.kodex/state/workspaces'") {
			expression = validation.Expression
		}
	}
	if expression == "" {
		t.Fatal("actual workspace storage boundary is absent")
	}
	environment, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("variables", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}
	program := runtimeCompile(t, environment, expression)
	fixture := workspaceHistoryBoundaryFixture()
	if !runtimeAccepted(program, fixture) {
		t.Fatal("separate exact provider history subpath rejected")
	}
	previousExpression := strings.Replace(expression, "mount.subPath == 'provider-state'", "!has(mount.subPath)", 1)
	if previousExpression == expression {
		t.Fatal("provider-state subpath predicate is absent")
	}
	previous := runtimeCompile(t, environment, previousExpression)
	unsafe := workspaceHistoryBoundaryFixture()
	for _, item := range unsafe["variables"].(map[string]any)["allContainers"].([]any) {
		container := item.(map[string]any)
		mounts, _ := container["volumeMounts"].([]any)
		for _, mount := range mounts {
			entry := mount.(map[string]any)
			if entry["mountPath"] == "/workspace/.kodex/state" {
				delete(entry, "subPath")
			}
		}
	}
	if !runtimeAccepted(previous, unsafe) || runtimeAccepted(program, unsafe) {
		t.Fatal("former full-PVC predicate was not reproduced and closed")
	}
	for _, name := range []string{"roleContainers", "providerContainers", "workspace-init"} {
		for _, mode := range []string{"no subpath", "empty subpath", "private parent", "another history", "nested work", "writable metadata"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				// JSON cloning must reconstruct shared fixture references before using them as both allContainers and named variables.
				// JSON cloning восстанавливает отдельные fixture references; allContainers ниже явно связывается с named consumer.
				input := runtimeClone(t, fixture)
				consumer := runtimePodContainer(input, name)
				variables := input["variables"].(map[string]any)
				all := variables["allContainers"].([]any)
				for index, item := range all {
					if item.(map[string]any)["name"] == consumer["name"] {
						all[index] = consumer
					}
				}
				mounts := consumer["volumeMounts"].([]any)
				state := mounts[0].(map[string]any)
				switch mode {
				case "no subpath":
					delete(state, "subPath")
				case "empty subpath":
					state["subPath"] = ""
				case "private parent":
					state["subPath"] = "workspaces"
				case "another history":
					state["subPath"] = "foreign-provider-state"
				case "nested work":
					state["mountPath"] = "/workspace/work/alias"
				case "writable metadata":
					mounts[2].(map[string]any)["readOnly"] = false
				}
				if runtimeAccepted(program, input) {
					t.Fatal("unsafe history or private generation alias accepted")
				}
			})
		}
	}
}
