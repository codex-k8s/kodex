package platform

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryEmailMailboxConfigurationOwner embeds exact organization/configuration ownership through
// EMAIL_MAILBOX set and email connection, returning server mailbox identity. It does not accept an
// owner assignment from content or establish caller permission.
// queryEmailMailboxConfigurationOwner встраивает точного владельца organization/configuration
// через EMAIL_MAILBOX set и email connection, возвращая серверную идентичность mailbox. Не
// принимает назначение владельца из content и не определяет право caller.
//
//go:embed sql/email_mailbox_configuration_owner.sql
var queryEmailMailboxConfigurationOwner string

// queryEmailMailboxConfigurationInsertOwner embeds creation of the mailbox owner binding only from
// matching organization, EMAIL_MAILBOX set and email connection. The caller supplies a
// server-generated mailbox ref and requires one inserted row.
// queryEmailMailboxConfigurationInsertOwner встраивает создание owner binding mailbox только из
// совпадающих organization, EMAIL_MAILBOX set и email connection. Caller передаёт серверный
// mailbox ref и требует одну вставленную строку.
//
//go:embed sql/email_mailbox_configuration_insert_owner.sql
var queryEmailMailboxConfigurationInsertOwner string

// newMailboxRef generates an opaque mailbox-prefixed identifier from16 cryptographically random
// bytes; entropy failure is Unavailable. The ref is identity, not a credential or authorization
// token.
// newMailboxRef создаёт непрозрачный идентификатор mailbox с префиксом из16 криптографически
// случайных байтов; ошибка энтропии даёт Unavailable. Ref является идентичностью, а не credential
// и authorization token.
func newMailboxRef() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errs.ErrUnavailable
	}
	return "mailbox-" + hex.EncodeToString(raw[:]), nil
}

