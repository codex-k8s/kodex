package grpc

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func castConfigOverlaySchema(value runtimecontract.ConfigOverlaySchema) *controlplanev1.ConfigOverlaySchema {
	result := &controlplanev1.ConfigOverlaySchema{Revision: value.Revision, Digest: value.Digest, MaximumBytes: value.MaximumBytes}
	for _, field := range value.Fields {
		result.Fields = append(result.Fields, &controlplanev1.ConfigOverlayField{Key: field.Key, ValueType: field.ValueType,
			AllowedValues: field.AllowedValues, DefaultValue: field.DefaultValue, Description: field.Description,
			Completion: field.Completion, Hover: field.Hover})
	}
	return result
}
