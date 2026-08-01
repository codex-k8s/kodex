// Package file реализует подписывающий компонент доказательств полномочий с
// независимо доставленным доверием.
package file

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/proofsigner"
	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
)

const (
	maximumFileBytes = 64 << 10
	proofType        = "mattercodex-internal-rpc-authority-proof+jws"
)

// Config фиксирует неизменяемое намерение доверия для одного pod.
type Config struct {
	PrivateJWKFile   string
	TrustFile        string
	Issuer           string
	Audience         string
	Generation       uint64
	MaximumClockSkew time.Duration
}

// Signer хранит только проверенный ключ; исходные bytes не сохраняются.
type Signer struct {
	config        Config
	key           internalrpcauth.ES256Key
	state         proofsigner.State
	notBefore     time.Time
	notAfter      time.Time
	privateDigest string
	trustDigest   string
	now           func() time.Time
	mu            sync.RWMutex
}

type trustDocument struct {
	Version        int              `json:"v"`
	Purpose        string           `json:"purpose"`
	SourceRevision uint64           `json:"source_revision"`
	SourceDigest   string           `json:"source_digest_sha256"`
	Predecessor    revisionDigest   `json:"predecessor"`
	History        []revisionDigest `json:"history"`
	Keys           []trustKey       `json:"keys"`
}

type revisionDigest struct {
	Revision uint64 `json:"revision"`
	Digest   string `json:"digest_sha256"`
}

