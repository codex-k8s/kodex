package platform

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

type providerUsageCursor struct {
	Source, Ref          string
	UpdatedAt, ExpiresAt time.Time
}

func encodeProviderUsageCursor(source string, updatedAt time.Time, ref string) string {
	raw, _ := json.Marshal(providerUsageCursor{Source: source, Ref: ref, UpdatedAt: updatedAt, ExpiresAt: time.Now().UTC().Add(10 * time.Second)})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeProviderUsageCursor(source, token string) (*time.Time, string, error) {
	if token == "" {
		return nil, "", nil
	}
	if len(token) > 1024 {
		return nil, "", errs.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, "", errs.ErrInvalid
	}
	var cursor providerUsageCursor
	if decodeStrict(raw, &cursor) != nil || cursor.Source != source || !validOverlayHistoryRef(cursor.Ref) || cursor.UpdatedAt.IsZero() || !cursor.ExpiresAt.After(time.Now().UTC()) || cursor.ExpiresAt.After(time.Now().UTC().Add(11*time.Second)) {
		return nil, "", errs.ErrInvalid
	}
	return &cursor.UpdatedAt, cursor.Ref, nil
}
