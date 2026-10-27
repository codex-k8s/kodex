package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryIntegrationPackageBoundRevision embeds exact tenant/connection binding to a published
// INTEGRATION_DEFINITION revision through tenant-consistent binding/set/revision joins. It returns
// content for subsequent parse/pin/executable validation, not direct execution authority.
// queryIntegrationPackageBoundRevision встраивает exact tenant/connection binding к published
// INTEGRATION_DEFINITION revision через tenant-consistent binding/set/revision joins. Возвращает
// content для parse/pin/executable validation, а не direct execution authority.
//
//go:embed sql/integration_package__bound_revision.sql
var queryIntegrationPackageBoundRevision string

// queryIntegrationPackageBindConnection embeds active connection package-pin replacement, clearing
// credential/materializer/test state and advancing version. Credential requirement determines the
// new masked status; caller access, dependency checks and atomic commit are not encoded here.
// queryIntegrationPackageBindConnection встраивает замену package pins active connection с
// очисткой credential/materializer/test state и увеличением version. Credential requirement задаёт
// новый masked status; caller access, dependency checks и атомарный commit здесь не закодированы.
//
//go:embed sql/integration_package__bind_connection.sql
var queryIntegrationPackageBindConnection string

// errIntegrationPackageUnavailable marks a missing executable revision alongside Forbidden so
// aggregate readers may omit only unsupported objects. Found content corruption, SQL failure and
// pin mismatch must not gain this marker or become successful empty metadata.
// errIntegrationPackageUnavailable помечает отсутствие executable revision вместе с Forbidden,
// чтобы aggregate readers могли опускать только unsupported objects. Found content corruption, SQL
// failure и pin mismatch не получают marker и не становятся successful empty metadata.
var errIntegrationPackageUnavailable = errors.New("integration package revision is unavailable")

// bindIntegrationPackage locks a connection, validates executable package/key and stored
// configuration, then rejects active effect dependencies before disabling grants/tests and
// replacing package pins. Exactly one update is required before an event snapshot; all mutations
// remain in the supplied transaction, with actor admission and commit owned by the caller.
// bindIntegrationPackage блокирует connection, проверяет executable package/key и stored
// configuration, затем отклоняет active effect dependencies до отключения grants/tests и замены
// package pins. До event snapshot требуется ровно один update; все mutations остаются в supplied
// transaction, actor admission и commit принадлежат caller.
func (repository *Repository) bindIntegrationPackage(ctx context.Context, tx pgx.Tx, current scope, connectionRef, format, content string) error {
	connection, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, connectionRef)
	if err != nil {
		return err
	}
	definition, err := repository.executableIntegrationPackage(format, content)
	if err != nil || definition.Metadata.Key != connection.definitionKey {
		return errs.ErrInvalid
	}
	stored, err := readConnection(ctx, tx, current, connectionRef)
	if err != nil {
		return err
	}
	configuration, valid := integrationStringConfiguration(stored.PublicConfiguration)
	if !valid || definition.ValidateConfiguration(configuration) != nil {
		return errs.ErrInvalid
	}
	effects, _, err := repository.integrationConnectionDependencies(ctx, tx, connection.id)
	if err != nil {
		return err
	}
	if effects != 0 {
		return errs.ErrConflict
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_ref": connectionRef}
	for _, query := range []string{queryConfigurationChangeconnectionUpdateIntegrationGrantsEnabledVersionUpdatedAt, queryConfigurationChangeconnectionUpdateIntegrationConnectionTestsStateLeaseRefFenceDigest} {
		if _, err := tx.Exec(ctx, query, args); err != nil {
			return errs.ErrUnavailable
		}
	}
	tag, err := tx.Exec(ctx, queryIntegrationPackageBindConnection, current.organizationID, connectionRef, definition.Metadata.Version, definition.Digest, definition.RequiresConnectionCredential())
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", connectionRef, "i18n:INTEGRATION_CONNECTION_UPDATED", connection.version+1, "")
}

