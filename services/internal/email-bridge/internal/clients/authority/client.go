package authority

import (
	"context"
	"io"
	"net/http"
	"strings"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
)

type Client struct {
	API        *api.Client
	BearerFile string
}

func (c *Client) Resolve(ctx context.Context, input api.AuthorizationRequest) (api.AuthorizationDecision, error) {
	var decision api.AuthorizationDecision
	token, e := securefile.Read(c.BearerFile, 16384)
	if e != nil || strings.ContainsAny(string(token), "\r\n") {
		return decision, errs.Unavailable
	}
	response, e := c.API.ResolveEmailAuthorization(ctx, input, func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+string(token))
		r.GetBody = nil
		return nil
	})
	if e != nil {
		return decision, errs.Unavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return decision, errs.Denied
	}
	if response.StatusCode != http.StatusOK {
		return decision, errs.Unavailable
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	if e != nil || len(raw) > 65536 || api.Decode(raw, &decision) != nil {
		return decision, errs.Unavailable
	}
	return decision, nil
}
