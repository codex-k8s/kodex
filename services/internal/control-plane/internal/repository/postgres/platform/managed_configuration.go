package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

type managedSet struct {
	entity.ManagedConfigurationSet
	id, projectID, currentRevisionID string
}

type managedHistoryCursor struct {
	Version int    `json:"v"`
	Filter  string `json:"f"`
	Before  int64  `json:"b"`
}

func (repository *Repository) changeManagedConfiguration(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ManagedConfigurationInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	kind, action := managedCommand(input.Kind)
	if action == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.Kind != "" && payload.Kind != kind {
		return commandOutcome{}, errs.ErrInvalid
	}
	if action == "COPY" {
		return repository.copyManagedConfiguration(ctx, tx, current, input, payload)
	}
	configuration, err := repository.resolveManagedSet(ctx, tx, current, payload, kind, action == "CREATE")
	if err != nil {
		return commandOutcome{}, err
	}
	if err := rejectShippedRoleImageMutation(ctx, tx, current.organizationID, configuration); err != nil {
		return commandOutcome{}, err
	}
	if (action != "CREATE" || payload.ConfigurationRef != "") &&
		(input.Mutation.ExpectedVersion == nil || configuration.Version != *input.Mutation.ExpectedVersion) {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var revision *entity.ManagedConfigurationRevision
	var publicationPlan *entity.RevisionImpactPlan
	if action == "DETACH" {
		if err := repository.cancelConfigurationWriteBacks(ctx, tx, current, configuration.Ref, ""); err != nil {
			return commandOutcome{}, err
		}
	}
	if (action == "CREATE" || action == "SAVE" || action == "DISCARD" || action == "VALIDATE" || action == "PUBLISH") && configuration.ManagedBy != "UI" {
		return commandOutcome{}, errs.ErrConflict
	}
	switch action {
	case "PREPARE_PROMPT_IMPACT":
		return repository.preparePromptTemplateImpact(ctx, tx, current, configuration, input)
	case "PREPARE_IMPACT":
		return repository.prepareRoleImageImpact(ctx, tx, current, configuration, input)
	case "SAVE", "DISCARD":
		locked, lockErr := repository.lockManagedRevision(ctx, tx, current, configuration, payload.RevisionRef)
		if lockErr != nil {
			return commandOutcome{}, lockErr
		}
		if locked.State != "DRAFT" && locked.State != "VALID" && locked.State != "INVALID" {
			return commandOutcome{}, errs.ErrConflict
		}
		item, discardErr := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationDiscardRevision, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "configuration_set_id": configuration.id, "revision_id": locked.RefID,
		}))
		if discardErr != nil {
			return commandOutcome{}, mapWriteError(discardErr)
		}
		revision = &item.ManagedConfigurationRevision
		if action == "SAVE" {
			format := strings.ToUpper(strings.TrimSpace(payload.ContentFormat))
			if len(payload.Content) > 256<<10 || !utf8.ValidString(payload.Content) || strings.ContainsRune(payload.Content, 0) ||
				kind == revisionservice.KindPromptTemplate && format != "TEXT" ||
				kind != revisionservice.KindPromptTemplate && format != "JSON" && format != "YAML" && format != "TOML" {
				return commandOutcome{}, errs.ErrInvalid
			}
			content := strings.TrimSpace(payload.Content)
			if kind == revisionservice.KindIntegrationDefinition {
				format, content = repository.normalizeIntegrationDraft(format, content, configuration.ManagedBy)
			}
			digest := sha256.Sum256([]byte(content))
			ref, refErr := newRef("mrev")
			if refErr != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			created, createErr := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationInsertRevision, pgx.StrictNamedArgs{
				"revision_ref": ref, "organization_id": current.organizationID, "configuration_set_id": configuration.id,
				"content_format": format, "content": content, "digest": hex.EncodeToString(digest[:]),
				"parent_revision_id": locked.RefID, "actor_id": current.actorID,
			}))
			if createErr != nil {
				return commandOutcome{}, mapWriteError(createErr)
			}
			revision = &created.ManagedConfigurationRevision
		}
		if err := tx.QueryRow(ctx, queryManagedConfigurationTouchSet, pgx.StrictNamedArgs{
			"configuration_set_id": configuration.id, "expected_version": configuration.Version,
		}).Scan(&configuration.Version, &configuration.UpdatedAt); err != nil {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
	case "CREATE":
		if strings.TrimSpace(payload.Name) == "" || len(payload.Name) > 160 || strings.TrimSpace(payload.Content) == "" || len(payload.Content) > 256<<10 {
			return commandOutcome{}, errs.ErrInvalid
		}
		format := strings.ToUpper(strings.TrimSpace(payload.ContentFormat))
		if kind == revisionservice.KindPromptTemplate && format != "TEXT" ||
			kind != revisionservice.KindPromptTemplate && format != "JSON" && format != "YAML" && format != "TOML" {
			return commandOutcome{}, errs.ErrInvalid
		}
		if configuration.ManagedBy != "UI" {
			return commandOutcome{}, errs.ErrConflict
		}
		content := strings.TrimSpace(payload.Content)
		if kind == revisionservice.KindIntegrationDefinition {
			format, content = repository.normalizeIntegrationDraft(format, content, configuration.ManagedBy)
		}
		digest := sha256.Sum256([]byte(content))
		revisionRef, refErr := newRef("mrev")
		if refErr != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		item, itemErr := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationInsertRevision, pgx.StrictNamedArgs{
			"revision_ref": revisionRef, "organization_id": current.organizationID,
			"configuration_set_id": configuration.id, "content_format": format,
			"content": content, "digest": hex.EncodeToString(digest[:]),
			"parent_revision_id": configuration.currentRevisionID, "actor_id": current.actorID,
		}))
		if itemErr != nil {
			return commandOutcome{}, mapWriteError(itemErr)
		}
		revision = &item.ManagedConfigurationRevision
		if payload.ConfigurationRef != "" {
			if err := tx.QueryRow(ctx, queryManagedConfigurationTouchSet, pgx.StrictNamedArgs{
				"configuration_set_id": configuration.id, "expected_version": configuration.Version,
			}).Scan(&configuration.Version, &configuration.UpdatedAt); err != nil {
				return commandOutcome{}, errs.ErrVersionMismatch
			}
		}
	case "VALIDATE":
		locked, lockErr := repository.lockManagedRevision(ctx, tx, current, configuration, payload.RevisionRef)
		if lockErr != nil {
			return commandOutcome{}, lockErr
		}
		_, diagnostics, validationErr := revisionservice.Validate(kind, locked.ContentFormat, locked.Content)
		if kind == revisionservice.KindPromptTemplate {
			diagnostics, validationErr = repository.validatePromptScopeTx(ctx, tx, current, locked.ManagedConfigurationRevision)
			if validationErr != nil && !errors.Is(validationErr, errs.ErrInvalid) && !errors.Is(validationErr, revisionservice.ErrInvalid) {
				return commandOutcome{}, validationErr
			}
		}
		if kind == revisionservice.KindRoleImage {
			validationErr = repository.validateSourceRoleImage(configuration, locked.ContentFormat, locked.Content)
			if errors.Is(validationErr, errs.ErrUnavailable) {
				return commandOutcome{}, validationErr
			}
			diagnostics = nil
			if validationErr != nil {
				diagnostics = []revisionservice.Diagnostic{{Code: "ROLE_IMAGE_CONFIGURATION_INVALID", Message: "Role image configuration is incompatible with the active catalog"}}
			}
		}
		if kind == revisionservice.KindEmailMailbox {
			diagnostics, validationErr = repository.validateEmailMailboxRevision(ctx, tx, current, configuration, locked.ManagedConfigurationRevision)
			if validationErr != nil && !errors.Is(validationErr, errs.ErrInvalid) {
				return commandOutcome{}, validationErr
			}
		}
		state := "VALID"
		if validationErr != nil {
			state = "INVALID"
		}
		messages := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			messages = append(messages, diagnostic.Code+":"+diagnostic.Message)
		}
		item, updateErr := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationValidateRevision, pgx.StrictNamedArgs{
			"revision_id": locked.RefID, "state": state, "diagnostics": string(asJSON(messages)),
		}))
		if updateErr != nil {
			return commandOutcome{}, mapWriteError(updateErr)
		}
		revision = &item.ManagedConfigurationRevision
		if err := tx.QueryRow(ctx, queryManagedConfigurationTouchSet, pgx.StrictNamedArgs{
			"configuration_set_id": configuration.id, "expected_version": configuration.Version,
		}).Scan(&configuration.Version, &configuration.UpdatedAt); err != nil {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
	case "PUBLISH":
		locked, lockErr := repository.lockManagedRevision(ctx, tx, current, configuration, payload.RevisionRef)
		if lockErr != nil || locked.State != "VALID" {
			return commandOutcome{}, errs.ErrConflict
		}
		var impact revisionImpactRow
		var impactItems []entity.RevisionImpactItem
		if kind == revisionservice.KindPromptTemplate {
			impact, impactItems, err = repository.promptPlanForPublish(ctx, tx, current, configuration, locked, payload)
			if err != nil {
				return commandOutcome{}, err
			}
		}
		if kind == revisionservice.KindSystemSTT && locked.ContentFormat != "JSON" {
			return commandOutcome{}, errs.ErrInvalid
		}
		if kind == revisionservice.KindPromptTemplate {
			if _, err := repository.validatePromptScopeTx(ctx, tx, current, locked.ManagedConfigurationRevision); err != nil {
				return commandOutcome{}, err
			}
		}
		if kind == revisionservice.KindEmailMailbox {
			if _, err := repository.validateEmailMailboxRevision(ctx, tx, current, configuration, locked.ManagedConfigurationRevision); err != nil {
				return commandOutcome{}, errs.ErrInvalid
			}
		}
		if kind == revisionservice.KindIntegrationDefinition {
			if _, err := revisionservice.IntegrationPackage(locked.ContentFormat, locked.Content); err != nil {
				return commandOutcome{}, errs.ErrInvalid
			}
		}
		if kind == revisionservice.KindRoleImage {
			if err := repository.publishSourceRoleImage(ctx, tx, current, configuration, locked.ManagedConfigurationRevision); err != nil {
				return commandOutcome{}, err
			}
		}
		item, setVersion, updatedAt, publishErr := scanPublishedManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationPublishRevision, pgx.StrictNamedArgs{
			"configuration_set_id": configuration.id, "revision_id": locked.RefID, "expected_version": configuration.Version,
		}))
		if publishErr != nil {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		revision, configuration.Version, configuration.UpdatedAt = &item.ManagedConfigurationRevision, setVersion, updatedAt
		configuration.CurrentRevision, configuration.currentRevisionID = revision, locked.RefID
		if kind == revisionservice.KindPromptTemplate {
			var plan entity.RevisionImpactPlan
			configuration, plan, err = repository.applyPromptImpact(ctx, tx, current, configuration, input, impact, impactItems)
			if err != nil {
				return commandOutcome{}, err
			}
			publicationPlan = &plan
		}
	case "REBIND":
		if kind == revisionservice.KindRoleImage {
			return repository.applyRoleImageImpact(ctx, tx, current, configuration, input)
		}
		locked, lockErr := repository.lockManagedRevision(ctx, tx, current, configuration, payload.RevisionRef)
		if lockErr != nil || locked.State != "PUBLISHED" {
			return commandOutcome{}, errs.ErrConflict
		}
		impact, impactErr := repository.managedImpactTx(ctx, tx, current, configuration.Ref, locked.Ref, query.Filter{Page: query.Page{Size: 1}})
		if impactErr != nil || payload.ImpactDigest != impact.Digest {
			return commandOutcome{}, errs.ErrConflict
		}
		if len(payload.Consumers) == 0 || len(payload.Consumers) > 128 {
			return commandOutcome{}, errs.ErrInvalid
		}
		for _, consumer := range payload.Consumers {
			if !managedConsumerAllowed(kind, consumer) {
				return commandOutcome{}, errs.ErrInvalid
			}
			if kind == revisionservice.KindPromptTemplate && (consumer.Kind == "AGENT" || consumer.Kind == "AGENT_CONTINUATION") {
				if err := repository.validateAgentPromptContextTx(ctx, tx, current, consumer.Ref, locked.Content, consumer.Kind == "AGENT_CONTINUATION"); err != nil {
					return commandOutcome{}, err
				}
			}
			if kind == revisionservice.KindPromptTemplate && consumer.Kind == "WORKFLOW" {
				if err := repository.validateWorkflowPromptContextTx(ctx, tx, current, consumer.Ref, locked.Content); err != nil {
					return commandOutcome{}, err
				}
			}
			switch consumer.Kind {
			case "AGENT", "AGENT_CONTINUATION":
				permission, target, resolveErr := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.manage", consumer.Ref)
				if resolveErr != nil || repository.requireAccess(ctx, tx, current, permission, target) != nil {
					return commandOutcome{}, errs.ErrNotFound
				}
			case "WORKFLOW", "SCHEDULE":
				permission := strings.ToLower(consumer.Kind) + ".manage"
				if err := repository.requireAccess(ctx, tx, current, permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: consumer.Kind, ResourceRef: consumer.Ref}); err != nil {
					return commandOutcome{}, errs.ErrNotFound
				}
			case "RUNTIME_ENVIRONMENT":
				if _, _, err := repository.environmentImpactTarget(ctx, tx, current, consumer.Ref, ""); err != nil {
					return commandOutcome{}, err
				}
			}
			expectedDefinitionKey := ""
			if kind == revisionservice.KindIntegrationDefinition {
				if configuration.projectID != "" {
					return commandOutcome{}, errs.ErrConflict
				}
				expectedDefinitionKey, err = revisionservice.IntegrationDefinitionKey(locked.ContentFormat, locked.Content)
				if err != nil {
					return commandOutcome{}, errs.ErrInvalid
				}
				connection, resolveErr := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
					Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: consumer.Ref,
				})
				if resolveErr != nil || repository.requireAccess(ctx, tx, current, "integration.manage", connection) != nil {
					return commandOutcome{}, errs.ErrNotFound
				}
				if err := repository.cancelConfigurationWriteBacks(ctx, tx, current, "", consumer.Ref); err != nil {
					return commandOutcome{}, err
				}
			}
			var allowed bool
			if err := tx.QueryRow(ctx, queryManagedConfigurationValidateConsumer, pgx.StrictNamedArgs{
				"consumer_kind": consumer.Kind, "consumer_ref": consumer.Ref, "organization_id": current.organizationID,
				"project_id": nullUUID(configuration.projectID), "expected_definition_key": expectedDefinitionKey,
			}).Scan(&allowed); err != nil || !allowed {
				return commandOutcome{}, errs.ErrNotFound
			}
			if kind == revisionservice.KindIntegrationDefinition {
				if err := repository.bindIntegrationPackage(ctx, tx, current, consumer.Ref, locked.ContentFormat, locked.Content); err != nil {
					return commandOutcome{}, err
				}
			}
			bindingRef, _ := newRef("mcbind")
			var bindingRefReadback string
			if err := tx.QueryRow(ctx, queryManagedConfigurationRebindConsumer, pgx.StrictNamedArgs{
				"binding_ref": bindingRef, "organization_id": current.organizationID, "project_id": nullUUID(configuration.projectID),
				"configuration_set_id": configuration.id, "revision_id": locked.RefID, "configuration_kind": kind, "consumer_kind": consumer.Kind,
				"consumer_ref": consumer.Ref, "actor_id": current.actorID,
			}).Scan(&bindingRefReadback, &consumer.Kind, &consumer.Ref, &consumer.RevisionRef, &consumer.Version); err != nil {
				return commandOutcome{}, mapWriteError(err)
			}
		}
		if err := tx.QueryRow(ctx, queryManagedConfigurationTouchSet, pgx.StrictNamedArgs{
			"configuration_set_id": configuration.id, "expected_version": configuration.Version,
		}).Scan(&configuration.Version, &configuration.UpdatedAt); err != nil {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		revision = &locked.ManagedConfigurationRevision
	case "DETACH":
		if configuration.ManagedBy != "GIT" || configuration.CurrentRevision == nil {
			return commandOutcome{}, errs.ErrConflict
		}
		revisionRef, err := newRef("mrev")
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		source := configuration.CurrentRevision
		format, content := source.ContentFormat, source.Content
		if kind == revisionservice.KindIntegrationDefinition || configuration.Kind == revisionservice.KindIntegrationDefinition {
			format, content = repository.normalizeIntegrationDraft(format, content, "UI")
		}
		digest := sha256.Sum256([]byte(content))
		draft, err := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationInsertRevision, pgx.StrictNamedArgs{
			"revision_ref": revisionRef, "organization_id": current.organizationID, "configuration_set_id": configuration.id,
			"content_format": format, "content": content, "digest": hex.EncodeToString(digest[:]),
			"parent_revision_id": configuration.currentRevisionID, "actor_id": current.actorID,
		}))
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		revision = &draft.ManagedConfigurationRevision
		if err := tx.QueryRow(ctx, queryManagedConfigurationDetach, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "configuration_ref": configuration.Ref,
			"expected_version": configuration.Version,
		}).Scan(&configuration.Version, &configuration.UpdatedAt); err != nil {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		configuration.ManagedBy, configuration.Source, configuration.SourceRevision = "UI", "control-center", ""
		if configuration.GitSource != nil {
			source, err := readConfigurationSource(ctx, tx, current.organizationID, configuration.Ref)
			if err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			if _, err := tx.Exec(ctx, queryConfigurationSourceCancelWork, configuration.id); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			result, err := repository.sourceState(ctx, tx, current, source, entity.ConfigurationSourceDetached, "")
			if err != nil {
				return commandOutcome{}, err
			}
			configuration.GitSource = &result
		}
	}
	if kind == revisionservice.KindPromptTemplate {
		if (action == "CREATE" || action == "SAVE") && revision != nil {
			if err := repository.savePromptScopeTx(ctx, tx, current, configuration.ProjectRef, revision, payload.PromptScope); err != nil {
				return commandOutcome{}, err
			}
		}
		if err := repository.hydratePromptScopeTx(ctx, tx, current, revision); err != nil {
			return commandOutcome{}, err
		}
		if err := repository.hydratePromptScopeTx(ctx, tx, current, configuration.CurrentRevision); err != nil {
			return commandOutcome{}, err
		}
	}
	outcome := managedOutcome(configuration, revision)
	outcome.result.RevisionImpactPlan = publicationPlan
	return outcome, nil
}

