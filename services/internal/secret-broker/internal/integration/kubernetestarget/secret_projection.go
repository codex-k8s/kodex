package kubernetestarget

import (
	"encoding/json"
	"errors"
	"slices"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	strictjson "sigs.k8s.io/json"
)

const (
	// maximumSecretProjectionInput permits one bounded Secret response without exposing its values.
	// maximumSecretProjectionInput допускает один bounded Secret response без выдачи его values.
	maximumSecretProjectionInput = 2 << 20
	// maximumSafeProjectionBytes also bounds key-name-only results before downstream framing.
	// maximumSafeProjectionBytes ограничивает key-name-only result до downstream framing.
	maximumSafeProjectionBytes = 64 << 10
	// invalidSecretProjection refuses malformed or foreign resources without including raw output.
	// invalidSecretProjection отклоняет неверный либо чужой resource без исходного output.
	invalidSecretProjection = "managed Kubernetes Secret projection is invalid"
)

// SecretProjection is the only Secret shape allowed beyond the broker output boundary.
// Data values, stringData, annotations, managed fields and arbitrary provider fields never survive
// projection. Formatters receive this shape, so JSONPath, describe and watch cannot recover values.
// SecretProjection — единственная форма Secret за пределами broker output boundary.
// Data values, stringData, annotations, managed fields и произвольные provider fields не проходят
// projection. Formatters получают эту форму: JSONPath, describe и watch не восстановят values.
type SecretProjection struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Metadata   SecretMetadataProjection `json:"metadata"`
	Type       string                   `json:"type"`
	Immutable  bool                     `json:"immutable"`
	KeyNames   []string                 `json:"keyNames"`
}

// SecretMetadataProjection deliberately excludes free-form metadata that can embed Secret bodies.
// SecretMetadataProjection исключает free-form metadata, способную содержать Secret bodies.
type SecretMetadataProjection struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	UID               string `json:"uid"`
	ResourceVersion   string `json:"resourceVersion"`
	CreationTimestamp string `json:"creationTimestamp,omitempty"`
}

// secretProjectionEnvelope parses only the registered Secret object, leaving value bytes private.
// The second metadata decode rejects duplicate keys hidden in its raw JSON object.
// secretProjectionEnvelope разбирает только registered Secret object, сохраняя value bytes private.
// Второй metadata decode отклоняет duplicate keys внутри raw JSON object.
type secretProjectionEnvelope struct {
	APIVersion string                     `json:"apiVersion"`
	Kind       string                     `json:"kind"`
	Metadata   json.RawMessage            `json:"metadata"`
	Type       string                     `json:"type"`
	Immutable  bool                       `json:"immutable"`
	Data       map[string]json.RawMessage `json:"data"`
	StringData map[string]json.RawMessage `json:"stringData"`
}

// ProjectSecret converts one exact namespaced Secret to metadata and key names before serialization
// or formatting. It never returns raw JSON as a fallback and does not decode base64 credential values.
// This projection does not authorize reading the Secret; fresh owner and Kubernetes checks precede it.
// ProjectSecret преобразует один exact namespaced Secret в metadata и key names до serialization
// либо formatting. Не возвращает raw JSON как fallback и не декодирует base64 credential values.
// Projection не разрешает чтение Secret; до неё нужны fresh owner и Kubernetes checks.
func ProjectSecret(raw []byte, expected ResourceAddress) (SecretProjection, error) {
	var result SecretProjection
	if len(raw) == 0 || len(raw) > maximumSecretProjectionInput || !utf8.Valid(raw) || expected.Validate() != nil ||
		expected.Group != "" || expected.Version != "v1" || expected.Resource != "secrets" ||
		!expected.Namespaced || expected.Name == "" || expected.Subresource != "" {
		return result, errors.New(invalidSecretProjection)
	}
	var envelope secretProjectionEnvelope
	strictErrors, err := strictjson.UnmarshalStrict(raw, &envelope)
	if err != nil || len(strictErrors) != 0 || envelope.APIVersion != "v1" || envelope.Kind != "Secret" ||
		len(envelope.Data)+len(envelope.StringData) > 4096 || len(envelope.Type) > 253 {
		return result, errors.New(invalidSecretProjection)
	}
	var metadata metav1.ObjectMeta
	strictErrors, err = strictjson.UnmarshalStrict(envelope.Metadata, &metadata)
	if err != nil || len(strictErrors) != 0 || metadata.Name != expected.Name || metadata.Namespace != expected.Namespace ||
		metadata.UID == "" || metadata.ResourceVersion == "" ||
		(expected.UID != "" && string(metadata.UID) != expected.UID) ||
		(expected.ResourceVersion != "" && metadata.ResourceVersion != expected.ResourceVersion) {
		return result, errors.New(invalidSecretProjection)
	}
	actual := expected
	actual.UID, actual.ResourceVersion = string(metadata.UID), metadata.ResourceVersion
	if actual.Validate() != nil {
		return result, errors.New(invalidSecretProjection)
	}
	keySet := make(map[string]struct{}, len(envelope.Data)+len(envelope.StringData))
	for _, values := range []map[string]json.RawMessage{envelope.Data, envelope.StringData} {
		for key, value := range values {
			if len(validation.IsConfigMapKey(key)) != 0 || len(value) == 0 || value[0] != '"' {
				return result, errors.New(invalidSecretProjection)
			}
			keySet[key] = struct{}{}
		}
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result = SecretProjection{APIVersion: "v1", Kind: "Secret", Type: envelope.Type,
		Immutable: envelope.Immutable, KeyNames: keys, Metadata: SecretMetadataProjection{
			Name: metadata.Name, Namespace: metadata.Namespace, UID: string(metadata.UID), ResourceVersion: metadata.ResourceVersion}}
	if !metadata.CreationTimestamp.IsZero() {
		result.Metadata.CreationTimestamp = metadata.CreationTimestamp.UTC().Format("2006-01-02T15:04:05Z")
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maximumSafeProjectionBytes {
		return SecretProjection{}, errors.New(invalidSecretProjection)
	}
	return result, nil
}
