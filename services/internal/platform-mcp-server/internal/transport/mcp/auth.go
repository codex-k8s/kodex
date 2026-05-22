package mcptransport

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
)

func staticTokenVerifier(expectedToken string, scope string, ttl time.Duration) mcpauth.TokenVerifier {
	trimmedToken := strings.TrimSpace(expectedToken)
	trimmedScope := strings.TrimSpace(scope)
	return func(_ context.Context, stringToken string, _ *http.Request) (*mcpauth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(stringToken)), []byte(trimmedToken)) != 1 {
			return nil, mcpauth.ErrInvalidToken
		}
		return &mcpauth.TokenInfo{
			Scopes:     []string{trimmedScope},
			Expiration: time.Now().Add(ttl),
			UserID:     "platform-mcp-shared-token",
		}, nil
	}
}

func bearerTokenMiddleware(cfg Config) func(http.Handler) http.Handler {
	if !cfg.AuthRequired {
		return func(handler http.Handler) http.Handler {
			return handler
		}
	}
	return mcpauth.RequireBearerToken(staticTokenVerifier(cfg.AuthToken, cfg.AuthScope, cfg.AuthTokenTTL), &mcpauth.RequireBearerTokenOptions{
		Scopes: []string{strings.TrimSpace(cfg.AuthScope)},
	})
}
