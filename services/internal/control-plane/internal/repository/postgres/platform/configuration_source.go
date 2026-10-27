package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryConfigurationSourceRead reads exact tenant/configuration Git source metadata, accepted
// provenance and internal root actor/format/project fields without exposing credential bytes.
// queryConfigurationSourceRead читает Git source metadata точных tenant/configuration, accepted
// provenance и внутренние root actor/format/project fields без выдачи credential bytes.
//
//go:embed sql/configuration_source__read.sql
var queryConfigurationSourceRead string

// queryConfigurationSourceConfigure creates or reconfigures the exact configuration source under
// current actor/connection and new immutable repository/ref/path/format inputs, advancing source
// generation.
// queryConfigurationSourceConfigure создаёт либо перенастраивает точный configuration source с
// текущими actor/connection и новыми immutable repository/ref/path/format inputs, увеличивая
// source generation.
//
//go:embed sql/configuration_source__configure.sql
var queryConfigurationSourceConfigure string

// queryConfigurationSourceCancelWork terminalizes open work belonging to the exact configuration
// source and revokes its active lease during reconfiguration or detach.
// queryConfigurationSourceCancelWork завершает открытую работу точного configuration source и
// отзывает active lease при перенастройке либо detach.
//
//go:embed sql/configuration_source__cancel_work.sql
var queryConfigurationSourceCancelWork string

// queryConfigurationSourceManageSet switches the exact versioned configuration set to GIT
// ownership/source revision, returning the new set version and update time for OCC readback.
// queryConfigurationSourceManageSet переключает точный versioned configuration set на GIT
// ownership/source revision, возвращая новые set version и update time для OCC readback.
//
//go:embed sql/configuration_source__manage_set.sql
var queryConfigurationSourceManageSet string

// queryConfigurationSourceCredential resolves internal connection/credential IDs only for the
// exact tenant, connection and active credential revision without reading secret material.
// queryConfigurationSourceCredential разрешает внутренние connection/credential IDs только для
// точных tenant, connection и active credential revision без чтения secret material.
//
//go:embed sql/configuration_source__credential.sql
var queryConfigurationSourceCredential string

// queryConfigurationSourceEnqueue persists a server work reference, source generation, root actor,
// exact connection/credential pins and immutable snapshot/digest/deadline for later owner claim.
// queryConfigurationSourceEnqueue сохраняет server work reference, source generation, root actor,
// точные connection/credential pins и immutable snapshot/digest/deadline для последующего owner
// claim.
//
//go:embed sql/configuration_source__enqueue.sql
var queryConfigurationSourceEnqueue string

// queryConfigurationSourceQueue marks an eligible existing source QUEUED for refresh without
// changing accepted commit or assigning a new worker lease.
// queryConfigurationSourceQueue отмечает допустимый существующий source как QUEUED для refresh без
// изменения accepted commit либо назначения нового worker lease.
//
//go:embed sql/configuration_source__queue.sql
var queryConfigurationSourceQueue string

// queryConfigurationSourceRetireDraft discards DRAFT/VALID candidates for the exact
// set before Git ownership, preserving their immutable content/history.
// queryConfigurationSourceRetireDraft помечает DRAFT/VALID candidates точного set
// как discarded до Git ownership, сохраняя immutable content/history.
//
//go:embed sql/configuration_source__retire_draft.sql
var queryConfigurationSourceRetireDraft string

// configurationSource pairs public Git source state/provenance with internal source/root-actor IDs
// and saved format/project locator for owner workflow checks; it carries no secret value.
// configurationSource связывает публичные Git source state/provenance с внутренними
// source/root-actor IDs и сохранёнными format/project locator для owner workflow checks; secret
// value не содержит.
type configurationSource struct {
	entity.ManagedConfigurationGitSource
	id, actorID, format, projectRef string
}

