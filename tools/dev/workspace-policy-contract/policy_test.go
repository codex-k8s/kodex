package workspacepolicy

import (
	"os"
	"path/filepath"
	"regexp"
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

// Проверяется реальный schema-to-CEL adapter Kubernetes 1.35.5, а не dyn-only
// окружение. Quantity сохраняет опубликованный oneOf без поля type.
func TestWorkspaceStoragePolicyKubernetes135TypeChecking(t *testing.T) {
	quantity := spec.Schema{SchemaProps: spec.SchemaProps{OneOf: []spec.Schema{*spec.StringProperty(), *spec.Float64Property()}}}
	resources := spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}, Properties: map[string]spec.Schema{
		"requests": *spec.MapProperty(&quantity), "limits": *spec.MapProperty(&quantity),
	}}}
	storageSpec := spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}, Properties: map[string]spec.Schema{
		"resources": resources, "accessModes": *spec.ArrayProperty(spec.StringProperty()),
		"volumeName": *spec.StringProperty(), "selector": *spec.MapProperty(spec.StringProperty()),
		"dataSource": *spec.MapProperty(spec.StringProperty()), "dataSourceRef": *spec.MapProperty(spec.StringProperty()),
	}}}
	schema := spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}, Properties: map[string]spec.Schema{"spec": storageSpec}}}
	declaration := common.SchemaDeclType(&openapi.Schema{Schema: &schema}, false).MaybeAssignTypeName("PVC")
	if _, ok := declaration.Fields["spec"].Type.Fields["resources"].Type.Fields["requests"]; ok {
		t.Fatal("Kubernetes 1.35 Quantity schema unexpectedly exposes requests")
	}
	base, err := cel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	options, err := apiservercel.NewDeclTypeProvider(declaration).EnvOptions(base.CELTypeProvider())
	if err != nil {
		t.Fatal(err)
	}
	options = append(options, cel.Variable("object", declaration.CelType()), cel.Variable("request", cel.DynType))
	environment, err := base.Extend(options...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "k8s", "base", "image-supply-chain", "image-admission-controller-policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var expression string
	for _, document := range strings.Split(string(content), "\n---\n") {
		var policy struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Validations []struct {
					Expression string `json:"expression"`
				} `json:"validations"`
			} `json:"spec"`
		}
		if err := yaml.Unmarshal([]byte(document), &policy); err != nil {
			t.Fatal(err)
		}
		if policy.Kind == "ValidatingAdmissionPolicy" && policy.Metadata.Name == "kodex-image-admission-controller-workspaces" {
			expression = policy.Spec.Validations[4].Expression
		}
	}
	if expression == "" {
		t.Fatal("workspace storage expression is absent")
	}
	if _, issues := environment.Compile(expression); issues != nil && issues.Err() != nil {
		t.Fatal(issues.Err())
	}
	previous := strings.Replace(expression, "dyn(object.spec.resources)", "object.spec.resources", 1)
	if previous == expression {
		t.Fatal("Quantity boundary must be explicitly dynamic")
	}
	if _, issues := environment.Compile(previous); issues == nil || issues.Err() == nil || !strings.Contains(issues.Err().Error(), "undefined field 'requests'") {
		t.Fatal("regression did not reproduce the original typed compilation warning")
	}
	for _, changed := range []string{
		strings.Replace(expression, "object.spec.accessModes", "object.spec.accessMode", 1),
		strings.Replace(expression, "object.spec.volumeName", "object.spec.volumeNames", 1),
	} {
		if _, issues := environment.Compile(changed); issues == nil || issues.Err() == nil {
			t.Fatal("dyn escaped the exact Quantity boundary")
		}
	}
}

func TestRuntimeEmptyDirQuantityTypeCheckingAndExactBounds(t *testing.T) {
	quantity := spec.Schema{SchemaProps: spec.SchemaProps{OneOf: []spec.Schema{*spec.StringProperty(), *spec.Float64Property()}}}
	emptyDir := spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}, Properties: map[string]spec.Schema{
		"sizeLimit": quantity, "medium": *spec.StringProperty(),
	}}}
	volume := spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}, Properties: map[string]spec.Schema{"emptyDir": emptyDir}}}
	declaration := common.SchemaDeclType(&openapi.Schema{Schema: &volume}, false).MaybeAssignTypeName("Volume")
	if _, ok := declaration.Fields["emptyDir"].Type.Fields["sizeLimit"]; ok {
		t.Fatal("Quantity unexpectedly has a static type")
	}
	base, err := cel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	options, err := apiservercel.NewDeclTypeProvider(declaration).EnvOptions(base.CELTypeProvider())
	if err != nil {
		t.Fatal(err)
	}
	options = append(options, cel.Variable("volume", declaration.CelType()), library.Quantity())
	environment, err := base.Extend(options...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "k8s", "base", "runtime-controller", "runtime-materialization-admission.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "volume.emptyDir.sizeLimit") {
		t.Fatal("unguarded Quantity schema reference remains")
	}
	expressions := regexp.MustCompile(`quantity\(string\(dyn\(volume\.emptyDir\)\.sizeLimit\)\)\.compareTo\(quantity\('([^']+)'\)\) == 0`).FindAllStringSubmatch(string(content), -1)
	if len(expressions) != 5 {
		t.Fatal("exact runtime emptyDir registry changed")
	}
	for _, match := range expressions {
		t.Run(match[1], func(t *testing.T) {
			ast, issues := environment.Compile(match[0])
			if issues != nil && issues.Err() != nil {
				t.Fatal(issues.Err())
			}
			previous := strings.Replace(match[0], "dyn(volume.emptyDir)", "volume.emptyDir", 1)
			if _, issues := environment.Compile(previous); issues == nil || issues.Err() == nil || !strings.Contains(issues.Err().Error(), "undefined field 'sizeLimit'") {
				t.Fatal("original Quantity warning was not reproduced")
			}
			program, err := environment.Program(ast)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []any{match[1], "1Ki", "100Gi", "invalid", int64(1), nil} {
				fields := map[string]any{"medium": "Memory"}
				if value != nil {
					fields["sizeLimit"] = value
				}
				result, _, err := program.Eval(map[string]any{"volume": map[string]any{"emptyDir": fields}})
				accepted := err == nil && result.Value() == true
				if accepted != (value == match[1]) {
					t.Fatalf("exact size %s boundary disagreed with fixture", match[1])
				}
			}
		})
	}
}
