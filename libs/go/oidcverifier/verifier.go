package oidcverifier

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/oidcidentity"
	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
)

const (
	// maximumBearerBytes caps the raw bearer before signature or claim parsing.
	// maximumBearerBytes ограничивает сырой bearer до разбора подписи и claims.
	maximumBearerBytes = 16384
	// maximumDisplayRunes caps the visible, normalized user display name.
	// maximumDisplayRunes ограничивает число символов видимого нормализованного имени.
	maximumDisplayRunes = 160
	// maximumEmailBytes limits the untrusted email claim before masking.
	// maximumEmailBytes ограничивает недоверенный email claim до маскирования.
	maximumEmailBytes = 254
	// maximumGroups caps the number of group claims accepted from the provider.
	// maximumGroups ограничивает число групп, принимаемых от провайдера.
	maximumGroups = 100
	// maximumGroupRunes caps each normalized group name.
	// maximumGroupRunes ограничивает длину каждого нормализованного имени группы.
	maximumGroupRunes = 200
	// maximumAMRValues caps the count of authentication methods in a token.
	// maximumAMRValues ограничивает число методов аутентификации в токене.
	maximumAMRValues = 16
	// maximumAMRRunes caps each normalized authentication method label.
	// maximumAMRRunes ограничивает длину каждого нормализованного обозначения метода.
	maximumAMRRunes = 64
	// maximumACRRunes caps the assurance-class claim after trimming.
	// maximumACRRunes ограничивает длину claim класса надёжности после очистки пробелов.
	maximumACRRunes = 160
	// ownerScope is necessary but insufficient for the owner claim without its matching realm role.
	// ownerScope необходим для признака владельца, но без соответствующей роли realm его недостаточно.
	ownerScope = "kodex.owner"
	// ownerRealmRole must accompany the owner scope before OwnerClaim is set.
	// ownerRealmRole должна сопровождать owner scope перед установкой OwnerClaim.
	ownerRealmRole = "kodex-owner"
	// unknownUserName is the localization key used when no safe display name remains.
	// unknownUserName — ключ локализации на случай отсутствия безопасного отображаемого имени.
	unknownUserName = "i18n:OIDC_USER_NAME"
)

// ErrSigningKeysUnavailable distinguishes missing or blocked trusted keys from an invalid bearer token.
// ErrSigningKeysUnavailable отличает отсутствие либо блокировку доверенных ключей от неверного bearer token.
var ErrSigningKeysUnavailable = errors.New("OIDC signing keys are unavailable")

// Config binds the issuer and JWKS to one exact TLS name, trusted CA, dial target and bounded timeout.
// Config связывает issuer и JWKS с точным TLS-именем, доверенным CA, адресом соединения и временем ожидания.
type Config struct {
	Issuer         string
	Audience       string
	JWKSURL        string
	ConnectAddress string
	TLSServerName  string
	CAFile         string
	Timeout        time.Duration
}

// Principal is the validated issuer-scoped identity and bounded presentation metadata derived from a bearer token.
// Principal содержит проверенную привязанную к issuer личность и ограниченные данные показа из bearer token.
type Principal struct {
	Subject         string
	OrganizationID  string
	Issuer          string
	SessionID       string
	DisplayName     string
	EmailHint       string
	Groups          []string
	OwnerClaim      bool
	SessionRevision uint64
	AuthenticatedAt time.Time
	ACR             string
	AMR             []string
	ExpiresAt       time.Time
}

// Verifier owns the exact OIDC transport and last verified signing-key snapshot used by bearer and browser flows.
// Verifier владеет точным OIDC transport и последним проверенным снимком ключей для bearer и браузерного входа.
type Verifier struct {
	verifier  *coreoidc.IDTokenVerifier
	keys      *boundedKeySet
	transport *http.Transport
	client    *http.Client
	issuer    string
}

// exactTransport rejects requests outside the configured HTTPS issuer host and port before network access.
// exactTransport отклоняет запросы вне заданных HTTPS-хоста и порта issuer до обращения к сети.
type exactTransport struct {
	next *http.Transport
	host string
	port string
}

