package entity

import api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"

// EmailMailboxSpecification contains only editable mailbox routing, protocol, endpoint, folder,
// limits, and operation-policy settings, without owner lineage. These settings are validated before
// publication and do not themselves authorize a send or carry raw authentication secrets.
// EmailMailboxSpecification содержит только редактируемые настройки маршрутизации, протокола, конечных
// точек, папок, лимитов и политик операций ящика без происхождения владельца. Настройки проверяются до
// публикации и сами не разрешают отправку и не передают исходные секреты аутентификации.
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
