package integrationegresspolicy

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
)

func TestRenderPinsAndImmutableDeploymentSwitch(t *testing.T) {
	document, err := Produce(t.Context(), []string{"api.example.test"}, 1, strings.Repeat("a", 64),
		resolverFixture{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := RenderFiles(document)
	if err != nil {
		t.Fatal(err)
	}
	var cm struct {
		Metadata  struct{ Name string }
		Immutable bool
		Data      map[string]string
	}
	if err := json.Unmarshal(files["integration-configmap.json"], &cm); err != nil {
		t.Fatal(err)
	}
	if !cm.Immutable || cm.Metadata.Name != "egress-gateway-integration-"+document.Digest()[:24] || cm.Data["integration-policy.json"] == "" {
		t.Fatal("immutable projection mismatch")
	}
	var np struct {
		Spec struct {
			Egress []struct {
				To    []struct{ IPBlock struct{ CIDR string } }
				Ports []struct{ Port int }
			}
		}
	}
	if err := json.Unmarshal(files["integration-networkpolicy.json"], &np); err != nil {
		t.Fatal(err)
	}
	if len(np.Spec.Egress) != 1 || len(np.Spec.Egress[0].To) != 1 || np.Spec.Egress[0].To[0].IPBlock.CIDR != "8.8.8.8/32" || np.Spec.Egress[0].Ports[0].Port != 443 {
		t.Fatal("network metadata differs from consumer pins")
	}
	document.Generation++
	next, err := RenderFiles(document)
	if err != nil {
		t.Fatal(err)
	}
	if string(next["integration-configmap.json"]) == string(files["integration-configmap.json"]) || string(next["integration-deployment-patch.json"]) == string(files["integration-deployment-patch.json"]) {
		t.Fatal("generation did not switch immutable projection")
	}
}
