package provider

import (
	"context"
	"encoding/json"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
)

type CredentialSource interface {
	Resolve(context.Context, entity.Connection) (map[string]string, error)
}

type Result struct {
	Status          enum.InvocationStatus
	Payload         json.RawMessage
	ProviderReceipt string
}

type Client interface {
	Execute(context.Context, entity.Connection, entity.Tool, json.RawMessage, map[string]string, string) (Result, error)
	Validate(context.Context, entity.Connection, map[string]string) enum.ValidationCode
}
