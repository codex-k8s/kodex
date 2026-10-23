package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	"github.com/google/uuid"
)

// websocketTicketLifetime caps a newly issued one-use upgrade ticket at thirty seconds, further clipped by the active family's idle and access expiry.
// websocketTicketLifetime ограничивает новый одноразовый upgrade ticket тридцатью секундами с дополнительным ограничением active family idle и access expiry.
const websocketTicketLifetime = 30 * time.Second

// maximumWebSocketTickets caps the encrypted family ledger at sixteen retained live/version-matching tickets; exhausted capacity returns pending rather than evicting another live tab's ticket.
// maximumWebSocketTickets ограничивает encrypted family ledger шестнадцатью сохранёнными live и version-matching tickets; при исчерпании возвращается pending без вытеснения живого ticket соседней вкладки.
const maximumWebSocketTickets = 16

// websocketTicketAAD is the fixed ledger domain separator used for its deterministic record ID and AEAD binding; ticket token values are not part of the storage key.
// websocketTicketAAD — фиксированный ledger domain separator для deterministic record ID и AEAD binding; значения ticket tokens не входят в storage key.
const websocketTicketAAD = "browser-ws-tickets-v1:"

// websocketTicket records browser ID, logical family version and expiry under a token digest in the encrypted ledger. It contains no raw ticket, OIDC token or independently usable grant.
// websocketTicket хранит browser ID, logical family version и expiry под token digest в encrypted ledger без raw ticket, OIDC token и самостоятельного пригодного grant.
type websocketTicket struct {
	BrowserSessionID string
	Version          uint64
	ExpiresAt        time.Time
}

// websocketTicketLedger is a separate encrypted owner record of one family's token-digest entries, keeping ticket mutations outside logical family Version. Its contents are not a public cookie or WebSocket URL payload.
// websocketTicketLedger — отдельная encrypted owner record token-digest entries одного family, не включающая ticket mutations в logical family Version. Содержимое не является public cookie и WebSocket URL payload.
type websocketTicketLedger struct {
	FamilyID string
	Tickets  map[string]websocketTicket
}

// IssueWebSocketTicket validates current family/browser/CSRF, prunes stale ledger entries and CAS-inserts one random ticket digest with clipped expiry within three attempts. It returns the raw token only after a successful write and does not renew or rewrite the family record.
// IssueWebSocketTicket проверяет current family, browser и CSRF, удаляет stale ledger entries и в пределах трёх попыток CAS добавляет один random ticket digest с ограниченным expiry. Raw token возвращается лишь после successful write без renewal и перезаписи family record.
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

// ConsumeWebSocketTicket requires exact token length and current family binding, CAS-deletes its matching live/version-bound digest before a final family recheck, and retries only bounded conflicts. A later refusal still leaves the ticket consumed; no WebSocket is opened by this method.
// ConsumeWebSocketTicket требует exact token length и current family binding, через CAS удаляет matching live и version-bound digest до итоговой повторной проверки family и повторяет лишь bounded conflicts. Поздний отказ оставляет ticket потреблённым; WebSocket этим методом не открывается.
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

// ticketFamily reads and validates the active family with exact browser/CSRF binding and a still-live access credential; it does not refresh credentials or extend idle/absolute deadlines.
// ticketFamily читает и проверяет active family с exact browser и CSRF binding и живым access credential без refresh credentials и продления idle и absolute deadlines.
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

// ticketDigest returns lowercase SHA-256 of the exact opaque token string for ledger lookup, without normalizing or validating its alphabet.
// ticketDigest возвращает lowercase SHA-256 точной opaque token string для ledger lookup без нормализации и проверки алфавита.
func ticketDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// ticketLedgerID preserves the deterministic namespace-OID SHA-1 UUID mapping over the ledger separator plus family ID. It identifies storage only and is neither a secret nor authority proof.
// ticketLedgerID сохраняет deterministic namespace-OID SHA-1 UUID mapping ledger separator и family ID. Обозначает лишь storage, не являясь секретом и authority proof.
func ticketLedgerID(familyID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(websocketTicketAAD+familyID)).String()
}

// readTicketLedger treats missing storage as an empty create-sequence ledger, otherwise decrypts with ledger-ID AAD and current/previous keys, validates JSON/identity/map/count/sequence and returns the exact storage sequence. Individual ticket freshness is checked by issue/consume, not this decoder.
// readTicketLedger считает отсутствие storage пустым create-sequence ledger, иначе расшифровывает с ledger-ID AAD и current и previous keys, проверяет JSON, identity, map, count и sequence и возвращает точный storage sequence. Freshness отдельных tickets проверяют issue и consume, а не decoder.
func (families *Families) readTicketLedger(ctx context.Context, familyID string) (websocketTicketLedger, uint64, error) {
	id := ticketLedgerID(familyID)
	record, err := families.store.Read(ctx, id)
	if errors.Is(err, browserstate.ErrNotFound) {
		return websocketTicketLedger{FamilyID: familyID, Tickets: map[string]websocketTicket{}}, 0, nil
	}
	if err != nil {
		return websocketTicketLedger{}, 0, err
	}
	plain, err := families.keys.openState(record.Ciphertext, websocketTicketAAD+id)
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

// writeTicketLedger JSON-encodes and encrypts the supplied ledger with exact deterministic record-ID AAD, then performs one CAS at the given sequence. It does not validate all entries again, modify family state or retry conflicts internally.
// writeTicketLedger кодирует и шифрует переданный ledger с exact deterministic record-ID AAD, затем делает один CAS по переданному sequence. Все entries повторно не проверяются, family state не меняется и conflicts внутри не повторяются.
func (families *Families) writeTicketLedger(ctx context.Context, ledger websocketTicketLedger, sequence uint64) error {
	plain, err := json.Marshal(ledger)
	if err != nil {
		return browserstate.ErrUnavailable
	}
	id := ticketLedgerID(ledger.FamilyID)
	sealed, err := families.keys.sealState(plain, websocketTicketAAD+id)
	if err != nil {
		return err
	}
	_, err = families.store.CompareAndSwap(ctx, id, sequence, sealed)
	return err
}
