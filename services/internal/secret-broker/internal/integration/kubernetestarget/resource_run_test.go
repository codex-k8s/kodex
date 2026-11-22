package kubernetestarget

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// resourceRunFixture supplies a create-only API and a collection intent without fabricated Pod identity.
// resourceRunFixture задаёт API только с create и collection intent без выдуманной identity Pod.
func resourceRunFixture(t *testing.T, options string) (ResourceAddress, managedkubernetes.OperationInput, *resourceWireFixture) {
	t.Helper()
	address := ResourceAddress{Version: "v1", Resource: "pods", Namespace: "example", Namespaced: true}
	input, _, err := managedkubernetes.DecodeOperationInput("kubernetes.run", []byte(`{"group":"","version":"v1","resource":"pods","namespace":"example","options":`+options+`}`))
	if err != nil {
		t.Fatal(err)
	}
	state := &resourceWireFixture{commandAddress: &address, commandObject: map[string]any{"apiVersion": "v1", "kind": "Pod"}, createOnly: true,
		response: `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"worker","namespace":"example","uid":"11111111-1111-4111-8111-111111111111","resourceVersion":"11"},"spec":{"containers":[{"name":"worker","image":"example.invalid/worker:fixture"}]}}`}
	return address, input, state
}

// TestResourceRunUsesOneNativeCollectionCreate verifies actual TLS requests, ordered arguments, defaults, explicit options and create-only RBAC.
// TestResourceRunUsesOneNativeCollectionCreate проверяет реальные TLS-запросы, порядок аргументов, defaults, явные параметры и RBAC только с create.
func TestResourceRunUsesOneNativeCollectionCreate(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "defaults", true: "explicit"}[full], func(t *testing.T) {
			options := `{"podName":"worker","image":"example.invalid/worker:fixture"}`
			if full {
				options = `{"podName":"worker","image":"example.invalid/worker:fixture","command":["/app/worker","second","first"],"args":["--mode","synthetic"],"environment":{"Z_LAST":"z","A_FIRST":"a"},"restartPolicy":"Never","port":8080,"labels":{"app":"synthetic"}}`
			}
			address, input, state := resourceRunFixture(t, options)
			state.wire = func(t *testing.T, request *http.Request, body []byte) {
				if request.Method != http.MethodPost || request.URL.Path != "/api/v1/namespaces/example/pods" || request.Header.Get("Content-Type") != "application/json" || request.URL.Query().Get("fieldValidation") != "Strict" {
					t.Error("run did not use the exact native collection create")
				}
				var object map[string]any
				if json.Unmarshal(body, &object) != nil {
					t.Fatal("invalid native Pod body")
				}
				meta, _ := object["metadata"].(map[string]any)
				spec, _ := object["spec"].(map[string]any)
				if object["kind"] != "Pod" || object["apiVersion"] != "v1" || meta["name"] != "worker" || meta["namespace"] != "example" || meta["uid"] != nil || meta["resourceVersion"] != nil || spec["automountServiceAccountToken"] != false {
					t.Fatal("native Pod lost the selected identity or gained credentials")
				}
				containers, _ := spec["containers"].([]any)
				if len(containers) != 1 {
					t.Fatal("unexpected container set")
				}
				container := containers[0].(map[string]any)
				if container["name"] != "worker" || container["image"] != "example.invalid/worker:fixture" {
					t.Error("unexpected container identity")
				}
				if full {
					if !reflect.DeepEqual(container["command"], []any{"/app/worker", "second", "first"}) || !reflect.DeepEqual(container["args"], []any{"--mode", "synthetic"}) {
						t.Error("command or argument order changed")
					}
					if !reflect.DeepEqual(container["env"], []any{map[string]any{"name": "A_FIRST", "value": "a"}, map[string]any{"name": "Z_LAST", "value": "z"}}) || !reflect.DeepEqual(container["ports"], []any{map[string]any{"containerPort": float64(8080), "protocol": "TCP"}}) || spec["restartPolicy"] != "Never" || !reflect.DeepEqual(meta["labels"], map[string]any{"app": "synthetic"}) {
						t.Error("explicit run options were not materialized")
					}
				} else if spec["restartPolicy"] != "Always" || !reflect.DeepEqual(meta["labels"], map[string]any{"run": "worker"}) || len(container) != 2 {
					t.Error("run defaults changed or implicit options appeared")
				}
			}
			credential := resourceTLSFixture(t, state)
			prepared, err := credential.PrepareResourceOperation(t.Context(), "kubernetes.run", input, address, subjectPin(t), nil)
			if err != nil {
				t.Fatal("create-only run preparation failed", err)
			}
			defer prepared.Close()
			result, err := prepared.Execute(t.Context(), time.Now().Add(2*time.Second))
			if err != nil || result.Outcome != "SUCCEEDED" || state.effects.Load() != 1 || state.reads.Load() != 0 {
				t.Fatal("run failed or read an existing Pod", err, result.Outcome, state.effects.Load(), state.reads.Load())
			}
			var receipt map[string]any
			if json.Unmarshal(result.SafeJSON, &receipt) != nil || len(receipt) != 3 || receipt["spec"] != nil || result.ObservedUID != "11111111-1111-4111-8111-111111111111" || result.ResourceVersion != "11" {
				t.Fatal("run returned an unbound receipt or exposed the native Pod body")
			}
			_, _ = prepared.Execute(t.Context(), time.Now().Add(time.Second))
			if state.effects.Load() != 1 {
				t.Fatal("run replayed")
			}
		})
	}
}

