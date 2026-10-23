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
	"strconv"
	"strings"
	"time"

	"github.com/gowebpki/jcs"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
)

const (
	// AlgorithmES256 pins the only accepted JWS signing algorithm for this RPC contract.
	// RU: AlgorithmES256 закрепляет единственный допустимый алгоритм подписи JWS.
	AlgorithmES256 = "ES256"
	// CriticalHeader names the mandatory protected contract-version header.
	// RU: CriticalHeader называет обязательный защищённый header версии контракта.
	CriticalHeader = "mcxv"
	// ContractVersion is the supported value of the critical JWS header.
	// RU: ContractVersion — поддерживаемое значение критического JWS header.
	ContractVersion = 1
	// AuthorityABIVersion pins the expected authority request/response ABI.
	// RU: AuthorityABIVersion закрепляет ожидаемый ABI authority-запросов и ответов.
	AuthorityABIVersion = 2
	// RequestBindingUnary binds a unary call to the hash of its exact protobuf request.
	// RU: RequestBindingUnary связывает unary-вызов с хешем точного protobuf-запроса.
	RequestBindingUnary = "UNARY_PROTO_SHA256"
	// RequestBindingStream binds a streaming call to its server-owned session.
	// RU: RequestBindingStream связывает потоковый вызов с серверной сессией.
	RequestBindingStream = "STREAM_SESSION"
	// ProfileBindingRequired requires a profile field in the signed authority contract.
	// RU: ProfileBindingRequired требует поле профиля в подписанном authority-контракте.
	ProfileBindingRequired = "REQUIRED"
	// ProfileBindingForbidden rejects a profile field that the contract does not allow.
	// RU: ProfileBindingForbidden отклоняет поле профиля, запрещённое контрактом.
	ProfileBindingForbidden = "FORBIDDEN"
	// MaxCompactJWSBytes bounds the ordinary compact token before parsing it.
	// RU: MaxCompactJWSBytes ограничивает обычный compact-токен до его разбора.
	MaxCompactJWSBytes = 8192
	// AuthorizationContextProtectedType separates authorization-context JWS from other signed kinds.
	// RU: AuthorizationContextProtectedType отличает JWS контекста авторизации от иных подписанных видов.
	AuthorizationContextProtectedType = "kodex-internal-rpc-auth+jws"
	// AuthorityProofProtectedType separates short-lived authority proofs from authorization contexts.
	// RU: AuthorityProofProtectedType отличает короткоживущий authority proof от контекста авторизации.
	AuthorityProofProtectedType = "kodex-internal-rpc-authority-proof+jws"
)

var (
	// ErrMalformedJWS classifies a structurally invalid or oversized compact token.
	// RU: ErrMalformedJWS обозначает неверную структуру или размер compact-токена.
	ErrMalformedJWS = errors.New("malformed compact JWS")
	// ErrProtectedHeader classifies a noncanonical, unknown or mismatched protected header.
	// RU: ErrProtectedHeader обозначает неканонический, неизвестный либо несовпадающий защищённый header.
	ErrProtectedHeader = errors.New("invalid protected header")
	// ErrSignature classifies a failed ES256 signature verification.
	// RU: ErrSignature обозначает неудачную проверку подписи ES256.
	ErrSignature = errors.New("invalid JWS signature")
	// ErrCanonicalPayload classifies payload JSON that fails strict JCS validation.
	// RU: ErrCanonicalPayload обозначает payload JSON, не прошедший строгую проверку JCS.
	ErrCanonicalPayload = errors.New("non-canonical JWS payload")
	// ErrKey classifies malformed, mismatched or unusable ES256 key material.
	// RU: ErrKey обозначает неверный, несовпадающий либо непригодный ключ ES256.
	ErrKey = errors.New("invalid ES256 key")
)