// RoundTrip enforces the configured endpoint boundary before forwarding a request to the protected transport.
// RoundTrip проверяет границу разрешённого endpoint перед передачей запроса защищённому transport.
func (transport exactTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || request.URL.Scheme != "https" ||
		!strings.EqualFold(request.URL.Hostname(), transport.host) || request.URL.Port() != transport.port ||
		request.URL.User != nil || request.URL.Fragment != "" {
		return nil, errors.New("OIDC endpoint is not permitted")
	}
	return transport.next.RoundTrip(request)
}

// claims holds untrusted token fields until signature, audience, issuer and semantic checks succeed.
// claims содержит недоверенные поля токена до успешной проверки подписи, audience, issuer и смысла.
type claims struct {
	SessionID       string   `json:"sid"`
	OrganizationID  string   `json:"organization_id"`
	SessionRevision uint64   `json:"session_revision"`
	TokenID         string   `json:"jti"`
	Scope           string   `json:"scope"`
	Name            string   `json:"name"`
	PreferredName   string   `json:"preferred_username"`
	Email           string   `json:"email"`
	EmailVerified   bool     `json:"email_verified"`
	Groups          []string `json:"groups"`
	AuthTime        int64    `json:"auth_time"`
	ACR             string   `json:"acr"`
	AMR             []string `json:"amr"`
	RealmAccess     struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

// New validates the exact OIDC endpoint and CA, then starts a verifier closed to tokens until JWKS refresh succeeds.
// New проверяет точные OIDC endpoint и CA, затем создаёт verifier, закрытый для токенов до успешного обновления JWKS.
func New(ctx context.Context, config Config) (*Verifier, error) {
	if ctx == nil || config.Issuer == "" || config.Audience == "" || config.TLSServerName == "" ||
		!filepath.IsAbs(config.CAFile) || config.Timeout < time.Second || config.Timeout > 10*time.Second {
		return nil, errors.New("OIDC verifier configuration is invalid")
	}
	issuerURL, err := url.Parse(config.Issuer)
	jwksURL, jwksErr := url.Parse(config.JWKSURL)
	connectHost, connectPort, connectErr := net.SplitHostPort(config.ConnectAddress)
	if err != nil || issuerURL.Scheme != "https" || issuerURL.Hostname() == "" || issuerURL.User != nil ||
		issuerURL.RawQuery != "" || issuerURL.Fragment != "" ||
		jwksErr != nil || jwksURL.Scheme != "https" || jwksURL.Hostname() != issuerURL.Hostname() ||
		jwksURL.User != nil || jwksURL.RawQuery != "" || jwksURL.Fragment != "" || jwksURL.Path == "" ||
		!strings.EqualFold(issuerURL.Hostname(), config.TLSServerName) || connectErr != nil ||
		connectHost == "" || net.ParseIP(connectHost) != nil || connectPort != "443" {
		return nil, errors.New("OIDC issuer is not permitted")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read OIDC CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse OIDC CA")
	}
	dialer := &net.Dialer{Timeout: config.Timeout, KeepAlive: 30 * time.Second}
	issuerAddress := net.JoinHostPort(issuerURL.Hostname(), "443")
	base := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if !strings.HasPrefix(network, "tcp") || !strings.EqualFold(address, issuerAddress) {
				return nil, errors.New("OIDC endpoint is not permitted")
			}
			return dialer.DialContext(ctx, network, config.ConnectAddress)
		},
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ServerName: config.TLSServerName,
			RootCAs:    roots,
		},
		DisableCompression: true,
	}
	client := &http.Client{
		Timeout:   config.Timeout,
		Transport: exactTransport{next: base, host: issuerURL.Hostname(), port: issuerURL.Port()},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("OIDC redirects are forbidden")
		},
	}
	keys := newBoundedKeySet(client, config.JWKSURL)
	// JWKS является соседней identity dependency, а не startup/readiness
	// authority текущего процесса. До первого успешного refresh verifier
	// остаётся fail-closed и рабочий запрос получает typed Unavailable.
	if refreshErr := keys.Refresh(ctx); refreshErr != nil && ctx.Err() != nil {
		base.CloseIdleConnections()
		return nil, ctx.Err()
	}
	return &Verifier{
		verifier:  coreoidc.NewVerifier(config.Issuer, keys, &coreoidc.Config{ClientID: config.Audience, SupportedSigningAlgs: []string{"RS256"}}),
		keys:      keys,
		transport: base,
		client:    client,
		issuer:    config.Issuer,
	}, nil
}