// hydrateConfigurationSource hydrates optional GitSource only for role-image or
// integration-definition sets, tolerating ErrNoRows and mapping other read failures to
// Unavailable; unrelated kinds and existing source absence remain unchanged.
// hydrateConfigurationSource добавляет необязательный GitSource только для role-image либо
// integration-definition sets, допуская ErrNoRows и преобразуя прочие read failures в Unavailable;
// остальные kinds и отсутствие source остаются прежними.
func hydrateConfigurationSource(ctx context.Context, querier connectionQuerier, organizationID string, set *managedSet) error {
	if set.Kind != "ROLE_IMAGE" && set.Kind != "INTEGRATION_DEFINITION" {
		return nil
	}
	source, err := readConfigurationSource(ctx, querier, organizationID, set.Ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	set.GitSource = &source.ManagedConfigurationGitSource
	return nil
}

// readConfigurationSource scans the exact tenant/configuration source tuple in its fixed
// eighteen-column order and returns the original scan error with any partial projection; it
// performs no operation authorization.
// readConfigurationSource сканирует source tuple точных tenant/configuration в фиксированном
// порядке восемнадцати колонок и возвращает исходную scan error с частичным представлением;
// авторизацию операции не выполняет.
func readConfigurationSource(ctx context.Context, querier connectionQuerier, organizationID, ref string) (configurationSource, error) {
	var source configurationSource
	err := querier.QueryRow(ctx, queryConfigurationSourceRead, organizationID, ref).Scan(&source.id, &source.Ref, &source.Version, &source.Generation, &source.State,
		&source.ConnectionRef, &source.ProviderKey, &source.RepositoryRef, &source.RefName, &source.Path, &source.AcceptedCommitSHA, &source.AcceptedContentSHA256,
		&source.AcceptedRevisionRef, &source.SyncedAt, &source.FailureCode, &source.actorID, &source.format, &source.projectRef)
	return source, err
}

// configurationSourceCommand maps the four registered role-image/integration Git source commands
// to kind and configure-versus-refresh mode; unknown commands return empty kind and false.
// configurationSourceCommand сопоставляет четыре зарегистрированные role-image/integration Git
// source команды с kind и режимом configure либо refresh; неизвестные команды дают пустой kind и
// false.
func configurationSourceCommand(kind command.Kind) (string, bool) {
	switch kind {
	case command.ConfigureRoleImageGitSource:
		return "ROLE_IMAGE", true
	case command.ConfigureIntegrationDefinitionGitSource:
		return "INTEGRATION_DEFINITION", true
	case command.RefreshRoleImageGitSource:
		return "ROLE_IMAGE", false
	case command.RefreshIntegrationDefinitionGitSource:
		return "INTEGRATION_DEFINITION", false
	default:
		return "", false
	}
}

// validConfigurationSourceInput checks nonempty configuration/connection locators, positive
// connection version, bounded UTF8 repository/ref/path, forbidden control/ref syntax, canonical
// relative path and exact JSON/YAML format. It does not contact Git, verify repository authority
// or resolve a credential.
// validConfigurationSourceInput проверяет непустые configuration/connection locators,
// положительную connection version, ограниченные UTF8 repository/ref/path, запрещённые control/ref
// syntax, канонический relative path и точный JSON/YAML format. К Git не обращается, repository
// authority не проверяет и credential не разрешает.
func validConfigurationSourceInput(input command.ManagedConfigurationGitSourceInput) bool {
	return input.ConfigurationRef != "" && input.ConnectionRef != "" && input.ExpectedConnectionVersion > 0 &&
		len(input.RepositoryRef) > 0 && len(input.RepositoryRef) <= 256 && len(input.RefName) > 0 && len(input.RefName) <= 256 &&
		len(input.Path) > 0 && len(input.Path) <= 512 && utf8.ValidString(input.RepositoryRef+input.RefName+input.Path) &&
		!strings.ContainsAny(input.RepositoryRef+input.RefName+input.Path, "\x00\r\n\\") &&
		!strings.ContainsAny(input.RefName, " ~^:?*[") && !strings.Contains(input.RefName, "..") && !strings.Contains(input.RefName, "@{") &&
		!strings.HasPrefix(input.RefName, "-") && !strings.HasSuffix(input.RefName, ".") && !strings.HasSuffix(input.RefName, ".lock") &&
		!strings.HasPrefix(input.Path, "/") && path.Clean(input.Path) == input.Path && input.Path != "." && input.Path != ".." && !strings.HasPrefix(input.Path, "../") &&
		(input.ContentFormat == "JSON" || input.ContentFormat == "YAML")
}

// configurationSourceAuthority resolves exact typed configuration ownership and current
// project/organization management, requiring image.build and source.view/manage for role images
// before shipped/archive guards. Configure authorizes the requested connection; refresh derives
// its connection from stored source. It requires integration.manage without treating request
// locators as authority.
// configurationSourceAuthority разрешает точное typed configuration ownership и текущий
// project/organization management, требуя image.build и source.view/manage для role images до
// shipped/archive guards. Configure авторизует запрошенную connection, refresh берёт connection из
// сохранённого source. Требует integration.manage без вывода authority из request locators.
func (repository *Repository) configurationSourceAuthority(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (managedSet, error) {
	payload, ok := input.Payload.(command.ManagedConfigurationGitSourceInput)
	kind, configure := configurationSourceCommand(input.Kind)
	if !ok || kind == "" || payload.ConfigurationRef == "" {
		return managedSet{}, errs.ErrInvalid
	}
	set, err := repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: payload.ConfigurationRef}, kind, false)
	if err != nil {
		return managedSet{}, err
	}
	permission, target := "organization.manage", organizationTarget(current.organizationRef)
	if set.ProjectRef != "" {
		permission, target = "project.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: set.ProjectRef, ResourceKind: "PROJECT", ResourceRef: set.ProjectRef}
	}
	if repository.requireAccess(ctx, tx, current, permission, target) != nil {
		return managedSet{}, errs.ErrNotFound
	}
	if kind == "ROLE_IMAGE" && repository.requireAccess(ctx, tx, current, "image.build", target) != nil {
		return managedSet{}, errs.ErrForbidden
	}
	if kind == "ROLE_IMAGE" {
		for _, sourcePermission := range []string{"image.source.view", "image.source.manage"} {
			if err := repository.managedRoleImageSourceAccess(ctx, tx, current, set.Ref, set.ProjectRef, sourcePermission); err != nil {
				return managedSet{}, err
			}
		}
	}
	if err := rejectShippedRoleImageMutation(ctx, tx, current.organizationID, set); err != nil {
		return managedSet{}, err
	}
	if set.Archived {
		return managedSet{}, errs.ErrConflict
	}
	connectionRef := payload.ConnectionRef
	if !configure {
		source, err := readConfigurationSource(ctx, tx, current.organizationID, set.Ref)
		if err != nil {
			return managedSet{}, errs.ErrNotFound
		}
		connectionRef = source.ConnectionRef
	}
	if repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef}) != nil {
		return managedSet{}, errs.ErrNotFound
	}
	return set, nil
}

