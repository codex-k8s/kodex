package kubernetestarget

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
)

// resourceCommandFixture describes synthetic command intent and independently expected business fields.
// resourceCommandFixture описывает synthetic command intent и независимо ожидаемые business fields.
type resourceCommandFixture struct {
	operation, group, resource, kind, options string
	check                                     func(*testing.T, map[string]any)
}

// resourceCommandCases covers each newly executable command rather than inferring coverage from the registry.
// resourceCommandCases покрывает каждую новую executable command, не выводя coverage из registry.
func resourceCommandCases() []resourceCommandFixture {
	return []resourceCommandFixture{
		{"kubernetes.set.image", "apps", "deployments", "Deployment", `{"assignments":{"app":"example.invalid/app:new"}}`, func(t *testing.T, o map[string]any) {
			if commandContainer(o)["image"] != "example.invalid/app:new" {
				t.Fatal("image was not changed")
			}
		}},
		{"kubernetes.set.env", "apps", "deployments", "Deployment", `{"assignments":{"MODE":"new","Z":"last","A":"first"},"removals":["REMOVE"],"containerNames":["app"]}`, func(t *testing.T, o map[string]any) {
			raw, _ := json.Marshal(commandContainer(o)["env"])
			if string(raw) != `[{"name":"MODE","value":"new"},{"name":"KEEP","value":"keep"},{"name":"A","value":"first"},{"name":"Z","value":"last"}]` {
				t.Fatal("environment order or replacement changed")
			}
		}},
		{"kubernetes.set.resources", "apps", "deployments", "Deployment", `{"requests":{"cpu":"100m"},"limits":{"memory":"128Mi"}}`, func(t *testing.T, o map[string]any) {
			r := commandContainer(o)["resources"].(map[string]any)
			if r["requests"].(map[string]any)["cpu"] != "100m" || r["requests"].(map[string]any)["memory"] != "64Mi" || r["limits"].(map[string]any)["memory"] != "128Mi" {
				t.Fatal("resource quantities changed unrelated keys")
			}
		}},
		{"kubernetes.set.serviceaccount", "apps", "deployments", "Deployment", `{"serviceAccount":"default"}`, func(t *testing.T, o map[string]any) {
			if commandPodSpec(o)["serviceAccountName"] != "default" {
				t.Fatal("service account not updated")
			}
		}},
		{"kubernetes.rollout.pause", "apps", "deployments", "Deployment", `{}`, func(t *testing.T, o map[string]any) {
			if o["spec"].(map[string]any)["paused"] != true {
				t.Fatal("pause lost")
			}
		}},
		{"kubernetes.rollout.resume", "apps", "deployments", "Deployment", `{}`, func(t *testing.T, o map[string]any) {
			if o["spec"].(map[string]any)["paused"] != false {
				t.Fatal("resume lost")
			}
		}},
		{"kubernetes.set.selector", "", "services", "Service", `{"selectorAssignments":{"app":"new"}}`, func(t *testing.T, o map[string]any) {
			s := o["spec"].(map[string]any)["selector"].(map[string]any)
			if len(s) != 1 || s["app"] != "new" {
				t.Fatal("selector did not replace existing mapping")
			}
		}},
		{"kubernetes.set.subject", "rbac.authorization.k8s.io", "rolebindings", "RoleBinding", `{"subjects":[{"kind":"User","name":"synthetic-new","apiGroup":"rbac.authorization.k8s.io"}]}`, func(t *testing.T, o map[string]any) {
			s := o["subjects"].([]any)
			if len(s) != 2 || s[0].(map[string]any)["name"] != "synthetic-existing" || s[1].(map[string]any)["name"] != "synthetic-new" || o["roleRef"].(map[string]any)["name"] != "synthetic-role" {
				t.Fatal("subjects or roleRef changed incorrectly")
			}
		}},
		{"kubernetes.cordon", "", "nodes", "Node", `{}`, func(t *testing.T, o map[string]any) {
			if o["spec"].(map[string]any)["unschedulable"] != true {
				t.Fatal("cordon lost")
			}
		}},
		{"kubernetes.uncordon", "", "nodes", "Node", `{}`, func(t *testing.T, o map[string]any) {
			if o["spec"].(map[string]any)["unschedulable"] != false {
				t.Fatal("uncordon lost")
			}
		}},
		{"kubernetes.taint", "", "nodes", "Node", `{"taints":[{"key":"workload","value":"new","effect":"NoSchedule"}],"removals":["obsolete"],"overwrite":true}`, func(t *testing.T, o map[string]any) {
			s := o["spec"].(map[string]any)["taints"].([]any)
			if len(s) != 1 || s[0].(map[string]any)["value"] != "new" || s[0].(map[string]any)["timeAdded"] != "2026-01-01T00:00:00Z" {
				t.Fatal("taint value/order/time changed incorrectly")
			}
		}},
	}
}

