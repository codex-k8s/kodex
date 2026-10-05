package workspacepolicy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/common"
	"k8s.io/apiserver/pkg/cel/library"
	"k8s.io/apiserver/pkg/cel/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/yaml"
)

type runtimePolicy struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		FailurePolicy string `json:"failurePolicy"`
		Validations   []struct {
			Expression string `json:"expression"`
		} `json:"validations"`
		MatchConditions []struct {
			Expression string `json:"expression"`
		} `json:"matchConditions"`
		MatchConstraints struct {
			ResourceRules []struct {
				Operations []string `json:"operations"`
				Resources  []string `json:"resources"`
			} `json:"resourceRules"`
		} `json:"matchConstraints"`
	} `json:"spec"`
}

func readRuntimePolicy(t *testing.T, name string) runtimePolicy {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "k8s", "base", "runtime-controller", "runtime-materialization-admission.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range strings.Split(string(content), "\n---\n") {
		var policy runtimePolicy
		if err := yaml.Unmarshal([]byte(document), &policy); err != nil {
			t.Fatal(err)
		}
		if policy.Kind == "ValidatingAdmissionPolicy" && policy.Metadata.Name == name {
			if policy.Spec.FailurePolicy != "Fail" {
				t.Fatal("policy must fail closed")
			}
			if len(policy.Spec.MatchConstraints.ResourceRules) != 1 || strings.Join(policy.Spec.MatchConstraints.ResourceRules[0].Operations, ",") != "CREATE" {
				t.Fatal("runtime materialization must remain create-only")
			}
			return policy
		}
	}
	t.Fatal("runtime policy is absent")
	return runtimePolicy{}
}

func runtimeObjectSchema(properties map[string]spec.Schema) spec.Schema {
	return spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}, Properties: properties}}
}

// Фрагменты схем сохраняют реальные различия Role/RoleBinding и уникальные
// CEL-типы main/init контейнеров; adapter принадлежит Kubernetes 1.35.5.
func runtimeTypedEnvironment(t *testing.T, schema spec.Schema, name string, pod bool) *cel.Env {
	t.Helper()
	declaration := common.SchemaDeclType(&openapi.Schema{Schema: &schema}, true).MaybeAssignTypeName(name)
	base, err := cel.NewEnv(cel.HomogeneousAggregateLiterals(), library.Quantity())
	if err != nil {
		t.Fatal(err)
	}
	options, err := apiservercel.NewDeclTypeProvider(declaration).EnvOptions(base.CELTypeProvider())
	if err != nil {
		t.Fatal(err)
	}
	options = append(options, cel.Variable("object", declaration.CelType()), cel.Variable("request", cel.DynType))
	if pod {
		// VAP composition переводит object-valued элементы variables в dyn;
		// object.spec.initContainers остаётся типизированным schema-to-CEL.
		options = append(options, cel.Variable("variables", cel.MapType(cel.StringType, cel.DynType)), cel.Variable("params", cel.MapType(cel.StringType, cel.DynType)))
	}
	environment, err := base.Extend(options...)
	if err != nil {
		t.Fatal(err)
	}
	return environment
}

func runtimeMetadataSchema() spec.Schema {
	return runtimeObjectSchema(map[string]spec.Schema{"name": *spec.StringProperty(), "generateName": *spec.StringProperty(), "namespace": *spec.StringProperty(), "labels": *spec.MapProperty(spec.StringProperty())})
}

func runtimeRBACSchema(kind string) spec.Schema {
	fields := map[string]spec.Schema{"apiVersion": *spec.StringProperty(), "kind": *spec.StringProperty(), "metadata": runtimeMetadataSchema()}
	if kind == "Role" {
		fields["rules"] = *spec.ArrayProperty(func() *spec.Schema {
			rule := runtimeObjectSchema(map[string]spec.Schema{"apiGroups": *spec.ArrayProperty(spec.StringProperty()), "verbs": *spec.ArrayProperty(spec.StringProperty()), "resourceNames": *spec.ArrayProperty(spec.StringProperty()), "resources": *spec.ArrayProperty(spec.StringProperty())})
			return &rule
		}())
	} else {
		subject := runtimeObjectSchema(map[string]spec.Schema{"kind": *spec.StringProperty(), "namespace": *spec.StringProperty(), "name": *spec.StringProperty()})
		fields["subjects"] = *spec.ArrayProperty(&subject)
		fields["roleRef"] = runtimeObjectSchema(map[string]spec.Schema{"kind": *spec.StringProperty(), "apiGroup": *spec.StringProperty(), "name": *spec.StringProperty()})
	}
	return runtimeObjectSchema(fields)
}