// VerifyAuthorization requires a single Bearer credential and returns its verified principal and original token.
// VerifyAuthorization требует единственный Bearer credential и возвращает проверенный principal и исходный токен.
func (verifier *Verifier) VerifyAuthorization(ctx context.Context, authorization string) (Principal, string, error) {
	parts := strings.Fields(authorization)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return Principal{}, "", errors.New("OIDC bearer is invalid")
	}
	principal, err := verifier.VerifyToken(ctx, parts[1])
	return principal, parts[1], err
}

// VerifyToken checks the bounded signed token and claims, including session lineage and degraded-key expiry.
// VerifyToken проверяет ограниченный подписанный токен и claims, включая происхождение сессии и срок ключей при деградации.
func (verifier *Verifier) VerifyToken(ctx context.Context, raw string) (Principal, error) {
	if verifier == nil || raw == "" || len(raw) > maximumBearerBytes || strings.TrimSpace(raw) != raw {
		return Principal{}, errors.New("OIDC bearer is invalid")
	}
	if err := verifier.keys.verificationError(); err != nil {
		return Principal{}, err
	}
	token, err := verifier.verifier.Verify(ctx, raw)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Principal{}, err
	}
	if err != nil {
		if availabilityErr := verifier.keys.verificationError(); availabilityErr != nil {
			return Principal{}, availabilityErr
		}
		return Principal{}, errors.New("OIDC bearer is invalid")
	}
	if token.Expiry.IsZero() {
		return Principal{}, errors.New("OIDC bearer is invalid")
	}
	if deadline, degraded := verifier.keys.DegradedDeadline(); degraded && token.Expiry.After(deadline) {
		return Principal{}, errors.New("OIDC bearer exceeds the signing-key grace window")
	}
	var values claims
	if token.Claims(&values) != nil || uuid.Validate(values.OrganizationID) != nil || values.SessionRevision == 0 {
		return Principal{}, errors.New("OIDC session claims are invalid")
	}
	groups, groupsErr := normalizeGroups(values.Groups)
	amr, amrErr := normalizeBoundedStrings(values.AMR, maximumAMRValues, maximumAMRRunes)
	acr := strings.TrimSpace(values.ACR)
	if groupsErr != nil || amrErr != nil || utf8.RuneCountInString(acr) > maximumACRRunes || strings.ContainsAny(acr, "\r\n\x00") ||
		values.AuthTime < 0 || values.AuthTime > token.Expiry.Unix() {
		return Principal{}, errors.New("OIDC session claims are invalid")
	}
	subject, subjectErr := oidcidentity.Subject(token.Issuer, token.Subject)
	sessionID, sessionErr := oidcidentity.SessionID(token.Issuer, values.SessionID)
	if subjectErr != nil || sessionErr != nil {
		return Principal{}, errors.New("OIDC session claims are invalid")
	}
	if _, tokenIDErr := oidcidentity.TokenID(token.Issuer, values.TokenID); tokenIDErr != nil {
		return Principal{}, errors.New("OIDC session claims are invalid")
	}
	var authenticatedAt time.Time
	if values.AuthTime > 0 {
		authenticatedAt = time.Unix(values.AuthTime, 0).UTC()
	}
	return Principal{
		Subject: subject, OrganizationID: values.OrganizationID, Issuer: token.Issuer, SessionID: sessionID,
		DisplayName: safeDisplayName(values.Name, values.PreferredName), EmailHint: maskedVerifiedEmail(values.Email, values.EmailVerified),
		Groups: groups, OwnerClaim: containsWord(values.Scope, ownerScope) && contains(values.RealmAccess.Roles, ownerRealmRole),
		SessionRevision: values.SessionRevision, AuthenticatedAt: authenticatedAt, ACR: acr, AMR: amr, ExpiresAt: token.Expiry,
	}, nil
}

