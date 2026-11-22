package operator

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// operatorFixture constructs synthetic non-root observations using documentation addresses; its hashes do not claim an actual installation.
// operatorFixture создаёт synthetic non-root observations с documentation addresses; hashes не объявляют реальную установку.
func operatorFixture(t *testing.T) (managedssh.Qualifications, int64) {
	t.Helper()
	now := int64(1800000000)
	profile := managedssh.TargetQualification{TargetRef: "ssh_target_example01", Address: "192.0.2.10", Port: 22, User: "fixture", Mode: managedssh.ExecuteTransport,
		HostKeyAlgorithm: managedssh.ED25519HostKey, HostKeyDigest: strings.Repeat("1", 64), AuthenticationKey: strings.Repeat("2", 64), UID: 1001, GID: 1001, Groups: []uint32{1001}, QualifiedAt: now - 60, ValidUntil: now + 3600,
		DeploymentDigest: strings.Repeat("3", 64), SSHDConfiguration: strings.Repeat("4", 64), IdentityInventory: strings.Repeat("5", 64), RuntimeReadback: strings.Repeat("6", 64), IdentityProbe: strings.Repeat("7", 64)}
	return managedssh.Qualifications{Schema: managedssh.QualificationSchema, Profiles: []managedssh.TargetQualification{profile}}, now
}

// TestRenderExactDestinations verifies deduplicated personal-key destinations, IPv4/IPv6 masks and the immutable canonical content/digest mounted by the broker.
// TestRenderExactDestinations проверяет deduplicated personal-key destinations, IPv4/IPv6 masks и immutable canonical content/digest для broker mount.
func TestRenderExactDestinations(t *testing.T) {
	document, now := operatorFixture(t)
	second := document.Profiles[0]
	second.AuthenticationKey = strings.Repeat("8", 64)
	document.Profiles = append(document.Profiles, second)
	third := document.Profiles[0]
	third.TargetRef = "ssh_target_example02"
	third.Address = "2001:db8::10"
	third.Port = 2222
	document.Profiles = append(document.Profiles, third)
	raw, err := managedssh.EncodeQualifications(document, now)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(raw)
	rendered, err := Render(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []json.RawMessage{rendered.ConfigMap, rendered.NetworkPolicy} {
		var resource struct {
			Metadata struct {
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
		}
		if json.Unmarshal(raw, &resource) != nil || resource.Metadata.Labels["app.kubernetes.io/part-of"] != "kodex" {
			t.Fatal("qualification object lacks portable ownership")
		}
	}
	var config struct {
		Immutable bool              `json:"immutable"`
		Data      map[string]string `json:"data"`
	}
	if json.Unmarshal(rendered.ConfigMap, &config) != nil || !config.Immutable || config.Data["profiles.json"] != string(original) {
		t.Fatal("canonical artifact binding changed")
	}
	var policy struct {
		Spec struct {
			Egress []struct {
				To []struct {
					IPBlock struct {
						CIDR string `json:"cidr"`
					} `json:"ipBlock"`
				} `json:"to"`
				Ports []struct {
					Protocol string `json:"protocol"`
					Port     int    `json:"port"`
				} `json:"ports"`
			} `json:"egress"`
		} `json:"spec"`
	}
	if json.Unmarshal(rendered.NetworkPolicy, &policy) != nil || len(policy.Spec.Egress) != 2 {
		t.Fatal("exact destination policy changed")
	}
	for index, expected := range []struct {
		cidr string
		port int
	}{{"192.0.2.10/32", 22}, {"2001:db8::10/128", 2222}} {
		rule := policy.Spec.Egress[index]
		if len(rule.To) != 1 || rule.To[0].IPBlock.CIDR != expected.cidr || len(rule.Ports) != 1 || rule.Ports[0].Port != expected.port || rule.Ports[0].Protocol != "TCP" {
			t.Fatal("destination rule widened")
		}
	}
	if !bytes.Contains(rendered.DeploymentPatch, []byte(managedssh.OperatorQualificationFile)) || !bytes.Contains(rendered.DeploymentPatch, []byte(rendered.ArtifactSHA256)) || !bytes.Contains(rendered.DeploymentPatch, []byte(`"readOnly":true`)) {
		t.Fatal("fixed deployment binding missing")
	}
	raw[0] = '!'
	if config.Data["profiles.json"] != string(original) {
		t.Fatal("caller buffer changed rendered artifact")
	}
	again, err := Render(original, now)
	if err != nil || !bytes.Equal(again.NetworkPolicy, rendered.NetworkPolicy) || !bytes.Equal(again.DeploymentPatch, rendered.DeploymentPatch) {
		t.Fatal("render is not deterministic")
	}
}

// TestRenderClosedInput rejects expired or alternate wire representations before producing deployment objects and keeps private parser contents out of diagnostics.
// TestRenderClosedInput отклоняет expired либо alternate wire representations до выдачи deployment objects и не выводит private parser contents в diagnostics.
func TestRenderClosedInput(t *testing.T) {
	document, now := operatorFixture(t)
	raw, err := managedssh.EncodeQualifications(document, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		raw []byte
		now int64
	}{{nil, now}, {append(bytes.Clone(raw), '\n'), now}, {raw, now + 4000}, {[]byte(`{"schema":"kodex-ssh-qualifications/v2","profiles":[],"private_key":"synthetic-private-marker"}`), now}} {
		result, err := Render(item.raw, item.now)
		if err != ErrRejected || result.ConfigMap != nil || result.DeploymentPatch != nil || result.NetworkPolicy != nil || strings.Contains(err.Error(), "synthetic-private-marker") {
			t.Fatal("unqualified artifact produced deployment output")
		}
	}
}
