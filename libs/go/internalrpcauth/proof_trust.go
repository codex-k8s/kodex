package internalrpcauth

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const (
	// AuthorityProofTrustPurpose separates authority-proof signer keys from
	// other internal RPC authority key sets in the trust document.
	// RU: AuthorityProofTrustPurpose отделяет ключи подписантов authority proof
	// от остальных наборов ключей internal RPC authority.
	AuthorityProofTrustPurpose = "AUTHORITY_PROOF_VERIFICATION"
	// maximumAuthorityProofTrustKeys bounds the number of signer keys accepted
	// from one trust snapshot before issuer and status validation.
	// RU: maximumAuthorityProofTrustKeys ограничивает число ключей подписантов
	// одного trust-снимка до проверки issuer и status.
	maximumAuthorityProofTrustKeys = 32
	// maximumTrustHistoryEntries bounds the forward-only predecessor window
	// carried in an authority-proof trust document.
	// RU: maximumTrustHistoryEntries ограничивает окно предшественников
	// forward-only цепочки в trust-документе authority proof.
	maximumTrustHistoryEntries = 32
	// zeroSHA256 is the required predecessor digest for the first trust revision.
	// RU: zeroSHA256 — обязательный digest предшественника первой trust-ревизии.
	zeroSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
)

// TrustRevisionDigest identifies one revision and exact content digest in
// the bounded forward-only history of authority-proof trust snapshots.
// RU: TrustRevisionDigest задаёт ревизию и точный digest содержимого одного
// звена ограниченной forward-only истории trust-снимков authority proof.
type TrustRevisionDigest struct {
	Revision     uint64 `json:"revision"`
	DigestSHA256 string `json:"digest_sha256"`
}

