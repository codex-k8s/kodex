package openai

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type EgressConfig struct {
	Revision string `env:"STT_EGRESS_EXPECTED_REVISION,required"`
	Digest   string `env:"STT_EGRESS_EXPECTED_DIGEST,required"`
}

func (config EgressConfig) Validate() error {
	digest, err := hex.DecodeString(config.Digest)
	if config.Revision == "" || len(config.Revision) > 128 || strings.ContainsAny(config.Revision, "\r\n\t ") || err != nil || len(digest) != 32 || strings.ToLower(config.Digest) != config.Digest {
		return errors.New("STT egress expectations are invalid")
	}
	return nil
}

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

func (config EgressConfig) onConnect(_ context.Context, proxy *url.URL, request *http.Request, response *http.Response) error {
	if proxy == nil || proxy.String() != ProxyURL || request == nil || request.Method != http.MethodConnect || request.Host != "api.openai.com:443" || response == nil || response.StatusCode != http.StatusOK {
		return errors.New("STT egress CONNECT boundary mismatch")
	}
	return config.check(response.Header)
}
