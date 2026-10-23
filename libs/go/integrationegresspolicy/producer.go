package integrationegresspolicy

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
)

// Snapshot carries one resolver result and its expiry; a producer must reject
// expired or non-public/mixed addresses before publishing a policy document.
// RU: Snapshot содержит результат DNS и срок действия; producer обязан
// отклонить просроченные и непубличные/смешанные адреса до публикации политики.
type Snapshot struct {
	Addresses []netip.Addr
	ExpiresAt time.Time
}

// Resolver is the DNS read boundary for exact hostnames from the owner path.
// RU: Resolver является границей DNS-чтения для точных имён из owner path.
type Resolver interface {
	// Resolve returns one timestamped address snapshot for a hostname.
	// RU: Resolve возвращает один снимок адресов с временем действия для имени узла.
	Resolve(context.Context, string) (Snapshot, error)
}

// Produce accepts hostnames only from a verified owner read path, sorts and
// validates the full origin list, then checks every live DNS snapshot before
// producing one document. It does not publish Kubernetes resources or turn
// caller-supplied names into authority.
// RU: Produce принимает имена только из проверенного owner read path,
// сортирует и проверяет весь список origins, затем проверяет каждый действующий
// DNS-снимок до создания одного документа. Она не публикует ресурсы Kubernetes
// и не превращает переданные вызывающим имена в полномочия.
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