type lockedManagedRevision struct {
	entity.ManagedConfigurationRevision
	RefID string
}

func (repository *Repository) resolveManagedSet(ctx context.Context, tx pgx.Tx, current scope, payload command.ManagedConfigurationInput, kind string, create bool) (managedSet, error) {
	if payload.ConfigurationRef != "" {
		item, err := scanManagedSet(tx.QueryRow(ctx, queryManagedConfigurationLockSet, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "configuration_ref": payload.ConfigurationRef,
		}))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return managedSet{}, errs.ErrNotFound
			}
			return managedSet{}, errs.ErrUnavailable
		}
		if kind != "" && item.Kind != kind {
			return managedSet{}, errs.ErrNotFound
		}
		if err := hydrateConfigurationSource(ctx, tx, current.organizationID, &item); err != nil {
			return managedSet{}, err
		}
		if item.currentRevisionID != "" {
			revision, err := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationCurrentRevision, current.organizationID, item.id, item.currentRevisionID))
			if err != nil || revision.State != "PUBLISHED" {
				return managedSet{}, errs.ErrUnavailable
			}
			item.CurrentRevision = &revision.ManagedConfigurationRevision
		}
		return item, nil
	}
	if !create || kind == "" {
		return managedSet{}, errs.ErrInvalid
	}
	ref, _ := newRef("mcfg")
	item, err := scanManagedSet(tx.QueryRow(ctx, queryManagedConfigurationInsertSet, pgx.StrictNamedArgs{
		"configuration_ref": ref, "organization_id": current.organizationID, "project_ref": strings.TrimSpace(payload.ProjectRef),
		"kind": kind, "name": strings.TrimSpace(payload.Name), "managed_by": "UI", "source": "control-center",
		"source_revision": "", "actor_id": current.actorID,
	}))
	if err != nil {
		return managedSet{}, mapWriteError(err)
	}
	return item, nil
}

