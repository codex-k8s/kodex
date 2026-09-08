package session

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	"github.com/google/uuid"
)

const websocketTicketLifetime = 30 * time.Second
const maximumWebSocketTickets = 16
const websocketTicketAAD = "browser-ws-tickets-v1:"

type websocketTicket struct {
	BrowserSessionID string
	Version          uint64
	ExpiresAt        time.Time
}

// Отдельная запись сохраняет совместимость JSON семейства со старыми Pods.
// Только digest билета хранится в encrypted CAS ledger; tokens OIDC не выдаются.
type websocketTicketLedger struct {
	FamilyID string
	Tickets  map[string]websocketTicket
}

func (families *Families) IssueWebSocketTicket(ctx context.Context, familyID, browserID, csrfHash string) (string, time.Time, error) {
	token, err := randomOpaque()
	if err != nil {
		return "", time.Time{}, err
	}
	for range 3 {
		family, err := families.ticketFamily(ctx, familyID, browserID, csrfHash)
		if err != nil {
			return "", time.Time{}, err
		}
		ledger, sequence, err := families.readTicketLedger(ctx, familyID)
		if err != nil {
			return "", time.Time{}, err
		}
		now := families.now().UTC()
		for key, ticket := range ledger.Tickets {
			if !ticket.ExpiresAt.After(now) || ticket.Version != family.Version || ticket.BrowserSessionID != browserID {
				delete(ledger.Tickets, key)
			}
		}
		if len(ledger.Tickets) >= maximumWebSocketTickets {
			return "", time.Time{}, ErrRenewalPending
		}
		expires := earlier(now.Add(websocketTicketLifetime), earlier(family.IdleExpiresAt, family.Principal.ExpiresAt))
		ledger.Tickets[ticketDigest(token)] = websocketTicket{BrowserSessionID: browserID, Version: family.Version, ExpiresAt: expires}
		err = families.writeTicketLedger(ctx, ledger, sequence)
		if errors.Is(err, browserstate.ErrConflict) {
			continue
		}
		if err != nil {
			return "", time.Time{}, err
		}
		return token, expires, nil
	}
	return "", time.Time{}, ErrRenewalPending
}

func (families *Families) ConsumeWebSocketTicket(ctx context.Context, familyID, browserID, csrfHash, token string) error {
	if len(token) != 43 {
		return ErrReauthentication
	}
	for range 3 {
		family, err := families.ticketFamily(ctx, familyID, browserID, csrfHash)
		if err != nil {
			return err
		}
		ledger, sequence, err := families.readTicketLedger(ctx, familyID)
		if err != nil {
			return err
		}
		key := ticketDigest(token)
		ticket, found := ledger.Tickets[key]
		if !found || ticket.BrowserSessionID != browserID || ticket.Version != family.Version || !ticket.ExpiresAt.After(families.now()) {
			return ErrReauthentication
		}
		delete(ledger.Tickets, key)
		err = families.writeTicketLedger(ctx, ledger, sequence)
		if errors.Is(err, browserstate.ErrConflict) {
			continue
		}
		if err != nil {
			return err
		}
		current, err := families.ticketFamily(ctx, familyID, browserID, csrfHash)
		if err != nil {
			return err
		}
		if current.Version != ticket.Version || !ticket.ExpiresAt.After(families.now()) {
			return ErrReauthentication
		}
		return nil
	}
	return ErrRenewalPending
}

func (families *Families) ticketFamily(ctx context.Context, familyID, browserID, csrfHash string) (Family, error) {
	family, err := families.Read(ctx, familyID)
	if err != nil {
		return Family{}, err
	}
	if family.State != familyActive || family.BrowserSessionID != browserID || family.CSRFHash != csrfHash || !family.Principal.ExpiresAt.After(families.now()) {
		return Family{}, ErrReauthentication
	}
	return family, nil
}

func ticketDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func ticketLedgerID(familyID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(websocketTicketAAD+familyID)).String()
}

func (families *Families) readTicketLedger(ctx context.Context, familyID string) (websocketTicketLedger, uint64, error) {
	id := ticketLedgerID(familyID)
	record, err := families.store.Read(ctx, id)
	if errors.Is(err, browserstate.ErrNotFound) {
		return websocketTicketLedger{FamilyID: familyID, Tickets: map[string]websocketTicket{}}, 0, nil
	}
	if err != nil {
		return websocketTicketLedger{}, 0, err
	}
	decrypt := func(key cipher.AEAD) ([]byte, error) {
		if key == nil || len(record.Ciphertext) < key.NonceSize()+key.Overhead() {
			return nil, ErrReauthentication
		}
		return key.Open(nil, record.Ciphertext[:key.NonceSize()], record.Ciphertext[key.NonceSize():], []byte(websocketTicketAAD+id))
	}
	plain, err := decrypt(families.keys.current)
	if err != nil {
		plain, err = decrypt(families.keys.previous)
	}
	if err != nil {
		return websocketTicketLedger{}, 0, ErrReauthentication
	}
	var ledger websocketTicketLedger
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&ledger) != nil || decoder.Decode(new(any)) != io.EOF || ledger.FamilyID != familyID || ledger.Tickets == nil || len(ledger.Tickets) > maximumWebSocketTickets || record.Sequence == 0 {
		return websocketTicketLedger{}, 0, ErrReauthentication
	}
	return ledger, record.Sequence, nil
}

func (families *Families) writeTicketLedger(ctx context.Context, ledger websocketTicketLedger, sequence uint64) error {
	plain, err := json.Marshal(ledger)
	if err != nil {
		return browserstate.ErrUnavailable
	}
	id := ticketLedgerID(ledger.FamilyID)
	nonce := make([]byte, families.keys.current.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return browserstate.ErrUnavailable
	}
	sealed := families.keys.current.Seal(nonce, nonce, plain, []byte(websocketTicketAAD+id))
	_, err = families.store.CompareAndSwap(ctx, id, sequence, sealed)
	return err
}
