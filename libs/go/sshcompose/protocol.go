// Package sshcompose defines the closed release and execution protocol shared by
// the integration worker, owner admission and the forced-command remote helper.
// Пакет sshcompose задаёт закрытый протокол релиза и исполнения для worker,
// owner admission и удалённого helper с принудительно заданной командой.
package sshcompose

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// ProtocolVersion prevents incompatible remote helpers from accepting commands.
// ProtocolVersion запрещает несовместимому remote helper принимать команды.
const ProtocolVersion = 1

// MaximumBundleBytes bounds the complete encoded source snapshot before decode.
// MaximumBundleBytes ограничивает полный закодированный снимок до разбора.
const MaximumBundleBytes = 2 << 20

// MaximumFiles bounds Git tree traversal and remote extraction independently.
// MaximumFiles ограничивает обход Git tree и извлечение на сервере.
const MaximumFiles = 256

// MaximumFileBytes rejects oversized files instead of silently truncating them.
// MaximumFileBytes отклоняет большие файлы вместо молчаливого усечения.
const MaximumFileBytes = 256 << 10

// MaximumLifetime bounds one deployment regardless of successive lease renewals.
// MaximumLifetime ограничивает один deploy независимо от продлений аренды.
const MaximumLifetime = 10 * time.Minute

// digestPattern accepts only a canonical SHA-256 commitment, never a locator.
// digestPattern допускает только канонический SHA-256, не адрес ресурса.
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// commitPattern accepts immutable Git SHA-1 commit and tree identifiers.
// commitPattern допускает неизменяемые SHA-1 идентификаторы Git commit/tree.
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

// refPattern confines identifiers to safe bounded opaque values without paths.
// refPattern ограничивает идентификаторы безопасными непрозрачными значениями.
var refPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// repositoryPattern restricts one GitHub owner/repository tuple without URL syntax.
// repositoryPattern ограничивает одну пару GitHub owner/repository без URL.
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

// imagePattern requires an immutable registry image digest with a literal name.
// imagePattern требует неизменяемый digest образа с буквальным именем registry.
var imagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9./:_-]{0,240}@sha256:[a-f0-9]{64}$`)

// ErrorInvalid is a fixed diagnostic that never carries source or credentials.
// ErrorInvalid — фиксированная диагностика без исходников или credentials.
var ErrorInvalid = errors.New("SSH Compose protocol rejected")

// Target is public connection metadata pinned before any credential is loaded.
// Target — общедоступные метаданные connection, закреплённые до чтения ключа.
type Target struct {
	Ref           string `json:"ref"`
	ProjectRef    string `json:"projectRef"`
	Repository    string `json:"repository"`
	Address       string `json:"address"`
	Port          int    `json:"port"`
	User          string `json:"user"`
	HostKey       string `json:"hostKey"`
	PublishedPort int    `json:"publishedPort"`
	RuntimeImage  string `json:"runtimeImage"`
}

// Validate rejects nonliteral destinations, SSH options and unbounded host pins.
// Validate отклоняет небуквальные адреса, SSH-опции и неограниченные host pins.
func (target Target) Validate() error {
	address, err := netip.ParseAddr(target.Address)
	if err != nil || address.String() != target.Address || address.Is4In6() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() ||
		!refPattern.MatchString(target.Ref) || !refPattern.MatchString(target.ProjectRef) || !repositoryPattern.MatchString(target.Repository) ||
		target.Port != 22 || target.PublishedPort < 1024 || target.PublishedPort > 65535 || !imagePattern.MatchString(target.RuntimeImage) ||
		!regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`).MatchString(target.User) || target.User == "root" ||
		len(target.HostKey) > 1024 || !strings.HasPrefix(target.HostKey, "ssh-ed25519 ") || strings.ContainsAny(target.HostKey, "\r\n\x00") {
		return ErrorInvalid
	}
	parts := strings.Fields(target.HostKey)
	if len(parts) != 2 {
		return ErrorInvalid
	}
	raw, decodeErr := base64.StdEncoding.Strict().DecodeString(parts[1])
	if decodeErr != nil || len(raw) != 51 || binary.BigEndian.Uint32(raw[:4]) != 11 || string(raw[4:15]) != "ssh-ed25519" || binary.BigEndian.Uint32(raw[15:19]) != 32 {
		return ErrorInvalid
	}
	return nil
}

// Permit binds one owner-issued command to the complete invocation and release.
// Permit связывает owner-команду с полными координатами invocation и релиза.
type Permit struct {
	Version               int    `json:"version"`
	Purpose               string `json:"purpose"`
	Target                Target `json:"target"`
	InvocationRef         string `json:"invocationRef"`
	DeploymentRef         string `json:"deploymentRef"`
	RootRunRef            string `json:"rootRunRef"`
	SessionRef            string `json:"sessionRef"`
	TurnRef               string `json:"turnRef"`
	Attempt               int    `json:"attempt"`
	LeaseRef              string `json:"leaseRef"`
	Generation            int64  `json:"generation"`
	EffectKey             string `json:"effectKey"`
	InputDigest           string `json:"inputDigest"`
	DeploymentInputDigest string `json:"deploymentInputDigest"`
	MergedSHA             string `json:"mergedSha"`
	TreeSHA               string `json:"treeSha"`
	BundleSHA256          string `json:"bundleSha256"`
	IssuedAt              int64  `json:"issuedAt"`
	ExpiresAt             int64  `json:"expiresAt"`
	Deadline              int64  `json:"deadline"`
}

