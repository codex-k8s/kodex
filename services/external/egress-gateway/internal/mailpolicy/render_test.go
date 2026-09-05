package mailpolicy

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestProjectionSchemaAndExactCNIArtifacts(t *testing.T) {
	doc := fixtureDocument(t)
	files, err := RenderFiles(doc)
	if err != nil {
		t.Fatal(err)
	}
	var cm struct {
		Metadata  struct{ Name string }
		Immutable bool
		Data      map[string]string
	}
	if err := json.Unmarshal(files["mail-configmap.json"], &cm); err != nil {
		t.Fatal(err)
	}
	if !cm.Immutable || cm.Metadata.Name != "egress-gateway-mail-"+doc.Digest()[:24] {
		t.Fatal("immutable policy name mismatch")
	}
	active, err := LoadMail([]byte(cm.Data["mail-policy.json"]), doc.Digest(), fixtureBase(t))
	if err != nil || !active.Configured() {
		t.Fatal("rendered policy not executable", err)
	}
	var np struct {
		Spec struct {
			Egress []struct {
				To    []struct{ IPBlock struct{ CIDR string } }
				Ports []struct {
					Protocol string
					Port     int
				}
			}
		}
	}
	if err := json.Unmarshal(files["mail-networkpolicy.json"], &np); err != nil {
		t.Fatal(err)
	}
	if len(np.Spec.Egress) != 1 || len(np.Spec.Egress[0].To) != 1 || np.Spec.Egress[0].To[0].IPBlock.CIDR != "8.8.8.8/32" || len(np.Spec.Egress[0].Ports) != 1 || np.Spec.Egress[0].Ports[0].Port != 587 || np.Spec.Egress[0].Ports[0].Protocol != "TCP" {
		t.Fatal("CNI pins do not match runtime")
	}
	raw, err := os.ReadFile("../../../../../contracts/egress/v1/egress-mail.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	var instance map[string]any
	if err := json.Unmarshal([]byte(cm.Data["mail-policy.json"]), &instance); err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(instance); err != nil {
		t.Fatal(err)
	}
	instance["destinations"].([]any)[0].(map[string]any)["port"] = float64(465)
	if err := resolved.Validate(instance); err == nil {
		t.Fatal("schema accepted STARTTLS on implicit port")
	}
	doc.ConfigurationRevision++
	next, err := RenderFiles(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(next["mail-configmap.json"]) == string(files["mail-configmap.json"]) || string(next["mail-deployment-patch.json"]) == string(files["mail-deployment-patch.json"]) {
		t.Fatal("source revision does not force new immutable mount")
	}
}

func TestBootstrapProjectionIsReproducible(t *testing.T) {
	var cm struct{ Data map[string]string }
	raw, err := os.ReadFile("../../../../../deploy/k8s/base/email-bridge/configuration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cm); err != nil {
		t.Fatal(err)
	}
	r := &fixtureResolver{}
	doc, err := Produce(t.Context(), []byte(cm.Data["mailboxes.yaml"]), fixtureBase(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if r.calls != 0 {
		t.Fatal("empty bootstrap performed DNS work")
	}
	files, err := RenderFiles(doc)
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range files {
		actual, err := os.ReadFile("../../../../../deploy/k8s/base/egress-gateway/mail/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != string(expected) {
			t.Fatalf("generated bootstrap artifact drift: %s", name)
		}
	}
}
