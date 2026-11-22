package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// gatePreviewFixture obtains exact persisted canonical input through the executable parser.
// gatePreviewFixture получает exact persisted canonical input через executable parser.
func gatePreviewFixture(t *testing.T, operation, raw string) []byte {
	t.Helper()
	_, canonical, err := DecodeOperationInput(operation, []byte(raw))
	if err != nil {
		t.Fatal("invalid fixture", err)
	}
	return canonical
}

// TestGatePreviewSubmittedBuiltinValues proves that scale-to-zero, images and limits are visible.
// Unknown siblings stay hidden rather than becoming an arbitrary body dump.
// TestGatePreviewSubmittedBuiltinValues проверяет видимость scale-to-zero, images и limits.
// Unknown siblings скрыты и не становятся arbitrary body dump.
func TestGatePreviewSubmittedBuiltinValues(t *testing.T) {
	raw := gatePreviewFixture(t, "kubernetes.create", `{"group":"apps","version":"v1","resource":"deployments","namespace":"example","body":{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"example","annotations":{"note":"private-note"}},"spec":{"replicas":0,"selector":{"matchLabels":{"app":"web"}},"template":{"metadata":{"labels":{"app":"web"}},"spec":{"containers":[{"name":"web","image":"example/web:v2","resources":{"limits":{"cpu":"500m","memory":"256Mi"}},"env":[{"name":"UNCLASSIFIED","value":"private-environment"}]}]}}}}}`)
	preview, err := BuildGateEffectPreview("kubernetes.create", raw, true, GatePreviewDetailBytes)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, change := range preview.Changes {
		if !change.Redacted {
			values[change.Path] = change.ValueJSON
		}
	}
	for path, want := range map[string]string{"/spec/replicas": "0", "/spec/selector/matchLabels/app": `"web"`, "/spec/template/spec/containers/0/image": `"example/web:v2"`, "/spec/template/spec/containers/0/resources/limits/memory": `"256Mi"`} {
		if values[path] != want {
			t.Fatalf("submitted value absent at %s: %q", path, values[path])
		}
	}
	encoded, _ := json.Marshal(preview)
	if strings.Contains(string(encoded), "private-note") || strings.Contains(string(encoded), "private-environment") || !slices.Contains(preview.Limitations, "SENSITIVE_OR_UNCLASSIFIED_FIELDS_HIDDEN") {
		t.Fatal("hidden content escaped or lost limitation")
	}
	hash := sha256.Sum256(raw)
	if preview.InputDigest != hex.EncodeToString(hash[:]) {
		t.Fatal("digest changed")
	}
}

// TestGatePreviewPatchAndRBACValues preserves native patch operations and submitted permission rules.
// TestGatePreviewPatchAndRBACValues сохраняет native patch operations и submitted permission rules.
func TestGatePreviewPatchAndRBACValues(t *testing.T) {
	raw := gatePreviewFixture(t, "kubernetes.patch", `{"group":"rbac.authorization.k8s.io","version":"v1","resource":"roles","namespace":"example","name":"reader","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17","body":[{"op":"replace","path":"/rules/0/verbs","value":["get","delete"]},{"op":"add","path":"/rules/0/resources","value":["pods"]}],"options":{"patchType":"JSON"}}`)
	preview, err := BuildGateEffectPreview("kubernetes.patch", raw, true, GatePreviewDetailBytes)
	if err != nil || len(preview.Changes) != 2 || preview.Changes[0].Operation != "replace" || preview.Changes[0].ValueJSON != `["get","delete"]` || preview.Changes[1].ValueJSON != `["pods"]` {
		t.Fatal("submitted RBAC change hidden", err)
	}
}

// TestGatePreviewOptions shows exact argv and scale options while explicitly hiding scripts.
// TestGatePreviewOptions показывает exact argv и scale options, явно скрывая scripts.
func TestGatePreviewOptions(t *testing.T) {
	coords := `"group":"","version":"v1","resource":"pods","namespace":"example","name":"web","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17"`
	for _, test := range []struct {
		command string
		hidden  bool
	}{{`["ls","-la","/work"]`, false}, {`["sh","-c","echo private-script"]`, true}, {`["tool","--token=private-value"]`, true}} {
		raw := gatePreviewFixture(t, "kubernetes.exec", `{`+coords+`,"options":{"command":`+test.command+`,"stdin":false,"tty":true,"timeoutSeconds":30}}`)
		preview, err := BuildGateEffectPreview("kubernetes.exec", raw, true, GatePreviewDetailBytes)
		if err != nil {
			t.Fatal(err)
		}
		if len(preview.Options) != 4 || preview.Options[0].Key != "command" || preview.Options[0].Redacted != test.hidden {
			t.Fatal("argv disclosure mismatch")
		}
		if !test.hidden && preview.Options[0].ValueJSON != test.command {
			t.Fatal("argv changed")
		}
		if test.hidden && preview.Options[0].ValueJSON != "" {
			t.Fatal("hidden argv leaked")
		}
	}
	raw := gatePreviewFixture(t, "kubernetes.scale", `{"group":"apps","version":"v1","resource":"deployments","namespace":"example","name":"web","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17","subresource":"scale","options":{"replicas":0,"currentReplicas":3}}`)
	preview, err := BuildGateEffectPreview("kubernetes.scale", raw, true, GatePreviewDetailBytes)
	if err != nil || len(preview.Options) != 2 || preview.Options[1].Key != "replicas" || preview.Options[1].ValueJSON != "0" {
		t.Fatal("scale value hidden", err)
	}
}

