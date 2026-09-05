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

const (
	MailSchema       = shared.MailSchema
	MailProfileName  = shared.MailProfileName
	MailWorkload     = shared.MailWorkload
	MailOperation    = shared.MailOperation
	MaximumFileBytes = shared.MaximumFileBytes
)

type MailDocument = shared.MailDocument
type MailDestination = shared.MailDestination

type MailActive struct {
	document MailDocument
	digest   string
	limits   policy.Limits
}

func MailEndpointValid(protocol string, port int, mode string) bool {
	return shared.MailEndpointValid(protocol, port, mode)
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