// commandPodSpec addresses the known synthetic Deployment fixture without recursive field matching.
// commandPodSpec адресует known synthetic Deployment fixture без recursive field matching.
func commandPodSpec(object map[string]any) map[string]any {
	return object["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
}

// commandContainer selects the fixture's existing first regular container.
// commandContainer выбирает existing first regular container fixture.
func commandContainer(object map[string]any) map[string]any {
	return commandPodSpec(object)["containers"].([]any)[0].(map[string]any)
}

// commandObject constructs ordinary synthetic targets and preserves fields outside the selected command.
// commandObject создаёт ordinary synthetic targets с полями вне selected command.
func commandObject(t *testing.T, test resourceCommandFixture) (ResourceAddress, map[string]any, managedkubernetes.OperationInput) {
	t.Helper()
	address, object, _ := resourcePolicyFixture(t)
	address.Group, address.Resource = test.group, test.resource
	address.UID, address.ResourceVersion = "11111111-1111-4111-8111-111111111111", "10"
	address.Namespaced = test.kind != "Node"
	if !address.Namespaced {
		address.Namespace = ""
	}
	version := "v1"
	if test.group != "" {
		version = test.group + "/v1"
	}
	object["apiVersion"], object["kind"] = version, test.kind
	metadata := object["metadata"].(map[string]any)
	metadata["uid"], metadata["resourceVersion"] = address.UID, address.ResourceVersion
	metadata["annotations"] = map[string]any{"keep": "synthetic"}
	if !address.Namespaced {
		delete(metadata, "namespace")
	}
	switch test.kind {
	case "Deployment":
		container := commandContainer(object)
		container["image"] = "example.invalid/app:old"
		container["env"] = []any{map[string]any{"name": "MODE", "value": "old"}, map[string]any{"name": "REMOVE", "value": "remove"}, map[string]any{"name": "KEEP", "value": "keep"}}
		container["resources"] = map[string]any{"requests": map[string]any{"memory": "64Mi"}}
		if test.operation == "kubernetes.rollout.resume" {
			object["spec"].(map[string]any)["paused"] = true
		}
	case "Service":
		object["spec"] = map[string]any{"selector": map[string]any{"old": "yes"}}
	case "Node":
		object["spec"] = map[string]any{"unschedulable": test.operation == "kubernetes.uncordon", "taints": []any{map[string]any{"key": "workload", "value": "old", "effect": "NoSchedule", "timeAdded": "2026-01-01T00:00:00Z"}, map[string]any{"key": "obsolete", "effect": "NoExecute"}}}
	case "RoleBinding":
		delete(object, "spec")
		object["subjects"] = []any{map[string]any{"kind": "User", "name": "synthetic-existing", "apiGroup": "rbac.authorization.k8s.io"}}
		object["roleRef"] = map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "synthetic-role"}
	}
	input := managedkubernetes.OperationInput{Group: address.Group, Version: address.Version, Resource: address.Resource, Namespace: address.Namespace, Name: address.Name, UID: address.UID, ResourceVersion: address.ResourceVersion, Options: json.RawMessage(test.options)}
	if test.options == `{}` {
		input.Options = nil
	}
	return address, object, input
}