func (repository *Repository) lockManagedRevision(ctx context.Context, tx pgx.Tx, current scope, set managedSet, ref string) (lockedManagedRevision, error) {
	item, err := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationLockRevision, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "configuration_set_id": set.id, "revision_ref": ref,
	}))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lockedManagedRevision{}, errs.ErrNotFound
		}
		return lockedManagedRevision{}, errs.ErrUnavailable
	}
	if set.Kind == revisionservice.KindPromptTemplate {
		if err := repository.hydratePromptScopeTx(ctx, tx, current, &item.ManagedConfigurationRevision); err != nil {
			return lockedManagedRevision{}, err
		}
	}
	return lockedManagedRevision{ManagedConfigurationRevision: item.ManagedConfigurationRevision, RefID: item.internalID}, nil
}

func (repository *Repository) copyManagedConfiguration(ctx context.Context, tx pgx.Tx, current scope, input command.Command, payload command.ManagedConfigurationInput) (commandOutcome, error) {
	if input.Mutation.ExpectedVersion == nil || payload.ConfigurationRef == "" || strings.TrimSpace(payload.Name) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	source, err := repository.resolveManagedSet(ctx, tx, current, payload, "", false)
	if err != nil {
		return commandOutcome{}, err
	}
	if source.ManagedBy != "GIT" || source.CurrentRevision == nil {
		return commandOutcome{}, errs.ErrConflict
	}
	format, content := source.CurrentRevision.ContentFormat, source.CurrentRevision.Content
	if source.Kind == revisionservice.KindIntegrationDefinition {
		format, content = repository.normalizeIntegrationDraft(format, content, "UI")
	}
	digest := sha256.Sum256([]byte(content))
	copyRef, _ := newRef("mcfg")
	revisionRef, _ := newRef("mrev")
	set, revision, err := scanManagedCopy(tx.QueryRow(ctx, queryManagedConfigurationCopy, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "configuration_ref": payload.ConfigurationRef,
		"expected_version": *input.Mutation.ExpectedVersion, "copy_ref": copyRef, "revision_ref": revisionRef,
		"name": strings.TrimSpace(payload.Name), "actor_id": current.actorID,
		"content_format": format, "content": content, "digest": hex.EncodeToString(digest[:]),
	}))
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if set.Kind == revisionservice.KindEmailMailbox {
		var connectionRef, sourceMailboxRef string
		if err := tx.QueryRow(ctx, queryEmailMailboxConfigurationOwner, current.organizationID, payload.ConfigurationRef).Scan(&connectionRef, &sourceMailboxRef); err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
		mailboxRef, err := newMailboxRef()
		if err != nil {
			return commandOutcome{}, err
		}
		tag, err := tx.Exec(ctx, queryEmailMailboxConfigurationInsertOwner, current.organizationID, set.Ref, connectionRef, mailboxRef)
		if err != nil || tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	return managedOutcome(set, &revision), nil
}

