package emailpolicy

import (
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// MailboxProjection не содержит endpoint, credential descriptors или содержимое писем.
type MailboxProjection struct {
	Ref                  string                `json:"ref"`
	OrganizationRef      string                `json:"organization_ref"`
	ConnectionRef        string                `json:"connection_ref"`
	Revision             int64                 `json:"revision"`
	CredentialGeneration int64                 `json:"credential_generation"`
	Enabled              bool                  `json:"enabled"`
	Sender               string                `json:"sender"`
	Folder               string                `json:"folder"`
	ArchiveFolder        string                `json:"archive_folder"`
	DraftsFolder         string                `json:"drafts_folder"`
	AllowedFolders       []string              `json:"allowed_folders"`
	Recipients           []string              `json:"recipients"`
	Policies             []api.OperationPolicy `json:"policies"`
	SourceDigest         string                `json:"source_digest"`
}

type ConfigurationProjection struct {
	Revision  int64
	Digest    string
	Mailboxes []MailboxProjection
}

func DecodeConfiguration(raw []byte) (ConfigurationProjection, error) {
	var config api.Configuration
	if api.Decode(raw, &config) != nil || api.ValidateConfiguration(config) != nil {
		return ConfigurationProjection{}, errs.ErrInvalid
	}
	result := ConfigurationProjection{Revision: config.Revision, Digest: api.Digest(config), Mailboxes: []MailboxProjection{}}
	for _, mailbox := range config.Mailboxes {
		if !mailboxNetworkShape(mailbox) {
			return ConfigurationProjection{}, errs.ErrInvalid
		}
		result.Mailboxes = append(result.Mailboxes, MailboxProjection{
			Ref: mailbox.Id, OrganizationRef: mailbox.TenantId, ConnectionRef: mailbox.ConnectionId,
			Revision: mailbox.Revision, CredentialGeneration: mailbox.CredentialGeneration,
			Enabled: mailbox.Enabled, Sender: mailbox.Sender, Folder: mailbox.Folder,
			ArchiveFolder: mailbox.ArchiveFolder, DraftsFolder: mailbox.DraftsFolder,
			AllowedFolders: mailbox.AllowedFolders, Recipients: mailbox.Recipients, Policies: mailbox.Policies,
			SourceDigest: api.Digest(mailbox),
		})
	}
	return result, nil
}
