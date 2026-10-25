package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	// queryEmailAuthorizationInvocation embeds a share-locked RUNNING invocation source under exact
	// tenant, gateway lease/fence/generation/expiry and current connection/package/grant/runtime pins.
	// It permits the existing approved-gate continuation shape for a succeeded node and projects
	// bounded input/resource scope plus root actor; parameter locators alone grant no email authority.
	// queryEmailAuthorizationInvocation встраивает share-locked RUNNING invocation source внутри
	// точных tenant, gateway lease/fence/generation/expiry и current connection/package/grant/runtime
	// pins. Разрешает прежнюю approved-gate continuation shape для succeeded node и проецирует bounded
	// input/resource scope с root actor; одни parameter locators не выдают email authority.
	//go:embed sql/email_authorization_invocation.sql
	queryEmailAuthorizationInvocation string
	// queryEmailAuthorizationTest embeds a share-locked CLAIMED email connection-test source with
	// exact gateway lease/fence/generation/expiry and enabled TESTING connection/ready adapter. It
	// projects the creating actor and fixed READ health operation without an agent scope; caller
	// resource/policy checks remain mandatory.
	// queryEmailAuthorizationTest встраивает share-locked CLAIMED email connection-test source с
	// точными gateway lease/fence/generation/expiry и enabled TESTING connection/ready adapter.
	// Проецирует creating actor и fixed READ health operation без agent scope; caller resource/policy
	// checks остаются обязательными.
	//go:embed sql/email_authorization_test.sql
	queryEmailAuthorizationTest string
	// queryEmailAuthorizationGet embeds a share-locked authorization lookup bound to organization,
	// source, lease, generation and fence digest. It returns the stored query/decision projections for
	// exact semantic replay comparison, not a refreshed expiry.
	// queryEmailAuthorizationGet встраивает share-locked authorization lookup с привязкой к
	// organization, source, lease, generation и fence digest. Возвращает сохранённые query/decision
	// projections для exact semantic replay comparison, а не обновлённый expiry.
	//go:embed sql/email_authorization_get.sql
	queryEmailAuthorizationGet string
	// queryEmailAuthorizationInsert embeds first-write authorization persistence with source IDs,
	// lease/fence/generation, semantic digest, safe projections and expiry. A duplicate
	// organization/source/lease/generation is ignored, so the caller must reread and compare the
	// existing decision rather than assuming the proposed row was accepted.
	// queryEmailAuthorizationInsert встраивает first-write authorization persistence с source IDs,
	// lease/fence/generation, semantic digest, safe projections и expiry. Повтор
	// organization/source/lease/generation игнорируется, поэтому caller обязан перечитать и сравнить
	// существующий decision вместо предположения о принятии новой строки.
	//go:embed sql/email_authorization_insert.sql
	queryEmailAuthorizationInsert string
)

// emailAuthorizationSource holds the private authoritative invocation/test projection:
// owner/resource/grant locators, operation/effect/package/risk pins, bounded input, public
// configuration and gate approval. Raw byte fields stay inside authorization processing and are
// not a public result, log payload or caller-issued grant.
// emailAuthorizationSource хранит private authoritative invocation/test projection:
// owner/resource/grant locators, operation/effect/package/risk pins, bounded input, public
// configuration и gate approval. Raw byte fields остаются внутри authorization processing и не
// являются public result, log payload или caller-issued grant.
type emailAuthorizationSource struct {
	id, connectionRef, actorRef, agentRef, projectRef, projectID, grantRef string
	operation, effectKey, definitionVersion, definitionDigest, risk        string
	boundedInput, resourceScope, configuration                             []byte
	gateApproved                                                           bool
}

// emailBindingSource prefers InvocationRef when present and otherwise returns ConnectionTestRef
// without validation. Callers must already enforce the mutually exclusive execution-binding shape;
// a nonempty ref is only a locator.
// emailBindingSource предпочитает InvocationRef при наличии, иначе возвращает ConnectionTestRef
// без валидации. Callers обязаны заранее потребовать взаимоисключающую execution-binding shape;
// непустой ref является только locator.
func emailBindingSource(binding entity.EmailExecutionBinding) string {
	if binding.InvocationRef != "" {
		return binding.InvocationRef
	}
	return binding.ConnectionTestRef
}

// emailFenceDigest returns lowercase SHA-256 of the exact fence bytes, including the ordinary hash
// of an empty string. It performs no normalization, freshness or ownership check and does not
// retain the raw fence.
// emailFenceDigest возвращает lowercase SHA-256 точных fence bytes, включая обычный hash пустой
// строки. Не выполняет normalization, freshness или ownership check и не сохраняет raw fence.
func emailFenceDigest(binding entity.EmailExecutionBinding) string {
	digest := sha256.Sum256([]byte(binding.Fence))
	return hex.EncodeToString(digest[:])
}