func managedCommand(kind command.Kind) (string, string) {
	mapping := map[command.Kind][2]string{
		command.PreparePromptTemplateImpact:        {revisionservice.KindPromptTemplate, "PREPARE_PROMPT_IMPACT"},
		command.PrepareRoleImageImpactPlan:         {revisionservice.KindRoleImage, "PREPARE_IMPACT"},
		command.CreateEmailMailboxDraft:            {revisionservice.KindEmailMailbox, "CREATE"},
		command.SaveEmailMailboxDraft:              {revisionservice.KindEmailMailbox, "SAVE"},
		command.ValidateEmailMailboxDraft:          {revisionservice.KindEmailMailbox, "VALIDATE"},
		command.PublishEmailMailboxDraft:           {revisionservice.KindEmailMailbox, "PUBLISH"},
		command.DiscardEmailMailboxDraft:           {revisionservice.KindEmailMailbox, "DISCARD"},
		command.SavePromptTemplateDraft:            {revisionservice.KindPromptTemplate, "SAVE"},
		command.DiscardPromptTemplateDraft:         {revisionservice.KindPromptTemplate, "DISCARD"},
		command.SaveRoleImageRevisionDraft:         {revisionservice.KindRoleImage, "SAVE"},
		command.DiscardRoleImageRevisionDraft:      {revisionservice.KindRoleImage, "DISCARD"},
		command.SaveIntegrationDefinitionDraft:     {revisionservice.KindIntegrationDefinition, "SAVE"},
		command.DiscardIntegrationDefinitionDraft:  {revisionservice.KindIntegrationDefinition, "DISCARD"},
		command.SaveSystemSTTConfigurationDraft:    {revisionservice.KindSystemSTT, "SAVE"},
		command.DiscardSystemSTTConfigurationDraft: {revisionservice.KindSystemSTT, "DISCARD"},
		command.CreatePromptTemplateDraft:          {revisionservice.KindPromptTemplate, "CREATE"}, command.ValidatePromptTemplateDraft: {revisionservice.KindPromptTemplate, "VALIDATE"}, command.PublishPromptTemplateDraft: {revisionservice.KindPromptTemplate, "PUBLISH"}, command.RebindPromptTemplate: {revisionservice.KindPromptTemplate, "REBIND"},
		command.CreateRoleImageRevisionDraft: {revisionservice.KindRoleImage, "CREATE"}, command.ValidateRoleImageRevision: {revisionservice.KindRoleImage, "VALIDATE"}, command.PublishRoleImageRevision: {revisionservice.KindRoleImage, "PUBLISH"}, command.RebindRoleImage: {revisionservice.KindRoleImage, "REBIND"},
		command.CreateIntegrationDefinition: {revisionservice.KindIntegrationDefinition, "CREATE"}, command.ValidateIntegrationDefinition: {revisionservice.KindIntegrationDefinition, "VALIDATE"}, command.PublishIntegrationDefinition: {revisionservice.KindIntegrationDefinition, "PUBLISH"}, command.RebindIntegrationDefinition: {revisionservice.KindIntegrationDefinition, "REBIND"},
		command.CreateSystemSTTDraft: {revisionservice.KindSystemSTT, "CREATE"}, command.ValidateSystemSTTDraft: {revisionservice.KindSystemSTT, "VALIDATE"}, command.PublishSystemSTTDraft: {revisionservice.KindSystemSTT, "PUBLISH"}, command.RebindSystemSTT: {revisionservice.KindSystemSTT, "REBIND"},
		command.DetachGitManagedConfiguration: {"", "DETACH"}, command.CopyGitManagedConfiguration: {"", "COPY"},
	}
	value := mapping[kind]
	return value[0], value[1]
}