// ProtectedHeaderExpectation pins the exact typ and kid that a signer or
// verifier accepts independently of any values supplied by a token.
// RU: ProtectedHeaderExpectation закрепляет точные typ и kid для подписи или
// проверки независимо от значений, переданных самим токеном.
type ProtectedHeaderExpectation struct {
	Type  string
	KeyID string
}

// ProtectedHeader contains strictly parsed typ and kid for trusted-key
// selection only; parsing this header does not verify the signature.
// RU: ProtectedHeader хранит строго разобранные typ и kid только для выбора
// доверенного ключа; разбор header не подтверждает подпись.
type ProtectedHeader struct {
	Type  string
	KeyID string
}

// VerifiedJWS carries canonical payload bytes and kid only after protected
// header, ES256 signature and strict JCS validation succeed.
// RU: VerifiedJWS содержит канонические байты payload и kid лишь после
// успешной проверки защищённого header, подписи ES256 и строгого JCS.
type VerifiedJWS struct {
	CanonicalPayload []byte
	KeyID            string
}

// ES256Key associates one kid with a P-256 public key and optional private
// signer; private material must not be exposed to verification consumers.
// RU: ES256Key связывает kid с публичным ключом P-256 и необязательным
// приватным подписантом; приватные байты не выдаются verifier-потребителям.
type ES256Key struct {
	KeyID   string
	Public  *ecdsa.PublicKey
	Private *ecdsa.PrivateKey
}

// PublicOnly returns a verifier-facing key value without the private signer;
// it shares the public-key pointer, so callers must not mutate that key.
// RU: PublicOnly возвращает ключ для verifier без приватного подписанта;
// указатель на публичный ключ общий, поэтому caller не должен его изменять.
func (key ES256Key) PublicOnly() ES256Key {
	return ES256Key{KeyID: key.KeyID, Public: key.Public}
}

// KeyGeneration parses the positive canonical generation suffix of a kid
// shaped <identity>-g<N>; it binds credentials to a key across ordinary
// short-lived token renewal without accepting leading zeroes or overflow.
// RU: KeyGeneration извлекает положительное каноническое поколение из kid
// вида <identity>-g<N>; оно связывает credential с ключом при обновлении
// краткоживущего токена, не допуская ведущий ноль и переполнение.
func KeyGeneration(keyID string) (uint64, error) {
	separator := strings.LastIndex(keyID, "-g")
	if separator < 1 || separator+2 >= len(keyID) {
		return 0, ErrKey
	}
	raw := keyID[separator+2:]
	if raw[0] == '0' || strings.IndexFunc(raw, func(value rune) bool {
		return value < '0' || value > '9'
	}) >= 0 {
		return 0, ErrKey
	}
	generation, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || generation == 0 || generation > 9007199254740991 {
		return 0, ErrKey
	}
	return generation, nil
}

// SignCanonicalJSON canonicalizes the input and signs a compact ES256 JWS
// under the ordinary token size limit and an exact protected typ/kid.
// RU: SignCanonicalJSON канонизирует вход и подписывает compact ES256 JWS
// с обычным лимитом размера и точными защищёнными typ/kid.
func SignCanonicalJSON(
	value any,
	key ES256Key,
	expect ProtectedHeaderExpectation,
) (string, error) {
	return SignCanonicalJSONWithLimit(value, key, expect, MaxCompactJWSBytes)
}

// SignCanonicalJSONWithLimit signs JCS bytes using the validated private key,
// required critical header and an explicit compact-size ceiling for larger
// service documents; exceeding the ceiling fails after serialization.
// RU: SignCanonicalJSONWithLimit подписывает байты JCS проверенным приватным
// ключом с обязательным критическим header и явным пределом размера для
// служебного документа; превышение закрыто отклоняется после сериализации.
func SignCanonicalJSONWithLimit(
	value any,
	key ES256Key,
	expect ProtectedHeaderExpectation,
	maxCompactBytes int,
) (string, error) {
	if maxCompactBytes <= 0 {
		return "", ErrMalformedJWS
	}
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
	if len(compact) > maxCompactBytes {
		return "", fmt.Errorf("%w: compact JWS exceeds limit", ErrMalformedJWS)
	}
	return compact, nil
}