func runtimeCompile(t *testing.T, environment *cel.Env, expression string) cel.Program {
	t.Helper()
	ast, issues := environment.Compile(expression)
	if issues != nil && issues.Err() != nil {
		t.Fatal(issues.Err())
	}
	program, err := environment.Program(ast)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func runtimeAccepted(program cel.Program, input map[string]any) bool {
	result, _, err := program.Eval(input)
	return err == nil && result.Value() == true
}

func runtimeClone(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

const runtimeExecutionHash = "0123456789abcdef"

func runtimeRBACFixture(kind string) map[string]any {
	object := map[string]any{"kind": kind, "metadata": map[string]any{"namespace": "kodex-runtime", "labels": map[string]any{"runtime.kodex.dev/managed": "true", "runtime.kodex.dev/mode": "turn", "runtime.kodex.dev/execution-hash": runtimeExecutionHash}}}
	metadata := object["metadata"].(map[string]any)
	if kind == "Role" {
		metadata["name"] = "runtime-role-" + runtimeExecutionHash
		object["rules"] = []any{
			map[string]any{"apiGroups": []any{""}, "verbs": []any{"get"}, "resourceNames": []any{"runtime-turn-" + runtimeExecutionHash}, "resources": []any{"pods"}},
			map[string]any{"apiGroups": []any{""}, "verbs": []any{"get"}, "resourceNames": []any{"runtime-turn-" + runtimeExecutionHash}, "resources": []any{"pods/log"}},
		}
	} else {
		metadata["name"] = "runtime-rb-" + runtimeExecutionHash
		object["subjects"] = []any{map[string]any{"kind": "ServiceAccount", "namespace": "kodex-runtime", "name": "runtime-sa-" + runtimeExecutionHash}}
		object["roleRef"] = map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "runtime-role-" + runtimeExecutionHash}
	}
	return map[string]any{"object": object, "request": map[string]any{"userInfo": map[string]any{"username": "system:serviceaccount:kodex-system:runtime-controller"}}}
}

func TestRuntimeRBACUnionKubernetes135TypeCheckingAndEvaluation(t *testing.T) {
	policy := readRuntimePolicy(t, "runtime-execution-rbac")
	for _, kind := range []string{"Role", "RoleBinding"} {
		t.Run(kind, func(t *testing.T) {
			environment := runtimeTypedEnvironment(t, runtimeRBACSchema(kind), kind, false)
			var programs []cel.Program
			for _, validation := range policy.Spec.Validations {
				programs = append(programs, runtimeCompile(t, environment, validation.Expression))
			}
			for _, index := range []int{2, 3} {
				previous := strings.ReplaceAll(policy.Spec.Validations[index].Expression, "dyn(object)", "object")
				_, issues := environment.Compile(previous)
				if (kind == "Role" && index == 3) || (kind == "RoleBinding" && index == 2) {
					if issues == nil || issues.Err() == nil || !strings.Contains(issues.Err().Error(), "undefined field") {
						t.Fatal("original cross-kind schema warning was not reproduced")
					}
				}
			}
			badMetadata := strings.Replace(policy.Spec.Validations[2].Expression, "object.metadata.name", "object.metadata.names", 1)
			if _, issues := environment.Compile(badMetadata); issues == nil || issues.Err() == nil {
				t.Fatal("dyn escaped union fields into metadata")
			}
			accepted := func(input map[string]any) bool {
				for _, program := range programs {
					if !runtimeAccepted(program, input) {
						return false
					}
				}
				return true
			}
			fixture := runtimeRBACFixture(kind)
			if !accepted(fixture) {
				t.Fatal("valid exact execution RBAC was rejected")
			}
			cases := map[string]func(map[string]any){
				"unknown-kind": func(o map[string]any) { o["kind"] = "ClusterRole" },
				"foreign-execution": func(o map[string]any) {
					o["metadata"].(map[string]any)["labels"].(map[string]any)["runtime.kodex.dev/execution-hash"] = "fedcba9876543210"
				},
				"wrong-mode": func(o map[string]any) {
					o["metadata"].(map[string]any)["labels"].(map[string]any)["runtime.kodex.dev/mode"] = "warm"
				},
				"missing-labels": func(o map[string]any) { delete(o["metadata"].(map[string]any), "labels") },
			}
			if kind == "Role" {
				cases["missing-rules"] = func(o map[string]any) { delete(o, "rules") }
				cases["malformed-rules"] = func(o map[string]any) { o["rules"] = "invalid" }
				for _, field := range []string{"apiGroups", "verbs", "resources", "resourceNames"} {
					cases["wildcard-"+field] = func(o map[string]any) { o["rules"].([]any)[0].(map[string]any)[field] = []any{"*"} }
				}
				cases["foreign-pod"] = func(o map[string]any) {
					o["rules"].([]any)[0].(map[string]any)["resourceNames"] = []any{"runtime-turn-fedcba9876543210"}
				}
				cases["extra-rule"] = func(o map[string]any) { o["rules"] = append(o["rules"].([]any), o["rules"].([]any)[0]) }
			} else {
				cases["missing-subjects"] = func(o map[string]any) { delete(o, "subjects") }
				cases["missing-roleRef"] = func(o map[string]any) { delete(o, "roleRef") }
				cases["extra-subject"] = func(o map[string]any) { o["subjects"] = append(o["subjects"].([]any), o["subjects"].([]any)[0]) }
				cases["foreign-subject-namespace"] = func(o map[string]any) { o["subjects"].([]any)[0].(map[string]any)["namespace"] = "foreign" }
				cases["foreign-subject-name"] = func(o map[string]any) {
					o["subjects"].([]any)[0].(map[string]any)["name"] = "runtime-sa-fedcba9876543210"
				}
				cases["cluster-role"] = func(o map[string]any) { o["roleRef"].(map[string]any)["kind"] = "ClusterRole" }
				cases["foreign-role"] = func(o map[string]any) { o["roleRef"].(map[string]any)["name"] = "runtime-role-fedcba9876543210" }
			}
			for name, mutate := range cases {
				t.Run(name, func(t *testing.T) {
					input := runtimeClone(t, fixture)
					mutate(input["object"].(map[string]any))
					if accepted(input) {
						t.Fatal("invalid execution RBAC was accepted")
					}
				})
			}
			input := runtimeClone(t, fixture)
			input["request"].(map[string]any)["userInfo"].(map[string]any)["username"] = "system:serviceaccount:kodex-runtime:agent-runner"
			if accepted(input) {
				t.Fatal("runtime actor passed controller validation")
			}
			// VAP schema type checking относится к validations; matchConditions
			// исполняются с динамическим object в admission evaluator.
			matchEnvironment, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("request", cel.DynType))
			if err != nil {
				t.Fatal(err)
			}
			match := runtimeCompile(t, matchEnvironment, policy.Spec.MatchConditions[0].Expression)
			if !runtimeAccepted(match, fixture) {
				t.Fatal("controller materialization did not match")
			}
			input = runtimeClone(t, fixture)
			input["object"].(map[string]any)["metadata"].(map[string]any)["namespace"] = "foreign"
			if runtimeAccepted(match, input) {
				t.Fatal("policy unexpectedly matched a foreign namespace")
			}
		})
	}
}

