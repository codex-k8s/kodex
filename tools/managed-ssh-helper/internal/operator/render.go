// Package operator renders the broker deployment inputs for an independently observed SSH qualification; it never contacts Kubernetes or an SSH host.
// Package operator формирует broker deployment inputs для отдельно наблюдённой SSH qualification; к Kubernetes либо SSH host он не обращается.
package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// ErrRejected is the only public renderer diagnostic; private target coordinates and artifact bytes are not included in errors.
// ErrRejected — единственная публичная диагностика renderer; private target coordinates и artifact bytes в ошибки не включаются.
var ErrRejected = errors.New("managed SSH qualification rejected")

// Overlay contains immutable ConfigMap data, a narrow broker-only strategic patch and exact destination policy; rendering does not establish operator provenance or authorize applying the result.
// Overlay содержит immutable ConfigMap data, узкий broker-only strategic patch и exact destination policy; render не доказывает operator provenance и не разрешает apply результата.
type Overlay struct {
	ArtifactSHA256  string
	ConfigMapName   string
	ConfigMap       json.RawMessage
	DeploymentPatch json.RawMessage
	NetworkPolicy   json.RawMessage
}

// Render validates the complete canonical qualification with the same decoder as the runtime consumer and emits only fixed namespace/workload/mount identities, without credential material, arbitrary destination selectors or caller-selected commands.
// Render проверяет полный canonical qualification тем же decoder, что runtime consumer, и формирует только fixed namespace/workload/mount identities без credential material, arbitrary destination selectors либо caller-selected commands.
func Render(raw []byte, now int64) (Overlay, error) {
	document, err := managedssh.DecodeQualifications(raw, now)
	if err != nil {
		return Overlay{}, ErrRejected
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	name := "ssh-qualification-" + hash[:40]
	labels := map[string]string{"app.kubernetes.io/part-of": "kodex", "app.kubernetes.io/name": "secret-broker", "app.kubernetes.io/component": "secret-broker", "kodex.dev/ssh-qualification": hash[:40]}
	config := map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": name, "namespace": "kodex-system", "labels": labels}, "immutable": true, "data": map[string]string{"profiles.json": string(raw)}}
	patch := map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "secret-broker", "namespace": "kodex-system"}, "spec": map[string]any{"template": map[string]any{
		"metadata": map[string]any{"annotations": map[string]string{"kodex.dev/ssh-qualification-sha256": hash}},
		"spec": map[string]any{"volumes": []any{map[string]any{"name": "ssh-qualifications", "configMap": map[string]any{"name": name, "defaultMode": 292}}}, "containers": []any{map[string]any{
			"name": "secret-broker", "env": []any{map[string]string{"name": managedssh.QualificationFileEnvironment, "value": managedssh.OperatorQualificationFile}, map[string]string{"name": managedssh.QualificationSHAEnvironment, "value": hash}},
			"volumeMounts": []any{map[string]any{"name": "ssh-qualifications", "mountPath": managedssh.OperatorQualificationMount, "readOnly": true}},
		}}},
	}}}
	destinations := make(map[string][]uint16)
	for _, profile := range document.Profiles {
		if !slices.Contains(destinations[profile.Address], profile.Port) {
			destinations[profile.Address] = append(destinations[profile.Address], profile.Port)
		}
	}
	addresses := make([]string, 0, len(destinations))
	for address := range destinations {
		addresses = append(addresses, address)
	}
	slices.Sort(addresses)
	rules := make([]any, 0, len(addresses))
	for _, address := range addresses {
		parsed, err := netip.ParseAddr(address)
		if err != nil {
			return Overlay{}, ErrRejected
		}
		ports := destinations[address]
		slices.Sort(ports)
		selectors := make([]any, 0, len(ports))
		for _, port := range ports {
			selectors = append(selectors, map[string]any{"protocol": "TCP", "port": port})
		}
		rules = append(rules, map[string]any{"to": []any{map[string]any{"ipBlock": map[string]string{"cidr": address + "/" + strconv.Itoa(parsed.BitLen())}}}, "ports": selectors})
	}
	policy := map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "metadata": map[string]any{"name": "secret-broker-ssh-qualified-targets", "namespace": "kodex-system", "labels": labels}, "spec": map[string]any{
		"podSelector": map[string]any{"matchLabels": map[string]string{"app.kubernetes.io/name": "secret-broker", "app.kubernetes.io/component": "secret-broker"}}, "policyTypes": []string{"Egress"}, "egress": rules,
	}}
	result := Overlay{ArtifactSHA256: hash, ConfigMapName: name}
	for _, item := range []struct {
		value  any
		output *json.RawMessage
	}{{config, &result.ConfigMap}, {patch, &result.DeploymentPatch}, {policy, &result.NetworkPolicy}} {
		encoded, err := json.Marshal(item.value)
		if err != nil {
			return Overlay{}, ErrRejected
		}
		*item.output = encoded
	}
	return result, nil
}