// VerifyCanonicalJSON verifies header, signature and JCS payload of a compact
// token within the ordinary size limit before exposing verified bytes.
// RU: VerifyCanonicalJSON проверяет header, подпись и JCS payload compact
// токена в обычном лимите до выдачи проверенных байтов.
func VerifyCanonicalJSON(
	compact string,
	key ES256Key,
	expect ProtectedHeaderExpectation,
) (VerifiedJWS, error) {
	return VerifyCanonicalJSONWithLimit(compact, key, expect, MaxCompactJWSBytes)
}

// VerifyCanonicalJSONWithLimit first pins the strict protected typ/kid, then
// verifies ES256 and canonical object payload within the explicit size limit;
// no claim is consumed before these checks succeed.
// RU: VerifyCanonicalJSONWithLimit сначала закрепляет строгие typ/kid,
// затем проверяет ES256 и канонический объект payload в заданном лимите;
// ни один claim не используется до успешного завершения проверок.
func VerifyCanonicalJSONWithLimit(
	compact string,
	key ES256Key,
	expect ProtectedHeaderExpectation,
	maxCompactBytes int,
) (VerifiedJWS, error) {
	header, err := ParseProtectedHeaderWithLimit(compact, maxCompactBytes)
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

// ParseProtectedHeader parses a canonical protected header under the ordinary
// size limit only to select a previously trusted key; it never authenticates
// the token until VerifyCanonicalJSON succeeds.
// RU: ParseProtectedHeader разбирает канонический защищённый header в обычном
// лимите лишь для выбора заранее доверенного ключа; токен не аутентифицирован
// до успешного VerifyCanonicalJSON.
func ParseProtectedHeader(compact string) (ProtectedHeader, error) {
	return ParseProtectedHeaderWithLimit(compact, MaxCompactJWSBytes)
}

// ParseProtectedHeaderWithLimit checks compact structure, explicit size,
// required critical version, ES256 and canonical header encoding; the
// returned typ/kid remain untrusted until signature verification.
// RU: ParseProtectedHeaderWithLimit проверяет структуру, размер, критическую
// версию, ES256 и каноническую кодировку header; возвращённые typ/kid
// остаются недоверенными до проверки подписи.
func ParseProtectedHeaderWithLimit(compact string, maxCompactBytes int) (ProtectedHeader, error) {
	if maxCompactBytes <= 0 || len(compact) == 0 || len(compact) > maxCompactBytes {
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

// DecodeCanonicalJSON decodes a previously verified JWS payload into the
// expected shape while rechecking strict JSON and exact JCS bytes before
// exposing claims to the caller.
// RU: DecodeCanonicalJSON декодирует ранее проверенный JWS payload в
// ожидаемую форму с повторной строгой проверкой JSON и точных байтов JCS
// перед выдачей claims вызывающей стороне.
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

// CanonicalJSON serializes a value to stable JCS bytes for signing or digest
// comparison; callers still decide the authoritative shape and purpose.
// RU: CanonicalJSON сериализует значение в устойчивые байты JCS для подписи
// или сравнения digest; авторитетную форму и назначение определяет caller.
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

// CanonicalJSONSHA256 returns a lowercase SHA-256 hex digest of canonical
// JSON bytes, not of the original input representation.
// RU: CanonicalJSONSHA256 возвращает SHA-256 hex в нижнем регистре от
// канонических байтов JSON, а не от исходного представления.
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

// ParsePublicJWK strictly parses verifier-only ES256 JWK material, rejecting
// fields or key operations that would widen it into a signer.
// RU: ParsePublicJWK строго разбирает публичный ES256 JWK для verifier,
// отклоняя поля либо операции, расширяющие его до signer.
func ParsePublicJWK(data []byte) (ES256Key, error) {
	return parseJWK(data, false)
}

// ParsePrivateJWK strictly parses an ES256 signing JWK and checks that its
// private scalar derives the declared public point.
// RU: ParsePrivateJWK строго разбирает ES256 JWK для подписи и проверяет,
// что приватный скаляр даёт заявленную публичную точку.
func ParsePrivateJWK(data []byte) (ES256Key, error) {
	return parseJWK(data, true)
}

// GenerateES256Key creates a fresh P-256 signing key under a bounded kid;
// generation or naming policy beyond this size check belongs to its caller.
// RU: GenerateES256Key создаёт новый ключ P-256 с ограниченным kid;
// поколение и политику имени сверх размера задаёт вызывающая сторона.
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

// MarshalPublicJWK emits canonical verifier-only JWK bytes after validating
// the public point and kid, without serializing private material.
// RU: MarshalPublicJWK выдаёт канонический JWK только для verifier после
// проверки публичной точки и kid, не сериализуя приватный материал.
func MarshalPublicJWK(key ES256Key) ([]byte, error) {
	return marshalJWK(key, false)
}

// MarshalPrivateJWK emits canonical signing JWK bytes only when the private
// scalar matches the public point, setting the output key operation to sign.
// RU: MarshalPrivateJWK выдаёт канонический JWK для подписи лишь при
// совпадении приватного скаляра с публичной точкой и задаёт операцию sign.
func MarshalPrivateJWK(key ES256Key) ([]byte, error) {
	return marshalJWK(key, true)
}

// PublicJWKThumbprintSHA256 hashes the canonical RFC 7638 public-key members
// only, excluding kid, private scalar and usage metadata.
// RU: PublicJWKThumbprintSHA256 хеширует только канонические публичные поля
// RFC 7638, исключая kid, приватный скаляр и metadata использования.
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

// marshalJWK validates key material and writes canonical public or private
// ES256 JWK with mutually exclusive verify/sign key operations.
// RU: marshalJWK проверяет ключ и пишет канонический публичный либо приватный
// ES256 JWK с взаимоисключающими key operations verify/sign.
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

// ValidateTimes enforces exact issued/not-before/expiry relationships and
// fixed TTL after second normalization, rejecting future issuance and tokens
// outside the bounded clock-skew window.
// RU: ValidateTimes после нормализации до секунд проверяет точные связи
// issued/not-before/expiry и фиксированный TTL, отклоняя будущую выдачу и
// токены вне ограниченного окна расхождения часов.
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

// strictProtectedHeader is the closed JSON shape of the protected JWS header;
// unknown or duplicate fields fail during strict decoding.
// RU: strictProtectedHeader задаёт закрытую JSON-форму защищённого JWS
// header; неизвестные и повторные поля отклоняет строгий декодер.
type strictProtectedHeader struct {
	Alg      string   `json:"alg"`
	Type     string   `json:"typ"`
	KeyID    string   `json:"kid"`
	Critical []string `json:"crit"`
	Version  int      `json:"mcxv"`
}

// encodedJWK is the closed JSON shape shared by public verification and
// private signing keys, with the private scalar omitted from public output.
// RU: encodedJWK задаёт закрытую JSON-форму публичного verifier-ключа и
// приватного signer-ключа; в публичном выводе скаляр отсутствует.
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

// parseJWK strictly decodes the expected public or private key operation,
// checks the P-256 point and, for signers, verifies the private/public match.
// RU: parseJWK строго декодирует ожидаемую публичную либо приватную
// операцию, проверяет точку P-256 и для подписанта — соответствие частей ключа.
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

// validateES256Key requires the expected kid, P-256 public point and,
// when signing, a valid private scalar that derives the same public point.
// RU: validateES256Key требует ожидаемый kid, публичную точку P-256 и при
// подписи корректный приватный скаляр с той же публичной точкой.
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