type trustKey struct {
	Issuer     string          `json:"issuer"`
	Generation uint64          `json:"generation"`
	Status     string          `json:"status"`
	Purpose    string          `json:"purpose"`
	Audiences  []string        `json:"audiences"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
	JWK        json.RawMessage `json:"jwk"`
}

// New загружает закрытый ключ и независимо доставленное доверие одним
// стартовым барьером.
func New(config Config) (*Signer, error) {
	if config.PrivateJWKFile == "" || config.TrustFile == "" ||
		config.Issuer == "" || config.Audience == "" ||
		config.Generation == 0 ||
		config.MaximumClockSkew < 0 ||
		config.MaximumClockSkew > 30*time.Second {
		return nil, errors.New("proof signer configuration is invalid")
	}
	privateRaw, privateDigest, err := readBounded(config.PrivateJWKFile)
	if err != nil {
		return nil, fmt.Errorf("read proof signer private key: %w", err)
	}
	key, err := internalrpcauth.ParsePrivateJWK(privateRaw)
	if err != nil {
		return nil, fmt.Errorf("parse proof signer private key: %w", err)
	}
	trustRaw, trustDigest, err := readBounded(config.TrustFile)
	if err != nil {
		return nil, fmt.Errorf("read proof trust: %w", err)
	}
	document, selected, err := verifyTrust(
		trustRaw,
		key,
		config,
		time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(key.PublicOnly())
	if err != nil {
		return nil, fmt.Errorf("calculate proof key thumbprint: %w", err)
	}
	return &Signer{
		config: config,
		key:    key,
		state: proofsigner.State{
			TrustRevision:    document.SourceRevision,
			TrustDigest:      document.SourceDigest,
			SignerGeneration: selected.Generation,
			PublicThumbprint: thumbprint,
		},
		notBefore:     time.Unix(selected.NotBefore, 0).UTC(),
		notAfter:      time.Unix(selected.NotAfter, 0).UTC(),
		privateDigest: privateDigest,
		trustDigest:   trustDigest,
		now:           time.Now,
	}, nil
}

// Sign подписывает запросы тем же снимком ключа и состояния без TOCTOU.
func (signer *Signer) Sign(
	ctx context.Context,
	claims authoritytype.ProofClaims,
) (string, string, proofsigner.State, error) {
	if err := ctx.Err(); err != nil {
		return "", "", proofsigner.State{}, err
	}
	signer.mu.RLock()
	defer signer.mu.RUnlock()
	if err := signer.checkFilesAndTime(); err != nil {
		return "", "", proofsigner.State{}, err
	}
	claims.SignerGeneration = signer.state.SignerGeneration
	compact, err := internalrpcauth.SignCanonicalJSON(
		claims,
		signer.key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  proofType,
			KeyID: signer.key.KeyID,
		},
	)
	if err != nil {
		return "", "", proofsigner.State{}, err
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(claims)
	if err != nil {
		return "", "", proofsigner.State{}, err
	}
	return compact, digest, signer.state, nil
}

// Check проверяет served key, trust files и срок действия.
func (signer *Signer) Check(ctx context.Context) (proofsigner.State, error) {
	if err := ctx.Err(); err != nil {
		return proofsigner.State{}, err
	}
	signer.mu.RLock()
	defer signer.mu.RUnlock()
	if err := signer.checkFilesAndTime(); err != nil {
		return proofsigner.State{}, err
	}
	return signer.state, nil
}

func (signer *Signer) checkFilesAndTime() error {
	now := signer.now().UTC()
	if now.Add(signer.config.MaximumClockSkew).Before(signer.notBefore) ||
		!now.Add(-signer.config.MaximumClockSkew).Before(signer.notAfter) {
		return errors.New("proof signer key is outside its validity window")
	}
	_, privateDigest, err := readBounded(signer.config.PrivateJWKFile)
	if err != nil || privateDigest != signer.privateDigest {
		return errors.New("served proof signer private key changed")
	}
	_, trustDigest, err := readBounded(signer.config.TrustFile)
	if err != nil || trustDigest != signer.trustDigest {
		return errors.New("served proof trust changed")
	}
	return nil
}

func verifyTrust(
	raw []byte,
	privateKey internalrpcauth.ES256Key,
	config Config,
	now time.Time,
) (trustDocument, trustKey, error) {
	var document trustDocument
	if err := decodeStrict(raw, &document); err != nil {
		return trustDocument{}, trustKey{}, fmt.Errorf("decode proof trust: %w", err)
	}
	if document.Version != 1 ||
		document.Purpose != "AUTHORITY_PROOF_VERIFICATION" ||
		document.SourceRevision == 0 ||
		!validDigest(document.SourceDigest) ||
		len(document.Keys) < 2 || len(document.Keys) > 32 ||
		len(document.History) > 32 ||
		document.Predecessor.Revision >= document.SourceRevision ||
		(document.Predecessor.Revision > 0 && !validDigest(document.Predecessor.Digest)) {
		return trustDocument{}, trustKey{}, errors.New("proof trust metadata is invalid")
	}
	for _, predecessor := range document.History {
		if predecessor.Revision >= document.SourceRevision ||
			(predecessor.Revision > 0 && !validDigest(predecessor.Digest)) {
			return trustDocument{}, trustKey{}, errors.New("proof trust history is invalid")
		}
	}
	privateThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(
		privateKey.PublicOnly(),
	)
	if err != nil {
		return trustDocument{}, trustKey{}, err
	}
	var selected trustKey
	matches := 0
	identities := make(map[string]struct{}, len(document.Keys))
	for _, candidate := range document.Keys {
		identity := fmt.Sprintf("%s/%d/%s", candidate.Issuer, candidate.Generation, candidate.Status)
		if _, duplicate := identities[identity]; duplicate {
			return trustDocument{}, trustKey{}, errors.New("proof trust key identity is ambiguous")
		}
		identities[identity] = struct{}{}
		if candidate.Generation == 0 ||
			(candidate.Status != "CURRENT" &&
				candidate.Status != "NEXT" &&
				candidate.Status != "PREVIOUS") ||
			candidate.Purpose != "AUTHORITY_PROOF" ||
			candidate.NotBefore < 0 ||
			candidate.NotAfter <= candidate.NotBefore {
			return trustDocument{}, trustKey{}, errors.New("proof trust key is invalid")
		}
		publicKey, err := internalrpcauth.ParsePublicJWK(candidate.JWK)
		if err != nil {
			return trustDocument{}, trustKey{}, fmt.Errorf("parse proof trust public key: %w", err)
		}
		if candidate.Issuer != config.Issuer ||
			candidate.Generation != config.Generation ||
			candidate.Status != "CURRENT" ||
			!slices.Contains(candidate.Audiences, config.Audience) {
			continue
		}
		publicThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(publicKey)
		if err != nil || publicThumbprint != privateThumbprint ||
			publicKey.KeyID != privateKey.KeyID {
			return trustDocument{}, trustKey{}, errors.New("proof signer does not match independently delivered trust")
		}
		if now.Add(config.MaximumClockSkew).Before(time.Unix(candidate.NotBefore, 0)) ||
			!now.Add(-config.MaximumClockSkew).Before(time.Unix(candidate.NotAfter, 0)) {
			return trustDocument{}, trustKey{}, errors.New("proof signer trust is not currently valid")
		}
		selected = candidate
		matches++
	}
	if matches != 1 {
		return trustDocument{}, trustKey{}, errors.New("exact current proof signer is not trusted")
	}
	return document, selected, nil
}

func readBounded(path string) ([]byte, string, error) {
	if !filepath.IsAbs(path) {
		return nil, "", errors.New("proof material path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > maximumFileBytes || info.Mode().Perm()&0o007 != 0 {
		return nil, "", errors.New("proof material file is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maximumFileBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumFileBytes {
		return nil, "", errors.New("bounded proof material is invalid")
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}
