// Package mailpolicy defines the shared wire and render contract for mail
// destination pins consumed by the control plane and egress gateway.
// RU: Пакет mailpolicy задаёт общий контракт передачи и отрисовки сетевых
// привязок почты для control-plane и egress-gateway.
package mailpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

const (
	// MailSchema identifies the exact version of the serialized mail projection.
	// RU: MailSchema обозначает точную версию сериализованной почтовой проекции.
	MailSchema = "egress-mail/v1"
	// MailProfileName binds the projection to the registered mail egress profile.
	// RU: MailProfileName привязывает проекцию к зарегистрированному почтовому профилю доступа.
	MailProfileName = "email-mail"
	// MailWorkload names the workload allowed to consume the mail destination pins.
	// RU: MailWorkload называет нагрузку, которой разрешено применять почтовые сетевые привязки.
	MailWorkload = "email-bridge"
	// MailOperation names the registered mail transport operation.
	// RU: MailOperation называет зарегистрированную операцию почтового транспорта.
	MailOperation = "email.transport"
	// MaximumFileBytes bounds the serialized policy consumed by the gateway.
	// RU: MaximumFileBytes ограничивает размер сериализованной политики для шлюза.
	MaximumFileBytes = 64 << 10
)

// MailDocument records a versioned, content-bound projection of enabled mail
// endpoints; it contains no mailbox credentials or unrelated connection fields.
// RU: MailDocument хранит версионированную проекцию разрешённых почтовых узлов,
// связанную дайджестами, без учётных данных или посторонних полей подключения.
type MailDocument struct {
	Schema                string            `json:"schema"`
	ConfigurationRevision int64             `json:"configurationRevision"`
	ConfigurationDigest   string            `json:"configurationDigest"`
	GatewayPolicyDigest   string            `json:"gatewayPolicyDigest"`
	Destinations          []MailDestination `json:"destinations"`
}

// MailDestination pins one validated mail hostname, transport mode and its
// complete resolved address set for the gateway's network policy.
// RU: MailDestination закрепляет одно проверенное имя узла, режим транспорта
// и полный набор разрешённых DNS-адресов для сетевой политики шлюза.
type MailDestination struct {
	Hostname  string   `json:"hostname"`
	Port      int      `json:"port"`
	Protocol  string   `json:"protocol"`
	TLSMode   string   `json:"tlsMode"`
	Addresses []string `json:"addresses"`
}

// MailEndpointValid accepts only the registered protocol, port and TLS-mode
// combinations; all other combinations fail closed before DNS resolution.
// RU: MailEndpointValid допускает только зарегистрированные сочетания
// протокола, порта и режима TLS; остальные отклоняются до запроса DNS.
func MailEndpointValid(protocol string, port int, mode string) bool {
	switch protocol {
	case "smtp":
		return port == 465 && mode == "implicit" || port == 587 && mode == "starttls"
	case "pop3":
		return port == 995 && mode == "implicit" || port == 110 && mode == "starttls"
	case "imap":
		return port == 993 && mode == "implicit" || port == 143 && mode == "starttls"
	default:
		return false
	}
}

// Validate checks the document schema, source digests, destination bounds,
// unique endpoints and every pinned public address before it is rendered.
// RU: Validate проверяет схему, дайджесты источников, пределы числа адресатов,
// уникальность узлов и каждый публичный закреплённый адрес до отрисовки.
func (document MailDocument) Validate() error {
	invalid := errors.New("mail destination projection is invalid")
	if document.Schema != MailSchema || document.ConfigurationRevision < 1 || !validDigest(document.ConfigurationDigest) || !validDigest(document.GatewayPolicyDigest) || document.Destinations == nil || len(document.Destinations) > 64 {
		return invalid
	}
	seen := map[string]bool{}
	for _, destination := range document.Destinations {
		if _, err := NormalizeHostname(destination.Hostname); err != nil || !MailEndpointValid(destination.Protocol, destination.Port, destination.TLSMode) || len(destination.Addresses) == 0 || len(destination.Addresses) > 32 {
			return invalid
		}
		key := destination.Hostname + ":" + strconv.Itoa(destination.Port)
		if seen[key] {
			return invalid
		}
		seen[key] = true
		addresses := []netip.Addr{}
		for _, raw := range destination.Addresses {
			address, err := netip.ParseAddr(raw)
			if err != nil || address.String() != raw {
				return invalid
			}
			addresses = append(addresses, address)
		}
		if ValidateAddresses(addresses) != nil {
			return invalid
		}
	}
	return nil
}

// validDigest accepts only a canonical lowercase SHA-256 hexadecimal digest.
// RU: validDigest принимает только канонический строчный шестнадцатеричный дайджест SHA-256.
func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

// Digest hashes the canonical JSON representation of this projection so its
// immutable ConfigMap name and gateway readback refer to the same bytes.
// RU: Digest хеширует каноническое JSON-представление проекции, связывая имя
// неизменяемой ConfigMap и обратную проверку шлюза с одними данными.
func (document MailDocument) Digest() string {
	raw, _ := json.Marshal(document)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// NormalizeHostname accepts only lowercase ASCII FQDNs with valid LDH labels;
// it rejects IP literals, wildcards, delimiters and noncanonical spelling.
// RU: NormalizeHostname принимает только строчные ASCII FQDN с корректными
// LDH-метками, отклоняя IP-литералы, шаблоны, разделители и неканоническую запись.
func NormalizeHostname(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "*@/\\[]:%") || len(value) > 254 {
		return "", errors.New("hostname is invalid")
	}
	if value != strings.ToLower(value) || strings.HasSuffix(value, ".") || len(value) > 253 {
		return "", errors.New("hostname is invalid")
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return "", errors.New("IP literal is prohibited")
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return "", errors.New("hostname must be a FQDN")
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("hostname label is invalid")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", errors.New("hostname must use ASCII LDH labels")
			}
		}
	}
	return value, nil
}
