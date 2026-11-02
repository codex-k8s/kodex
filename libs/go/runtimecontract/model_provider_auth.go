package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// GatewayProviderAuthenticationMode prevents legacy Codex clients from accepting gateway credentials.
// GatewayProviderAuthenticationMode не позволяет legacy Codex clients принять gateway credentials.
const GatewayProviderAuthenticationMode = "gateway-api-key"

// ErrGatewayProviderAuthentication is a value-free error for the secret-only authentication channel.
// ErrGatewayProviderAuthentication — value-free ошибка secret-only authentication channel.
var ErrGatewayProviderAuthentication = errors.New("gateway provider authentication is invalid")

// gatewayProviderAuthentication is secret-only material, never part of RunnerInput or public configuration.
// Definition binding prevents a key from being routed to another configured gateway identity.
// gatewayProviderAuthentication — secret-only material, не часть RunnerInput или public configuration.
// Definition binding запрещает направлять ключ другой configured gateway identity.
type gatewayProviderAuthentication struct {
	Mode                  string `json:"auth_mode"`
	ProviderDefinitionKey string `json:"provider_definition_key"`
	APIKey                string `json:"API_KEY"`
}

// DefinitionKey returns the stable provider-definition locator; it confers no account or credential authority.
// DefinitionKey возвращает stable provider-definition locator, не account/credential authority.
func (config ModelProviderConfiguration) DefinitionKey() string {
	return "opencode-" + config.ProviderID
}

// EncodeGatewayProviderAuthentication creates canonical backend-only credential bytes from explicit inputs.
// Callers must store them only in the approved secret backend and clear the returned byte slice after use.
// EncodeGatewayProviderAuthentication создаёт canonical backend-only credential bytes из explicit inputs.
// Вызывающий сохраняет их только в secret backend и очищает byte slice после использования.
func EncodeGatewayProviderAuthentication(definitionKey string, key []byte) ([]byte, error) {
	if !validGatewayProviderDefinition(definitionKey) || !validGatewayProviderKey(key) {
		return nil, ErrGatewayProviderAuthentication
	}
	raw, err := json.Marshal(gatewayProviderAuthentication{Mode: GatewayProviderAuthenticationMode,
		ProviderDefinitionKey: definitionKey, APIKey: string(key)})
	if err != nil {
		return nil, ErrGatewayProviderAuthentication
	}
	return raw, nil
}

// DecodeGatewayProviderAuthentication requires the expected owner-selected definition and a closed object.
// It returns secret bytes only to the protected provider caller; it performs no network or environment access.
// DecodeGatewayProviderAuthentication требует expected owner-selected definition и closed object.
// Возвращает secret bytes только protected provider caller, без network/environment access.
func DecodeGatewayProviderAuthentication(raw []byte, expectedDefinitionKey string) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 32<<10 || !utf8.Valid(raw) || !validGatewayProviderDefinition(expectedDefinitionKey) ||
		boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 4) != nil {
		return nil, ErrGatewayProviderAuthentication
	}
	var value gatewayProviderAuthentication
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.Mode != GatewayProviderAuthenticationMode ||
		value.ProviderDefinitionKey != expectedDefinitionKey || !validGatewayProviderKey([]byte(value.APIKey)) {
		return nil, ErrGatewayProviderAuthentication
	}
	return []byte(value.APIKey), nil
}

// validGatewayProviderDefinition accepts only the registered OpenCode definition namespace.
// validGatewayProviderDefinition принимает только namespace зарегистрированных OpenCode definitions.
func validGatewayProviderDefinition(value string) bool {
	return strings.HasPrefix(value, "opencode-") && len(value) <= 96 && ValidDNSLabel(strings.TrimPrefix(value, "opencode-"))
}

// validGatewayProviderKey bounds one UTF-8 credential without accepting whitespace or control characters.
// validGatewayProviderKey ограничивает одну UTF-8 credential без whitespace и control characters.
func validGatewayProviderKey(value []byte) bool {
	return len(value) >= 8 && len(value) <= 16<<10 && utf8.Valid(value) &&
		strings.IndexFunc(string(value), func(char rune) bool { return unicode.IsSpace(char) || unicode.IsControl(char) }) < 0
}
