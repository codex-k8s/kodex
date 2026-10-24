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

// errMailboxDelivery marks invalid or incomplete mailbox delivery setup/input with a fixed runtime
// error rather than embedding source content or credentials.
// errMailboxDelivery обозначает неверные либо неполные настройку и ввод доставки mailbox
// фиксированной runtime-ошибкой вместо включения source content либо credentials.
var errMailboxDelivery = errors.New("mailbox delivery is incomplete")

// mailboxDeliveryStore defines owner-side source reconciliation, exact publication claims, staged
// policy, applied/completed state and recovery/release operations.
// mailboxDeliveryStore определяет согласование источника владельцем, точные publication claims,
// staged policy, applied/completed состояние и операции recovery и release.
type mailboxDeliveryStore interface {
	// ReconcileConfiguredEmail asks the owner to reconcile a validated configured source without
	// bypassing an already pending mailbox publication.
	// ReconcileConfiguredEmail передаёт владельцу согласование проверенного configured source без
	// обхода уже ожидающей публикации mailbox.
	ReconcileConfiguredEmail(context.Context, api.Configuration) error
	// EmailCredentialDigests reads owner-resolved credential commitments for the exact configuration
	// without returning credential values.
	// EmailCredentialDigests читает разрешённые владельцем commitments credentials точной конфигурации
	// без возврата значений credentials.
	EmailCredentialDigests(context.Context, api.Configuration) (map[string]string, error)
	// ClaimEmailMailboxPublication asks the owner for one publication work item bound to the claimant
	// and reports absence without authorizing legacy replay.
	// ClaimEmailMailboxPublication запрашивает у владельца одну работу публикации с привязкой к
	// claimant и сообщает отсутствие без допуска legacy replay.
	ClaimEmailMailboxPublication(context.Context, string) (entity.EmailMailboxPublicationWork, bool, error)
	// StageEmailMailboxPolicy persists the produced network policy against the exact claimed
	// publication work through its authoritative owner.
	// StageEmailMailboxPolicy сохраняет построенную network policy для точной claimed работы
	// публикации через её авторитетного владельца.
	StageEmailMailboxPolicy(context.Context, entity.EmailMailboxPublicationWork, mailpolicy.MailDocument) error
	// MarkEmailMailboxApplied records the application step for the exact claimed publication before
	// readback and final completion.
	// MarkEmailMailboxApplied фиксирует шаг применения точной claimed публикации до readback и
	// окончательного завершения.
	MarkEmailMailboxApplied(context.Context, entity.EmailMailboxPublicationWork) error
	// CompleteEmailMailboxPublication delegates final publication completion to the owner after the
	// caller has obtained the required external readback.
	// CompleteEmailMailboxPublication передаёт окончательное завершение публикации владельцу после
	// получения вызывающим обязательного внешнего readback.
	CompleteEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) error
	// ReleaseEmailMailboxPublication releases the exact claimed publication through its owner;
	// cancellation may leave durable recovery to a later generation.
	// ReleaseEmailMailboxPublication освобождает точную claimed публикацию через владельца; отмена
	// может оставить durable recovery следующему поколению.
	ReleaseEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) error
	// RecoverEmailMailboxPublication asks the owner whether the exact claimed publication can be
	// recovered before attempting another external publication effect.
	// RecoverEmailMailboxPublication запрашивает у владельца возможность восстановления точной claimed
	// публикации до попытки очередного внешнего эффекта публикации.
	RecoverEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) (bool, error)
}

// mailboxDeliveryPublisher separates publication admission, external publication and exact
// configuration/credential/policy readback without owning the domain completion transaction.
// mailboxDeliveryPublisher разделяет admission публикации, внешнюю публикацию и точный readback
// configuration, credentials и policy без владения доменной транзакцией завершения.
type mailboxDeliveryPublisher interface {
	// CheckPublicationAdmission checks publisher-side admission independently of an individual mailbox
	// publication reaching READY.
	// CheckPublicationAdmission проверяет admission стороны publisher независимо от достижения READY
	// отдельной публикацией mailbox.
	CheckPublicationAdmission(context.Context) error
	// PublishMailbox publishes one claimed configuration with exact credential commitments and network
	// policy, returning a publisher receipt or failure.
	// PublishMailbox публикует одну claimed конфигурацию с точными commitments credentials и network
	// policy, возвращая квитанцию publisher либо отказ.
	PublishMailbox(context.Context, api.Configuration, map[string]string, mailpolicy.MailDocument) (emailprojection.Receipt, error)
	// CheckMailbox reads back the expected configuration, credential commitments and policy rather
	// than treating publication intent as accepted owner state.
	// CheckMailbox читает ожидаемые configuration, commitments credentials и policy вместо признания
	// intent публикации принятым состоянием владельца.
	CheckMailbox(context.Context, api.Configuration, map[string]string, mailpolicy.MailDocument) (emailprojection.Receipt, error)
}

// mailboxDelivery coordinates claimant-bound owner work with DNS-derived policy and external
// publication while keeping configured source input distinct from recovered owner work.
// mailboxDelivery согласует claimant-bound работу владельца с policy из DNS и внешней публикацией,
// сохраняя различие configured source input и восстановленной owner работы.
type mailboxDelivery struct {
	store                   mailboxDeliveryStore
	publisher               mailboxDeliveryPublisher
	resolver                mailpolicy.Resolver
	claimant, gatewayDigest string
	configured              *api.Configuration
	sourceFile              string
}

// newMailboxDelivery validates the gateway policy digest, constructs a bounded DNS resolver from
// system servers and assigns a fresh claimant without publishing configuration.
// newMailboxDelivery проверяет digest gateway policy, создаёт ограниченный DNS resolver по
// системным servers и назначает новый claimant без публикации конфигурации.
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

// reconcile checks admission, reloads bounded Git input and continues owner recovery even on
// import failure; claimed work follows stage, publish, applied, readback and complete, while an
// absent claim still marks the path handled.
// reconcile проверяет admission, перечитывает ограниченный Git ввод и продолжает owner recovery
// даже при отказе импорта; claimed работа проходит stage, publish, applied, readback и complete, а
// отсутствие claim также помечает путь обработанным.
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