// projectConnectionPackage projects a bound parsed package only when key/version/digest exactly
// match the connection. Missing openapi-mcp binding marks credentials unconfigured; missing other
// bindings leaves existing projection. It rebuilds capability/schema metadata without execution
// admission; later errors may leave partial item mutations that callers must discard.
// projectConnectionPackage проецирует bound parsed package только при exact key/version/digest
// connection. Missing openapi-mcp binding отмечает credentials unconfigured; отсутствие других
// bindings сохраняет existing projection. Перестраивает capability/schema metadata без execution
// admission; поздние errors могут оставить partial item mutations, которые caller обязан
// отбросить.
func projectConnectionPackage(ctx context.Context, querier connectionQuerier, current scope, item *entity.IntegrationConnection) error {
	var format, content string
	err := querier.QueryRow(ctx, queryIntegrationPackageBoundRevision, current.organizationID, item.Ref).Scan(&format, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		if item.DefinitionKey == "openapi-mcp" {
			// Поставленный шаблон не исполняется до явной привязки ревизии владельца.
			item.MaskedCredentialsState = "NOT_CONFIGURED"
		}
		return nil
	}
	if err != nil || (format != "JSON" && format != "YAML") {
		return errs.ErrUnavailable
	}
	definition, err := integrationpackage.Parse([]byte(content))
	if err != nil || definition.Metadata.Key != item.DefinitionKey || definition.Metadata.Version != item.DefinitionVersion || definition.Digest != item.DefinitionDigest {
		return errs.ErrUnavailable
	}
	item.DefinitionName = definition.Spec.Name
	item.CredentialSecretKey = ""
	if definition.RequiresConnectionCredential() {
		item.CredentialSecretKey = definition.Spec.Credential.SecretKey
	}
	item.Capabilities = make([]entity.IntegrationCapability, 0, len(definition.Spec.Capabilities))
	for _, capability := range definition.Spec.Capabilities {
		schema, err := capability.InputSchema()
		if err != nil {
			return errs.ErrUnavailable
		}
		digest, err := capability.InputSchemaDigest()
		if err != nil {
			return errs.ErrUnavailable
		}
		item.Capabilities = append(item.Capabilities, entity.IntegrationCapability{
			Key: capability.Key, Name: capability.Name, Description: capability.Description,
			Operation: capability.Operation, Risk: capability.Risk, ApprovalPolicy: capability.ApprovalPolicy,
			AllowedApprovalPolicies: append([]string{}, capability.AllowedApprovalPolicies...),
			ResourceKind:            capability.ResourceScope.Kind, InputFields: integrationConfigurationFields(capability.InputFields),
			InputSchema: string(schema), InputSchemaSHA256: digest,
		})
		if capability.Operation == definition.Spec.HealthCheck.Operation {
			item.TestRequiresApproval = capability.ApprovalPolicy != "NONE"
		}
	}
	return nil
}

// integrationPackage uses a compatible shipped revision except the nonexecutable openapi-mcp
// template, otherwise reads the exact published connection binding and validates executable
// key/version/digest. Missing registry/binding returns Forbidden with an unavailable marker; found
// corruption/pin mismatch remains plain Forbidden and backend failure Unavailable. It does not
// authorize the actor or invoke an adapter.
// integrationPackage использует compatible shipped revision, кроме nonexecutable openapi-mcp
// template, иначе читает exact published connection binding и проверяет executable
// key/version/digest. Missing registry/binding даёт Forbidden с unavailable marker; found
// corruption/pin mismatch остаётся plain Forbidden, backend failure — Unavailable. Не авторизует
// actor и не вызывает adapter.
func (repository *Repository) integrationPackage(ctx context.Context, runner queryRunner, organizationID, connectionRef, key, version, digest string) (integrationpackage.Package, error) {
	shipped, ok := repository.integrationDefinitions[key]
	if !ok {
		return integrationpackage.Package{}, errors.Join(errs.ErrForbidden, errIntegrationPackageUnavailable)
	}
	if compatible, ok := integrationpackage.ResolveShippedRevision(shipped, version, digest); ok {
		if key == "openapi-mcp" {
			// Поставленный шаблон нужен только для создания подключения.
			// Без опубликованной owner-ревизии его нельзя исполнять.
			return integrationpackage.Package{}, errs.ErrForbidden
		}
		return compatible, nil
	}
	var format, content string
	err := runner.QueryRow(ctx, queryIntegrationPackageBoundRevision, organizationID, connectionRef).Scan(&format, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return integrationpackage.Package{}, errors.Join(errs.ErrForbidden, errIntegrationPackageUnavailable)
	}
	if err != nil {
		return integrationpackage.Package{}, errs.ErrUnavailable
	}
	definition, err := repository.executableIntegrationPackage(format, content)
	if err != nil || definition.Metadata.Key != key || definition.Metadata.Version != version || definition.Digest != digest {
		return integrationpackage.Package{}, errs.ErrForbidden
	}
	return definition, nil
}