// TestGatePreviewHiddenShapes exercises proof-based boundaries with non-credential-looking sentinels.
// A custom body, configuration data or forged builtin field never becomes a public value.
// TestGatePreviewHiddenShapes проверяет proof-based boundary с sentinel без credential markers.
// Custom body, configuration data и forged builtin field не становятся public value.
func TestGatePreviewHiddenShapes(t *testing.T) {
	for _, test := range []struct{ group, resource, body string }{
		{"example.org", "widgets", `{"spec":{"replicas":0,"nested":{"value":"opaque-sentinel"}}}`},
		{"", "configmaps", `{"data":{"setting":"opaque-sentinel"}}`},
		{"apps", "deployments", `{"spec":{"replicas":{"nested":"opaque-sentinel"},"unknown":{"metadata":{"labels":{"a":"opaque-sentinel"}}}}}`},
	} {
		raw := gatePreviewFixture(t, "kubernetes.patch", `{"group":"`+test.group+`","version":"v1","resource":"`+test.resource+`","namespace":"example","name":"web","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17","body":`+test.body+`,"options":{"patchType":"MERGE"}}`)
		preview, err := BuildGateEffectPreview("kubernetes.patch", raw, true, GatePreviewDetailBytes)
		encoded, _ := json.Marshal(preview)
		if err != nil || strings.Contains(string(encoded), "opaque-sentinel") || !slices.Contains(preview.Limitations, "SENSITIVE_OR_UNCLASSIFIED_FIELDS_HIDDEN") {
			t.Fatal("unclassified value leaked", err)
		}
	}
	raw := gatePreviewFixture(t, "kubernetes.patch", `{"group":"","version":"v1","resource":"secrets","namespace":"example","name":"app","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17","body":[],"options":{"patchType":"JSON","secretMaterialRef":"krs_example"}}`)
	preview, err := BuildGateEffectPreview("kubernetes.patch", raw, true, GatePreviewDetailBytes)
	if err != nil || preview.SecretMaterialRef != "krs_example" || !slices.Contains(preview.Limitations, "PROTECTED_MATERIAL_HIDDEN") {
		t.Fatal("protected material boundary absent", err)
	}
}

// TestGatePreviewDisclosureAndBudgets keeps events opaque and list truncation explicit.
// Detailed reads may show larger safe argv without increasing the list budget.
// TestGatePreviewDisclosureAndBudgets сохраняет opaque events и явную list truncation.
// Detailed read показывает больший safe argv без увеличения list budget.
func TestGatePreviewDisclosureAndBudgets(t *testing.T) {
	argv := make([]string, 12)
	for index := range argv {
		argv[index] = strings.Repeat("x", 2000)
	}
	encoded, _ := json.Marshal(argv)
	raw := gatePreviewFixture(t, "kubernetes.exec", `{"group":"","version":"v1","resource":"pods","namespace":"example","name":"web","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17","options":{"command":`+string(encoded)+`,"timeoutSeconds":30}}`)
	opaque, err := BuildGateEffectPreview("kubernetes.exec", raw, false, GatePreviewDetailBytes)
	if err != nil || len(opaque.Options) != 0 || len(opaque.Changes) != 0 || !slices.Contains(opaque.Limitations, "NOT_AUTHORIZED") {
		t.Fatal("event disclosed options", err)
	}
	summary, err := BuildGateEffectPreview("kubernetes.exec", raw, true, GatePreviewListBytes)
	if err != nil || !summary.Options[0].Redacted || !slices.Contains(summary.Limitations, "BUDGET_EXCEEDED") {
		t.Fatal("list not bounded", err)
	}
	detail, err := BuildGateEffectPreview("kubernetes.exec", raw, true, GatePreviewDetailBytes)
	if err != nil || detail.Options[0].Redacted || detail.Options[0].ValueJSON != string(encoded) {
		t.Fatal("detail lost safe argv", err)
	}
	if _, err := BuildGateEffectPreview("kubernetes.exec", append(raw, ' '), true, GatePreviewListBytes); err == nil {
		t.Fatal("noncanonical source accepted")
	}
	summary.Options[0].ValueJSON = `"hidden"`
	if summary.Validate(GatePreviewListBytes) == nil {
		t.Fatal("redacted value accepted")
	}
}

