package authority

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
)

// ReportConfigurationReadback validates a positive revision and canonical digest, then requires a bounded owner acknowledgement without retries.
// The acknowledgement reports durable configuration acceptance, not provider health, mail delivery or an echoed remote configuration snapshot.
// ReportConfigurationReadback проверяет положительную ревизию и канонический дайджест, затем требует ограниченное подтверждение владельца без повторов.
// Подтверждается устойчивый приём конфигурации, не готовность провайдера, доставка почты или возвращённый удалённый снимок настроек.
func (c *Client) ReportConfigurationReadback(ctx context.Context, revision int64, digest string) error {
	decoded, err := hex.DecodeString(digest)
	if c == nil || c.API == nil || revision <= 0 || err != nil || len(decoded) != 32 || strings.ToLower(digest) != digest {
		return errs.Unavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	response, err := c.API.ReportEmailConfigurationReadback(ctx, &cp.ReportEmailConfigurationReadbackRequest{Revision: revision, Digest: digest})
	if err != nil || response == nil || !response.Accepted || ctx.Err() != nil {
		return errs.Unavailable
	}
	return nil
}
