package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// RuntimeRevisionCredentialSource pins the credential source by Secret name, UID and resource version without including secret values in revision material.
// RuntimeRevisionCredentialSource закрепляет источник реквизитов по имени Secret, UID и версии ресурса без включения значений секрета в материал ревизии.
type RuntimeRevisionCredentialSource struct {
	SecretName            string `json:"secret_name"`
	SecretUID             string `json:"secret_uid"`
	SecretResourceVersion string `json:"secret_resource_version"`
}

// RuntimeRevisionDigest hashes the revision and exact credential-source metadata after clearing execution-local leases, transport bindings and container paths; remaining revision inputs stay bound.
// RuntimeRevisionDigest хэширует ревизию и точные метаданные источника реквизитов после очистки локальных аренд, транспортных привязок и путей контейнера; остальные входы ревизии остаются связанными.
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

// RuntimeBoundedInputDigest hashes the JSON encoding of a bounded input map, treating nil as an empty object and propagating unsupported-value encoding failures.
// RuntimeBoundedInputDigest хэширует JSON-представление ограниченного входного словаря, трактуя nil как пустой объект и возвращая ошибки кодирования неподдерживаемых значений.
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
