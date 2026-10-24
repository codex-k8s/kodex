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

// managedSet pairs public configuration metadata with internal set/project/current-revision IDs
// needed by owner SQL; neither stored ownership fields nor this projection grant authority.
// managedSet связывает публичные configuration metadata с внутренними set/project/current-revision
// IDs для owner SQL; сохранённые owner fields и само представление не выдают полномочий.
type managedSet struct {
	entity.ManagedConfigurationSet
	id, projectID, currentRevisionID string
}

// managedHistoryCursor encodes version1, a configuration-reference fingerprint and the exclusive
// revision boundary for managed history; it is a locator checked after current access, not an
// authority token.
// managedHistoryCursor кодирует version1, fingerprint configuration reference и исключающую
// границу revision для managed history; это locator, проверяемый после текущего доступа, а не
// authority token.
type managedHistoryCursor struct {
	Version int    `json:"v"`
	Filter  string `json:"f"`
	Before  int64  `json:"b"`
}

// changeManagedConfiguration dispatches the closed managed command/action mapping within the
// caller transaction, resolving typed ownership, shipped/archive/UI guards and set OCC before
// specialized draft, validation, publication, impact/rebind, detach or copy effects. It preserves
// kind-specific source validation, immutable revisions, exact consumer expectations and prompt
// scope, returning the managed outcome for outer audit/event/receipt/commit; SAVE and CREATE
// retain their different content checks.
// changeManagedConfiguration исполняет закрытое соответствие managed command/action в транзакции
// вызывающего метода, разрешая typed ownership, shipped/archive/UI guards и set OCC до
// специализированных draft, validation, publication, impact/rebind, detach либо copy effects.
// Сохраняет проверки source по kind, неизменяемые revisions, точные consumer expectations и prompt
// scope, возвращая managed outcome для внешних audit/event/receipt/commit; SAVE и CREATE сохраняют
// разные content checks.
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
	if action == "COPY_CFG" {
		return repository.copyCFG(ctx, tx, current, input, payload, kind)
	}
	var configuration managedSet
	var err error
	if input.Kind == command.PrepareRoleImageImpactPlan || input.Kind == command.RebindRoleImage {
		configuration, err = repository.resolveRoleImageImpactSet(ctx, tx, current, payload)
	} else {
		configuration, err = repository.resolveManagedSet(ctx, tx, current, payload, kind, action == "CREATE")
	}
	if err != nil {
		return commandOutcome{}, err
	}
	if err := rejectShippedRoleImageMutation(ctx, tx, current.organizationID, configuration); err != nil {
		return commandOutcome{}, err
	}
	if configuration.Archived {
		return commandOutcome{}, errs.ErrConflict
	}
	if (action != "CREATE" || payload.ConfigurationRef != "") &&
		(input.Mutation.ExpectedVersion == nil || configuration.Version != *input.Mutation.ExpectedVersion) {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var revision *entity.ManagedConfigurationRevision
	if action == "ARCHIVE" {
		return repository.archiveCFG(ctx, tx, current, configuration)
	}
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
				!managedContentFormatAllowed(kind, format) {
				return commandOutcome{}, errs.ErrInvalid
			}
			content := strings.TrimSpace(payload.Content)
			if kind == revisionservice.KindIntegrationDefinition {
				var importErr error
				format, content, importErr = repository.prepareIntegrationDraft(ctx, format, content, configuration.ManagedBy)
				if importErr != nil {
					return commandOutcome{}, importErr
				}
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
		if !managedContentFormatAllowed(kind, format) {
			return commandOutcome{}, errs.ErrInvalid
		}
		if configuration.ManagedBy != "UI" {
			return commandOutcome{}, errs.ErrConflict
		}
		content := strings.TrimSpace(payload.Content)
		if kind == revisionservice.KindIntegrationDefinition {
			var importErr error
			format, content, importErr = repository.prepareIntegrationDraft(ctx, format, content, configuration.ManagedBy)
			if importErr != nil {
				return commandOutcome{}, importErr
			}
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
		if impactErr != nil {
			return commandOutcome{}, impactErr
		}
		if payload.ImpactDigest != impact.Digest {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if len(payload.Consumers) == 0 || len(payload.Consumers) > 128 {
			return commandOutcome{}, errs.ErrInvalid
		}
		seenConsumers := make(map[string]struct{}, len(payload.Consumers))
		for _, consumer := range payload.Consumers {
			key := consumer.Kind + "\x00" + consumer.Ref
			if _, duplicate := seenConsumers[key]; duplicate || !validManagedBindingExpectation(consumer) {
				return commandOutcome{}, errs.ErrInvalid
			}
			seenConsumers[key] = struct{}{}
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
			if err := tx.QueryRow(ctx, queryManagedConfigurationCASConsumer, pgx.StrictNamedArgs{
				"binding_ref": bindingRef, "organization_id": current.organizationID, "project_id": nullUUID(configuration.projectID),
				"configuration_set_id": configuration.id, "revision_id": locked.RefID, "configuration_kind": kind, "consumer_kind": consumer.Kind,
				"consumer_ref": consumer.Ref, "actor_id": current.actorID,
				"expected_absent": consumer.ExpectedAbsent, "expected_version": consumer.Version, "expected_revision_ref": consumer.RevisionRef,
			}).Scan(&bindingRefReadback, &consumer.Kind, &consumer.Ref, &consumer.RevisionRef, &consumer.Version); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return commandOutcome{}, errs.ErrVersionMismatch
				}
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

// lockedManagedRevision carries a managed revision and its internal locked-row ID for exact
// set/revision writes; the value itself does not acquire or keep a database lock.
// lockedManagedRevision содержит managed revision и внутренний locked-row ID для точных
// set/revision writes; само значение не получает и не удерживает блокировку БД.
type lockedManagedRevision struct {
	entity.ManagedConfigurationRevision
	RefID string
}

// resolveManagedSet selects PROJECT scope for the common managed-set resolver; it resolves owner
// metadata but leaves operation authorization to the caller.
// resolveManagedSet выбирает PROJECT scope общего managed-set resolver; разрешает owner metadata,
// оставляя авторизацию операции вызывающему методу.
func (repository *Repository) resolveManagedSet(ctx context.Context, tx pgx.Tx, current scope, payload command.ManagedConfigurationInput, kind string, create bool) (managedSet, error) {
	return repository.resolveManagedSetScope(ctx, tx, current, payload, kind, create, "PROJECT")
}

// resolveManagedSetScope locks an exact tenant/configuration, checks optional kind and explicit
// role-image project/organization scope, hydrates source and requires a valid current published
// revision when pinned. With no reference it requires create/kind and inserts the UI set using
// trimmed project/name; it does not perform a general access check or commit.
// resolveManagedSetScope блокирует точные tenant/configuration, проверяет необязательный kind и
// явный role-image project/organization scope, добавляет source и требует допустимую current
// published revision при наличии pin. Без reference требует create/kind и вставляет UI set с
// очищенными project/name; общей проверки доступа и commit не выполняет.
func (repository *Repository) resolveManagedSetScope(ctx context.Context, tx pgx.Tx, current scope, payload command.ManagedConfigurationInput, kind string, create bool, scopeKind string) (managedSet, error) {
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
		if item.Kind == revisionservice.KindRoleImage && ((scopeKind == "ORGANIZATION") != (item.projectID == "")) {
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

// lockManagedRevision reads the exact tenant/set/revision locked tuple with distinct
// NotFound/Unavailable mapping, hydrates prompt scope only for prompt templates and exposes its
// internal ID without owning commit.
// lockManagedRevision читает locked tuple точных tenant/set/revision с разными
// NotFound/Unavailable, добавляет prompt scope только для prompt templates и возвращает внутренний
// ID без владения commit.
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

// copyManagedConfiguration requires expected version, source reference and nonempty copy name,
// resolves a GIT set with current revision, normalizes integration content for UI and atomically
// copies set/draft through owner SQL. Mailbox copies additionally resolve connection ownership and
// create a fresh mailbox mapping; current operation authority and final transaction lifecycle
// remain outside.
// copyManagedConfiguration требует expected version, source reference и непустое имя копии,
// разрешает GIT set с current revision, нормализует integration content для UI и атомарно копирует
// set/draft через owner SQL. Mailbox copies дополнительно разрешают connection ownership и создают
// новую mailbox mapping; текущие полномочия операции и финальная транзакция остаются снаружи.
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

// managedCommand maps only registered command kinds to configuration kind and specialized action,
// including copy/archive/impact and Git detach/copy; unknown commands return two empty strings
// rather than a generic mutation.
// managedCommand сопоставляет только зарегистрированные command kinds с configuration kind и
// специализированным action, включая copy/archive/impact и Git detach/copy; неизвестные команды
// дают две пустые строки вместо generic mutation.
func managedCommand(kind command.Kind) (string, string) {
	mapping := map[command.Kind][2]string{
		command.CopyRoleImageConfiguration:                {revisionservice.KindRoleImage, "COPY_CFG"},
		command.CopyIntegrationDefinitionConfiguration:    {revisionservice.KindIntegrationDefinition, "COPY_CFG"},
		command.ArchiveRoleImageConfiguration:             {revisionservice.KindRoleImage, "ARCHIVE"},
		command.ArchiveIntegrationDefinitionConfiguration: {revisionservice.KindIntegrationDefinition, "ARCHIVE"},
		command.PreparePromptTemplateImpact:               {revisionservice.KindPromptTemplate, "PREPARE_PROMPT_IMPACT"},
		command.PrepareRoleImageImpactPlan:                {revisionservice.KindRoleImage, "PREPARE_IMPACT"},
		command.CreateEmailMailboxDraft:                   {revisionservice.KindEmailMailbox, "CREATE"},
		command.SaveEmailMailboxDraft:                     {revisionservice.KindEmailMailbox, "SAVE"},
		command.ValidateEmailMailboxDraft:                 {revisionservice.KindEmailMailbox, "VALIDATE"},
		command.PublishEmailMailboxDraft:                  {revisionservice.KindEmailMailbox, "PUBLISH"},
		command.DiscardEmailMailboxDraft:                  {revisionservice.KindEmailMailbox, "DISCARD"},
		command.SavePromptTemplateDraft:                   {revisionservice.KindPromptTemplate, "SAVE"},
		command.DiscardPromptTemplateDraft:                {revisionservice.KindPromptTemplate, "DISCARD"},
		command.SaveRoleImageRevisionDraft:                {revisionservice.KindRoleImage, "SAVE"},
		command.DiscardRoleImageRevisionDraft:             {revisionservice.KindRoleImage, "DISCARD"},
		command.SaveIntegrationDefinitionDraft:            {revisionservice.KindIntegrationDefinition, "SAVE"},
		command.DiscardIntegrationDefinitionDraft:         {revisionservice.KindIntegrationDefinition, "DISCARD"},
		command.SaveSystemSTTConfigurationDraft:           {revisionservice.KindSystemSTT, "SAVE"},
		command.DiscardSystemSTTConfigurationDraft:        {revisionservice.KindSystemSTT, "DISCARD"},
		command.CreatePromptTemplateDraft:                 {revisionservice.KindPromptTemplate, "CREATE"}, command.ValidatePromptTemplateDraft: {revisionservice.KindPromptTemplate, "VALIDATE"}, command.PublishPromptTemplateDraft: {revisionservice.KindPromptTemplate, "PUBLISH"}, command.RebindPromptTemplate: {revisionservice.KindPromptTemplate, "REBIND"},
		command.CreateRoleImageRevisionDraft: {revisionservice.KindRoleImage, "CREATE"}, command.ValidateRoleImageRevision: {revisionservice.KindRoleImage, "VALIDATE"}, command.PublishRoleImageRevision: {revisionservice.KindRoleImage, "PUBLISH"}, command.RebindRoleImage: {revisionservice.KindRoleImage, "REBIND"},
		command.CreateIntegrationDefinition: {revisionservice.KindIntegrationDefinition, "CREATE"}, command.ValidateIntegrationDefinition: {revisionservice.KindIntegrationDefinition, "VALIDATE"}, command.PublishIntegrationDefinition: {revisionservice.KindIntegrationDefinition, "PUBLISH"}, command.RebindIntegrationDefinition: {revisionservice.KindIntegrationDefinition, "REBIND"},
		command.CreateSystemSTTDraft: {revisionservice.KindSystemSTT, "CREATE"}, command.ValidateSystemSTTDraft: {revisionservice.KindSystemSTT, "VALIDATE"}, command.PublishSystemSTTDraft: {revisionservice.KindSystemSTT, "PUBLISH"}, command.RebindSystemSTT: {revisionservice.KindSystemSTT, "REBIND"},
		command.DetachGitManagedConfiguration: {"", "DETACH"}, command.CopyGitManagedConfiguration: {"", "COPY"},
	}
	value := mapping[kind]
	return value[0], value[1]
}

// managedConsumerAllowed checks a nonempty consumer reference against the closed kind-to-consumer
// registry for prompt, role image, integration and STT metadata; this structural check does not
// establish current consumer eligibility.
// managedConsumerAllowed проверяет непустую consumer reference по закрытому kind-to-consumer
// registry для prompt, role image, integration и STT metadata; структурная проверка не
// устанавливает текущую consumer eligibility.
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

// validManagedBindingExpectation requires zero version/empty revision for expected absence,
// otherwise a positive JSON-safe version and valid mrev history reference; an expected-absence
// flag is not proof of actual absence.
// validManagedBindingExpectation требует нулевые version/пустую revision для expected absence,
// иначе положительную JSON-safe version и допустимую mrev history reference; expected-absence flag
// не доказывает фактическое отсутствие.
func validManagedBindingExpectation(consumer entity.ManagedConfigurationConsumer) bool {
	if consumer.ExpectedAbsent {
		return consumer.Version == 0 && consumer.RevisionRef == ""
	}
	return consumer.Version > 0 && consumer.Version <= 9007199254740991 && strings.HasPrefix(consumer.RevisionRef, "mrev_") && validOverlayHistoryRef(consumer.RevisionRef)
}

// managedOutcome projects set/revision metadata and the existing managed-change event descriptor,
// setting CurrentRevision only for PUBLISHED input; it does not persist an event or infer
// permissions.
// managedOutcome формирует set/revision metadata и прежний managed-change event descriptor,
// назначая CurrentRevision только для PUBLISHED input; событие не сохраняет и полномочий не
// выводит.
func managedOutcome(set managedSet, revision *entity.ManagedConfigurationRevision) commandOutcome {
	if revision != nil && revision.State == "PUBLISHED" {
		set.CurrentRevision = revision
	}
	return commandOutcome{result: command.Result{ManagedConfiguration: &set.ManagedConfigurationSet, ManagedRevision: revision},
		projectID: set.projectID, projectRef: set.ProjectRef, resourceKind: set.Kind, resourceRef: set.Ref,
		summary: "i18n:MANAGED_CONFIGURATION_CHANGED", platformEvent: "MANAGED_CONFIGURATION_CHANGED"}
}

// managedRevisionScan adds an internal revision ID to public revision fields for ordered SQL
// scanning; decode and partial-result policy are selected by each scanner.
// managedRevisionScan добавляет внутренний revision ID к публичным revision fields для
// упорядоченного SQL scanning; decode и partial-result policy выбирает каждый scanner.
type managedRevisionScan struct {
	entity.ManagedConfigurationRevision
	internalID string
}

// scanManagedRevision scans twelve revision columns and decodes diagnostics; malformed diagnostics
// after successful scanning return a zero revision with Unavailable, while row-scan errors
// preserve the partially assigned revision and original error.
// scanManagedRevision сканирует двенадцать revision columns и декодирует diagnostics; неверные
// diagnostics после успешного scan дают нулевую revision с Unavailable, row-scan errors сохраняют
// частично присвоенную revision и исходную ошибку.
func scanManagedRevision(row rowScanner) (managedRevisionScan, error) {
	var item managedRevisionScan
	var raw []byte
	err := row.Scan(managedRevisionScanTargets(&item, &raw)...)
	if err == nil && json.Unmarshal(raw, &item.ValidationDiagnostics) != nil {
		return managedRevisionScan{}, errs.ErrUnavailable
	}
	return item, err
}

// scanPublishedManagedRevision scans revision plus set version/update time and maps malformed
// diagnostics to Unavailable while preserving the assigned revision and extra outputs; scan errors
// remain unchanged.
// scanPublishedManagedRevision сканирует revision с set version/update time и преобразует неверные
// diagnostics в Unavailable, сохраняя присвоенные revision и дополнительные результаты; scan
// errors не меняются.
func scanPublishedManagedRevision(row rowScanner) (managedRevisionScan, int64, time.Time, error) {
	var item managedRevisionScan
	var raw []byte
	var version int64
	var updated time.Time
	targets := append(managedRevisionScanTargets(&item, &raw), &version, &updated)
	err := row.Scan(targets...)
	if err == nil && json.Unmarshal(raw, &item.ValidationDiagnostics) != nil {
		err = errs.ErrUnavailable
	}
	return item, version, updated, err
}

// scanManagedSet scans fourteen ordered managed-set columns including internal IDs, archive flag
// and copy provenance, returning any partial assignments with the original scan error without
// authority checks.
// scanManagedSet сканирует четырнадцать упорядоченных managed-set columns с внутренними IDs,
// archive flag и copy provenance, возвращая частичные присваивания с исходной scan error без
// проверок полномочий.
func scanManagedSet(row rowScanner) (managedSet, error) {
	var item managedSet
	err := row.Scan(&item.id, &item.Ref, &item.projectID, &item.ProjectRef, &item.Kind, &item.Name,
		&item.ManagedBy, &item.Source, &item.SourceRevision, &item.Version, &item.UpdatedAt, &item.currentRevisionID, &item.Archived, &item.CopyProvenance)
	return item, err
}

// scanManagedCopy scans the copied set prefix and revision tuple, then decodes diagnostics only
// after successful scanning; it preserves partial set/revision outputs and the raw JSON decode
// error rather than remapping it.
// scanManagedCopy сканирует copied set prefix и revision tuple, затем декодирует diagnostics
// только после успешного scan; сохраняет частичные set/revision outputs и исходную JSON decode
// error без преобразования.
func scanManagedCopy(row rowScanner) (managedSet, entity.ManagedConfigurationRevision, error) {
	var set managedSet
	var revision managedRevisionScan
	var raw []byte
	targets := []any{&set.id, &set.Ref, &set.projectID, &set.ProjectRef, &set.Kind, &set.Name, &set.ManagedBy,
		&set.Source, &set.SourceRevision, &set.Version, &set.UpdatedAt}
	targets = append(targets, managedRevisionScanTargets(&revision, &raw)...)
	err := row.Scan(targets...)
	if err == nil {
		err = json.Unmarshal(raw, &revision.ValidationDiagnostics)
	}
	return set, revision.ManagedConfigurationRevision, err
}

// scanManagedBinding scans binding identity, managed owner metadata, revision and trailing
// archive/provenance fields, then projects public configuration/revision even on error; malformed
// diagnostics map to Unavailable without discarding other assigned fields.
// scanManagedBinding сканирует binding identity, managed owner metadata, revision и завершающие
// archive/provenance fields, затем формирует публичные configuration/revision даже при ошибке;
// неверные diagnostics дают Unavailable без удаления остальных присвоенных fields.
func scanManagedBinding(row rowScanner) (entity.ManagedConfigurationBindingSnapshot, error) {
	var result entity.ManagedConfigurationBindingSnapshot
	var set managedSet
	var revision managedRevisionScan
	var diagnostics []byte
	targets := []any{&result.Ref, &result.Version, &result.ConsumerKind, &result.ConsumerRef,
		&set.id, &set.Ref, &set.projectID, &set.ProjectRef, &set.Kind, &set.Name, &set.ManagedBy,
		&set.Source, &set.SourceRevision, &set.Version, &set.UpdatedAt, &set.currentRevisionID}
	targets = append(targets, managedRevisionScanTargets(&revision, &diagnostics)...)
	targets = append(targets, &set.Archived, &set.CopyProvenance)
	err := row.Scan(targets...)
	if err == nil && json.Unmarshal(diagnostics, &revision.ValidationDiagnostics) != nil {
		err = errs.ErrUnavailable
	}
	result.Configuration = set.ManagedConfigurationSet
	result.Revision = revision.ManagedConfigurationRevision
	return result, err
}

// ListManagedConfigurationHistory resolves current owner access before source hydration and
// configuration-bound pagination, separately redacting role-image source or full prompt content.
// It decodes immutable history with total/cursor, hydrates prompt scopes and current action hints,
// guards a repeated next token and commits before returning; the cursor itself is not actor
// authority.
// ListManagedConfigurationHistory разрешает текущий owner access до source hydration и
// configuration-bound pagination, отдельно скрывая role-image source либо полный prompt content.
// Декодирует immutable history с total/cursor, добавляет prompt scopes и текущие action hints,
// отклоняет повторный next token и делает commit до ответа; cursor сам не является actor
// authority.
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
	if set.currentRevisionID != "" {
		revision, err := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationCurrentRevision, current.organizationID, set.id, set.currentRevisionID))
		if err != nil {
			return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrUnavailable
		}
		set.CurrentRevision = &revision.ManagedConfigurationRevision
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
		if set.CurrentRevision != nil {
			if !includeContent {
				set.CurrentRevision.Content = ""
			}
			if err := repository.hydratePromptScopeTx(ctx, tx, current, set.CurrentRevision); err != nil {
				return entity.ManagedConfigurationSet{}, nil, 0, "", err
			}
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
		targets := append(managedRevisionScanTargets(&item, &raw), &total)
		if scanErr := rows.Scan(targets...); scanErr != nil || json.Unmarshal(raw, &item.ValidationDiagnostics) != nil {
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
	if err := repository.projectCFGActions(ctx, tx, current, &set.ManagedConfigurationSet); err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", err
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

// managedHistoryFilterDigest trims the configuration reference and returns the raw-URL-base64
// first twelve SHA256 bytes as a history cursor binding fingerprint, without signing the token.
// managedHistoryFilterDigest очищает configuration reference и возвращает первые двенадцать SHA256
// bytes в raw-URL-base64 как fingerprint привязки history cursor без подписи токена.
func managedHistoryFilterDigest(configurationRef string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(configurationRef)))
	return base64.RawURLEncoding.EncodeToString(digest[:12])
}

// encodeManagedHistoryCursor serializes the version1 configuration fingerprint and exclusive
// revision boundary to raw URL base64, preserving the existing ignored JSON-marshal error
// convention.
// encodeManagedHistoryCursor сериализует version1 configuration fingerprint и исключающую revision
// boundary в raw URL base64, сохраняя прежнее игнорирование JSON-marshal error.
func encodeManagedHistoryCursor(configurationRef string, before int64) string {
	raw, _ := json.Marshal(managedHistoryCursor{Version: 1, Filter: managedHistoryFilterDigest(configurationRef), Before: before})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeManagedHistoryCursor accepts an omitted whitespace-only token as the initial page,
// otherwise bounds decoded bytes and requires valid JSON version, exact configuration fingerprint
// and positive revision boundary.
// decodeManagedHistoryCursor принимает пропущенный либо пробельный token как первую страницу,
// иначе ограничивает decoded bytes и требует допустимые JSON version, точный configuration
// fingerprint и положительную revision boundary.
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

// GetManagedConfigurationImpact bounds the read at five seconds, validates trimmed search and
// current scoped management access, then reads the target revision impact in the caller-derived
// transaction. It preserves NotFound on set-read failure and returns the populated impact together
// with the raw commit error when commit fails.
// GetManagedConfigurationImpact ограничивает чтение пятью секундами, проверяет очищенный search и
// текущий scoped management access, затем читает target revision impact в транзакции от
// разрешённого caller. Сохраняет NotFound при set-read failure и возвращает заполненный impact
// вместе с исходной commit error при отказе commit.
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

// GetEffectiveManagedConfiguration reads the exact kind/consumer binding in a read-only
// RepeatableRead transaction, using explicit runtime-environment context and source projection for
// role images. Missing binding and infrastructure errors stay distinct; current read authorization
// is not inferred from binding metadata, and commit failure returns zero output with Conflict.
// GetEffectiveManagedConfiguration читает binding точных kind/consumer в read-only RepeatableRead
// транзакции, используя явный runtime-environment context и source projection для role images.
// Отсутствие binding и инфраструктурные ошибки различаются; текущая read authorization не
// выводится из binding metadata, отказ commit даёт нулевой результат с Conflict.
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

// requireManagedSetAccess uses mailbox connection integration.manage for mailbox sets, otherwise
// the caller-supplied organization permission for empty project or exact project permission for
// project-owned sets; metadata resolution alone does not grant access.
// requireManagedSetAccess использует integration.manage connection владельца для mailbox sets,
// иначе переданный organization permission при пустом project либо точный project permission для
// project-owned sets; одно разрешение metadata не выдаёт доступа.
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

// managedImpactTx locks the set and exact revision, binds the catalog cursor to
// actor/configuration/revision/query, then reads SQL-filtered eligible consumers with
// authoritative total/digest and bounded next page. It performs no consumer rebind and leaves
// permission and commit ownership with callers.
// managedImpactTx блокирует set и точную revision, связывает catalog cursor с
// actor/configuration/revision/query, затем читает отфильтрованных SQL eligible consumers с
// авторитетными total/digest и ограниченной next page. Не выполняет consumer rebind, оставляя
// полномочия и commit вызывающим методам.
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

// GetSystemSTTConfiguration uses organization.view normally and platform.stt.use only for the
// exact stt-tts-service policy-resolve path, performs authorized organization reading and maps
// commit failure to Conflict before returning configuration readiness.
// GetSystemSTTConfiguration обычно использует organization.view, а platform.stt.use — только для
// точного stt-tts-service policy-resolve path; выполняет авторизованное чтение организации и
// возвращает Conflict при отказе commit до выдачи readiness конфигурации.
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

// getSystemSTTConfigurationTx reads current STT source/provider metadata and assembles fixed
// readiness blockers for permission, enablement, account and credential status. It requires
// decodable provider capabilities, parses and digest-checks stored content to project
// parameters/limits, and derives Ready only from blocker absence; this method performs no provider
// network probe or capability-map evaluation.
// getSystemSTTConfigurationTx читает текущие STT source/provider metadata и собирает фиксированные
// readiness blockers по permission, enablement, account и credential status. Требует декодируемые
// provider capabilities, разбирает и сверяет digest сохранённого content для проекции
// parameters/limits и выводит Ready только из отсутствия blockers; provider network probe и оценку
// capability-map не выполняет.
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

// systemSTTModelSupported delegates model/language support to the executable STT parameter
// validator rather than the agent LLM catalog, without contacting a provider.
// systemSTTModelSupported передаёт проверку model/language исполняемому STT parameter validator
// вместо agent LLM catalog без обращения к provider.
func systemSTTModelSupported(model, language string) bool {
	return (value.STTParameters{}).Validate(model, language) == nil
}

// GetEffectivePromptTemplate authorizes exact agent.view before selecting its effective immutable
// prompt projection, maps missing/read errors separately, marks the result PUBLISHED and commits
// before returning; it does not publish a new instruction version.
// GetEffectivePromptTemplate проверяет точный agent.view до выбора effective immutable prompt
// projection, различает missing/read errors, отмечает результат PUBLISHED и делает commit до
// ответа; новую instruction version не публикует.
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

// managedRevisionScanTargets returns a fresh ordered list for the twelve shared revision columns,
// including raw diagnostics and nullable validation/publication times. Each scanner appends its
// own prefix or trailing columns and retains its original decode, error and partial-result policy;
// assembling pointers neither queries SQL nor clears assigned fields.
// managedRevisionScanTargets возвращает новый упорядоченный список двенадцати общих revision
// columns, включая raw diagnostics и nullable validation/publication times. Каждый scanner добавляет
// собственные prefix либо trailing columns и сохраняет прежние decode, error и partial-result policy;
// сборка pointers не выполняет SQL и не очищает присвоенные fields.
func managedRevisionScanTargets(item *managedRevisionScan, diagnostics *[]byte) []any {
	return []any{&item.internalID, &item.Ref, &item.Revision, &item.State, &item.ContentFormat, &item.Content,
		&item.Digest, &item.ParentRevisionRef, diagnostics, &item.CreatedAt, &item.ValidatedAt, &item.PublishedAt}
}

// managedContentFormatAllowed preserves the shared CREATE/SAVE format predicate: prompt templates
// use TEXT, integration definitions additionally accept OPENAPI_IMPORT, and all other kinds retain
// JSON/YAML/TOML. It neither normalizes format nor validates kind/content; caller-specific size,
// UTF8, NUL, nonempty and ownership checks remain separate.
// managedContentFormatAllowed сохраняет общий CREATE/SAVE format predicate: prompt templates
// используют TEXT, integration definitions дополнительно принимают OPENAPI_IMPORT, остальные kinds
// сохраняют JSON/YAML/TOML. Не нормализует format и не проверяет kind/content; разные size, UTF8, NUL,
// nonempty и ownership checks остаются у callers.
func managedContentFormatAllowed(kind, format string) bool {
	if kind == revisionservice.KindPromptTemplate {
		return format == "TEXT"
	}
	return format == "JSON" || format == "YAML" || format == "TOML" ||
		kind == revisionservice.KindIntegrationDefinition && format == "OPENAPI_IMPORT"
}
