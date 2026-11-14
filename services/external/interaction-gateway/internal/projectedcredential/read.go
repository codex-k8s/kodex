// Package projectedcredential verifies immutable owner descriptors against bounded projected credential bytes without exposing them in diagnostics.
// Пакет projectedcredential сверяет immutable owner descriptors с ограниченными projected credential bytes без выдачи их в диагностике.
package projectedcredential

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/credentialfs"
	"github.com/google/uuid"
)

// SecretPrefix confines descriptor reads to the one projected integration credential Secret, independently of caller filenames.
// SecretPrefix ограничивает descriptor reads единственным projected Secret реквизитов интеграции независимо от caller filenames.
const SecretPrefix = "kodex-system/kodex-integration-credentials#"

// errUnavailable deliberately discards filesystem paths, descriptor contents and credential values.
// errUnavailable намеренно отбрасывает filesystem paths, descriptor contents и credential values.
var errUnavailable = errors.New("projected interaction credential is unavailable")

// Read waits within the supplied finite context budget for projected bytes matching the exact owner digest; the caller owns clearing the returned buffer.
// Read ожидает в конечном context budget projected bytes с точным owner digest; очисткой возвращённого буфера владеет вызывающая сторона.
func Read(ctx context.Context, store *credentialfs.Store, descriptor *cp.IntegrationCredentialRevision, timeout time.Duration) ([]byte, error) {
	if store == nil || descriptor == nil || descriptor.GetRef() == "" || descriptor.GetRevision() < 1 || uuid.Validate(descriptor.GetSecretUid()) != nil ||
		descriptor.GetSecretResourceVersion() == "" || len(descriptor.GetSecretResourceVersion()) > 128 || len(descriptor.GetContentSha256()) != 64 ||
		!strings.HasPrefix(descriptor.GetSecretRef(), SecretPrefix) || timeout <= 0 || timeout > 2*time.Minute {
		return nil, errUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	key := strings.TrimPrefix(descriptor.GetSecretRef(), SecretPrefix)
	for {
		if ctx.Err() != nil {
			return nil, errUnavailable
		}
		value, err := store.ReadKey(key)
		if err == nil {
			trimmed := bytes.TrimSpace(value)
			digest := sha256.Sum256(trimmed)
			if len(trimmed) == 0 || len(trimmed) > 16<<10 || bytes.ContainsAny(trimmed, "\r\n") || hex.EncodeToString(digest[:]) != descriptor.GetContentSha256() {
				clear(value)
				return nil, errUnavailable
			}
			result := bytes.Clone(trimmed)
			clear(value)
			return result, nil
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errUnavailable
		case <-timer.C:
		}
	}
}
