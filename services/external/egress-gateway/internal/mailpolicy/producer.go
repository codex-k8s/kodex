package mailpolicy

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/dnsresolver"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

type Resolver interface {
	Resolve(context.Context, string) (dnsresolver.Snapshot, error)
}

// Produce выводит CNI/runtime pins из единого typed mailbox source, без credential values.
func Produce(ctx context.Context, raw []byte, base *policy.Active, resolver Resolver) (MailDocument, error) {
	var configuration api.Configuration
	if ctx.Err() != nil || base == nil || resolver == nil || api.Decode(raw, &configuration) != nil || api.ValidateConfiguration(configuration) != nil {
		return MailDocument{}, errors.New("mail projection source is invalid")
	}
	result := MailDocument{Schema: MailSchema, ConfigurationRevision: configuration.Revision,
		ConfigurationDigest: api.Digest(configuration), GatewayPolicyDigest: base.Digest(), Destinations: []MailDestination{}}
	seen := map[string]MailDestination{}
	for _, mailbox := range configuration.Mailboxes {
		for _, endpoint := range []struct {
			protocol string
			value    *api.Endpoint
		}{{"smtp", &mailbox.Smtp}, {"pop3", mailbox.Pop}, {"imap", mailbox.Imap}} {
			if endpoint.value == nil {
				continue
			}
			e := endpoint.value
			if !MailEndpointValid(endpoint.protocol, e.Port, string(e.TlsMode)) || e.ServerName != e.Host {
				return MailDocument{}, errors.New("mail endpoint is not registered")
			}
			key := e.Host + ":" + strconv.Itoa(e.Port)
			if previous, ok := seen[key]; ok {
				if previous.Protocol != endpoint.protocol || previous.TLSMode != string(e.TlsMode) {
					return MailDocument{}, errors.New("mail endpoint has conflicting transport modes")
				}
				continue
			}
			if len(seen) == 64 {
				return MailDocument{}, errors.New("mail destination count exceeds bound")
			}
			snapshot, err := resolver.Resolve(ctx, e.Host)
			if err != nil || ctx.Err() != nil || !time.Now().Before(snapshot.ExpiresAt) || dnsresolver.ValidateAddresses(snapshot.Addresses) != nil {
				return MailDocument{}, errors.New("mail endpoint DNS snapshot is invalid")
			}
			destination := MailDestination{Hostname: e.Host, Port: e.Port, Protocol: endpoint.protocol, TLSMode: string(e.TlsMode), Addresses: []string{}}
			for _, address := range snapshot.Addresses {
				destination.Addresses = append(destination.Addresses, address.String())
			}
			sort.Strings(destination.Addresses)
			seen[key] = destination
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result.Destinations = append(result.Destinations, seen[key])
	}
	if err := result.Validate(); err != nil {
		return MailDocument{}, err
	}
	return result, nil
}