// emailAuthorization resolves the exact live invocation/test source in the caller transaction,
// uses its actor ref for current integration/project/agent access, checks the exact callable email
// package/risk/resource scope and current mailbox. It delegates canonical
// command/digest/policy/gate checks, then bounds decision expiry by the lease and authorization
// maximum age. AgentScope is present only for an agent source; shared exact scopes are explanatory
// output. It creates no receipt, commit or external email effect itself.
// emailAuthorization разрешает exact live invocation/test source в caller transaction, использует
// его actor ref для current integration/project/agent access, проверяет exact callable email
// package/risk/resource scope и current mailbox. Делегирует canonical command/digest/policy/gate
// checks, затем ограничивает decision expiry lease и authorization maximum age. AgentScope
// присутствует только для agent source; общие exact scopes являются пояснительным output. Сам не
// создаёт receipt, commit или внешний email effect.
func (repository *Repository) emailAuthorization(ctx context.Context, tx pgx.Tx, current scope, input query.EmailAuthorization) (entity.EmailAuthorization, emailAuthorizationSource, error) {
	var source emailAuthorizationSource
	statement := queryEmailAuthorizationInvocation
	if input.Binding.ConnectionTestRef != "" {
		statement = queryEmailAuthorizationTest
	}
	err := tx.QueryRow(ctx, statement, current.organizationID, emailBindingSource(input.Binding), input.Binding.LeaseRef,
		emailFenceDigest(input.Binding), input.Binding.Generation, input.Binding.ExpiresAt.UTC()).Scan(
		&source.id, &source.connectionRef, &source.actorRef, &source.agentRef, &source.projectRef, &source.projectID, &source.grantRef,
		&source.operation, &source.effectKey, &source.boundedInput, &source.resourceScope, &source.configuration,
		&source.definitionVersion, &source.definitionDigest, &source.risk, &source.gateApproved)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.EmailAuthorization{}, source, errs.ErrForbidden
	}
	if err != nil {
		return entity.EmailAuthorization{}, source, errs.ErrUnavailable
	}
	actor := current
	actor.actorRef = source.actorRef
	if err := repository.requireAccess(ctx, tx, actor, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: source.connectionRef}); err != nil {
		return entity.EmailAuthorization{}, source, errs.ErrForbidden
	}
	if source.agentRef != "" {
		for _, target := range []struct{ permission, kind, ref string }{
			{"project.view", "PROJECT", source.projectRef}, {"agent.view", "AGENT", source.agentRef},
		} {
			if err := repository.requireAccess(ctx, tx, actor, target.permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: target.kind, ResourceRef: target.ref}); err != nil {
				return entity.EmailAuthorization{}, source, errs.ErrForbidden
			}
		}
	}
	definition, packageErr := repository.integrationPackage(ctx, tx, current.organizationID, source.connectionRef, "email", source.definitionVersion, source.definitionDigest)
	capability, capabilityFound := definition.Capability(source.operation)
	if packageErr != nil || !capabilityFound || !capability.CallableByAgent() || definition.Metadata.Version != source.definitionVersion ||
		definition.Digest != source.definitionDigest || capability.Risk != source.risk {
		return entity.EmailAuthorization{}, source, errs.ErrForbidden
	}
	var configuration, resourceScope map[string]string
	if json.Unmarshal(source.configuration, &configuration) != nil || json.Unmarshal(source.resourceScope, &resourceScope) != nil {
		return entity.EmailAuthorization{}, source, errs.ErrUnavailable
	}
	exactScope, err := capability.ResourceScopeValues(configuration)
	if err != nil || source.agentRef != "" && !maps.Equal(exactScope, resourceScope) {
		return entity.EmailAuthorization{}, source, errs.ErrForbidden
	}
	mailbox, err := repository.readEmailMailbox(ctx, tx, current, input.MailboxRef, input.ConfigurationRevision)
	if err != nil {
		return entity.EmailAuthorization{}, source, err
	}
	if mailbox.ConnectionRef != source.connectionRef || mailbox.OrganizationRef != current.organizationRef {
		return entity.EmailAuthorization{}, source, errs.ErrForbidden
	}
	exact, policy, err := emailpolicy.AuthorizeCommand(mailbox, source.operation, source.effectKey, source.boundedInput, input, source.gateApproved)
	if err != nil {
		return entity.EmailAuthorization{}, source, err
	}
	expires := time.Now().UTC().Add(emailpolicy.AuthorizationMaximumAge)
	if input.Binding.ExpiresAt.Before(expires) {
		expires = input.Binding.ExpiresAt.UTC()
	}
	result := entity.EmailAuthorization{Allowed: true, GateApproved: source.gateApproved, ActorRef: source.actorRef,
		AgentRef: source.agentRef, OrganizationRef: current.organizationRef, ProjectRef: source.projectRef,
		ConnectionRef: source.connectionRef, MailboxRef: mailbox.Ref, GrantRef: source.grantRef,
		Operation: input.Operation, SemanticInputDigest: input.SemanticInputDigest, EffectKey: input.EffectKey,
		ConfigurationRevision: mailbox.Revision, CredentialGeneration: mailbox.CredentialGeneration, Policy: policy,
		UserScope: exact, ConnectionScope: exact, ResourceScope: exact, ExpiresAt: expires, Binding: input.Binding}
	if source.agentRef != "" {
		result.AgentScope = &exact
	}
	return result, source, nil
}

