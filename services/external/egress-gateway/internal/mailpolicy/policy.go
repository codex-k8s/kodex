package mailpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"strconv"

	shared "github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// Mail policy constants reuse the closed wire schema and profile registry.
// Константы почтовой политики используют закрытую схему и реестр профиля.
const (
	MailSchema       = shared.MailSchema
	MailProfileName  = shared.MailProfileName
	MailWorkload     = shared.MailWorkload
	MailOperation    = shared.MailOperation
	MaximumFileBytes = shared.MaximumFileBytes
)

// MailDocument is the validated mail projection shared with its producer.
// MailDocument — проверяемая почтовая проекция, общая с её producer.
type MailDocument = shared.MailDocument

// MailDestination is an exact mail host, port, mode, and DNS pin set.
// MailDestination — точный набор почтового узла, порта, режима и DNS-pins.
type MailDestination = shared.MailDestination

// MailActive holds a validated projection and base-policy runtime limits.
// MailActive хранит проверенную проекцию и лимиты базовой политики.
type MailActive struct {
	document MailDocument
	digest   string
	limits   policy.Limits
}

// MailEndpointValid checks protocol, port, and TLS mode against the closed registry.
// MailEndpointValid сверяет протокол, порт и режим TLS с закрытым реестром.
func MailEndpointValid(protocol string, port int, mode string) bool {
	return shared.MailEndpointValid(protocol, port, mode)
}

// LoadMailFile bounds a projected mail policy file before strict decoding.
// LoadMailFile ограничивает размер файла почтовой политики до строгого разбора.
func LoadMailFile(path, expectedDigest string, base *policy.Active) (*MailActive, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open mail policy file")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, MaximumFileBytes+1))
	if err != nil {
		return nil, errors.New("read mail policy file")
	}
	return LoadMail(raw, expectedDigest, base)
}

// LoadMail binds a strict projection to the immutable gateway digest and expected digest.
// LoadMail привязывает строгую проекцию к дайджестам gateway и ожидаемому.
func LoadMail(raw []byte, expectedDigest string, base *policy.Active) (*MailActive, error) {
	if base == nil || len(raw) == 0 || len(raw) > MaximumFileBytes || policy.RejectDuplicateFields(raw) != nil {
		return nil, errors.New("mail policy document is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document MailDocument
	if decoder.Decode(&document) != nil || document.Validate() != nil || document.GatewayPolicyDigest != base.Digest() {
		return nil, errors.New("mail policy projection is invalid")
	}
	digest := document.Digest()
	if expectedDigest != digest {
		return nil, errors.New("mail policy digest mismatch")
	}
	return &MailActive{document: document, digest: digest, limits: base.Limits()}, nil
}

// Revision derives the displayed mail revision from the validated configuration.
// Revision формирует почтовую ревизию из проверенной конфигурации.
func (a *MailActive) Revision() string {
	return "mail-" + strconv.FormatInt(a.document.ConfigurationRevision, 10)
}

// Digest returns the validated canonical projection digest.
// Digest возвращает проверенный канонический дайджест проекции.
func (a *MailActive) Digest() string { return a.digest }

// Limits returns the base gateway limits preserved by this projection.
// Limits возвращает лимиты базового gateway, сохранённые проекцией.
func (a *MailActive) Limits() policy.Limits { return a.limits }

// ProfileIdentity returns the fixed mail workload and operation identity.
// ProfileIdentity возвращает фиксированную идентичность почтовой нагрузки и операции.
func (a *MailActive) ProfileIdentity() (string, string, string) {
	return MailProfileName, MailWorkload, MailOperation
}

// Configured reports whether at least one exact mail destination is present.
// Configured сообщает, задан ли хотя бы один точный почтовый адрес.
func (a *MailActive) Configured() bool { return len(a.document.Destinations) != 0 }

// Allows requires an exact host and port with a registered TLS mode.
// Allows требует точные узел и порт с зарегистрированным режимом TLS.
func (a *MailActive) Allows(host string, port int) bool { return a.TLSMode(host, port) != "" }

// RequireCompleteDNSPinning marks this policy as requiring every answer to be pinned.
// RequireCompleteDNSPinning отмечает требование закрепить каждый DNS-ответ.
func (a *MailActive) RequireCompleteDNSPinning() {}

// TLSMode returns only the mode bound to an exact mail host and port.
// TLSMode возвращает режим, привязанный к точным почтовым узлу и порту.
func (a *MailActive) TLSMode(host string, port int) string {
	for _, d := range a.document.Destinations {
		if d.Hostname == host && d.Port == port {
			return d.TLSMode
		}
	}
	return ""
}

// AllowsLiteral checks an exact address against the destination's DNS pins.
// AllowsLiteral проверяет точный адрес по DNS-pins назначения.
func (a *MailActive) AllowsLiteral(host string, port int, address netip.Addr) bool {
	for _, d := range a.document.Destinations {
		if d.Hostname != host || d.Port != port {
			continue
		}
		for _, pin := range d.Addresses {
			if pin == address.String() {
				return true
			}
		}
	}
	return false
}

// ConfigurationIdentity returns the source revision and digest, not mailbox secrets.
// ConfigurationIdentity возвращает ревизию и дайджест источника, не секреты ящика.
func (a *MailActive) ConfigurationIdentity() (string, string) {
	return strconv.FormatInt(a.document.ConfigurationRevision, 10), a.document.ConfigurationDigest
}

// Destinations copies exact host and port pairs for readiness checks.
// Destinations копирует точные пары узла и порта для проверок готовности.
func (a *MailActive) Destinations() []policy.Destination {
	result := make([]policy.Destination, 0, len(a.document.Destinations))
	for _, d := range a.document.Destinations {
		result = append(result, policy.Destination{Hostname: d.Hostname, Port: d.Port})
	}
	return result
}
