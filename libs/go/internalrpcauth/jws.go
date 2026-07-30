package internalrpcauth

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gowebpki/jcs"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
)

const (
	AlgorithmES256     = "ES256"
	CriticalHeader     = "mcxv"
	ContractVersion    = 1
	MaxCompactJWSBytes = 8192
)

var (
	ErrMalformedJWS     = errors.New("malformed compact JWS")
	ErrProtectedHeader  = errors.New("invalid protected header")
	ErrSignature        = errors.New("invalid JWS signature")
	ErrCanonicalPayload = errors.New("non-canonical JWS payload")
	ErrKey              = errors.New("invalid ES256 key")
)

type ProtectedHeaderExpectation struct {
	Type  string
	KeyID string
}

type ProtectedHeader struct {
	Type  string
	KeyID string
}

type VerifiedJWS struct {
	CanonicalPayload []byte
	KeyID            string
}

type ES256Key struct {
	KeyID   string
	Public  *ecdsa.PublicKey
	Private *ecdsa.PrivateKey
}

func (key ES256Key) PublicOnly() ES256Key {
	return ES256Key{KeyID: key.KeyID, Public: key.Public}
}

func SignCanonicalJSON(
	value any,
	key ES256Key,
	expect ProtectedHeaderExpectation,
) (string, error) {
	if err := validateES256Key(key, expect.KeyID, true); err != nil {
		return "", err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal canonical payload input: %w", err)
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return "", fmt.Errorf("canonicalize JWS payload: %w", err)
	}
	headers := jws.NewHeaders()
	for name, value := range map[string]any{
		jws.TypeKey:     expect.Type,
		jws.KeyIDKey:    expect.KeyID,
		jws.CriticalKey: []string{CriticalHeader},
		CriticalHeader:  ContractVersion,
	} {
		if err := headers.Set(name, value); err != nil {
			return "", fmt.Errorf("set protected header: %w", err)
		}
	}
	serialized, err := jws.Sign(
		canonical,
		jws.WithKey(
			jwa.ES256(),
			key.Private,
			jws.WithProtectedHeaders(headers),
		),
	)
	if err != nil {
		return "", fmt.Errorf("sign compact ES256 JWS: %w", err)
	}
	compact := string(serialized)
	if len(compact) > MaxCompactJWSBytes {
		return "", fmt.Errorf("%w: compact JWS exceeds limit", ErrMalformedJWS)
	}
	return compact, nil
}

func VerifyCanonicalJSON(
	compact string,
	key ES256Key,
	expect ProtectedHeaderExpectation,
) (VerifiedJWS, error) {
	header, err := ParseProtectedHeader(compact)
	if err != nil {
		return VerifiedJWS{}, err
	}
	if header.Type != expect.Type || header.KeyID != expect.KeyID {
		return VerifiedJWS{}, ErrProtectedHeader
	}
	if err := validateES256Key(key, expect.KeyID, false); err != nil {
		return VerifiedJWS{}, err
	}
	payload, err := jws.Verify(
		[]byte(compact),
		jws.WithKey(jwa.ES256(), key.Public),
		jws.WithCritValidation(true),
		jws.WithCritExtension(CriticalHeader),
	)
	if err != nil {
		return VerifiedJWS{}, fmt.Errorf("%w: %v", ErrSignature, err)
	}
	if err := ensureStrictJSONObject(payload); err != nil {
		return VerifiedJWS{}, fmt.Errorf("%w: %v", ErrCanonicalPayload, err)
	}
	canonicalPayload, err := jcs.Transform(payload)
	if err != nil || !bytes.Equal(canonicalPayload, payload) {
		return VerifiedJWS{}, ErrCanonicalPayload
	}
	return VerifiedJWS{CanonicalPayload: payload, KeyID: header.KeyID}, nil
}

// ParseProtectedHeader строго разбирает только канонический защищённый header.
// Результат не подтверждает подпись и используется лишь для выбора заранее
// доверенного ключа перед VerifyCanonicalJSON.
func ParseProtectedHeader(compact string) (ProtectedHeader, error) {
	if len(compact) == 0 || len(compact) > MaxCompactJWSBytes {
		return ProtectedHeader{}, ErrMalformedJWS
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return ProtectedHeader{}, ErrMalformedJWS
	}
	protected, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ProtectedHeader{}, ErrMalformedJWS
	}
	var header strictProtectedHeader
	if err := decodeStrictJSON(protected, &header); err != nil {
		return ProtectedHeader{}, fmt.Errorf("%w: %v", ErrProtectedHeader, err)
	}
	if header.Alg != AlgorithmES256 ||
		header.Type == "" ||
		header.KeyID == "" ||
		header.Version != ContractVersion ||
		len(header.Critical) != 1 ||
		header.Critical[0] != CriticalHeader {
		return ProtectedHeader{}, ErrProtectedHeader
	}
	canonicalHeader, err := jcs.Transform(protected)
	if err != nil || !bytes.Equal(canonicalHeader, protected) {
		return ProtectedHeader{}, ErrProtectedHeader
	}
	return ProtectedHeader{Type: header.Type, KeyID: header.KeyID}, nil
}

