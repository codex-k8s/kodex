package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// RuntimeRevisionCredentialSource связывает логическую credential revision с
// точной immutable Kubernetes Secret, доступной только trusted controller.
type RuntimeRevisionCredentialSource struct {
	SecretName            string `json:"secret_name"`
	SecretUID             string `json:"secret_uid"`
	SecretResourceVersion string `json:"secret_resource_version"`
}

// RuntimeRevisionDigest вычисляется без execution-local lease, callback
// locator и путей контейнера. Все server-owned inputs самой revision остаются.
func RuntimeRevisionDigest(input RunnerInput, source RuntimeRevisionCredentialSource) (string, error) {
	if input.RuntimeRevisionRef == "" || input.RuntimeRevisionVersion < 1 ||
		source.SecretName == "" || source.SecretUID == "" || source.SecretResourceVersion == "" {
		return "", errors.New("runtime revision digest input is incomplete")
	}
	material := input
	material.Schema = ""
	material.Mode = ""
	material.WorkloadInstance = ""
	material.LeaseRef = ""
	material.LeaseFence = ""
	material.LeaseGeneration = 0
	material.RuntimeRevisionDigest = ""
	material.ExecutionBindingDigest = ""
	material.MCPBindingDigest = ""
	material.CodexSandbox = ""
	material.CodexApprovalPolicy = ""
	material.CallbackURL = ""
	material.CallbackTLS = RuntimeTLSBinding{}
	material.ExecutionTicketFile = ""
	material.ProviderAuthFile = ""
	material.ProviderAuthSHA256File = ""
	material.WorkspaceRoot = ""
	material.OutboxRoot = ""
	material.CodexHome = ""
	raw, err := json.Marshal(struct {
		Revision   RunnerInput                     `json:"revision"`
		Credential RuntimeRevisionCredentialSource `json:"credential_source"`
	}{Revision: material, Credential: source})
	if err != nil {
		return "", errors.New("encode runtime revision digest")
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func RuntimeBoundedInputDigest(input map[string]any) (string, error) {
	if input == nil {
		input = map[string]any{}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", errors.New("encode runtime bounded input digest")
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