// sourceInput validates source shape and exact connection OCC, current connected
// credential/package/configuration and repository restriction, then requires the precise Git
// provider READ capability with no approval gate. It returns a bounded immutable fifteen-minute
// work snapshot plus internal connection/credential IDs, retaining provider/credential provenance
// and leaving secret retrieval to the worker protocol.
// sourceInput проверяет форму source и точный connection OCC, текущие connected
// credential/package/configuration и repository restriction, затем требует точную Git provider
// READ capability без approval gate. Возвращает ограниченный immutable work snapshot на пятнадцать
// минут и внутренние connection/credential IDs, сохраняя provider/credential provenance и оставляя
// получение секрета worker protocol.
func (repository *Repository) sourceInput(ctx context.Context, tx pgx.Tx, current scope, set managedSet, payload command.ManagedConfigurationGitSourceInput) (entity.ManagedConfigurationSourceWork, string, string, error) {
	if !validConfigurationSourceInput(payload) {
		return entity.ManagedConfigurationSourceWork{}, "", "", errs.ErrInvalid
	}
	locked, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, payload.ConnectionRef)
	if err != nil {
		return entity.ManagedConfigurationSourceWork{}, "", "", err
	}
	if locked.version != payload.ExpectedConnectionVersion {
		return entity.ManagedConfigurationSourceWork{}, "", "", errs.ErrVersionMismatch
	}
	connection, err := readConnection(ctx, tx, current, payload.ConnectionRef)
	if err != nil {
		return entity.ManagedConfigurationSourceWork{}, "", "", err
	}
	if !connection.Enabled || connection.State != "CONNECTED" || connection.CredentialRevision == nil || (connection.DefinitionKey != "github" && connection.DefinitionKey != "gitlab") {
		return entity.ManagedConfigurationSourceWork{}, "", "", errs.ErrConflict
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, connection.Ref, connection.DefinitionKey, connection.DefinitionVersion, connection.DefinitionDigest)
	if err != nil {
		return entity.ManagedConfigurationSourceWork{}, "", "", err
	}
	configuration, ok := integrationStringConfiguration(connection.PublicConfiguration)
	if !ok || definition.ValidateConfiguration(configuration) != nil {
		return entity.ManagedConfigurationSourceWork{}, "", "", errs.ErrInvalid
	}
	repositoryRef := configuration["project_path"]
	if connection.DefinitionKey == "github" {
		repositoryRef = configuration["owner"] + "/" + configuration["repository"]
	}
	if payload.RepositoryRef != repositoryRef {
		return entity.ManagedConfigurationSourceWork{}, "", "", errs.ErrForbidden
	}
	operation := "github.repository.content.read"
	if connection.DefinitionKey == "gitlab" {
		operation = "gitlab.repository.file.read"
	}
	capability, ok := definition.Capability(operation)
	if !ok || capability.Operation != operation || capability.Risk != "READ" || capability.ApprovalPolicy != "NONE" {
		return entity.ManagedConfigurationSourceWork{}, "", "", errs.ErrForbidden
	}
	var connectionID, credentialID string
	if err := tx.QueryRow(ctx, queryConfigurationSourceCredential, current.organizationID, connection.Ref, connection.CredentialRevision.Ref).Scan(&connectionID, &credentialID); err != nil {
		return entity.ManagedConfigurationSourceWork{}, "", "", errs.ErrForbidden
	}
	work := entity.ManagedConfigurationSourceWork{ConfigurationRef: set.Ref, Kind: set.Kind, ConnectionRef: connection.Ref, ConnectionVersion: connection.Version,
		DefinitionKey: definition.Metadata.Key, DefinitionVersion: definition.Metadata.Version, DefinitionDigest: definition.Digest, DefinitionPackage: asJSON(definition),
		RepositoryRef: repositoryRef, RefName: payload.RefName, Path: payload.Path, ContentFormat: payload.ContentFormat, MaximumContentBytes: 256 << 10,
		Deadline: time.Now().UTC().Truncate(time.Microsecond).Add(15 * time.Minute), PublicConfiguration: connection.PublicConfiguration, CredentialRevision: *connection.CredentialRevision}
	return work, connectionID, credentialID, nil
}