// DecodeCanonicalJSON декодирует уже проверенный JWS payload и повторно
// подтверждает строгий JSON/JCS boundary перед использованием claims.
func DecodeCanonicalJSON(data []byte, target any) error {
	if err := decodeStrictJSON(data, target); err != nil {
		return err
	}
	canonical, err := jcs.Transform(data)
	if err != nil {
		return fmt.Errorf("canonicalize JSON payload: %w", err)
	}
	if !bytes.Equal(canonical, data) {
		return ErrCanonicalPayload
	}
	return nil
}

// CanonicalJSON возвращает JCS-представление JSON для устойчивого хранения
// подписываемого или сравниваемого состояния.
func CanonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical JSON input: %w", err)
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return nil, fmt.Errorf("canonicalize JSON input: %w", err)
	}
	return canonical, nil
}

func CanonicalJSONSHA256(value any) (string, error) {
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	digest := crypto.SHA256.New()
	if _, err := digest.Write(canonical); err != nil {
		return "", fmt.Errorf("hash canonical digest input: %w", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func ParsePublicJWK(data []byte) (ES256Key, error) {
	return parseJWK(data, false)
}

func ParsePrivateJWK(data []byte) (ES256Key, error) {
	return parseJWK(data, true)
}

func GenerateES256Key(keyID string) (ES256Key, error) {
	if keyID == "" || len(keyID) > 64 {
		return ES256Key{}, ErrKey
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return ES256Key{}, fmt.Errorf("generate ES256 key: %w", err)
	}
	return ES256Key{
		KeyID:   keyID,
		Public:  &privateKey.PublicKey,
		Private: privateKey,
	}, nil
}

func MarshalPublicJWK(key ES256Key) ([]byte, error) {
	return marshalJWK(key, false)
}

func MarshalPrivateJWK(key ES256Key) ([]byte, error) {
	return marshalJWK(key, true)
}

func PublicJWKThumbprintSHA256(key ES256Key) (string, error) {
	if err := validateES256Key(key, key.KeyID, false); err != nil {
		return "", err
	}
	publicBytes, err := key.Public.Bytes()
	if err != nil || len(publicBytes) != 65 || publicBytes[0] != 4 {
		return "", ErrKey
	}
	canonical, err := jcs.Transform([]byte(fmt.Sprintf(
		`{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`,
		base64.RawURLEncoding.EncodeToString(publicBytes[1:33]),
		base64.RawURLEncoding.EncodeToString(publicBytes[33:65]),
	)))
	if err != nil {
		return "", fmt.Errorf("canonicalize JWK thumbprint input: %w", err)
	}
	digest := crypto.SHA256.New()
	if _, err := digest.Write(canonical); err != nil {
		return "", fmt.Errorf("hash JWK thumbprint input: %w", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func marshalJWK(key ES256Key, includePrivate bool) ([]byte, error) {
	if err := validateES256Key(key, key.KeyID, includePrivate); err != nil {
		return nil, err
	}
	publicBytes, err := key.Public.Bytes()
	if err != nil || len(publicBytes) != 65 || publicBytes[0] != 4 {
		return nil, ErrKey
	}
	encoded := encodedJWK{
		KTY: "EC", Curve: "P-256", Use: "sig", Alg: AlgorithmES256,
		KeyID: key.KeyID,
		X:     base64.RawURLEncoding.EncodeToString(publicBytes[1:33]),
		Y:     base64.RawURLEncoding.EncodeToString(publicBytes[33:65]),
	}
	if includePrivate {
		encoded.KeyOps = []string{"sign"}
		privateBytes, privateErr := key.Private.Bytes()
		if privateErr != nil || len(privateBytes) != 32 {
			return nil, ErrKey
		}
		encoded.D = base64.RawURLEncoding.EncodeToString(privateBytes)
	} else {
		encoded.KeyOps = []string{"verify"}
	}
	raw, err := json.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("marshal ES256 JWK: %w", err)
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return nil, fmt.Errorf("canonicalize ES256 JWK: %w", err)
	}
	return canonical, nil
}

func ValidateTimes(now time.Time, issuedAt, notBefore, expiresAt time.Time, maxTTL, skew time.Duration) error {
	now = now.UTC().Truncate(time.Second)
	issuedAt = issuedAt.UTC().Truncate(time.Second)
	notBefore = notBefore.UTC().Truncate(time.Second)
	expiresAt = expiresAt.UTC().Truncate(time.Second)
	if maxTTL <= 0 || skew < 0 ||
		!notBefore.Equal(issuedAt) ||
		!expiresAt.Equal(issuedAt.Add(maxTTL)) {
		return errors.New("invalid token lifetime")
	}
	if issuedAt.After(now) {
		return errors.New("token issued-at is in the future")
	}
	if now.Add(skew).Before(notBefore) {
		return errors.New("token is not yet valid")
	}
	if !now.Add(-skew).Before(expiresAt) {
		return errors.New("token is expired")
	}
	return nil
}

type strictProtectedHeader struct {
	Alg      string   `json:"alg"`
	Type     string   `json:"typ"`
	KeyID    string   `json:"kid"`
	Critical []string `json:"crit"`
	Version  int      `json:"mcxv"`
}

type encodedJWK struct {
	KTY    string   `json:"kty"`
	Curve  string   `json:"crv"`
	Use    string   `json:"use"`
	KeyOps []string `json:"key_ops"`
	Alg    string   `json:"alg"`
	KeyID  string   `json:"kid"`
	X      string   `json:"x"`
	Y      string   `json:"y"`
	D      string   `json:"d,omitempty"`
}

func parseJWK(data []byte, requirePrivate bool) (ES256Key, error) {
	var encoded encodedJWK
	if err := decodeStrictJSON(data, &encoded); err != nil {
		return ES256Key{}, fmt.Errorf("%w: %v", ErrKey, err)
	}
	if encoded.KTY != "EC" || encoded.Curve != "P-256" ||
		encoded.Use != "sig" || encoded.Alg != AlgorithmES256 ||
		encoded.KeyID == "" || len(encoded.KeyOps) != 1 ||
		(requirePrivate && encoded.KeyOps[0] != "sign") ||
		(!requirePrivate && encoded.KeyOps[0] != "verify") {
		return ES256Key{}, ErrKey
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(encoded.X)
	if err != nil || len(xBytes) != 32 {
		return ES256Key{}, ErrKey
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(encoded.Y)
	if err != nil || len(yBytes) != 32 {
		return ES256Key{}, ErrKey
	}
	encodedPublic := make([]byte, 1+len(xBytes)+len(yBytes))
	encodedPublic[0] = 4
	copy(encodedPublic[1:33], xBytes)
	copy(encodedPublic[33:], yBytes)
	publicKey, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), encodedPublic)
	if err != nil {
		return ES256Key{}, ErrKey
	}
	result := ES256Key{KeyID: encoded.KeyID, Public: publicKey}
	if requirePrivate {
		dBytes, err := base64.RawURLEncoding.DecodeString(encoded.D)
		if err != nil || len(dBytes) == 0 || len(dBytes) > 32 {
			return ES256Key{}, ErrKey
		}
		rawPrivate := make([]byte, 32)
		copy(rawPrivate[len(rawPrivate)-len(dBytes):], dBytes)
		privateKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), rawPrivate)
		if err != nil {
			return ES256Key{}, ErrKey
		}
		derivedPublic, err := privateKey.PublicKey.Bytes()
		if err != nil || !bytes.Equal(derivedPublic, encodedPublic) {
			return ES256Key{}, ErrKey
		}
		result.Private = privateKey
	}
	return result, nil
}

func validateES256Key(key ES256Key, expectedKeyID string, requirePrivate bool) error {
	if key.Public == nil || key.Public.Curve != elliptic.P256() ||
		key.KeyID != expectedKeyID || key.KeyID == "" {
		return ErrKey
	}
	publicBytes, err := key.Public.Bytes()
	if err != nil {
		return ErrKey
	}
	if requirePrivate {
		if key.Private == nil {
			return ErrKey
		}
		if _, err := key.Private.Bytes(); err != nil {
			return ErrKey
		}
		privatePublicBytes, err := key.Private.PublicKey.Bytes()
		if err != nil || !bytes.Equal(privatePublicBytes, publicBytes) {
			return ErrKey
		}
	}
	return nil
}
