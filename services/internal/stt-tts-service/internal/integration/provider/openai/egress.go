package openai

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// EgressConfig pins the expected proxy policy generation; these values are correlation metadata, not credentials.
// EgressConfig фиксирует ожидаемое поколение политики прокси; это метаданные сверки, не учётные данные.
type EgressConfig struct {
	Revision string `env:"STT_EGRESS_EXPECTED_REVISION,required"`
	Digest   string `env:"STT_EGRESS_EXPECTED_DIGEST,required"`
}

// Validate requires a bounded whitespace-free revision and a lowercase 32-byte hexadecimal digest.
// It checks local syntax, not the provenance or actual deployment of that policy generation.
// Validate требует ограниченную ревизию без пробелов и шестнадцатеричный 32-байтовый дайджест в нижнем регистре.
// Проверяется локальный формат, не происхождение или фактическое развёртывание поколения политики.
func (config EgressConfig) Validate() error {
	digest, err := hex.DecodeString(config.Digest)
	if config.Revision == "" || len(config.Revision) > 128 || strings.ContainsAny(config.Revision, "\r\n\t ") || err != nil || len(digest) != 32 || strings.ToLower(config.Digest) != config.Digest {
		return errors.New("STT egress expectations are invalid")
	}
	return nil
}

// check requires exactly one matching header for each expected generation, profile, workload, and operation pin.
// check требует ровно один совпадающий заголовок для каждого ожидаемого поколения, профиля, workload и операции.
func (config EgressConfig) check(headers http.Header) error {
	if err := config.Validate(); err != nil {
		return err
	}
	for name, expected := range map[string]string{
		"Revision": config.Revision, "Digest": config.Digest, "Profile": "openai-stt",
		"Workload": "stt-tts-service", "Operation": "openai.transcription",
	} {
		values := headers.Values("X-Kodex-Egress-" + name)
		if len(values) != 1 || values[0] != expected {
			return errors.New("STT egress generation mismatch")
		}
	}
	return nil
}

// onConnect rejects a different proxy, CONNECT target, status, or policy generation before provider TLS begins.
// Header equality is proxy readback, not a cryptographic signature or a replacement for provider TLS verification.
// onConnect отвергает иной прокси, адрес CONNECT, статус или поколение политики до начала TLS с провайдером.
// Равенство заголовков сверяет прокси, но не является подписью и не заменяет проверку TLS провайдера.
func (config EgressConfig) onConnect(_ context.Context, proxy *url.URL, request *http.Request, response *http.Response) error {
	if proxy == nil || proxy.String() != ProxyURL || request == nil || request.Method != http.MethodConnect || request.Host != "api.openai.com:443" || response == nil || response.StatusCode != http.StatusOK {
		return errors.New("STT egress CONNECT boundary mismatch")
	}
	return config.check(response.Header)
}
