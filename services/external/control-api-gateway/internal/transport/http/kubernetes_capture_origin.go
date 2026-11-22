package httptransport

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// errKubernetesCaptureOrigin rejects invalid saved provenance without reflecting source details.
// errKubernetesCaptureOrigin отклоняет invalid saved provenance без отражения source details.
var errKubernetesCaptureOrigin = errors.New("Kubernetes artifact capture origin is invalid")

// normalizeKubernetesCaptureOrigin restores only declared public defaults and validates the same
// closed projection as its owner. It grants neither artifact bytes nor current Kubernetes access.
// normalizeKubernetesCaptureOrigin восстанавливает только declared public defaults и проверяет
// ту же closed projection, что owner; artifact bytes либо current Kubernetes access не выдаёт.
func normalizeKubernetesCaptureOrigin(value map[string]any, descriptor protoreflect.MessageDescriptor) error {
	if descriptor.FullName() != "controlplane.v1.KubernetesArtifactCaptureOrigin" {
		return nil
	}
	for key := range value {
		if descriptor.Fields().ByJSONName(key) == nil {
			return errKubernetesCaptureOrigin
		}
	}
	for _, key := range []string{"acknowledgedPrefix", "outputWatermark", "droppedBytes"} {
		if _, ok := value[key]; !ok {
			value[key] = "0"
		}
	}
	for _, key := range []string{"nativeTerminalKnown", "transportJoined"} {
		if _, ok := value[key]; !ok {
			value[key] = false
		}
	}
	if _, ok := value["reasonCode"]; !ok {
		value["reasonCode"] = ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return errKubernetesCaptureOrigin
	}
	var origin managedkubernetes.ArtifactCaptureOrigin
	// The descriptor already owns exact field names; the typed decoder also checks unknown fields.
	// DecodeExactJSON deliberately excludes time.Time, whose bounded RFC3339 decoder is required here.
	// Descriptor уже задаёт exact field names; typed decoder дополнительно запрещает unknown fields.
	// DecodeExactJSON запрещает time.Time, чей bounded RFC3339 decoder нужен этой read projection.
	if internalrpcauth.DecodeStrictJSON(raw, &origin) != nil || origin.Validate() != nil {
		return errKubernetesCaptureOrigin
	}
	return nil
}

// normalizeKubernetesCaptureOrdinal preserves exact bigint text across public JSON and JavaScript.
// normalizeKubernetesCaptureOrdinal сохраняет exact bigint text в public JSON и JavaScript.
func normalizeKubernetesCaptureOrdinal(value any, field protoreflect.FieldDescriptor) (any, bool, error) {
	if field.ContainingMessage().FullName() != "controlplane.v1.KubernetesArtifactCaptureOrigin" {
		return nil, false, nil
	}
	switch field.JSONName() {
	case "acknowledgedPrefix", "outputWatermark", "droppedBytes":
	default:
		return nil, false, nil
	}
	text, ok := value.(string)
	parsed, err := strconv.ParseUint(text, 10, 64)
	if !ok || err != nil || parsed > math.MaxInt64 || strconv.FormatUint(parsed, 10) != text {
		return nil, true, errKubernetesCaptureOrigin
	}
	return text, true, nil
}
