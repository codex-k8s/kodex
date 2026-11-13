package integrationegresspolicy

import (
	"encoding/json"
	"net/netip"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// TestRenderPinsAndImmutableDeploymentSwitch verifies the rendered immutable
// ConfigMap, exact /32 HTTPS egress pin and changed deployment projection
// after a new generation, without applying any Kubernetes resource.
// RU: TestRenderPinsAndImmutableDeploymentSwitch проверяет immutable ConfigMap,
// точный /32 HTTPS pin и смену проекции Deployment при новом поколении,
// не применяя ресурсы Kubernetes.
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

// TestInitialNetworkFenceMatchesArtifact keeps fresh readback stable across the first owner publication.
// TestInitialNetworkFenceMatchesArtifact сохраняет точную сверку до и после первой owner publication.
func TestInitialNetworkFenceMatchesArtifact(t *testing.T) {
	document := Document{Schema: Schema, Generation: 1, SourceDigest: SourceDigest(nil), GatewayPolicyDigest: strings.Repeat("a", 64), Destinations: []Destination{}}
	files, err := RenderFiles(document)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../deploy/k8s/base/egress-gateway/integration/integration-networkpolicy.json")
	if err != nil {
		t.Fatal(err)
	}
	var rendered, artifact map[string]any
	if json.Unmarshal(files["integration-networkpolicy.json"], &rendered) != nil || json.Unmarshal(raw, &artifact) != nil || !reflect.DeepEqual(rendered, artifact) {
		t.Fatal("initial network policy artifact differs from owner projection")
	}
	document.Generation = 2
	files, err = RenderFiles(document)
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(files["integration-networkpolicy.json"], &rendered) != nil {
		t.Fatal("invalid network policy")
	}
	annotations := rendered["metadata"].(map[string]any)["annotations"].(map[string]any)
	if annotations["kodex.dev/integration-egress-generation"] != strconv.FormatInt(document.Generation, 10) || annotations["kodex.dev/integration-egress-source-digest"] != document.SourceDigest {
		t.Fatal("network publication fence differs from document")
	}
}