// TestResourceCommandsUseNativeConditionalPatch exercises every command through actual discovery/RBAC/TLS and one native PATCH.
// TestResourceCommandsUseNativeConditionalPatch проводит каждую command через actual discovery/RBAC/TLS и один native PATCH.
func TestResourceCommandsUseNativeConditionalPatch(t *testing.T) {
	for _, test := range resourceCommandCases() {
		t.Run(test.operation, func(t *testing.T) {
			address, object, input := commandObject(t, test)
			before, _ := json.Marshal(object)
			state := &resourceWireFixture{commandAddress: &address, commandObject: object}
			state.wire = func(t *testing.T, request *http.Request, body []byte) {
				if request.Method != "PATCH" || request.Header.Get("Content-Type") != "application/merge-patch+json" || request.URL.Query().Get("fieldValidation") != "Strict" {
					t.Error("wrong native effect")
				}
				var patch map[string]any
				if json.Unmarshal(body, &patch) != nil {
					t.Error("malformed patch")
					return
				}
				meta, _ := patch["metadata"].(map[string]any)
				if meta["uid"] != address.UID || meta["resourceVersion"] != "10" {
					t.Error("patch lost conditional identity")
				}
				after, err := jsonpatch.MergePatch(before, body)
				if err != nil {
					t.Error("patch cannot apply")
					return
				}
				var got map[string]any
				_ = json.Unmarshal(after, &got)
				test.check(t, got)
				metadata := got["metadata"].(map[string]any)
				if metadata["annotations"].(map[string]any)["keep"] != "synthetic" {
					t.Error("unrelated metadata removed")
				}
				metadata["resourceVersion"] = "11"
				response, _ := json.Marshal(got)
				state.response = string(response)
			}
			credential := resourceTLSFixture(t, state)
			prepared, err := credential.PrepareResourceOperation(t.Context(), test.operation, input, address, subjectPin(t), nil)
			if err != nil {
				t.Fatal("valid command preparation failed", err)
			}
			defer prepared.Close()
			result, err := prepared.Execute(t.Context(), time.Now().Add(2*time.Second))
			if err != nil || result.Outcome != "SUCCEEDED" || state.effects.Load() != 1 {
				t.Fatal("native command did not complete once", err, result.Outcome)
			}
			if _, err := prepared.Execute(t.Context(), time.Now().Add(time.Second)); err == nil || state.effects.Load() != 1 {
				t.Fatal("native command replayed")
			}
		})
	}
}

// TestResourceCommandsPreserveUnchangedIntegers checks the actual wire patch for regular and init containers without decoding numbers as float64.
// TestResourceCommandsPreserveUnchangedIntegers проверяет сохранность целых чисел обычного и init-контейнера в фактическом HTTP patch без преобразования в float64.
func TestResourceCommandsPreserveUnchangedIntegers(t *testing.T) {
	for _, test := range resourceCommandCases()[:3] {
		t.Run(test.operation, func(t *testing.T) {
			address, object, input := commandObject(t, test)
			commandContainer(object)["securityContext"] = map[string]any{"runAsUser": int64(9007199254740993)}
			commandPodSpec(object)["initContainers"] = []any{map[string]any{"name": "init", "image": "example.invalid/init:stable", "securityContext": map[string]any{"runAsUser": int64(9007199254740995)}}}
			before, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			state := &resourceWireFixture{commandAddress: &address, commandObject: object}
			state.wire = func(t *testing.T, _ *http.Request, body []byte) {
				after, err := jsonpatch.MergePatch(before, body)
				if err != nil {
					t.Error(err)
					return
				}
				got, err := decodeResourceJSON(after, resourceResponseBytes, 64)
				if err != nil {
					t.Error(err)
					return
				}
				for field, want := range map[string]int64{"containers": 9007199254740993, "initContainers": 9007199254740995} {
					container := commandPodSpec(got)[field].([]any)[0].(map[string]any)
					if value := container["securityContext"].(map[string]any)["runAsUser"]; value != want {
						t.Errorf("command changed unrelated integer in %s: got %v, want %d", field, value, want)
					}
				}
				got["metadata"].(map[string]any)["resourceVersion"] = "11"
				response, _ := json.Marshal(got)
				state.response = string(response)
			}
			credential := resourceTLSFixture(t, state)
			prepared, err := credential.PrepareResourceOperation(t.Context(), test.operation, input, address, subjectPin(t), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			result, err := prepared.Execute(t.Context(), time.Now().Add(2*time.Second))
			if err != nil || result.Outcome != "SUCCEEDED" || state.effects.Load() != 1 {
				t.Fatal("integer-preserving command did not complete once", err, result.Outcome)
			}
		})
	}
}

// TestResourceCommandsKeepFreshDenialsAndUnknownOutcomes checks revocation, schema drift, explicit conflict and uncertain write without retries.
// TestResourceCommandsKeepFreshDenialsAndUnknownOutcomes проверяет revoke, schema drift, conflict и uncertain write без retries.
func TestResourceCommandsKeepFreshDenialsAndUnknownOutcomes(t *testing.T) {
	for _, mode := range []string{"revoke", "schema", "conflict", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			test := resourceCommandCases()[0]
			address, object, input := commandObject(t, test)
			state := &resourceWireFixture{commandAddress: &address, commandObject: object}
			credential := resourceTLSFixture(t, state)
			prepared, err := credential.PrepareResourceOperation(t.Context(), test.operation, input, address, subjectPin(t), nil)
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
			}
			result, _ := prepared.Execute(t.Context(), time.Now().Add(2*time.Second))
			if result.Outcome == "SUCCEEDED" || mode == "unknown" && result.Outcome != "UNKNOWN" || state.effects.Load() != count {
				t.Fatal("denial or uncertainty became success/replay")
			}
			_, _ = prepared.Execute(t.Context(), time.Now().Add(time.Second))
			if state.effects.Load() != count {
				t.Fatal("failed command replayed")
			}
		})
	}
}

