package value

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// oidcPolicySchema pins the operator-owned policy format independently of any JWT claims.
// oidcPolicySchema закрепляет формат operator-owned policy независимо от любых JWT claims.
const oidcPolicySchema = "kodex.oidc-identity-policy/v1"

// invalidOIDCPolicyMessage avoids serializing issuer, owner identifiers or arbitrary parser input in errors.
// invalidOIDCPolicyMessage исключает issuer, owner identifiers и произвольный parser input из ошибок.
const invalidOIDCPolicyMessage = "OIDC identity policy is invalid"

// OIDCIdentityPolicy is immutable deployment intent for one installation's standard-identity mode.
// BootstrapSubjects contains already issuer-qualified subject UUIDs; it grants first ownership only while the installation is unclaimed.
// OIDCIdentityPolicy — неизменяемый deployment intent standard-identity режима одной установки.
// BootstrapSubjects содержит issuer-qualified subject UUIDs; первое ownership допускается лишь для ещё не claimed установки.
type OIDCIdentityPolicy struct {
	Schema                 string   `json:"schema"`
	Revision               uint64   `json:"revision"`
	Issuer                 string   `json:"issuer"`
	BootstrapSubjects      []string `json:"bootstrap_subjects"`
	SessionLifetimeSeconds int64    `json:"session_lifetime_seconds"`
}

// Validate checks closed format, exact HTTPS issuer, bounded revision/lifetime and a unique canonical bootstrap allowlist.
// It does not choose an organization, create a user or establish trust in an issuer's signing keys.
// Validate проверяет закрытый формат, точный HTTPS issuer, пределы revision/lifetime и уникальный canonical bootstrap allowlist.
// Метод не выбирает организацию, не создаёт пользователя и не устанавливает доверие signing keys issuer.
func (policy OIDCIdentityPolicy) Validate() error {
	issuer, err := url.Parse(policy.Issuer)
	if policy.Schema != oidcPolicySchema || policy.Revision == 0 || policy.Revision > 1<<53-1 ||
		len(policy.Issuer) > 2048 || strings.TrimSpace(policy.Issuer) != policy.Issuer || err != nil || issuer.Scheme != "https" ||
		issuer.Hostname() == "" || net.ParseIP(issuer.Hostname()) != nil || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" ||
		issuer.Port() != "" && issuer.Port() != "443" || policy.SessionLifetimeSeconds < 60 || policy.SessionLifetimeSeconds > 12*60*60 ||
		len(policy.BootstrapSubjects) < 1 || len(policy.BootstrapSubjects) > 16 {
		return errors.New(invalidOIDCPolicyMessage)
	}
	seen := make(map[string]bool, len(policy.BootstrapSubjects))
	for _, subject := range policy.BootstrapSubjects {
		parsed, err := uuid.Parse(subject)
		if err != nil || parsed == uuid.Nil || parsed.String() != subject || seen[subject] {
			return errors.New(invalidOIDCPolicyMessage)
		}
		seen[subject] = true
	}
	return nil
}

// Digest hashes the validated canonical policy without allowing bootstrap-list ordering to create a different authority revision.
// Digest хеширует проверенную canonical policy, не позволяя порядку bootstrap-list менять authority revision.
func (policy OIDCIdentityPolicy) Digest() (string, error) {
	if err := policy.Validate(); err != nil {
		return "", err
	}
	policy.BootstrapSubjects = slices.Clone(policy.BootstrapSubjects)
	slices.Sort(policy.BootstrapSubjects)
	raw, err := json.Marshal(policy)
	if err != nil {
		return "", errors.New(invalidOIDCPolicyMessage)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// BootstrapOwner matches only an exact configured issuer-qualified subject; absent or arbitrary first users remain ineligible.
// BootstrapOwner сопоставляет только явно заданный issuer-qualified subject; произвольный первый пользователь остаётся непригодным.
func (policy OIDCIdentityPolicy) BootstrapOwner(subject string) bool {
	return slices.Contains(policy.BootstrapSubjects, subject)
}
