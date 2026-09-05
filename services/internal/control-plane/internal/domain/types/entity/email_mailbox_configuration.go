package entity

import api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"

// EmailMailboxSpecification содержит только редактируемые параметры, без owner lineage.
type EmailMailboxSpecification struct {
	Enabled         bool                       `json:"enabled"`
	ReceiveProtocol api.MailboxReceiveProtocol `json:"receive_protocol"`
	AllowedFolders  []string                   `json:"allowed_folders"`
	ArchiveFolder   string                     `json:"archive_folder"`
	DraftsFolder    string                     `json:"drafts_folder"`
	Folder          string                     `json:"folder"`
	Sender          string                     `json:"sender"`
	ReplyTo         string                     `json:"reply_to"`
	Recipients      []string                   `json:"recipients"`
	HelloName       string                     `json:"hello_name"`
	SMTP            api.Endpoint               `json:"smtp"`
	IMAP            *api.Endpoint              `json:"imap,omitempty"`
	POP             *api.Endpoint              `json:"pop,omitempty"`
	Limits          api.Limits                 `json:"limits"`
	Policies        []api.OperationPolicy      `json:"policies"`
}