// normalizeIntegrationDraft canonicalizes supported JSON/YAML managed revisions against shipped
// definitions into JSON. Unsupported format or normalization error preserves the original draft
// for diagnostics; successful normalization does not publish or establish executable admission.
// normalizeIntegrationDraft канонизирует поддержанные JSON/YAML managed revisions по shipped
// definitions в JSON. Unsupported format либо normalization error сохраняет исходный draft для
// диагностики; успешная normalization не публикует revision и не даёт executable admission.
func (repository *Repository) normalizeIntegrationDraft(format, content, managedBy string) (string, string) {
	if format != "JSON" && format != "YAML" {
		return format, content
	}
	_, canonical, err := integrationpackage.NormalizeManagedRevision([]byte(content), managedBy, repository.integrationDefinitions)
	if err != nil {
		// Невалидный draft сохраняется для диагностики; публикация запрещена.
		return format, content
	}
	return "JSON", string(canonical)
}

// prepareIntegrationDraft allows OPENAPI_IMPORT only for UI origin, converts it to a draft package
// and JSON, then applies ordinary normalization. Import errors are Invalid, encoding failure
// Unavailable and wrong origin Conflict; no revision is stored or published here.
// prepareIntegrationDraft разрешает OPENAPI_IMPORT только для UI origin, преобразует его в draft
// package и JSON, затем применяет ordinary normalization. Import errors дают Invalid, encoding
// failure — Unavailable, wrong origin — Conflict; revision здесь не сохраняется и не публикуется.
func (repository *Repository) prepareIntegrationDraft(ctx context.Context, format, content, managedBy string) (string, string, error) {
	if format == "OPENAPI_IMPORT" {
		if managedBy != integrationpackage.OriginUI {
			return "", "", errs.ErrConflict
		}
		definition, err := integrationpackage.DraftOpenAPIPackageFromJSON(ctx, []byte(content))
		if err != nil {
			return "", "", errs.ErrInvalid
		}
		canonical, err := json.Marshal(definition)
		if err != nil {
			return "", "", errs.ErrUnavailable
		}
		format, content = "JSON", string(canonical)
	}
	format, content = repository.normalizeIntegrationDraft(format, content, managedBy)
	return format, content, nil
}

// executableIntegrationPackage accepts JSON/YAML parsed content only when its definition key
// exists in the shipped registry and executable-revision compatibility passes against that
// baseline. Every shape/parse/registry/compatibility failure is Invalid; this pure admission does
// not validate connection config or actor permissions.
// executableIntegrationPackage принимает JSON/YAML parsed content только при definition key в
// shipped registry и successful executable-revision compatibility с baseline. Любой
// shape/parse/registry/compatibility failure даёт Invalid; pure admission не проверяет connection
// config либо actor permissions.
func (repository *Repository) executableIntegrationPackage(format, content string) (integrationpackage.Package, error) {
	if format != "JSON" && format != "YAML" {
		return integrationpackage.Package{}, errs.ErrInvalid
	}
	definition, err := integrationpackage.Parse([]byte(content))
	baseline, ok := repository.integrationDefinitions[definition.Metadata.Key]
	if err != nil || !ok || integrationpackage.ValidateExecutableRevision(definition, baseline) != nil {
		return integrationpackage.Package{}, errs.ErrInvalid
	}
	return definition, nil
}
