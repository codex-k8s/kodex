package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/google/uuid"
)

// CreateBearerWithCSRF places a verified platform API credential into its own encrypted family without inventing refresh credentials.
// Its absolute lifetime is bounded by the access token and owner session; renewal cannot call the provider or extend either bound.
// CreateBearerWithCSRF помещает verified platform API credential в отдельную encrypted family без выдуманных refresh credentials.
// Absolute lifetime ограничен access token и owner session; renewal не вызывает provider и не продлевает эти пределы.
func (families *Families) CreateBearerWithCSRF(ctx context.Context, principal oidcverifier.Principal, bearer string, elevation *Elevation) (Family, string, error) {
	now := families.now().UTC()
	if !principal.PlatformManagedOIDC || bearer == "" || principal.AuthenticatedAt.IsZero() || principal.AuthenticatedAt.After(now) || !principal.ExpiresAt.After(now) ||
		!principal.PlatformSessionExpiresAt.After(now) || principal.PlatformSessionExpiresAt.After(principal.AuthenticatedAt.Add(absoluteSSOLifetime)) {
		return Family{}, "", ErrReauthentication
	}
	csrf, err := randomOpaque()
	if err != nil {
		return Family{}, "", err
	}
	digest := sha256.Sum256([]byte(csrf))
	absolute := earlier(principal.ExpiresAt, principal.PlatformSessionExpiresAt)
	family := Family{ID: uuid.NewString(), BrowserSessionID: uuid.NewString(), Version: 1, State: familyActive, Principal: principal, AccessToken: bearer, BearerOnly: true,
		CSRFHash: hex.EncodeToString(digest[:]), CreatedAt: now, IdleExpiresAt: earlier(now.Add(families.keys.ttl), absolute), AbsoluteExpiresAt: absolute, Elevation: elevation}
	stored, err := families.write(ctx, family)
	if err != nil {
		return Family{}, "", err
	}
	return stored, csrf, nil
}

// ReadForClosure reads only an existing platform family for the exact logout path, including pending revoke and confirmed terminal receipt.
// Expired or unconfirmed terminal state never becomes an application credential; no provider token is recreated from a cookie.
// ReadForClosure читает лишь existing platform family для exact logout path, включая pending revoke и confirmed terminal receipt.
// Expired и unconfirmed terminal state не становятся application credential; provider token не восстанавливается из cookie.
func (families *Families) ReadForClosure(ctx context.Context, id string) (Family, error) {
	record, err := families.store.Read(ctx, id)
	if errors.Is(err, browserstate.ErrNotFound) {
		return Family{}, ErrReauthentication
	}
	if err != nil {
		return Family{}, err
	}
	family, err := families.decode(id, record)
	if err != nil {
		return Family{}, err
	}
	now := families.now()
	if !family.Principal.PlatformManagedOIDC || !family.IdleExpiresAt.After(now) || !family.AbsoluteExpiresAt.After(now) || family.State == familyTerminal && !family.OwnerRevokeConfirmed {
		return Family{}, ErrReauthentication
	}
	return family, nil
}

// BeginPlatformRevoke atomically fences one browser/CSRF-bound family before contacting the owner, retaining only the access token needed for exact reconciliation.
// A refresh already in flight cannot commit across the changed CAS sequence; repeats rejoin the same intent or confirmed receipt.
// BeginPlatformRevoke атомарно блокирует одну browser/CSRF-bound family до owner call, сохраняя лишь access token для exact reconciliation.
// Уже начатый refresh не коммитит через изменённую CAS sequence; повторы присоединяются к тому же intent либо confirmed receipt.
func (families *Families) BeginPlatformRevoke(ctx context.Context, id, browserID, csrfHash string) (Family, error) {
	for range 3 {
		family, err := families.ReadForClosure(ctx, id)
		if err != nil {
			return Family{}, err
		}
		if family.BrowserSessionID != browserID || family.CSRFHash != csrfHash {
			return Family{}, ErrReauthentication
		}
		if family.State == familyRevoking || family.OwnerRevokeConfirmed {
			return family, nil
		}
		family.State, family.RefreshToken, family.Elevation = familyRevoking, "", nil
		family.AttemptID, family.RefreshDeadline = "", time.Time{}
		family.Version++
		updated, err := families.write(ctx, family)
		if !errors.Is(err, browserstate.ErrConflict) {
			return updated, err
		}
	}
	return Family{}, browserstate.ErrConflict
}

// CompletePlatformRevoke records only a confirmed owner response for the exact pending browser tuple, then erases access/refresh credentials.
// The durable receipt permits cookie cleanup after a lost response without authorizing any ordinary HTTP/WS or renewal path.
// CompletePlatformRevoke сохраняет лишь confirmed owner response exact pending browser tuple и очищает access/refresh credentials.
// Durable receipt позволяет cookie cleanup после потерянного ответа без допуска ordinary HTTP/WS и renewal.
func (families *Families) CompletePlatformRevoke(ctx context.Context, id, browserID, csrfHash string, revision uint64) error {
	for range 3 {
		family, err := families.ReadForClosure(ctx, id)
		if err != nil {
			return err
		}
		if family.BrowserSessionID != browserID || family.CSRFHash != csrfHash || family.Principal.SessionRevision != revision {
			return ErrReauthentication
		}
		if family.OwnerRevokeConfirmed {
			return nil
		}
		if family.State != familyRevoking {
			return ErrReauthentication
		}
		family.State, family.AccessToken, family.RefreshToken = familyTerminal, "", ""
		family.OwnerRevokeConfirmed = true
		family.Version++
		_, err = families.write(ctx, family)
		if !errors.Is(err, browserstate.ErrConflict) {
			return err
		}
	}
	return browserstate.ErrConflict
}
