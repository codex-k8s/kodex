package integrationegresspolicy

import (
	"encoding/json"
	"errors"
	"net/netip"
	"strconv"
)

const NetworkPolicyName = "egress-gateway-integration-destinations"

// RenderFiles материализует только metadata о разрешённых HTTPS endpoints.
// Вызов Kubernetes API и проверка готовности принадлежат publisher.
func RenderFiles(document Document) (map[string][]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	digest := document.Digest()
	name := "egress-gateway-integration-" + digest[:24]
	raw, err := json.Marshal(document)
	if err != nil || len(raw) > MaximumFileBytes {
		return nil, errors.New("integration egress policy exceeds render bound")
	}
	labels := map[string]string{"app.kubernetes.io/name": "egress-gateway", "app.kubernetes.io/component": "platform-egress"}
	metadata := func(name string) map[string]any {
		return map[string]any{"name": name, "namespace": "kodex-system", "labels": labels}
	}
	egress := make([]any, 0, len(document.Destinations))
	for _, destination := range document.Destinations {
		peers := make([]any, 0, len(destination.Addresses))
		for _, pin := range destination.Addresses {
			address := netip.MustParseAddr(pin)
			peers = append(peers, map[string]any{"ipBlock": map[string]string{
				"cidr": netip.PrefixFrom(address, address.BitLen()).String(),
			}})
		}
		egress = append(egress, map[string]any{"to": peers, "ports": []any{map[string]any{"protocol": "TCP", "port": 443}}})
	}
	objects := map[string]any{
		"integration-configmap.json": map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": metadata(name), "immutable": true,
			"data": map[string]string{"integration-policy.json": string(raw)}},
		"integration-networkpolicy.json": map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "metadata": metadata(NetworkPolicyName),
			"spec": map[string]any{"podSelector": map[string]any{"matchLabels": labels}, "policyTypes": []string{"Egress"}, "egress": egress}},
		"integration-deployment-patch.json": map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]string{"name": "egress-gateway"},
			"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{
				"kodex.dev/integration-egress-generation":    strconv.FormatInt(document.Generation, 10),
				"kodex.dev/integration-egress-source-digest": document.SourceDigest,
			}}, "spec": map[string]any{
				"containers": []any{map[string]any{"name": "egress-gateway", "env": []any{map[string]string{
					"name": "EGRESS_GATEWAY_INTEGRATION_POLICY_DIGEST", "value": digest,
				}}}},
				"volumes": []any{map[string]any{"name": "integration-policy", "configMap": map[string]any{
					"name": name, "defaultMode": 292, "items": []any{map[string]string{
						"key": "integration-policy.json", "path": "integration-policy.json",
					}},
				}}},
			}}}},
	}
	files := make(map[string][]byte, len(objects))
	for name, object := range objects {
		value, err := json.MarshalIndent(object, "", "  ")
		if err != nil {
			return nil, errors.New("integration egress render failed")
		}
		files[name] = append(value, '\n')
	}
	return files, nil
}
