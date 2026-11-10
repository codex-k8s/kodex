package workspacepolicy

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"k8s.io/apiserver/pkg/cel/library"
)

// TestManagedWorkspaceActualProducerFullAdmission evaluates every Pod validation against
// actual producer fixtures, so a passing observer cannot mask incompatible deployed admission.
// TestManagedWorkspaceActualProducerFullAdmission проверяет все Pod validations по оснасткам
// actual producer: успешный observer не скрывает несовместимость устанавливаемого admission.
func TestManagedWorkspaceActualProducerFullAdmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pods.json")
	command := exec.CommandContext(t.Context(), "go", "test", "./internal/workload", "-run", "^TestManagedWorkspaceExportAdmissionFixtures$", "-count=1")
	command.Dir = filepath.Join("..", "..", "..", "services", "internal", "runtime-controller")
	command.Env = []string{"KODEX_TEST_RUNTIME_ADMISSION_FIXTURES=" + path, "GOWORK=off", "GOENV=off"}
	for _, key := range []string{"PATH", "HOME", "GOTOOLCHAIN", "GOPATH", "GOMODCACHE", "GOCACHE"} {
		if value, ok := os.LookupEnv(key); ok {
			command.Env = append(command.Env, key+"="+value)
		}
	}

	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual producer fixture failed: %s: %v", out, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name   string         `json:"name"`
		Pod    map[string]any `json:"pod"`
		Params map[string]any `json:"params"`
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil || len(fixtures) != 7 {
		t.Fatal("actual fixtures missing", err)
	}
	policy := readRuntimePolicy(t, "runtime-role-pod-exact-secret-projection")
	env, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("variables", cel.DynType), cel.Variable("params", cel.DynType), cel.Variable("request", cel.DynType), library.Quantity())
	if err != nil {
		t.Fatal(err)
	}
	programs := make([]cel.Program, len(policy.Spec.Validations))
	for i, rule := range policy.Spec.Validations {
		programs[i] = runtimeCompile(t, env, rule.Expression)
	}

	variablePrograms := make([]cel.Program, len(policy.Spec.Variables))
	for i, declaration := range policy.Spec.Variables {
		variablePrograms[i] = runtimeCompile(t, env, declaration.Expression)
	}
	evaluate := func(object map[string]any, params map[string]any, user string) []string {
		variables := map[string]any{}
		activation := map[string]any{"object": object, "variables": variables, "params": params, "request": map[string]any{"userInfo": map[string]any{"username": user}}}
		for i, program := range variablePrograms {
			value, _, err := program.Eval(activation)
			if err != nil {
				return []string{"variable " + policy.Spec.Variables[i].Name}
			}
			variables[policy.Spec.Variables[i].Name] = value
		}
		var failures []string
		for i, program := range programs {
			value, _, err := program.Eval(activation)
			if err != nil || value == nil || value.Value() != true {
				failures = append(failures, policy.Spec.Validations[i].Message)
			}
		}
		return failures
	}
	user := "system:serviceaccount:kodex-system:runtime-controller"
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			if failures := evaluate(fixture.Pod, fixture.Params, user); len(failures) > 0 {
				t.Fatal("producer rejected:", failures)
			}
			if len(evaluate(fixture.Pod, fixture.Params, "system:serviceaccount:example:untrusted")) == 0 {
				t.Fatal("untrusted actor accepted")
			}
			mutations := map[string]func(map[string]any){
				"host PID":   func(p map[string]any) { p["spec"].(map[string]any)["hostPID"] = true },
				"shared PID": func(p map[string]any) { p["spec"].(map[string]any)["shareProcessNamespace"] = true },
				"foreign volume": func(p map[string]any) {
					spec := p["spec"].(map[string]any)
					spec["volumes"] = append(spec["volumes"].([]any), map[string]any{"name": "foreign-credential", "secret": map[string]any{"secretName": "synthetic"}})
				},
			}
			if strings.HasPrefix(fixture.Name, "commands") {
				mutations["wrong executor image"] = func(p map[string]any) {
					managedContainer(p, "workspace-executor")["image"] = "registry.example.invalid/foreign@sha256:" + strings.Repeat("f", 64)
				}
				mutations["inherited image init"] = func(p map[string]any) {
					managedContainer(p, "workspace-executor")["command"] = []any{"/usr/local/bin/kodex-init"}
				}
				mutations["unmasked proc"] = func(p map[string]any) {
					managedContainer(p, "workspace-executor")["securityContext"].(map[string]any)["procMount"] = "Unmasked"
				}
				mutations["alternate SELinux context"] = func(p map[string]any) {
					managedContainer(p, "workspace-executor")["securityContext"].(map[string]any)["seLinuxOptions"] = map[string]any{"type": "unconfined_t"}
				}
				mutations["wrong executor UID"] = func(p map[string]any) {
					managedContainer(p, "workspace-executor")["securityContext"].(map[string]any)["runAsUser"] = float64(10001)
				}
				mutations["wrong executor group"] = func(p map[string]any) {
					managedContainer(p, "workspace-executor")["securityContext"].(map[string]any)["runAsGroup"] = float64(10002)
				}
				mutations["unconfined executor"] = func(p map[string]any) {
					managedContainer(p, "workspace-executor")["securityContext"].(map[string]any)["seccompProfile"] = map[string]any{"type": "Unconfined"}
				}
				mutations["credential environment"] = func(p map[string]any) {
					c := managedContainer(p, "workspace-executor")
					c["env"] = append(c["env"].([]any), map[string]any{"name": "SYNTHETIC_KEY", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "synthetic", "key": "key"}}})
				}
				mutations["credential mount"] = func(p map[string]any) {
					c := managedContainer(p, "workspace-executor")
					c["volumeMounts"] = append(c["volumeMounts"].([]any), map[string]any{"name": "provider-auth", "mountPath": "/credential"})
				}
				mutations["provider socket access"] = func(p map[string]any) {
					c := managedContainer(p, "provider-runtime")
					c["volumeMounts"] = append(c["volumeMounts"].([]any), map[string]any{"name": "workspace-executor-socket", "mountPath": "/run/kodex/workspace"})
				}
				mutations["private state subpath"] = func(p map[string]any) {
					c := managedContainer(p, "workspace-executor")
					c["volumeMounts"].([]any)[0].(map[string]any)["subPath"] = "provider-state"
				}
				mutations["unbalanced budget"] = func(p map[string]any) {
					c := managedContainer(p, "workspace-executor")
					c["resources"].(map[string]any)["requests"].(map[string]any)["cpu"] = "8"
				}
				mutations["writable provider workspace"] = func(p map[string]any) {
					for _, v := range managedContainer(p, "provider-runtime")["volumeMounts"].([]any) {
						m := v.(map[string]any)
						if m["mountPath"] == "/workspace" {
							m["readOnly"] = false
						}
					}
				}
				mutations["oversized scratch"] = func(p map[string]any) {
					for _, v := range p["spec"].(map[string]any)["volumes"].([]any) {
						m := v.(map[string]any)
						if m["name"] == "workspace-executor-tmp" {
							m["emptyDir"].(map[string]any)["sizeLimit"] = "1Gi"
						}
					}
				}
				mutations["probe execution"] = func(p map[string]any) {
					managedContainer(p, "workspace-executor")["readinessProbe"].(map[string]any)["exec"] = map[string]any{"command": []any{"/bin/bash", "-c", "true"}}
				}
			}
			for name, mutate := range mutations {
				t.Run(name, func(t *testing.T) {
					object := runtimeClone(t, fixture.Pod)
					mutate(object)
					if len(evaluate(object, fixture.Params, user)) == 0 {
						t.Fatal("unsafe producer mutation accepted")
					}
				})
			}
		})
	}
}

// managedContainer selects a named synthetic producer container for one independent mutation.
// managedContainer выбирает именованный synthetic producer container для независимой мутации.
func managedContainer(object map[string]any, name string) map[string]any {
	for _, entry := range object["spec"].(map[string]any)["containers"].([]any) {
		container := entry.(map[string]any)
		if container["name"] == name {
			return container
		}
	}
	panic("synthetic container missing")
}