func runtimePodSchema() spec.Schema {
	quantity := spec.Schema{SchemaProps: spec.SchemaProps{OneOf: []spec.Schema{*spec.StringProperty(), *spec.Float64Property()}}}
	resources := runtimeObjectSchema(map[string]spec.Schema{"requests": *spec.MapProperty(&quantity), "limits": *spec.MapProperty(&quantity)})
	container := runtimeObjectSchema(map[string]spec.Schema{"name": *spec.StringProperty(), "image": *spec.StringProperty(), "imagePullPolicy": *spec.StringProperty(), "command": *spec.ArrayProperty(spec.StringProperty()), "args": *spec.ArrayProperty(spec.StringProperty()), "resources": resources})
	podSpec := runtimeObjectSchema(map[string]spec.Schema{"containers": *spec.ArrayProperty(&container), "initContainers": *spec.ArrayProperty(&container)})
	return runtimeObjectSchema(map[string]spec.Schema{"apiVersion": *spec.StringProperty(), "kind": *spec.StringProperty(), "metadata": runtimeMetadataSchema(), "spec": podSpec})
}

func runtimePodFixture(mode string) map[string]any {
	image := "example.invalid/runtime@sha256:" + strings.Repeat("a", 64)
	relayImage := "example.invalid/relay@sha256:" + strings.Repeat("b", 64)
	container := func(name, image, arg string) map[string]any {
		return map[string]any{"name": name, "image": image, "imagePullPolicy": "IfNotPresent", "args": []any{arg}, "resources": map[string]any{"requests": map[string]any{"cpu": "25m", "memory": "64Mi"}, "limits": map[string]any{"cpu": "500m", "memory": "256Mi"}}}
	}
	arg := "runtime-session"
	if mode == "warm" {
		arg = "runtime-warm"
	}
	role := container("role-runtime", image, arg)
	provider := container("provider-runtime", image, "runtime-provider")
	relay := container("provider-credential-relay", relayImage, "runtime-provider-credential-relay")
	init := container("workspace-init", image, "runtime-init-workspace")
	return map[string]any{"object": map[string]any{"metadata": map[string]any{"labels": map[string]any{"runtime.kodex.dev/mode": mode}}, "spec": map[string]any{"containers": []any{role, provider, relay}, "initContainers": []any{container("workspace-prepare", image, "runtime-prepare-workspace"), init}}}, "variables": map[string]any{"roleContainers": []any{role}, "providerContainers": []any{provider}, "relayContainers": []any{relay}}, "params": map[string]any{"data": map[string]any{"nodeReadbackImage": relayImage}}}
}

