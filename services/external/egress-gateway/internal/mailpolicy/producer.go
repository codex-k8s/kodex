package mailpolicy

import (
	"context"
	"errors"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	shared "github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

type Resolver = shared.Resolver

// Produce адаптирует CLI document и проверенный gateway policy к общему
// typed producer. Правила pins и render имеют единственную реализацию в libs/go.
func Produce(ctx context.Context, raw []byte, base *policy.Active, resolver Resolver) (MailDocument, error) {
	var configuration api.Configuration
	if base == nil || api.Decode(raw, &configuration) != nil {
		return MailDocument{}, errors.New("mail projection source is invalid")
	}
	return shared.Produce(ctx, configuration, base.Digest(), resolver)
}

func RenderFiles(document MailDocument) (map[string][]byte, error) {
	return shared.RenderFiles(document)
}