func managedConsumerAllowed(kind string, consumer entity.ManagedConfigurationConsumer) bool {
	allowed := map[string][]string{
		revisionservice.KindPromptTemplate:        {"AGENT", "AGENT_CONTINUATION", "WORKFLOW", "SCHEDULE"},
		revisionservice.KindRoleImage:             {"RUNTIME_ENVIRONMENT"},
		revisionservice.KindIntegrationDefinition: {"INTEGRATION_CONNECTION"},
		revisionservice.KindSystemSTT:             {"STT_SERVICE"},
	}
	if consumer.Ref == "" {
		return false
	}
	for _, value := range allowed[kind] {
		if consumer.Kind == value {
			return true
		}
	}
	return false
}

func managedOutcome(set managedSet, revision *entity.ManagedConfigurationRevision) commandOutcome {
	if revision != nil && revision.State == "PUBLISHED" {
		set.CurrentRevision = revision
	}
	return commandOutcome{result: command.Result{ManagedConfiguration: &set.ManagedConfigurationSet, ManagedRevision: revision},
		projectID: set.projectID, projectRef: set.ProjectRef, resourceKind: set.Kind, resourceRef: set.Ref,
		summary: "i18n:MANAGED_CONFIGURATION_CHANGED"}
}

type managedRevisionScan struct {
	entity.ManagedConfigurationRevision
	internalID string
}

func scanManagedRevision(row rowScanner) (managedRevisionScan, error) {
	var item managedRevisionScan
	var raw []byte
	err := row.Scan(&item.internalID, &item.Ref, &item.Revision, &item.State, &item.ContentFormat, &item.Content,
		&item.Digest, &item.ParentRevisionRef, &raw, &item.CreatedAt, &item.ValidatedAt, &item.PublishedAt)
	if err == nil && json.Unmarshal(raw, &item.ValidationDiagnostics) != nil {
		return managedRevisionScan{}, errs.ErrUnavailable
	}
	return item, err
}
func scanPublishedManagedRevision(row rowScanner) (managedRevisionScan, int64, time.Time, error) {
	var item managedRevisionScan
	var raw []byte
	var version int64
	var updated time.Time
	err := row.Scan(&item.internalID, &item.Ref, &item.Revision, &item.State, &item.ContentFormat, &item.Content,
		&item.Digest, &item.ParentRevisionRef, &raw, &item.CreatedAt, &item.ValidatedAt, &item.PublishedAt, &version, &updated)
	if err == nil && json.Unmarshal(raw, &item.ValidationDiagnostics) != nil {
		err = errs.ErrUnavailable
	}
	return item, version, updated, err
}

