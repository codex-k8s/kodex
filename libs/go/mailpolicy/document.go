// Package mailpolicy задаёт единый wire/render contract почтовых сетевых pins
// для producer control-plane и consumer egress-gateway.
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
	MailSchema       = "egress-mail/v1"
	MailProfileName  = "email-mail"
	MailWorkload     = "email-bridge"
	MailOperation    = "email.transport"
	MaximumFileBytes = 64 << 10
)

type MailDocument struct {
	Schema                string            `json:"schema"`
	ConfigurationRevision int64             `json:"configurationRevision"`
	ConfigurationDigest   string            `json:"configurationDigest"`
	GatewayPolicyDigest   string            `json:"gatewayPolicyDigest"`
	Destinations          []MailDestination `json:"destinations"`
}
type MailDestination struct {
	Hostname  string   `json:"hostname"`
	Port      int      `json:"port"`
	Protocol  string   `json:"protocol"`
	TLSMode   string   `json:"tlsMode"`
	Addresses []string `json:"addresses"`
}

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

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}
func (document MailDocument) Digest() string {
	raw, _ := json.Marshal(document)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// NormalizeHostname принимает только canonical lowercase ASCII FQDN.
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
