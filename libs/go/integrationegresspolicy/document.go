// Package integrationegresspolicy связывает owner snapshot OpenAPI origins,
// публичные DNS pins и отдельный HTTPS CONNECT listener интеграций.
package integrationegresspolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"

	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
)

const (
	Schema              = "egress-integration/v1"
	ProfileName         = "integration-openapi"
	Workload            = "integration-gateway"
	Operation           = "integration.openapi"
	MaximumFileBytes    = 64 << 10
	MaximumDestinations = 64
)

// Document не содержит конфигурацию подключения, учётные данные или input
// инструмента: только поколение owner-проекции и exact сетевые pins.
type Document struct {
	Schema              string        `json:"schema"`
	Generation          int64         `json:"generation"`
	SourceDigest        string        `json:"sourceDigest"`
	GatewayPolicyDigest string        `json:"gatewayPolicyDigest"`
	Destinations        []Destination `json:"destinations"`
}

type Destination struct {
	Hostname  string   `json:"hostname"`
	Port      int      `json:"port"`
	Addresses []string `json:"addresses"`
}

func (document Document) Validate() error {
	invalid := errors.New("integration egress document is invalid")
	if document.Schema != Schema || document.Generation < 1 || !validDigest(document.SourceDigest) ||
		!validDigest(document.GatewayPolicyDigest) || document.Destinations == nil ||
		len(document.Destinations) > MaximumDestinations {
		return invalid
	}
	previousHost := ""
	hostnames := make([]string, 0, len(document.Destinations))
	for _, destination := range document.Destinations {
		if _, err := mailpolicy.NormalizeHostname(destination.Hostname); err != nil ||
			destination.Port != 443 || destination.Hostname <= previousHost ||
			len(destination.Addresses) == 0 || len(destination.Addresses) > 32 {
			return invalid
		}
		previousHost = destination.Hostname
		hostnames = append(hostnames, destination.Hostname)
		addresses := make([]netip.Addr, 0, len(destination.Addresses))
		previousAddress := ""
		for _, raw := range destination.Addresses {
			address, err := netip.ParseAddr(raw)
			if err != nil || address.String() != raw || raw <= previousAddress {
				return invalid
			}
			previousAddress = raw
			addresses = append(addresses, address)
		}
		if mailpolicy.ValidateAddresses(addresses) != nil {
			return invalid
		}
	}
	if document.SourceDigest != SourceDigest(hostnames) {
		return invalid
	}
	return nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

// Digest использует единственный канонический encoding/json порядок полей.
func (document Document) Digest() string {
	raw, _ := json.Marshal(document)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SourceDigest связывает опубликованный документ с полным множеством origins.
func SourceDigest(hostnames []string) string {
	ordered := append([]string{}, hostnames...)
	slices.Sort(ordered)
	raw, _ := json.Marshal(ordered)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