func runtimePodContainer(input map[string]any, name string) map[string]any {
	if name == "workspace-init" {
		return input["object"].(map[string]any)["spec"].(map[string]any)["initContainers"].([]any)[1].(map[string]any)
	}
	return input["variables"].(map[string]any)[name].([]any)[0].(map[string]any)
}

func TestRuntimePodMixedContainerKubernetes135TypeCheckingAndEvaluation(t *testing.T) {
	policy := readRuntimePolicy(t, "runtime-role-pod-exact-secret-projection")
	environment := runtimeTypedEnvironment(t, runtimePodSchema(), "Pod", true)
	for _, index := range []int{6, 9} {
		expression := policy.Spec.Validations[index].Expression
		program := runtimeCompile(t, environment, expression)
		previous := strings.ReplaceAll(expression, "dyn(object.spec.initContainers[1])", "object.spec.initContainers[1]")
		if previous == expression {
			t.Fatal("only the mixed-list init element may become dynamic")
		}
		if _, issues := environment.Compile(previous); issues == nil || issues.Err() == nil || !strings.Contains(issues.Err().Error(), "expected type 'dyn'") {
			t.Fatal("original heterogeneous-list warning was not reproduced")
		}
		for _, replacement := range []string{"object.spec.initContainer[1]", "object.specs.initContainers[1]"} {
			if _, issues := environment.Compile(strings.ReplaceAll(expression, "object.spec.initContainers[1]", replacement)); issues == nil || issues.Err() == nil {
				t.Fatal("dyn escaped the container element boundary")
			}
		}
		for _, mode := range []string{"turn", "warm"} {
			fixture := runtimePodFixture(mode)
			if !runtimeAccepted(program, fixture) {
				t.Fatal("valid pinned runtime container contract was rejected")
			}
			names := []string{"roleContainers", "providerContainers", "relayContainers", "workspace-init"}
			if index == 9 {
				names = []string{"roleContainers", "relayContainers", "workspace-init"}
			}
			for _, name := range names {
				cases := map[string]func(map[string]any){}
				if index == 6 {
					cases["mutable-image"] = func(c map[string]any) { c["image"] = "example.invalid/runtime:latest" }
					cases["foreign-digest"] = func(c map[string]any) { c["image"] = "example.invalid/foreign@sha256:" + strings.Repeat("c", 64) }
					cases["pull-always"] = func(c map[string]any) { c["imagePullPolicy"] = "Always" }
					cases["command-override"] = func(c map[string]any) { c["command"] = []any{"sh"} }
					cases["argument-override"] = func(c map[string]any) { c["args"] = []any{"sh"} }
				} else {
					cases["extra-request"] = func(c map[string]any) {
						c["resources"].(map[string]any)["requests"].(map[string]any)["ephemeral-storage"] = "1Gi"
					}
					cases["missing-resources"] = func(c map[string]any) { delete(c, "resources") }
					for _, side := range []string{"requests", "limits"} {
						for _, field := range []string{"cpu", "memory"} {
							cases[side+"-"+field+"-above"] = func(c map[string]any) { c["resources"].(map[string]any)[side].(map[string]any)[field] = "100Gi" }
							cases[side+"-"+field+"-below"] = func(c map[string]any) { c["resources"].(map[string]any)[side].(map[string]any)[field] = "1m" }
							cases[side+"-"+field+"-invalid"] = func(c map[string]any) { c["resources"].(map[string]any)[side].(map[string]any)[field] = "invalid" }
						}
					}
				}
				for caseName, mutate := range cases {
					t.Run(mode+"/"+name+"/"+caseName, func(t *testing.T) {
						input := runtimeClone(t, fixture)
						mutate(runtimePodContainer(input, name))
						if runtimeAccepted(program, input) {
							t.Fatal("invalid runtime container contract was accepted")
						}
					})
				}
			}
		}
	}
}
