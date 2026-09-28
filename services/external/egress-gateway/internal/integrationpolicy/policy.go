// Package integrationpolicy загружает отдельный immutable допуск OpenAPI CONNECT.
package integrationpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"strconv"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

type Active struct {
	document shared.Document
	digest   string
	limits   policy.Limits
}

func LoadFile(path, expectedDigest string, base *policy.Active) (*Active, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open integration policy file")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, shared.MaximumFileBytes+1))
	if err != nil {
		return nil, errors.New("read integration policy file")
	}
	return Load(raw, expectedDigest, base)
}

func Load(raw []byte, expectedDigest string, base *policy.Active) (*Active, error) {
	if base == nil || len(raw) == 0 || len(raw) > shared.MaximumFileBytes || policy.RejectDuplicateFields(raw) != nil {
		return nil, errors.New("integration policy document is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document shared.Document
	if decoder.Decode(&document) != nil || document.Validate() != nil || document.GatewayPolicyDigest != base.Digest() {
		return nil, errors.New("integration policy projection is invalid")
	}
	if document.Digest() != expectedDigest {
		return nil, errors.New("integration policy digest mismatch")
	}
	return &Active{document: document, digest: expectedDigest, limits: base.Limits()}, nil
}

func (a *Active) Revision() string {
	return "integration-" + strconv.FormatInt(a.document.Generation, 10)
}
func (a *Active) Digest() string        { return a.digest }
func (a *Active) Limits() policy.Limits { return a.limits }
func (a *Active) ProfileIdentity() (string, string, string) {
	return shared.ProfileName, shared.Workload, shared.Operation
}
func (a *Active) Configured() bool                  { return len(a.document.Destinations) > 0 }
func (a *Active) Allows(host string, port int) bool { return a.TLSMode(host, port) != "" }

// TLSMode включает обязательную проверку ClientHello/SNI до DNS и dial.
func (a *Active) TLSMode(host string, port int) string {
	for _, destination := range a.document.Destinations {
		if destination.Hostname == host && destination.Port == port {
			return "implicit"
		}
	}
	return ""
}
func (a *Active) AllowsLiteral(host string, port int, address netip.Addr) bool {
	for _, destination := range a.document.Destinations {
		if destination.Hostname != host || destination.Port != port {
			continue
		}
		for _, pin := range destination.Addresses {
			if pin == address.String() {
				return true
			}
		}
	}
	return false
}
func (a *Active) Destinations() []policy.Destination {
	result := make([]policy.Destination, 0, len(a.document.Destinations))
	for _, destination := range a.document.Destinations {
		result = append(result, policy.Destination{Hostname: destination.Hostname, Port: destination.Port})
	}
	return result
}
