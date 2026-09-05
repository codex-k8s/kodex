package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/emailprojection"
	"github.com/google/uuid"
)

var errMailboxDelivery = errors.New("mailbox delivery is incomplete")

type mailboxDeliveryStore interface {
	ReconcileConfiguredEmail(context.Context, api.Configuration) error
	EmailCredentialDigests(context.Context, api.Configuration) (map[string]string, error)
	ClaimEmailMailboxPublication(context.Context, string) (entity.EmailMailboxPublicationWork, bool, error)
	StageEmailMailboxPolicy(context.Context, entity.EmailMailboxPublicationWork, mailpolicy.MailDocument) error
	MarkEmailMailboxApplied(context.Context, entity.EmailMailboxPublicationWork) error
	CompleteEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) error
	ReleaseEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) error
	RecoverEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) (bool, error)
}
type mailboxDeliveryPublisher interface {
	CheckPublicationAdmission(context.Context) error
	PublishMailbox(context.Context, api.Configuration, map[string]string, mailpolicy.MailDocument) (emailprojection.Receipt, error)
	CheckMailbox(context.Context, api.Configuration, map[string]string, mailpolicy.MailDocument) (emailprojection.Receipt, error)
}
type mailboxDelivery struct {
	store                   mailboxDeliveryStore
	publisher               mailboxDeliveryPublisher
	resolver                mailpolicy.Resolver
	claimant, gatewayDigest string
	configured              *api.Configuration
	sourceFile              string
}

func newMailboxDelivery(store mailboxDeliveryStore, publisher mailboxDeliveryPublisher, digest string) (*mailboxDelivery, error) {
	if !emailpolicy.ValidDigest(digest) {
		return nil, errMailboxDelivery
	}
	servers, err := dnsresolver.LoadSystemServers("/etc/resolv.conf")
	if err != nil {
		return nil, errMailboxDelivery
	}
	resolver, err := dnsresolver.New(dnsresolver.Config{MinimumTTLSeconds: 5, MaximumTTLSeconds: 300,
		MaximumCacheEntries: 64, MaximumQueries: 8, MaximumCNAMEDepth: 8, MaximumRecords: 64,
		MaximumMessageBytes: 4096, QueryTimeoutMilliseconds: 2000}, servers, nil, nil)
	if err != nil {
		return nil, errMailboxDelivery
	}
	return &mailboxDelivery{store: store, publisher: publisher, resolver: resolver, claimant: uuid.NewString(), gatewayDigest: digest}, nil
}
func (delivery *mailboxDelivery) reconcile(ctx context.Context) (handled bool, resultErr error) {
	if err := delivery.publisher.CheckPublicationAdmission(ctx); err != nil {
		return false, err
	}
	configured := delivery.configured
	var importErr error
	if delivery.sourceFile != "" {
		raw, err := readBoundedFileLimit(delivery.sourceFile, 24<<20)
		var candidate api.Configuration
		if err != nil || api.Decode(raw, &candidate) != nil || api.ValidateConfiguration(candidate) != nil {
			importErr = errMailboxDelivery
		} else if candidate.Source != "release-bootstrap" || candidate.ManagedBy != "git" || candidate.Revision != 1 || len(candidate.Mailboxes) != 0 {
			configured = &candidate
		}
	}
	if configured != nil && importErr == nil {
		importErr = delivery.store.ReconcileConfiguredEmail(ctx, *configured)
		if errors.Is(importErr, errs.ErrMailboxPublicationPending) {
			importErr = nil
		}
	}
	// Непринятый Git source не отменяет восстановление уже одобренного owner effect.
	defer func() { resultErr = errors.Join(resultErr, importErr) }()
	work, found, err := delivery.store.ClaimEmailMailboxPublication(ctx, delivery.claimant)
	if err != nil || !found {
		// Другой claimant не разрешает повтор старого accepted snapshot.
		return true, err
	}
	// При истечении контекста lease остаётся durable и освобождается новым поколением.
	defer func() { _ = delivery.store.ReleaseEmailMailboxPublication(ctx, work) }()
	if recovered, err := delivery.store.RecoverEmailMailboxPublication(ctx, work); recovered || err != nil {
		return true, err
	}
	var document mailpolicy.MailDocument
	if len(work.PolicyDocument) == 0 {
		document, err = mailpolicy.Produce(ctx, work.Configuration, delivery.gatewayDigest, delivery.resolver)
		if err != nil {
			return true, err
		}
		if err := delivery.store.StageEmailMailboxPolicy(ctx, work, document); err != nil {
			return true, err
		}
	} else if json.Unmarshal(work.PolicyDocument, &document) != nil || document.Validate() != nil || document.GatewayPolicyDigest != delivery.gatewayDigest ||
		document.ConfigurationRevision != work.Configuration.Revision || document.ConfigurationDigest != api.Digest(work.Configuration) {
		return true, errMailboxDelivery
	}
	credentials, err := delivery.store.EmailCredentialDigests(ctx, work.Configuration)
	if err != nil {
		return true, err
	}
	if _, err := delivery.publisher.PublishMailbox(ctx, work.Configuration, credentials, document); err != nil {
		return true, err
	}
	if err := delivery.store.MarkEmailMailboxApplied(ctx, work); err != nil {
		return true, err
	}
	if _, err := delivery.publisher.CheckMailbox(ctx, work.Configuration, credentials, document); err != nil {
		return true, err
	}
	return true, delivery.store.CompleteEmailMailboxPublication(ctx, work)
}
