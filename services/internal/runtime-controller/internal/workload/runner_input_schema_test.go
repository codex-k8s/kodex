package workload

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRunnerInputSchemaV6MatchesRuntimePayload(t *testing.T) {
	compiled, properties, required := loadRunnerInputSchema(t)
	assertRunnerInputShape(t, properties, required)

	manager := newTestManager(t, fake.NewSimpleClientset())
	turn, _, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	validateRunnerInputSchema(t, compiled, turn)

	warm, _, err := manager.BuildWarmInput(testWarmRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	validateRunnerInputSchema(t, compiled, warm)
}

func TestRunnerInputSchemaV6CarriesOnlySecretDescriptors(t *testing.T) {
	compiled, _, _ := loadRunnerInputSchema(t)
	secretValue := []byte("runner-schema-secret-fixture")
	digest := sha256.Sum256(secretValue)
	values := []runtimecontract.RuntimeEnvironmentValue{{Name: "FEATURE_FLAG", Value: "enabled"}}
	projections := []runtimecontract.RuntimeSecretProjection{{
		Name: "SERVICE_TOKEN", SecretName: "runtime-agent-environment-r1", SecretKey: "token",
		SecretUID: "20000000-0000-4000-8000-000000000001", SecretResourceVersion: "7",
		ContentSHA256: hex.EncodeToString(digest[:]),
	}}
	execution := testExecution(false)
	image, tools := runtimeEnvironmentContract(execution.Revision)
	environmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(values, projections, image, tools)
	if err != nil {
		t.Fatalf("RuntimeEnvironmentDigest() error = %v", err)
	}
	execution.Revision.EnvironmentValues = []*controlplanev1.RuntimeEnvironmentValue{{Name: values[0].Name, Value: values[0].Value}}
	execution.Revision.SecretProjections = []*controlplanev1.RuntimeSecretDescriptor{{
		Name: projections[0].Name, SecretName: projections[0].SecretName, SecretKey: projections[0].SecretKey,
		SecretUid: projections[0].SecretUID, SecretResourceVersion: projections[0].SecretResourceVersion,
		ContentSha256: projections[0].ContentSHA256,
	}}
	execution.Revision.RuntimeEnvironmentDigest = environmentDigest
	sealTestTurnExecution(execution)
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, _, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	raw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		t.Fatalf("EncodeRunnerInput() error = %v", err)
	}
	if bytes.Contains(raw, secretValue) {
		t.Fatal("runner input contains a runtime Secret value")
	}
	instance := decodeJSONInstance(t, raw)
	if err := compiled.Validate(instance); err != nil {
		t.Fatalf("runner input with an exact Secret descriptor does not match v6 schema: %v", err)
	}

	object := instance.(map[string]any)
	secret := object["secret_projections"].([]any)[0].(map[string]any)
	secret["value"] = string(secretValue)
	if err := compiled.Validate(object); err == nil {
		t.Fatal("v6 schema accepted a Secret value inside runner input")
	}
	delete(secret, "value")
	object["unexpected"] = true
	if err := compiled.Validate(object); err == nil {
		t.Fatal("v6 schema accepted an unknown top-level field")
	}
}

func TestRunnerInputSchemaV6RejectsDetachedAttachmentLineage(t *testing.T) {
	compiled, _, _ := loadRunnerInputSchema(t)
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, _, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	raw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		t.Fatalf("EncodeRunnerInput() error = %v", err)
	}

	tests := map[string]func(map[string]any){
		"missing turn lineage": func(object map[string]any) {
			delete(object["attachment_sets"].([]any)[0].(map[string]any), "turn_ref")
		},
		"invalid set provenance": func(object map[string]any) {
			object["attachment_sets"].([]any)[0].(map[string]any)["provenance"] = "SESSION_HISTORY"
		},
		"missing artifact set": func(object map[string]any) {
			delete(object["input_artifacts"].([]any)[0].(map[string]any), "attachment_set_ref")
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			object := decodeJSONInstance(t, raw).(map[string]any)
			mutate(object)
			if err := compiled.Validate(object); err == nil {
				t.Fatal("v6 schema accepted detached attachment provenance")
			}
		})
	}
}

func loadRunnerInputSchema(t *testing.T) (*jsonschema.Resolved, map[string]json.RawMessage, []string) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "contracts", "runtime-controller", "v6", "agent-runner-input.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var compiled jsonschema.Schema
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&compiled); err != nil {
		t.Fatalf("runner input schema is unsupported: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatal("runner input schema contains trailing data")
	}
	resolved, err := compiled.Resolve(nil)
	if err != nil {
		t.Fatalf("runner input schema cannot be resolved: %v", err)
	}
	var shape struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatal(err)
	}
	return resolved, shape.Properties, shape.Required
}

func assertRunnerInputShape(t *testing.T, properties map[string]json.RawMessage, required []string) {
	t.Helper()
	typeOfInput := reflect.TypeOf(runtimecontract.RunnerInput{})
	wantProperties := make([]string, 0, typeOfInput.NumField())
	wantRequired := make([]string, 0, typeOfInput.NumField())
	for index := 0; index < typeOfInput.NumField(); index++ {
		tag := typeOfInput.Field(index).Tag.Get("json")
		parts := strings.Split(tag, ",")
		wantProperties = append(wantProperties, parts[0])
		if len(parts) == 1 || parts[1] != "omitempty" {
			wantRequired = append(wantRequired, parts[0])
		}
	}
	gotProperties := make([]string, 0, len(properties))
	for name := range properties {
		gotProperties = append(gotProperties, name)
	}
	sort.Strings(gotProperties)
	sort.Strings(wantProperties)
	sort.Strings(required)
	sort.Strings(wantRequired)
	if !reflect.DeepEqual(gotProperties, wantProperties) {
		t.Fatalf("schema properties do not match RunnerInput JSON fields:\n got %v\nwant %v", gotProperties, wantProperties)
	}
	if !reflect.DeepEqual(required, wantRequired) {
		t.Fatalf("schema required fields do not match RunnerInput JSON tags:\n got %v\nwant %v", required, wantRequired)
	}
}

func validateRunnerInputSchema(t *testing.T, schema *jsonschema.Resolved, input runtimecontract.RunnerInput) {
	t.Helper()
	raw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		t.Fatalf("EncodeRunnerInput() error = %v", err)
	}
	if err := schema.Validate(decodeJSONInstance(t, raw)); err != nil {
		t.Fatalf("actual %s runner input does not match v6 schema: %v", input.Mode, err)
	}
}

func decodeJSONInstance(t *testing.T, raw []byte) any {
	t.Helper()
	var instance any
	if err := json.Unmarshal(raw, &instance); err != nil {
		t.Fatal(err)
	}
	return instance
}
