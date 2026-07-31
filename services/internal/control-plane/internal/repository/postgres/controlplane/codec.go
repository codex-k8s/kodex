package controlplane

import (
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
)

func marshalSpec(spec entity.Spec) ([]byte, error) {
	return entity.MarshalSpec(spec)
}

func unmarshalSpec(kind enum.Kind, raw []byte) (entity.Spec, error) {
	return entity.UnmarshalSpec(kind, raw)
}

func marshalResource(resource entity.Resource) ([]byte, error) {
	return entity.MarshalSnapshot(resource)
}

func unmarshalResource(raw []byte) (entity.Resource, error) {
	return entity.UnmarshalSnapshot(raw)
}