// changeConfigurationSource reauthorizes current configuration before set OCC, cancels write-back
// proposals, then either retires UI drafts and open work while configuring a new generation or
// rebuilds refresh input from stored source/current connection. It enqueues exact hashed work with
// server reference/generation/deadline, switches the set to GIT and returns safe source metadata
// in the caller transaction without commit.
// changeConfigurationSource повторно проверяет текущую configuration authority до set OCC,
// отменяет write-back proposals, затем либо завершает UI drafts и открытую работу при настройке
// новой generation, либо восстанавливает refresh input из сохранённого source/current connection.
// Ставит в очередь точную hashed work с server reference/generation/deadline, переводит set в GIT
// и возвращает безопасные source metadata в транзакции вызывающего метода без commit.
func (repository *Repository) changeConfigurationSource(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	set, err := repository.configurationSourceAuthority(ctx, tx, current, input)
	if err != nil {
		return commandOutcome{}, err
	}
	if input.Mutation.ExpectedVersion == nil || set.Version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	payload := input.Payload.(command.ManagedConfigurationGitSourceInput)
	if err := repository.cancelConfigurationWriteBacks(ctx, tx, current, set.Ref, ""); err != nil {
		return commandOutcome{}, err
	}
	_, configure := configurationSourceCommand(input.Kind)
	source, readErr := readConfigurationSource(ctx, tx, current.organizationID, set.Ref)
	if readErr != nil && !errors.Is(readErr, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if !configure {
		if readErr != nil || set.ManagedBy != "GIT" || (source.State != "READY" && source.State != "SYNC_BLOCKED") {
			return commandOutcome{}, errs.ErrConflict
		}
		connection, err := readConnection(ctx, tx, current, source.ConnectionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		payload = command.ManagedConfigurationGitSourceInput{ConfigurationRef: set.Ref, ConnectionRef: source.ConnectionRef, ExpectedConnectionVersion: connection.Version,
			RepositoryRef: source.RepositoryRef, RefName: source.RefName, Path: source.Path, ContentFormat: source.format}
	}
	work, connectionID, credentialID, err := repository.sourceInput(ctx, tx, current, set, payload)
	if err != nil {
		return commandOutcome{}, err
	}
	if configure {
		if _, err := tx.Exec(ctx, queryConfigurationSourceRetireDraft, set.id); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryConfigurationSourceCancelWork, set.id); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		ref, err := newRef("mcsrc")
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var sourceID string
		if err := tx.QueryRow(ctx, queryConfigurationSourceConfigure, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID, "configuration_id": set.id, "actor_id": current.actorID,
			"connection_id": connectionID, "provider": work.DefinitionKey, "repository": work.RepositoryRef, "ref_name": work.RefName, "path": work.Path, "format": work.ContentFormat}).Scan(&sourceID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		if _, err := tx.Exec(ctx, queryConfigurationSourceQueue, source.id); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	source, err = readConfigurationSource(ctx, tx, current.organizationID, set.Ref)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	work.SourceRef = source.Ref
	work.PreviousCommitSHA = source.AcceptedCommitSHA
	work.Lease.SourceGeneration = source.Generation
	ref, err := newRef("mcswork")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	work.Lease.WorkRef = ref
	snapshot := asJSON(work)
	digest := sha256.Sum256(snapshot)
	if len(snapshot) > 256<<10 {
		return commandOutcome{}, errs.ErrInvalid
	}
	if _, err := tx.Exec(ctx, queryConfigurationSourceEnqueue, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID, "source_id": source.id, "generation": source.Generation,
		"actor_id": source.actorID, "connection_id": connectionID, "connection_version": work.ConnectionVersion, "credential_id": credentialID, "snapshot": snapshot, "digest": hex.EncodeToString(digest[:]), "deadline": work.Deadline}); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if err := tx.QueryRow(ctx, queryConfigurationSourceManageSet, set.id, source.Ref, source.AcceptedCommitSHA, set.Version).Scan(&set.Version, &set.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	set.ManagedBy, set.Source, set.SourceRevision = "GIT", source.Ref, source.AcceptedCommitSHA
	set.GitSource = &source.ManagedConfigurationGitSource
	return managedOutcome(set, nil), nil
}
