package mailpolicy

import (
	"encoding/json"
	"errors"
	"net/netip"
)

// RenderFiles материализует согласованные immutable policy, CNI pins и Deployment patch.
// Результат содержит только endpoint metadata, исходные descriptor values не копируются.
func RenderFiles(document MailDocument) (map[string][]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	digest := document.Digest()
	name := "egress-gateway-mail-" + digest[:24]
	raw, err := json.Marshal(document)
	if err != nil || len(raw) > MaximumFileBytes {
		return nil, errors.New("mail policy exceeds render bound")
	}
	labels := map[string]string{"app.kubernetes.io/name": "egress-gateway", "app.kubernetes.io/component": "platform-egress"}
	metadata := func(name string) map[string]any {
		return map[string]any{"name": name, "namespace": "kodex-system", "labels": labels}
	}
	egress := []any{}
	for _, d := range document.Destinations {
		peers := []any{}
		for _, pin := range d.Addresses {
			address := netip.MustParseAddr(pin)
			peers = append(peers, map[string]any{"ipBlock": map[string]string{"cidr": netip.PrefixFrom(address, address.BitLen()).String()}})
		}
		egress = append(egress, map[string]any{"to": peers, "ports": []any{map[string]any{"protocol": "TCP", "port": d.Port}}})
	}
	objects := map[string]any{
		"mail-configmap.json":     map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": metadata(name), "immutable": true, "data": map[string]string{"mail-policy.json": string(raw)}},
		"mail-networkpolicy.json": map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "metadata": metadata("egress-gateway-mail-destinations"), "spec": map[string]any{"podSelector": map[string]any{"matchLabels": labels}, "policyTypes": []string{"Egress"}, "egress": egress}},
		"mail-deployment-patch.json": map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]string{"name": "egress-gateway"}, "spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"kodex.dev/mail-configuration-digest": document.ConfigurationDigest}}, "spec": map[string]any{
			"containers": []any{map[string]any{"name": "egress-gateway", "env": []any{map[string]string{"name": "EGRESS_GATEWAY_MAIL_POLICY_DIGEST", "value": digest}}}},
			"volumes":    []any{map[string]any{"name": "mail-policy", "configMap": map[string]any{"name": name, "defaultMode": 292, "items": []any{map[string]string{"key": "mail-policy.json", "path": "mail-policy.json"}}}}},
		}}}},
	}
	files := map[string][]byte{}
	for name, object := range objects {
		value, err := json.MarshalIndent(object, "", "  ")
		if err != nil {
			return nil, err
		}
		files[name] = append(value, '\n')
	}
	return files, nil
}