// TestGatePreviewProtectedPatchPlaceholder keeps protected placeholders readable without
// fabricating submitted private changes. The ordinary empty-patch refusal remains unchanged.
// TestGatePreviewProtectedPatchPlaceholder сохраняет читаемость protected placeholders без
// выдуманных private changes. Отклонение ordinary empty patch остаётся неизменным.
func TestGatePreviewProtectedPatchPlaceholder(t *testing.T) {
	for _, patchType := range []string{"JSON", "MERGE", "STRATEGIC"} {
		body := "{}"
		if patchType == "JSON" {
			body = "[]"
		}
		raw := gatePreviewFixture(t, "kubernetes.patch", `{"group":"","version":"v1","resource":"secrets","namespace":"example","name":"app","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17","body":`+body+`,"options":{"patchType":"`+patchType+`","secretMaterialRef":"krs_example"}}`)
		preview, err := BuildGateEffectPreview("kubernetes.patch", raw, true, GatePreviewDetailBytes)
		if err != nil || len(preview.Changes) != 0 || len(preview.Options) != 1 || preview.Options[0].ValueJSON != `"`+patchType+`"` || preview.SecretMaterialRef != "krs_example" || !slices.Contains(preview.Limitations, "PROTECTED_MATERIAL_HIDDEN") {
			t.Fatal("private placeholder lost its disclosure boundary", err)
		}
	}
}

// TestGatePreviewPortForwardDisclosureIsServerDerived binds repeated connection consent to one approved bounded stream and rejects hostile caller policy options.
// TestGatePreviewPortForwardDisclosureIsServerDerived связывает repeated connection consent с approved bounded stream и отклоняет hostile caller policy options.
func TestGatePreviewPortForwardDisclosureIsServerDerived(t *testing.T) {
	raw := `{"group":"","version":"v1","resource":"pods","namespace":"example","name":"web","uid":"30000000-0000-4000-8000-000000000001","resourceVersion":"17","options":{"ports":[{"port":18080,"targetPort":8080}],"timeoutSeconds":30}}`
	canonical := gatePreviewFixture(t, "kubernetes.port-forward", raw)
	preview, err := BuildGateEffectPreview("kubernetes.port-forward", canonical, true, GatePreviewDetailBytes)
	if err != nil || MaximumPortForwardConnections != 16 || !slices.Contains(preview.Limitations, GatePortForwardLogicalStreamDisclosure) || preview.ValidateForOperation("kubernetes.port-forward", GatePreviewDetailBytes) != nil || preview.ValidateForOperation("kubernetes.exec", GatePreviewDetailBytes) == nil {
		t.Fatal("logical stream disclosure was missing or accepted for another operation")
	}
	opaque, err := BuildGateEffectPreview("kubernetes.port-forward", canonical, false, GatePreviewDetailBytes)
	if err != nil || slices.Contains(opaque.Limitations, GatePortForwardLogicalStreamDisclosure) || opaque.ValidateForOperation("kubernetes.port-forward", GatePreviewDetailBytes) != nil {
		t.Fatal("opaque projection disclosed connection semantics")
	}
	preview.Limitations = nil
	if preview.ValidateForOperation("kubernetes.port-forward", GatePreviewDetailBytes) == nil {
		t.Fatal("disclosed PF lost its required approval scope")
	}
	for _, key := range []string{"maximumActiveConnections", "sequentialConnections", "connectionLifecycle", "approvalScope"} {
		var hostileInput map[string]any
		if json.Unmarshal(canonical, &hostileInput) != nil {
			t.Fatal("decode hostile source fixture")
		}
		hostileInput["options"].(map[string]any)[key] = 999
		hostile, _ := json.Marshal(hostileInput)
		if _, _, err := DecodeOperationInput("kubernetes.port-forward", hostile); err == nil {
			t.Fatal("caller policy option reached normalized input")
		}
		if _, err := BuildGateEffectPreview("kubernetes.port-forward", hostile, true, GatePreviewDetailBytes); err == nil {
			t.Fatal("caller policy option supplied approval semantics")
		}
	}
}
