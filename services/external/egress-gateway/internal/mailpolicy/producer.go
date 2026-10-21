package mailpolicy

import (
	"context"
	"errors"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	shared "github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// Resolver is the DNS lookup contract of the shared mail projection producer.
// Resolver — контракт DNS-поиска общего producer почтовой проекции.
type Resolver = shared.Resolver

// Produce decodes the mailbox source and delegates pinning to the shared producer.
// Produce разбирает источник ящиков и передаёт закрепление адресов общему producer.
func Produce(ctx context.Context, raw []byte, base *policy.Active, resolver Resolver) (MailDocument, error) {
	var configuration api.Configuration
	if base == nil || api.Decode(raw, &configuration) != nil {
		return MailDocument{}, errors.New("mail projection source is invalid")
	}
	return shared.Produce(ctx, configuration, base.Digest(), resolver)
}

// RenderFiles emits deployment artifacts from one validated mail projection.
// RenderFiles создаёт артефакты установки из одной проверенной почтовой проекции.
func RenderFiles(document MailDocument) (map[string][]byte, error) {
	return shared.RenderFiles(document)
}