// normalizeBoundedStrings trims, validates, deduplicates and sorts a bounded list such as AMR methods.
// normalizeBoundedStrings очищает, проверяет, удаляет повторы и сортирует ограниченный список, например AMR.
func normalizeBoundedStrings(values []string, maximumValues, maximumRunes int) ([]string, error) {
	if len(values) > maximumValues {
		return nil, errors.New("too many values")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || utf8.RuneCountInString(value) > maximumRunes || strings.ContainsAny(value, "\r\n\x00") {
			return nil, errors.New("value is invalid")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

// normalizeGroups rejects oversized or unsafe group claims, then deduplicates and sorts valid group names.
// normalizeGroups отвергает слишком большие и небезопасные группы, затем удаляет повторы и сортирует имена.
func normalizeGroups(values []string) ([]string, error) {
	if len(values) > maximumGroups {
		return nil, errors.New("too many OIDC groups")
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || utf8.RuneCountInString(value) > maximumGroupRunes || strings.ContainsAny(value, "\r\n\x00") {
			return nil, errors.New("OIDC group is invalid")
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

// safeDisplayName chooses the first usable name, strips invisible controls and bounds the visible rune count.
// safeDisplayName выбирает первое пригодное имя, убирает невидимые управляющие символы и ограничивает число символов.
func safeDisplayName(name, preferredName string) string {
	for _, candidate := range []string{name, preferredName} {
		var normalized strings.Builder
		spacePending := false
		for _, character := range strings.TrimSpace(candidate) {
			if unicode.IsSpace(character) {
				spacePending = normalized.Len() > 0
				continue
			}
			if !unicode.IsGraphic(character) || unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
				continue
			}
			if spacePending {
				normalized.WriteByte(' ')
				spacePending = false
			}
			normalized.WriteRune(character)
		}
		value := normalized.String()
		if value == "" {
			continue
		}
		if utf8.RuneCountInString(value) > maximumDisplayRunes {
			value = string([]rune(value)[:maximumDisplayRunes])
		}
		return value
	}
	return unknownUserName
}

// maskedVerifiedEmail returns only a bounded first-rune/domain hint for a verified and syntactically safe email.
// maskedVerifiedEmail возвращает лишь ограниченную подсказку из первого символа и домена проверенного корректного email.
func maskedVerifiedEmail(email string, verified bool) string {
	if !verified || email == "" || len(email) > maximumEmailBytes || strings.TrimSpace(email) != email || strings.ContainsAny(email, "\r\n\t ") {
		return ""
	}
	separator := strings.LastIndexByte(email, '@')
	if separator < 1 || separator != strings.IndexByte(email, '@') || separator == len(email)-1 {
		return ""
	}
	local, domain := email[:separator], strings.ToLower(email[separator+1:])
	if !validEmailDomain(domain) {
		return ""
	}
	first, _ := utf8.DecodeRuneInString(local)
	if first == utf8.RuneError || !unicode.IsGraphic(first) || unicode.IsControl(first) {
		return ""
	}
	masked := string(first) + "***@" + domain
	if len(masked) > 200 {
		return ""
	}
	return masked
}

// validEmailDomain allows ordinary lowercase DNS labels separated by dots and rejects empty or hyphen-edge labels.
// validEmailDomain допускает строчные DNS-метки через точки и отвергает пустые метки либо дефис на их краю.
func validEmailDomain(domain string) bool {
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || !strings.ContainsRune(domain, '.') {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, character := range label {
			if character < 'a' || character > 'z' {
				if character < '0' || character > '9' {
					if character != '-' {
						return false
					}
				}
			}
		}
	}
	return true
}

// Refresh updates JWKS independently of request readiness; temporary outages have a bounded old-key window.
// Refresh обновляет JWKS отдельно от readiness запросов; при временном сбое старые ключи действуют ограниченное время.
func (verifier *Verifier) Refresh(ctx context.Context) error {
	if verifier == nil || verifier.keys == nil {
		return errors.New("OIDC verifier is unavailable")
	}
	return verifier.keys.Refresh(ctx)
}

// containsWord checks for an exact whitespace-delimited scope rather than a substring match.
// containsWord ищет точный scope среди слов, разделённых пробелами, а не подстроку.
func containsWord(words, expected string) bool {
	return contains(strings.Fields(words), expected)
}

// contains reports an exact element match in a small token claim list.
// contains сообщает о точном совпадении элемента в небольшом списке token claims.
func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

// Close closes only idle HTTP connections owned by the verifier; active requests retain their own lifecycle.
// Close закрывает только неиспользуемые HTTP-соединения verifier; активные запросы живут отдельно.
func (verifier *Verifier) Close() {
	if verifier != nil && verifier.transport != nil {
		verifier.transport.CloseIdleConnections()
	}
}
