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

//go:embed sql/email_mailbox_configuration_owner.sql
var queryEmailMailboxConfigurationOwner string

//go:embed sql/email_mailbox_configuration_insert_owner.sql
var queryEmailMailboxConfigurationInsertOwner string

func newMailboxRef() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errs.ErrUnavailable
	}
	return "mailbox-" + hex.EncodeToString(raw[:]), nil
}

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