func scanManagedSet(row rowScanner) (managedSet, error) {
	var item managedSet
	err := row.Scan(&item.id, &item.Ref, &item.projectID, &item.ProjectRef, &item.Kind, &item.Name,
		&item.ManagedBy, &item.Source, &item.SourceRevision, &item.Version, &item.UpdatedAt, &item.currentRevisionID)
	return item, err
}
func scanManagedCopy(row rowScanner) (managedSet, entity.ManagedConfigurationRevision, error) {
	var set managedSet
	var revision managedRevisionScan
	var raw []byte
	err := row.Scan(&set.id, &set.Ref, &set.projectID, &set.ProjectRef, &set.Kind, &set.Name, &set.ManagedBy,
		&set.Source, &set.SourceRevision, &set.Version, &set.UpdatedAt, &revision.internalID, &revision.Ref,
		&revision.Revision, &revision.State, &revision.ContentFormat, &revision.Content, &revision.Digest,
		&revision.ParentRevisionRef, &raw, &revision.CreatedAt, &revision.ValidatedAt, &revision.PublishedAt)
	if err == nil {
		err = json.Unmarshal(raw, &revision.ValidationDiagnostics)
	}
	return set, revision.ManagedConfigurationRevision, err
}

func scanManagedBinding(row rowScanner) (entity.ManagedConfigurationBindingSnapshot, error) {
	var result entity.ManagedConfigurationBindingSnapshot
	var set managedSet
	var revision managedRevisionScan
	var diagnostics []byte
	err := row.Scan(&result.Ref, &result.Version, &result.ConsumerKind, &result.ConsumerRef,
		&set.id, &set.Ref, &set.projectID, &set.ProjectRef, &set.Kind, &set.Name, &set.ManagedBy,
		&set.Source, &set.SourceRevision, &set.Version, &set.UpdatedAt, &set.currentRevisionID,
		&revision.internalID, &revision.Ref, &revision.Revision, &revision.State, &revision.ContentFormat,
		&revision.Content, &revision.Digest, &revision.ParentRevisionRef, &diagnostics,
		&revision.CreatedAt, &revision.ValidatedAt, &revision.PublishedAt)
	if err == nil && json.Unmarshal(diagnostics, &revision.ValidationDiagnostics) != nil {
		err = errs.ErrUnavailable
	}
	result.Configuration = set.ManagedConfigurationSet
	result.Revision = revision.ManagedConfigurationRevision
	return result, err
}

func (repository *Repository) ListManagedConfigurationHistory(ctx context.Context, principal value.Principal, ref string, page query.Page) (entity.ManagedConfigurationSet, []entity.ManagedConfigurationRevision, int64, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	set, err := scanManagedSet(tx.QueryRow(ctx, queryManagedConfigurationLockSet, pgx.StrictNamedArgs{"organization_id": current.organizationID, "configuration_ref": ref}))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrNotFound
	}
	if err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrUnavailable
	}
	if err := repository.requireManagedSetAccess(ctx, tx, current, set, "project.view", "organization.view"); err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrNotFound
	}
	if err := hydrateConfigurationSource(ctx, tx, current.organizationID, &set); err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", err
	}
	includeContent := true
	if set.Kind == revisionservice.KindRoleImage {
		canRead, canEdit, accessErr := repository.managedRoleImageSourceProjection(ctx, tx, current, set.Ref, set.ProjectRef)
		if accessErr != nil {
			return entity.ManagedConfigurationSet{}, nil, 0, "", accessErr
		}
		includeContent = canRead
		set.SourceEditable = &canEdit
		projectManagedRevisionSource(set.CurrentRevision, canRead)
	}
	if set.Kind == revisionservice.KindPromptTemplate {
		var fullTarget any = organizationTarget(current.organizationRef)
		if set.ProjectRef != "" {
			resolved, resolveErr := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
				Kind: "RESOURCE_INSTANCE", ProjectRef: set.ProjectRef, ResourceKind: "PROJECT", ResourceRef: set.ProjectRef,
			})
			if resolveErr != nil {
				includeContent = false
			} else {
				fullTarget = resolved
			}
		}
		if includeContent && repository.requireAccess(ctx, tx, current, "prompt.full.view", fullTarget) != nil {
			includeContent = false
		}
	}
	cursor, err := decodeManagedHistoryCursor(page.Token, ref)
	if err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", err
	}
	limit := boundedPage(page)
	rows, err := tx.Query(ctx, queryManagedConfigurationListHistory, pgx.StrictNamedArgs{"organization_id": current.organizationID, "configuration_ref": ref, "before_revision": cursor.Before, "page_size": limit + 1})
	if err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.ManagedConfigurationRevision, 0, limit+1)
	var total int64
	for rows.Next() {
		var item managedRevisionScan
		var raw []byte
		if scanErr := rows.Scan(&item.internalID, &item.Ref, &item.Revision, &item.State, &item.ContentFormat, &item.Content,
			&item.Digest, &item.ParentRevisionRef, &raw, &item.CreatedAt, &item.ValidatedAt, &item.PublishedAt, &total); scanErr != nil || json.Unmarshal(raw, &item.ValidationDiagnostics) != nil {
			return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrUnavailable
		}
		if !includeContent {
			item.Content = ""
		}
		if set.Kind == revisionservice.KindRoleImage {
			projectManagedRevisionSource(&item.ManagedConfigurationRevision, includeContent)
		}
		items = append(items, item.ManagedConfigurationRevision)
	}
	if rows.Err() != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrUnavailable
	}
	rows.Close()
	if set.Kind == revisionservice.KindPromptTemplate {
		for i := range items {
			if err := repository.hydratePromptScopeTx(ctx, tx, current, &items[i]); err != nil {
				return entity.ManagedConfigurationSet{}, nil, 0, "", err
			}
		}
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = encodeManagedHistoryCursor(ref, items[len(items)-1].Revision)
		if next == page.Token {
			return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrConflict
	}
	return set.ManagedConfigurationSet, items, total, next, nil
}