// ResolveEmailAuthorization accepts only unscoped email-bridge work with a valid live
// binding/digest/revision and a five-second budget. It reauthorizes current state, stores
// query/decision copies with raw fences cleared, rereads the bound ledger and requires canonical
// input/decision equality plus unexpired original expiry before commit. The returned copy restores
// the caller fence only after commit; replay does not renew authorization. Existing ignored
// marshal/rollback errors remain unchanged and no SMTP/IMAP/POP action is performed.
// ResolveEmailAuthorization принимает только unscoped email-bridge work с допустимыми live
// binding/digest/revision и пятисекундным budget. Заново авторизует current state, сохраняет
// query/decision copies с очищенными raw fences, перечитывает bound ledger и требует canonical
// input/decision equality с неистёкшим исходным expiry до commit. Возвращаемая копия
// восстанавливает caller fence только после commit; replay не продлевает authorization. Прежние
// ignored marshal/rollback errors не меняются, SMTP/IMAP/POP action не выполняется.
func (repository *Repository) ResolveEmailAuthorization(ctx context.Context, principal value.Principal, input query.EmailAuthorization) (entity.EmailAuthorization, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if principal.CallerWorkload != "email-bridge" || principal.ProjectRef != "" {
		return entity.EmailAuthorization{}, errs.ErrForbidden
	}
	if emailpolicy.ValidateExecutionBinding(input.Binding, time.Now(), false) != nil || !emailpolicy.ValidDigest(input.SemanticInputDigest) || input.ConfigurationRevision < 1 {
		return entity.EmailAuthorization{}, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.EmailAuthorization{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.EmailAuthorization{}, errs.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	decision, source, err := repository.emailAuthorization(ctx, tx, current, input)
	if err != nil {
		return entity.EmailAuthorization{}, err
	}
	storedInput, storedDecision := input, decision
	storedInput.Binding.Fence, storedDecision.Binding.Fence = "", ""
	inputJSON, err := json.Marshal(storedInput)
	if err != nil {
		return entity.EmailAuthorization{}, errs.ErrInvalid
	}
	decisionJSON, err := json.Marshal(storedDecision)
	if err != nil {
		return entity.EmailAuthorization{}, errs.ErrUnavailable
	}
	ref, err := newRef("emau")
	if err != nil {
		return entity.EmailAuthorization{}, errs.ErrUnavailable
	}
	invocationID, testID := source.id, ""
	if input.Binding.ConnectionTestRef != "" {
		invocationID, testID = "", source.id
	}
	if _, err := tx.Exec(ctx, queryEmailAuthorizationInsert, ref, current.organizationID, invocationID, testID,
		emailBindingSource(input.Binding), input.Binding.LeaseRef, emailFenceDigest(input.Binding), input.Binding.Generation,
		input.SemanticInputDigest, inputJSON, decisionJSON, decision.ExpiresAt); err != nil {
		return entity.EmailAuthorization{}, errs.ErrUnavailable
	}
	var previousInput, previousDecision []byte
	if err := tx.QueryRow(ctx, queryEmailAuthorizationGet, current.organizationID, emailBindingSource(input.Binding), input.Binding.LeaseRef,
		input.Binding.Generation, emailFenceDigest(input.Binding)).Scan(&ref, &previousInput, &previousDecision); err != nil {
		return entity.EmailAuthorization{}, errs.ErrForbidden
	}
	var previous entity.EmailAuthorization
	var previousQuery query.EmailAuthorization
	if json.Unmarshal(previousDecision, &previous) != nil || json.Unmarshal(previousInput, &previousQuery) != nil {
		return entity.EmailAuthorization{}, errs.ErrUnavailable
	}
	canonicalInput, _ := json.Marshal(previousQuery)
	storedDecision.ExpiresAt = previous.ExpiresAt
	canonicalDecision, _ := json.Marshal(storedDecision)
	previousCanonical, _ := json.Marshal(previous)
	if !bytes.Equal(canonicalInput, inputJSON) || !bytes.Equal(canonicalDecision, previousCanonical) || !previous.ExpiresAt.After(time.Now()) {
		return entity.EmailAuthorization{}, errs.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.EmailAuthorization{}, errs.ErrUnavailable
	}
	previous.Binding.Fence = input.Binding.Fence
	return previous, nil
}