// changeEmailMailbox constrains organization-level EMAIL_MAILBOX input, resolves immutable
// connection ownership and locks its version. Create/save canonicalize decoded specification as
// JSON; existing sets require OCC and an allowed lifecycle action. It delegates managed revision
// changes, creates an owner binding if needed and returns fresh mailbox view in the caller
// transaction without publishing externally.
// changeEmailMailbox ограничивает вход организационным EMAIL_MAILBOX, разрешает неизменяемого
// connection owner и блокирует его версию. Create/save канонизируют декодированную specification в
// JSON; прежние sets требуют OCC и допустимого lifecycle action. Передаёт изменение managed
// revisions, при необходимости создаёт owner binding и возвращает свежий mailbox view в переданной
// транзакции без внешней публикации.
func (repository *Repository) changeEmailMailbox(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.EmailMailboxInput)
	if !ok || payload.Managed.ProjectRef != "" || payload.Managed.Kind != "" && payload.Managed.Kind != revisionservice.KindEmailMailbox {
		return commandOutcome{}, errs.ErrInvalid
	}
	connectionRef, mailboxRef := payload.ConnectionRef, ""
	if payload.Managed.ConfigurationRef != "" {
		if err := tx.QueryRow(ctx, queryEmailMailboxConfigurationOwner, current.organizationID, payload.Managed.ConfigurationRef).Scan(&connectionRef, &mailboxRef); err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
		if payload.ConnectionRef != "" && connectionRef != payload.ConnectionRef {
			return commandOutcome{}, errs.ErrNotFound
		}
	}
	var connectionID string
	var connectionVersion int64
	if err := tx.QueryRow(ctx, queryEmailCredentialLock, current.organizationID, connectionRef).Scan(&connectionID, &connectionVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		return commandOutcome{}, errs.ErrUnavailable
	}
	if input.Kind == command.CreateEmailMailboxDraft || input.Kind == command.SaveEmailMailboxDraft {
		specification, err := emailpolicy.DecodeSpecification(payload.Managed.ContentFormat, payload.Managed.Content)
		if err != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		canonical, err := json.Marshal(specification)
		if err != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		payload.Managed.Content, payload.Managed.ContentFormat = string(canonical), "JSON"
	}
	input.Payload = payload.Managed
	if payload.Managed.ConfigurationRef != "" {
		view, err := repository.emailMailboxViewTx(ctx, tx, current, connectionRef, connectionVersion, payload.Managed.ConfigurationRef, payload.Managed.RevisionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		if input.Mutation.ExpectedVersion == nil || view.Configuration.Version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		action := map[command.Kind]string{command.CreateEmailMailboxDraft: "CREATE_DRAFT", command.SaveEmailMailboxDraft: "SAVE", command.ValidateEmailMailboxDraft: "VALIDATE", command.PublishEmailMailboxDraft: "PUBLISH", command.DiscardEmailMailboxDraft: "DISCARD"}[input.Kind]
		if !emailpolicy.ActionAllowed(view.NextActions, action) {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	outcome, err := repository.changeManagedConfiguration(ctx, tx, current, input)
	if err != nil {
		return commandOutcome{}, err
	}
	if mailboxRef == "" {
		mailboxRef, err = newMailboxRef()
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		tag, err := tx.Exec(ctx, queryEmailMailboxConfigurationInsertOwner, current.organizationID, outcome.result.ManagedConfiguration.Ref, connectionRef, mailboxRef)
		if err != nil || tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	view, err := repository.emailMailboxViewTx(ctx, tx, current, connectionRef, connectionVersion, outcome.result.ManagedConfiguration.Ref, outcome.result.ManagedRevision.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	outcome.result.EmailMailbox = &view
	return outcome, nil
}

// validateEmailMailboxRevision decodes and materializes a revision using server
// mailbox/tenant/connection and current credential generation. It forces enabled=true only on the
// local validation copy so disabled specifications still require credential ownership.
// Syntax/config/credential conflicts return a safe diagnostic with Invalid; other backend failures
// propagate without external delivery.
// validateEmailMailboxRevision декодирует и материализует ревизию с серверными
// mailbox/tenant/connection и текущим credential generation. Назначает enabled=true только
// локальной проверяемой копии, чтобы disabled specification также требовала владельца credential.
// Syntax/config/credential conflicts возвращают безопасную diagnostic с Invalid, остальные backend
// errors передаются без внешней доставки.
func (repository *Repository) validateEmailMailboxRevision(ctx context.Context, tx pgx.Tx, current scope, configuration managedSet, revision entity.ManagedConfigurationRevision) ([]revisionservice.Diagnostic, error) {
	invalid := func(code string) ([]revisionservice.Diagnostic, error) {
		diagnostic := emailpolicy.Diagnostic(code)
		return []revisionservice.Diagnostic{{Code: diagnostic.Code, Message: diagnostic.Message}}, errs.ErrInvalid
	}
	specification, err := emailpolicy.DecodeSpecification(revision.ContentFormat, revision.Content)
	if err != nil {
		return invalid(emailpolicy.DiagnosticSyntax)
	}
	var connectionRef, mailboxRef string
	if err := tx.QueryRow(ctx, queryEmailMailboxConfigurationOwner, current.organizationID, configuration.Ref).Scan(&connectionRef, &mailboxRef); err != nil {
		return nil, errs.ErrUnavailable
	}
	var connectionID string
	var connectionVersion int64
	if err := tx.QueryRow(ctx, queryEmailCredentialLock, current.organizationID, connectionRef).Scan(&connectionID, &connectionVersion); err != nil {
		return nil, errs.ErrUnavailable
	}
	mailbox, err := emailpolicy.MaterializeMailbox(specification, emailpolicy.MailboxBinding{
		Ref: mailboxRef, OrganizationRef: current.organizationRef, ConnectionRef: connectionRef,
		Revision: revision.Revision, CredentialGeneration: connectionVersion,
	})
	if err != nil {
		return invalid(emailpolicy.DiagnosticConfiguration)
	}
	// Проверка владельца обязательна и для выключенной конфигурации.
	mailbox.Enabled = true
	if _, err := emailCredentialDigests(ctx, tx, api.Configuration{Mailboxes: []api.Mailbox{mailbox}}); err != nil {
		if errors.Is(err, errs.ErrConflict) {
			return invalid(emailpolicy.DiagnosticCredential)
		}
		return nil, err
	}
	return nil, nil
}