// TestResourceRunKeepsFailedAndUnknownCreatesNonreplayable checks fresh revocation, schema drift and lost or misbound native acknowledgements.
// TestResourceRunKeepsFailedAndUnknownCreatesNonreplayable проверяет свежий отзыв, schema drift и потерянные либо чужие native ACK без повторения create.
func TestResourceRunKeepsFailedAndUnknownCreatesNonreplayable(t *testing.T) {
	for _, mode := range []string{"revoke", "schema", "conflict", "unknown", "foreign-ack"} {
		t.Run(mode, func(t *testing.T) {
			address, input, state := resourceRunFixture(t, `{"podName":"worker","image":"example.invalid/worker:fixture"}`)
			credential := resourceTLSFixture(t, state)
			prepared, err := credential.PrepareResourceOperation(t.Context(), "kubernetes.run", input, address, subjectPin(t), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			count := int64(0)
			switch mode {
			case "revoke":
				state.deny.Store(true)
			case "schema":
				state.changeSchema.Store(true)
			case "conflict":
				state.status = 409
				count = 1
			case "unknown":
				state.status = 500
				count = 1
			case "foreign-ack":
				state.response = strings.ReplaceAll(state.response, `"name":"worker"`, `"name":"foreign"`)
				count = 1
			}
			result, _ := prepared.Execute(t.Context(), time.Now().Add(2*time.Second))
			if result.Outcome == "SUCCEEDED" || (mode == "unknown" || mode == "foreign-ack") && result.Outcome != "UNKNOWN" || state.effects.Load() != count {
				t.Fatal("failed or uncertain run became success", result.Outcome, state.effects.Load())
			}
			_, _ = prepared.Execute(t.Context(), time.Now().Add(time.Second))
			if state.effects.Load() != count || state.reads.Load() != 0 {
				t.Fatal("run replayed or read an existing Pod")
			}
		})
	}
}

// TestResourceRunRejectsMalformedOrForeignIntent proves denied input never reaches a native mutation.
// TestResourceRunRejectsMalformedOrForeignIntent доказывает отсутствие native mutation для запрещённого input.
func TestResourceRunRejectsMalformedOrForeignIntent(t *testing.T) {
	for _, mode := range []string{"name", "image", "environment", "command", "port", "labels", "restart", "protected", "foreign-resource"} {
		t.Run(mode, func(t *testing.T) {
			address, input, state := resourceRunFixture(t, `{"podName":"worker","image":"example.invalid/worker:fixture"}`)
			options := map[string]any{"podName": "worker", "image": "example.invalid/worker:fixture"}
			switch mode {
			case "name":
				options["podName"] = "../escape"
			case "image":
				options["image"] = " image "
			case "environment":
				options["environment"] = map[string]any{"INVALID=NAME": "synthetic"}
			case "command":
				options["command"] = []any{"invalid\x00command"}
			case "port":
				options["port"] = 65536
			case "labels":
				options["labels"] = map[string]any{"invalid/key/name": "x"}
			case "restart":
				options["restartPolicy"] = "Sometimes"
			case "protected":
				address.Namespace = "kodex-system"
				input.Namespace = address.Namespace
			case "foreign-resource":
				address.Resource = "configmaps"
				input.Resource = address.Resource
			}
			input.Options, _ = json.Marshal(options)
			state.commandAddress = &address
			credential := resourceTLSFixture(t, state)
			prepared, err := credential.PrepareResourceOperation(t.Context(), "kubernetes.run", input, address, subjectPin(t), []string{"kodex-system"})
			if prepared != nil {
				prepared.Close()
			}
			if err == nil || state.effects.Load() != 0 {
				t.Fatal("invalid run reached native execution")
			}
		})
	}
}