func managedHistoryFilterDigest(configurationRef string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(configurationRef)))
	return base64.RawURLEncoding.EncodeToString(digest[:12])
}

func encodeManagedHistoryCursor(configurationRef string, before int64) string {
	raw, _ := json.Marshal(managedHistoryCursor{Version: 1, Filter: managedHistoryFilterDigest(configurationRef), Before: before})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeManagedHistoryCursor(token, configurationRef string) (managedHistoryCursor, error) {
	if strings.TrimSpace(token) == "" {
		return managedHistoryCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) > 512 {
		return managedHistoryCursor{}, errs.ErrInvalid
	}
	var cursor managedHistoryCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 ||
		cursor.Filter != managedHistoryFilterDigest(configurationRef) || cursor.Before < 1 {
		return managedHistoryCursor{}, errs.ErrInvalid
	}
	return cursor, nil
}

func (repository *Repository) GetManagedConfigurationImpact(ctx context.Context, principal value.Principal, ref, revisionRef string, filter query.Filter) (entity.ManagedConfigurationImpact, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	filter.Query = strings.TrimSpace(filter.Query)
	if !utf8.ValidString(filter.Query) || utf8.RuneCountInString(filter.Query) > 200 || strings.ContainsRune(filter.Query, 0) {
		return entity.ManagedConfigurationImpact{}, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ManagedConfigurationImpact{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.ManagedConfigurationImpact{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	set, err := scanManagedSet(tx.QueryRow(ctx, queryManagedConfigurationLockSet, pgx.StrictNamedArgs{"organization_id": current.organizationID, "configuration_ref": ref}))
	if err != nil {
		return entity.ManagedConfigurationImpact{}, errs.ErrNotFound
	}
	if err := repository.requireManagedSetAccess(ctx, tx, current, set, "project.manage", "organization.manage"); err != nil {
		return entity.ManagedConfigurationImpact{}, errs.ErrNotFound
	}
	impact, err := repository.managedImpactTx(ctx, tx, current, ref, revisionRef, filter)
	if err != nil {
		return entity.ManagedConfigurationImpact{}, err
	}
	return impact, tx.Commit(ctx)
}

func (repository *Repository) GetEffectiveManagedConfiguration(ctx context.Context, principal value.Principal, kind, consumerKind, consumerRef string) (entity.ManagedConfigurationBindingSnapshot, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ManagedConfigurationBindingSnapshot{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.ManagedConfigurationBindingSnapshot{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if kind == revisionservice.KindRoleImage {
		if consumerKind != "RUNTIME_ENVIRONMENT" {
			return entity.ManagedConfigurationBindingSnapshot{}, errs.ErrInvalid
		}
		if _, err := repository.resolveAssistantContext(ctx, tx, current, entity.AssistantContextDescriptor{
			EntityKind: "ENVIRONMENT", EntityRef: consumerRef,
		}, ""); err != nil {
			return entity.ManagedConfigurationBindingSnapshot{}, errs.ErrNotFound
		}
	}
	result, err := scanManagedBinding(tx.QueryRow(ctx, queryManagedConfigurationGetConsumerBinding, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "configuration_kind": kind,
		"consumer_kind": consumerKind, "consumer_ref": consumerRef,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ManagedConfigurationBindingSnapshot{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ManagedConfigurationBindingSnapshot{}, errs.ErrUnavailable
	}
	if kind == revisionservice.KindRoleImage {
		projection := command.Result{ManagedConfiguration: &result.Configuration, ManagedRevision: &result.Revision}
		if err := repository.projectManagedRoleImageSource(ctx, tx, current, &projection); err != nil {
			return entity.ManagedConfigurationBindingSnapshot{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ManagedConfigurationBindingSnapshot{}, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) requireManagedSetAccess(ctx context.Context, tx pgx.Tx, current scope, set managedSet, projectPermission, organizationPermission string) error {
	if set.Kind == revisionservice.KindEmailMailbox {
		var connectionRef, mailboxRef string
		if err := tx.QueryRow(ctx, queryEmailMailboxConfigurationOwner, current.organizationID, set.Ref).Scan(&connectionRef, &mailboxRef); err != nil {
			return errs.ErrNotFound
		}
		return repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef})
	}
	if set.ProjectRef == "" {
		return repository.requireAccess(ctx, tx, current, organizationPermission, organizationTarget(current.organizationRef))
	}
	return repository.requireAccess(ctx, tx, current, projectPermission, entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ProjectRef: set.ProjectRef, ResourceKind: "PROJECT", ResourceRef: set.ProjectRef,
	})
}
func (repository *Repository) managedImpactTx(ctx context.Context, tx pgx.Tx, current scope, ref, revisionRef string, filter query.Filter) (entity.ManagedConfigurationImpact, error) {
	set, err := scanManagedSet(tx.QueryRow(ctx, queryManagedConfigurationLockSet, pgx.StrictNamedArgs{"organization_id": current.organizationID, "configuration_ref": ref}))
	if err != nil {
		return entity.ManagedConfigurationImpact{}, errs.ErrNotFound
	}
	revision, err := repository.lockManagedRevision(ctx, tx, current, set, revisionRef)
	if err != nil {
		return entity.ManagedConfigurationImpact{}, err
	}
	filter = query.Filter{ResourceRef: ref, Category: revision.Ref, Query: filter.Query, Page: filter.Page}
	cursor, err := decodeCatalogCursor(current, "MANAGED_IMPACT", filter)
	if err != nil {
		return entity.ManagedConfigurationImpact{}, err
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryManagedConfigurationListBindings, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "configuration_ref": ref, "revision_ref": revision.Ref,
		"actor_id": current.actorID, "authority_project": current.authorityProjectID,
		"organization_managed": set.ProjectRef == "", "evaluated_at": time.Now().UTC(),
		"query": filter.Query, "cursor_ref": cursor, "page_size": limit + 1,
	})
	if err != nil {
		return entity.ManagedConfigurationImpact{}, errs.ErrUnavailable
	}
	defer rows.Close()
	result := entity.ManagedConfigurationImpact{ConfigurationRef: ref, TargetRevisionRef: revision.Ref}
	for rows.Next() {
		var item entity.ManagedConfigurationConsumer
		if rows.Scan(&item.Kind, &item.Ref, &item.RevisionRef, &item.Version, &result.Total, &result.Digest) != nil {
			return entity.ManagedConfigurationImpact{}, errs.ErrUnavailable
		}
		if item.Ref != "" {
			result.Consumers = append(result.Consumers, item)
		}
	}
	if rows.Err() != nil {
		return entity.ManagedConfigurationImpact{}, errs.ErrUnavailable
	}
	if len(result.Consumers) > int(limit) {
		result.Consumers = result.Consumers[:limit]
		last := result.Consumers[len(result.Consumers)-1]
		result.NextPageToken = encodeCatalogCursor(current, "MANAGED_IMPACT", filter, last.Kind+":"+last.Ref)
	}
	return result, nil
}

func (repository *Repository) GetSystemSTTConfiguration(ctx context.Context, principal value.Principal) (entity.SystemSTTConfiguration, error) {
	permission := "organization.view"
	if principal.CallerWorkload == "stt-tts-service" && principal.Permission == "platform.stt.policy.resolve" {
		permission = "platform.stt.use"
	}
	current, tx, err := repository.authorizedRead(ctx, principal, permission, func(current scope) entity.AccessScope {
		return organizationTarget(current.organizationRef)
	})
	if err != nil {
		return entity.SystemSTTConfiguration{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := repository.getSystemSTTConfigurationTx(ctx, tx, current)
	if err != nil {
		return entity.SystemSTTConfiguration{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.SystemSTTConfiguration{}, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) getSystemSTTConfigurationTx(ctx context.Context, tx pgx.Tx, current scope) (entity.SystemSTTConfiguration, error) {
	var result entity.SystemSTTConfiguration
	var eligible, providerEnabled, apiKey, enabled bool
	var rawProviderCapabilities []byte
	var content string
	err := tx.QueryRow(ctx, queryManagedConfigurationGetSTT, pgx.StrictNamedArgs{"organization_id": current.organizationID}).Scan(
		&result.ConfigurationRef, &result.RevisionRef, &result.Revision, &result.Digest, &result.ProviderAccountRef, &result.Model, &result.Language, &result.PermissionKey,
		&eligible, &providerEnabled, &rawProviderCapabilities, &result.ProviderCredentialGeneration, &apiKey, &enabled, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.SystemSTTConfiguration{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.SystemSTTConfiguration{}, errs.ErrUnavailable
	}
	if result.PermissionKey != "platform.stt.use" {
		result.ReadinessBlockers = append(result.ReadinessBlockers, "STT_PERMISSION_INVALID")
	}
	if err := repository.requireAccess(ctx, tx, current, "platform.stt.use", organizationTarget(current.organizationRef)); err != nil {
		if !errors.Is(err, errs.ErrNotFound) {
			return entity.SystemSTTConfiguration{}, err
		}
		result.ReadinessBlockers = append(result.ReadinessBlockers, "STT_PERMISSION_DENIED")
	}
	if !enabled {
		result.ReadinessBlockers = append(result.ReadinessBlockers, "STT_DISABLED")
	}
	if !eligible {
		result.ReadinessBlockers = append(result.ReadinessBlockers, "STT_PROVIDER_ACCOUNT_INELIGIBLE")
	}
	if !apiKey {
		result.ReadinessBlockers = append(result.ReadinessBlockers, "STT_PROVIDER_CREDENTIAL_UNSUPPORTED")
	}
	var providerCapabilities map[string]any
	if json.Unmarshal(rawProviderCapabilities, &providerCapabilities) != nil {
		return entity.SystemSTTConfiguration{}, errs.ErrUnavailable
	}
	if !providerEnabled {
		result.ReadinessBlockers = append(result.ReadinessBlockers, "STT_PROVIDER_DISABLED")
	}
	specification, specificationErr := revisionservice.ParseSystemSTT(content)
	digest := sha256.Sum256([]byte(content))
	if specificationErr != nil || hex.EncodeToString(digest[:]) != result.Digest {
		result.ReadinessBlockers = append(result.ReadinessBlockers, "STT_MODEL_UNSUPPORTED")
	} else {
		result.Parameters = specification.Parameters
		result.Enabled = specification.Enabled
		result.MaximumAudioBytes = specification.MaximumAudioBytes
		result.MaximumAudioDurationMilliseconds = specification.MaximumAudioDurationMilliseconds
		result.ProviderTimeoutMilliseconds = specification.ProviderTimeoutMilliseconds
	}
	result.Ready = len(result.ReadinessBlockers) == 0
	return result, nil
}

// Профиль совпадает с исполняемым adapter stt-tts-service, а не каталогом LLM агента.
func systemSTTModelSupported(model, language string) bool {
	return (value.STTParameters{}).Validate(model, language) == nil
}

func (repository *Repository) GetEffectivePromptTemplate(ctx context.Context, principal value.Principal, agentRef string) (entity.InstructionVersion, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "agent.view", func(current scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: agentRef}
	})
	if err != nil {
		return entity.InstructionVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item entity.InstructionVersion
	err = tx.QueryRow(ctx, queryManagedConfigurationEffectivePrompt, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "agent_ref": agentRef,
	}).Scan(&item.Ref, &item.Content, &item.Digest, &item.VersionNumber, &item.CreatedAt, &item.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.InstructionVersion{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.InstructionVersion{}, errs.ErrUnavailable
	}
	item.State = "PUBLISHED"
	if err := tx.Commit(ctx); err != nil {
		return entity.InstructionVersion{}, errs.ErrConflict
	}
	return item, nil
}
