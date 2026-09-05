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
	//go:embed sql/email_authorization_invocation.sql
	queryEmailAuthorizationInvocation string
	//go:embed sql/email_authorization_test.sql
	queryEmailAuthorizationTest string
	//go:embed sql/email_authorization_get.sql
	queryEmailAuthorizationGet string
	//go:embed sql/email_authorization_insert.sql
	queryEmailAuthorizationInsert string
)

type emailAuthorizationSource struct {
	id, connectionRef, actorRef, agentRef, projectRef, projectID, grantRef string
	operation, effectKey, definitionVersion, definitionDigest, risk        string
	boundedInput, resourceScope, configuration                             []byte
	gateApproved                                                           bool
}

func emailBindingSource(binding entity.EmailExecutionBinding) string {
	if binding.InvocationRef != "" {
		return binding.InvocationRef
	}
	return binding.ConnectionTestRef
}

func emailFenceDigest(binding entity.EmailExecutionBinding) string {
	digest := sha256.Sum256([]byte(binding.Fence))
	return hex.EncodeToString(digest[:])
}

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
	definition, found := repository.integrationDefinitions["email"]
	capability, capabilityFound := definition.Capability(source.operation)
	if !found || !capabilityFound || !capability.CallableByAgent() || definition.Metadata.Version != source.definitionVersion ||
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
