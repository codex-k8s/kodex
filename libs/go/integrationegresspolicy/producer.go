package integrationegresspolicy

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
)

type Snapshot struct {
	Addresses []netip.Addr
	ExpiresAt time.Time
}

type Resolver interface {
	Resolve(context.Context, string) (Snapshot, error)
}

// Produce принимает hostnames только от проверенного owner read path.
// Вся DNS-картина проверяется до публикации одного сетевого допуска.
func Produce(ctx context.Context, hostnames []string, generation int64, gatewayPolicyDigest string, resolver Resolver) (Document, error) {
	invalid := errors.New("integration egress source is invalid")
	if ctx.Err() != nil || !validDigest(gatewayPolicyDigest) || resolver == nil || generation < 1 ||
		len(hostnames) > MaximumDestinations {
		return Document{}, invalid
	}
	ordered := append([]string{}, hostnames...)
	slices.Sort(ordered)
	for index, hostname := range ordered {
		if _, err := mailpolicy.NormalizeHostname(hostname); err != nil ||
			index > 0 && ordered[index-1] == hostname {
			return Document{}, invalid
		}
	}
	document := Document{
		Schema: Schema, Generation: generation, SourceDigest: SourceDigest(ordered),
		GatewayPolicyDigest: gatewayPolicyDigest, Destinations: []Destination{},
	}
	for _, hostname := range ordered {
		snapshot, err := resolver.Resolve(ctx, hostname)
		if err != nil || ctx.Err() != nil || !time.Now().Before(snapshot.ExpiresAt) ||
			len(snapshot.Addresses) > 32 || mailpolicy.ValidateAddresses(snapshot.Addresses) != nil {
			return Document{}, errors.New("integration egress DNS snapshot is invalid")
		}
		pins := make([]string, 0, len(snapshot.Addresses))
		for _, address := range snapshot.Addresses {
			pins = append(pins, address.String())
		}
		slices.Sort(pins)
		pins = slices.Compact(pins)
		document.Destinations = append(document.Destinations, Destination{Hostname: hostname, Port: 443, Addresses: pins})
	}
	if document.Validate() != nil {
		return Document{}, invalid
	}
	return document, nil
}
