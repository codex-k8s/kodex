package mailpolicy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"strconv"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/dnsresolver"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

const (
	MailSchema       = "egress-mail/v1"
	MailProfileName  = "email-mail"
	MailWorkload     = "email-bridge"
	MailOperation    = "email.transport"
	MaximumFileBytes = policy.MaximumFileBytes
)

// MailDocument содержит только произведённые endpoint pins, не mailbox credentials.
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

type MailActive struct {
	document MailDocument
	digest   string
	limits   policy.Limits
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

func (d MailDocument) Validate() error {
	invalid := errors.New("mail destination projection is invalid")
	if d.Schema != MailSchema || d.ConfigurationRevision < 1 || !mailDigest(d.ConfigurationDigest) || !mailDigest(d.GatewayPolicyDigest) || d.Destinations == nil || len(d.Destinations) > 64 {
		return invalid
	}
	seen := map[string]bool{}
	for _, destination := range d.Destinations {
		if _, err := policy.NormalizeHostname(destination.Hostname); err != nil || !MailEndpointValid(destination.Protocol, destination.Port, destination.TLSMode) || len(destination.Addresses) == 0 || len(destination.Addresses) > 32 {
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
		if dnsresolver.ValidateAddresses(addresses) != nil {
			return invalid
		}
	}
	return nil
}

func mailDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func (d MailDocument) Digest() string {
	raw, _ := json.Marshal(d)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (a *MailActive) Revision() string {
	return "mail-" + strconv.FormatInt(a.document.ConfigurationRevision, 10)
}
func (a *MailActive) Digest() string        { return a.digest }
func (a *MailActive) Limits() policy.Limits { return a.limits }
func (a *MailActive) ProfileIdentity() (string, string, string) {
	return MailProfileName, MailWorkload, MailOperation
}
func (a *MailActive) Configured() bool                  { return len(a.document.Destinations) != 0 }
func (a *MailActive) Allows(host string, port int) bool { return a.TLSMode(host, port) != "" }
func (a *MailActive) TLSMode(host string, port int) string {
	for _, d := range a.document.Destinations {
		if d.Hostname == host && d.Port == port {
			return d.TLSMode
		}
	}
	return ""
}
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
func (a *MailActive) ConfigurationIdentity() (string, string) {
	return strconv.FormatInt(a.document.ConfigurationRevision, 10), a.document.ConfigurationDigest
}

func (a *MailActive) Destinations() []policy.Destination {
	result := make([]policy.Destination, 0, len(a.document.Destinations))
	for _, d := range a.document.Destinations {
		result = append(result, policy.Destination{Hostname: d.Hostname, Port: d.Port})
	}
	return result
}
