package emailpolicy

import (
	"encoding/json"
	"slices"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

const MaxMailboxSpecificationBytes = 256 << 10

// MailboxBinding передаётся только после разрешения существующего owner в CP.
type MailboxBinding struct {
	Ref, OrganizationRef, ConnectionRef string
	Revision, CredentialGeneration      int64
}

// BoundSpecification допускает неполный черновик, но ограничивает размер до записи.
func BoundSpecification(spec entity.EmailMailboxSpecification) error {
	if len(spec.AllowedFolders) > 100 || len(spec.Recipients) > 1000 || len(spec.Policies) > len(api.Operations()) {
		return errs.ErrInvalid
	}
	for _, policy := range spec.Policies {
		if len(policy.Folders) > 100 {
			return errs.ErrInvalid
		}
	}
	raw, err := json.Marshal(spec)
	if err != nil || len(raw) > MaxMailboxSpecificationBytes {
		return errs.ErrInvalid
	}
	return nil
}

// MaterializeMailbox не принимает owner-поля из пользовательской спецификации.
// EnvelopeFrom выводится из Sender, а коллекции не разделяют память с черновиком.
func MaterializeMailbox(spec entity.EmailMailboxSpecification, binding MailboxBinding) (api.Mailbox, error) {
	if err := BoundSpecification(spec); err != nil {
		return api.Mailbox{}, err
	}
	mailbox := api.Mailbox{
		Id: binding.Ref, TenantId: binding.OrganizationRef, ConnectionId: binding.ConnectionRef,
		Revision: binding.Revision, CredentialGeneration: binding.CredentialGeneration,
		Enabled: spec.Enabled, ReceiveProtocol: spec.ReceiveProtocol,
		AllowedFolders: slices.Clone(spec.AllowedFolders), ArchiveFolder: spec.ArchiveFolder, DraftsFolder: spec.DraftsFolder,
		Folder: spec.Folder, Sender: spec.Sender, EnvelopeFrom: spec.Sender, ReplyTo: spec.ReplyTo,
		Recipients: slices.Clone(spec.Recipients), HelloName: spec.HelloName, Smtp: spec.SMTP,
		Limits: spec.Limits, Policies: slices.Clone(spec.Policies),
	}
	if spec.IMAP != nil {
		endpoint := *spec.IMAP
		mailbox.Imap = &endpoint
	}
	if spec.POP != nil {
		endpoint := *spec.POP
		mailbox.Pop = &endpoint
	}
	for index := range mailbox.Policies {
		mailbox.Policies[index].Folders = slices.Clone(mailbox.Policies[index].Folders)
	}
	configuration := api.Configuration{Version: "email-bridge/v1", Revision: 1, ManagedBy: "ui", Source: "control-plane", Mailboxes: []api.Mailbox{mailbox}}
	if api.ValidateConfiguration(configuration) != nil || !mailboxNetworkShape(mailbox) {
		return api.Mailbox{}, errs.ErrInvalid
	}
	return mailbox, nil
}

// Закрытая матрица соответствует исполняемым SMTP/IMAP/POP3 маршрутам #1029.
func mailboxNetworkShape(mailbox api.Mailbox) bool {
	valid := func(endpoint *api.Endpoint, implicit, startTLS int) bool {
		return endpoint == nil || endpoint.Port == implicit && endpoint.TlsMode == "implicit" || endpoint.Port == startTLS && endpoint.TlsMode == "starttls"
	}
	return valid(&mailbox.Smtp, 465, 587) && valid(mailbox.Imap, 993, 143) && valid(mailbox.Pop, 995, 110)
}