// AuthorityProofTrustKey describes one workload signer key, issuer, allowed
// audiences and validity interval for short-lived authority proofs.
// RU: AuthorityProofTrustKey описывает ключ подписанта workload, issuer,
// допустимые аудитории и срок для короткоживущих authority proof.
type AuthorityProofTrustKey struct {
	Issuer     string          `json:"issuer"`
	Generation uint64          `json:"generation"`
	Status     string          `json:"status"`
	Purpose    string          `json:"purpose"`
	Audiences  []string        `json:"audiences"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
	JWK        json.RawMessage `json:"jwk"`
}

// AuthorityProofTrustDocument is the canonical publisher/consumer contract
// for authority-proof keys and their source revision, digest and history.
// RU: AuthorityProofTrustDocument — канонический контракт publisher и
// consumers для ключей authority proof, ревизии, digest и истории источника.
type AuthorityProofTrustDocument struct {
	Version        int                      `json:"v"`
	Purpose        string                   `json:"purpose"`
	SourceRevision uint64                   `json:"source_revision"`
	SourceDigest   string                   `json:"source_digest_sha256"`
	Predecessor    TrustRevisionDigest      `json:"predecessor"`
	History        []TrustRevisionDigest    `json:"history"`
	Keys           []AuthorityProofTrustKey `json:"keys"`
}

// DecodeAuthorityProofTrustDocument verifies canonical JSON, bounded history,
// key metadata and one CURRENT key per issuer; a consumer must still validate
// its exact issuer, audience and time before accepting a proof.
// RU: DecodeAuthorityProofTrustDocument проверяет канонический JSON,
// ограниченную историю, свойства ключей и один CURRENT на issuer; consumer
// отдельно проверяет точный issuer, audience и время перед допуском proof.
func DecodeAuthorityProofTrustDocument(data []byte) (AuthorityProofTrustDocument, error) {
	var document AuthorityProofTrustDocument
	if err := DecodeCanonicalJSON(data, &document); err != nil {
		return AuthorityProofTrustDocument{}, errors.New("decode authority proof trust document")
	}
	if document.Version != ContractVersion ||
		document.Purpose != AuthorityProofTrustPurpose ||
		document.SourceRevision == 0 ||
		!validSHA256(document.SourceDigest) ||
		len(document.Keys) == 0 ||
		len(document.Keys) > maximumAuthorityProofTrustKeys {
		return AuthorityProofTrustDocument{}, errors.New("authority proof trust document metadata is invalid")
	}
	if err := validateTrustHistory(document.SourceRevision, document.Predecessor, document.History); err != nil {
		return AuthorityProofTrustDocument{}, err
	}
	keyIDs := make(map[string]struct{}, len(document.Keys))
	currentByIssuer := make(map[string]int)
	issuers := make(map[string]struct{})
	for _, key := range document.Keys {
		if key.Issuer == "" || key.Generation == 0 ||
			(key.Status != "CURRENT" && key.Status != "NEXT" && key.Status != "PREVIOUS") ||
			key.Purpose != "AUTHORITY_PROOF" || len(key.Audiences) == 0 ||
			key.NotBefore >= key.NotAfter {
			return AuthorityProofTrustDocument{}, errors.New("authority proof trust key metadata is invalid")
		}
		seenAudiences := make(map[string]struct{}, len(key.Audiences))
		for _, audience := range key.Audiences {
			if audience == "" {
				return AuthorityProofTrustDocument{}, errors.New("authority proof trust key audience is invalid")
			}
			if _, duplicate := seenAudiences[audience]; duplicate {
				return AuthorityProofTrustDocument{}, errors.New("authority proof trust key audience is duplicated")
			}
			seenAudiences[audience] = struct{}{}
		}
		parsed, err := ParsePublicJWK(key.JWK)
		if err != nil {
			return AuthorityProofTrustDocument{}, errors.New("authority proof trust public key is invalid")
		}
		if _, duplicate := keyIDs[parsed.KeyID]; duplicate {
			return AuthorityProofTrustDocument{}, errors.New("authority proof trust key id is duplicated")
		}
		keyIDs[parsed.KeyID] = struct{}{}
		issuers[key.Issuer] = struct{}{}
		if key.Status == "CURRENT" {
			currentByIssuer[key.Issuer]++
		}
	}
	for issuer := range issuers {
		if currentByIssuer[issuer] != 1 {
			return AuthorityProofTrustDocument{}, errors.New("authority proof trust issuer must have exactly one CURRENT key")
		}
	}
	return document, nil
}

// validateTrustHistory enforces an unbroken bounded predecessor chain, with
// a zero-digest bootstrap and the final history entry equal to predecessor.
// RU: validateTrustHistory требует непрерывную ограниченную цепочку, нулевой
// digest при bootstrap и совпадение последнего звена с predecessor.
func validateTrustHistory(
	sourceRevision uint64,
	predecessor TrustRevisionDigest,
	history []TrustRevisionDigest,
) error {
	if len(history) > maximumTrustHistoryEntries {
		return errors.New("authority proof trust history exceeds the bounded window")
	}
	if sourceRevision == 1 {
		if predecessor.Revision != 0 || predecessor.DigestSHA256 != zeroSHA256 || len(history) != 0 {
			return errors.New("authority proof trust bootstrap history is invalid")
		}
		return nil
	}
	if predecessor.Revision != sourceRevision-1 ||
		!validSHA256(predecessor.DigestSHA256) ||
		len(history) == 0 || uint64(len(history)) >= sourceRevision {
		return errors.New("authority proof trust predecessor is invalid")
	}
	firstRevision := sourceRevision - uint64(len(history))
	for index, entry := range history {
		if entry.Revision != firstRevision+uint64(index) || !validSHA256(entry.DigestSHA256) {
			return errors.New("authority proof trust history is gapped or malformed")
		}
	}
	last := history[len(history)-1]
	if last != predecessor {
		return errors.New("authority proof trust history does not end at the predecessor")
	}
	return nil
}

// validSHA256 accepts only a 64-character lowercase hexadecimal digest for
// source and predecessor references in trust documents.
// RU: validSHA256 принимает для ссылок на source и predecessor только
// 64-символьный digest SHA-256 в нижнем регистре hex.
func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	if strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