// TestResourceCommandsRejectAmbiguousIntent proves malformed or escalating intent never reaches a native effect.
// TestResourceCommandsRejectAmbiguousIntent доказывает, что malformed либо escalating intent не достигает native effect.
func TestResourceCommandsRejectAmbiguousIntent(t *testing.T) {
	for _, test := range []struct {
		name, operation, options string
		fixture                  int
	}{
		{"unknown container", "kubernetes.set.image", `{"assignments":{"missing":"example.invalid/app:new"}}`, 0},
		{"mixed wildcard", "kubernetes.set.image", `{"assignments":{"*":"example.invalid/app:new","app":"example.invalid/app:other"}}`, 0},
		{"blank image", "kubernetes.set.image", `{"assignments":{"app":" "}}`, 0},
		{"foreign kind", "kubernetes.set.selector", `{"selectorAssignments":{"app":"new"}}`, 0},
		{"invalid label", "kubernetes.set.selector", `{"selectorAssignments":{"bad/key/extra":"new"}}`, 6},
		{"unknown env container", "kubernetes.set.env", `{"assignments":{"A":"new"},"containerNames":["missing"]}`, 0},
		{"env conflict", "kubernetes.set.env", `{"assignments":{"A":"new"},"removals":["A"]}`, 0},
		{"invalid env", "kubernetes.set.env", `{"assignments":{"A=B":"new"}}`, 0},
		{"negative resource", "kubernetes.set.resources", `{"requests":{"cpu":"-1"}}`, 0},
		{"invalid quantity", "kubernetes.set.resources", `{"limits":{"memory":"nonsense"}}`, 0},
		{"credential account", "kubernetes.set.serviceaccount", `{"serviceAccount":"privileged-controller"}`, 0},
		{"subject namespace", "kubernetes.set.subject", `{"subjects":[{"kind":"User","name":"synthetic","namespace":"foreign"}]}`, 7},
		{"subject service namespace", "kubernetes.set.subject", `{"subjects":[{"kind":"ServiceAccount","name":"synthetic"}]}`, 7},
		{"taint overwrite", "kubernetes.taint", `{"taints":[{"key":"workload","value":"new","effect":"NoSchedule"}]}`, 10},
		{"taint conflict", "kubernetes.taint", `{"taints":[{"key":"workload","value":"new","effect":"NoSchedule"}],"removals":["workload"],"overwrite":true}`, 10},
		{"taint missing removal", "kubernetes.taint", `{"removals":["missing:NoSchedule"]}`, 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := resourceCommandCases()[test.fixture]
			base.operation, base.options = test.operation, test.options
			address, object, input := commandObject(t, base)
			before, _ := json.Marshal(object)
			state := &resourceWireFixture{commandAddress: &address, commandObject: object}
			credential := resourceTLSFixture(t, state)
			prepared, err := credential.PrepareResourceOperation(t.Context(), base.operation, input, address, subjectPin(t), nil)
			if prepared != nil {
				prepared.Close()
			}
			if err == nil || state.effects.Load() != 0 {
				t.Fatal("invalid command reached effect")
			}
			after, _ := json.Marshal(object)
			if string(before) != string(after) {
				t.Fatal("failed reconstruction changed original object")
			}
		})
	}
}

// TestResourceSubjectAdditionIsIdempotentForOmittedEmptyGroup keeps an existing ServiceAccount subject without fabricating a duplicate.
// TestResourceSubjectAdditionIsIdempotentForOmittedEmptyGroup сохраняет existing ServiceAccount subject без duplicate из-за omitted empty group.
func TestResourceSubjectAdditionIsIdempotentForOmittedEmptyGroup(t *testing.T) {
	object := map[string]any{"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "synthetic", "namespace": "example"}}}
	options := map[string]any{"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "synthetic", "namespace": "example"}}}
	if addResourceSubjects(object, options) != nil || len(object["subjects"].([]any)) != 1 {
		t.Fatal("subject was duplicated")
	}
}
