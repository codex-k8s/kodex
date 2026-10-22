package restore

import "testing"

// TestDecodeConfigMapEnvelopeAcceptsKubernetesObjectMetadata checks modeled standard metadata survives strict local envelope decoding using synthetic JSON; it does not contact Kubernetes or execute coordination CAS.
// TestDecodeConfigMapEnvelopeAcceptsKubernetesObjectMetadata на синтетическом JSON проверяет сохранение описанных стандартных metadata после строгого локального декодирования envelope без обращения к Kubernetes и выполнения coordination CAS.
func TestDecodeConfigMapEnvelopeAcceptsKubernetesObjectMetadata(t *testing.T) {
	// raw supplies modeled Kubernetes metadata with blank coordination state for envelope-only decoding.
	// raw задаёт описанные Kubernetes metadata с пустым coordination state для декодирования лишь envelope.
	const raw = `{"apiVersion":"v1","data":{"state.json":""},"kind":"ConfigMap","metadata":{"creationTimestamp":"2026-08-12T00:00:00Z","labels":{"app.kubernetes.io/name":"internal-rpc-authority"},"name":"internal-rpc-authority-restore-coordination","namespace":"kodex-system","resourceVersion":"42","uid":"00000000-0000-4000-8000-000000000001"}}`

	var envelope configMapEnvelope
	if err := decodeStrictJSON([]byte(raw), &envelope); err != nil {
		t.Fatalf("decode Kubernetes ConfigMap envelope: %v", err)
	}
	if envelope.Metadata.Name != "internal-rpc-authority-restore-coordination" ||
		envelope.Metadata.Namespace != "kodex-system" ||
		envelope.Metadata.ResourceVersion != "42" ||
		envelope.Metadata.UID == "" ||
		envelope.Metadata.CreationTimestamp == "" ||
		envelope.Metadata.Labels["app.kubernetes.io/name"] != "internal-rpc-authority" {
		t.Fatal("Kubernetes ConfigMap metadata binding was not preserved")
	}
}