// Validate checks structure and the short authority window without granting access.
// Validate проверяет форму и короткий срок полномочий, не предоставляя доступ.
func (permit Permit) Validate(now time.Time) error {
	if permit.Version != ProtocolVersion || permit.Target.Validate() != nil ||
		!refPattern.MatchString(permit.InvocationRef) || !refPattern.MatchString(permit.RootRunRef) ||
		!refPattern.MatchString(permit.SessionRef) || !refPattern.MatchString(permit.TurnRef) || !refPattern.MatchString(permit.LeaseRef) ||
		!refPattern.MatchString(permit.EffectKey) || !digestPattern.MatchString(permit.InputDigest) || permit.Generation < 1 || permit.Attempt < 1 ||
		permit.IssuedAt < now.Unix()-35 || permit.IssuedAt > now.Unix()+5 || permit.ExpiresAt <= now.Unix() || permit.ExpiresAt > now.Add(35*time.Second).Unix() ||
		permit.ExpiresAt > permit.Deadline || permit.Deadline > now.Add(MaximumLifetime+5*time.Second).Unix() {
		return ErrorInvalid
	}
	switch permit.Purpose {
	case "INSPECT", "DEPLOY", "STATUS", "RENEW", "CANCEL":
	default:
		return ErrorInvalid
	}
	if permit.Purpose != "INSPECT" && (!refPattern.MatchString(permit.DeploymentRef) || !digestPattern.MatchString(permit.DeploymentInputDigest) || !commitPattern.MatchString(permit.MergedSHA) || !commitPattern.MatchString(permit.TreeSHA) || !digestPattern.MatchString(permit.BundleSHA256)) {
		return ErrorInvalid
	}
	return nil
}

// SignedPermit transports canonical owner bytes and a detached Ed25519 signature.
// SignedPermit передаёт канонические owner bytes и отделённую подпись Ed25519.
type SignedPermit struct {
	KeyID     string `json:"keyId"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// Sign validates and signs one canonical payload under a configured owner key.
// Sign проверяет и подписывает один канонический payload настроенным ключом owner.
func Sign(permit Permit, key ed25519.PrivateKey, now time.Time) (SignedPermit, error) {
	if len(key) != ed25519.PrivateKeySize || permit.Validate(now) != nil {
		return SignedPermit{}, ErrorInvalid
	}
	raw, err := json.Marshal(permit)
	if err != nil {
		return SignedPermit{}, ErrorInvalid
	}
	public := key.Public().(ed25519.PublicKey)
	return SignedPermit{Digest(public), base64.StdEncoding.EncodeToString(raw), base64.StdEncoding.EncodeToString(ed25519.Sign(key, raw))}, nil
}

// Verify checks the configured key, signature, exact canonical shape and expiry.
// Verify проверяет настроенный ключ, подпись, каноническую форму и срок действия.
func Verify(signed SignedPermit, keys map[string]ed25519.PublicKey, now time.Time) (Permit, error) {
	key, exists := keys[signed.KeyID]
	if !exists || len(key) != ed25519.PublicKeySize || len(signed.Payload) > 8192 {
		return Permit{}, ErrorInvalid
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(signed.Payload)
	signature, sigErr := base64.StdEncoding.Strict().DecodeString(signed.Signature)
	if err != nil || sigErr != nil || !ed25519.Verify(key, raw, signature) {
		return Permit{}, ErrorInvalid
	}
	var permit Permit
	if Decode(raw, &permit, 8192) != nil || permit.Validate(now) != nil {
		return Permit{}, ErrorInvalid
	}
	canonical, err := json.Marshal(permit)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Permit{}, ErrorInvalid
	}
	return permit, nil
}

// Digest commits exact bytes and does not establish their provenance or authority.
// Digest фиксирует точные bytes, не устанавливая происхождение или полномочия.
func Digest(raw []byte) string { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }

// Decode rejects oversized, unknown-field, trailing and duplicate-key JSON input.
// Decode отклоняет большой JSON, неизвестные поля, хвост и повторные ключи.
func Decode(raw []byte, target any, maximum int) error {
	if len(raw) == 0 || len(raw) > maximum {
		return ErrorInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if rejectDuplicateKeys(decoder) != nil {
		return ErrorInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrorInvalid
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrorInvalid
	}
	return nil
}

// rejectDuplicateKeys traverses bounded JSON tokens before typed semantic use.
// rejectDuplicateKeys обходит ограниченные JSON tokens до семантического разбора.
func rejectDuplicateKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			name, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := name.(string)
			if !ok || seen[key] {
				return ErrorInvalid
			}
			seen[key] = true
			if err := rejectDuplicateKeys(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := rejectDuplicateKeys(decoder); err != nil {
				return err
			}
		}
	default:
		return ErrorInvalid
	}
	_, err = decoder.Token()
	return err
}
