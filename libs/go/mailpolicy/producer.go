package mailpolicy

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strconv"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

// Snapshot captures one resolver result together with its expiry; its entire
// address set must pass validation before any network permission is rendered.
// RU: Snapshot хранит результат DNS и срок его действия; весь набор адресов
// проходит проверку до построения сетевого разрешения.
type Snapshot struct {
	Addresses []netip.Addr
	ExpiresAt time.Time
}

// Resolver supplies a bounded DNS snapshot for one registered hostname.
// RU: Resolver предоставляет ограниченный снимок DNS для зарегистрированного имени узла.
type Resolver interface {
	// Resolve obtains addresses and their expiry, respecting cancellation.
	// RU: Resolve получает адреса и срок их действия с учётом отмены контекста.
	Resolve(context.Context, string) (Snapshot, error)
}

// Produce derives immutable mail egress pins from enabled typed mailboxes and
// fresh public DNS snapshots, without exporting credentials or disabled endpoints.
// RU: Produce выводит неизменяемые почтовые сетевые привязки из включённых
// типизированных ящиков и свежих публичных DNS-снимков, не раскрывая пароли
// и не сохраняя выключенные узлы.
func Produce(ctx context.Context, configuration api.Configuration, gatewayPolicyDigest string, resolver Resolver) (MailDocument, error) {
	if ctx.Err() != nil || !validDigest(gatewayPolicyDigest) || resolver == nil || api.ValidateConfiguration(configuration) != nil {
		return MailDocument{}, errors.New("mail projection source is invalid")
	}
	result := MailDocument{Schema: MailSchema, ConfigurationRevision: configuration.Revision,
		ConfigurationDigest: api.Digest(configuration), GatewayPolicyDigest: gatewayPolicyDigest, Destinations: []MailDestination{}}
	seen := map[string]MailDestination{}
	for _, mailbox := range configuration.Mailboxes {
		if !mailbox.Enabled {
			continue
		}
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
			if err != nil || ctx.Err() != nil || !time.Now().Before(snapshot.ExpiresAt) || ValidateAddresses(snapshot.Addresses) != nil {
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
