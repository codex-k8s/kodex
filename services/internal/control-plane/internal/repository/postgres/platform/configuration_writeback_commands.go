package platform

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func writeBackKind(kind command.Kind) string {
	switch kind {
	case command.PrepareRoleImageGitWriteBack:
		return "ROLE_IMAGE"
	case command.PrepareIntegrationDefinitionGitWriteBack:
		return "INTEGRATION_DEFINITION"
	default:
		return ""
	}
}

func (repository *Repository) writeBackSetAuthority(ctx context.Context, tx pgx.Tx, current scope, ref, kind string, mutation bool) (managedSet, error) {
	set, err := repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: ref}, kind, false)
	if err != nil {
		return set, err
	}
	if set.Kind != "ROLE_IMAGE" && set.Kind != "INTEGRATION_DEFINITION" {
		return set, errs.ErrNotFound
	}
	if repository.requireManagedSetAccess(ctx, tx, current, set, "project.view", "organization.view") != nil {
		return set, errs.ErrNotFound
	}
	if mutation {
		if repository.requireManagedSetAccess(ctx, tx, current, set, "project.manage", "organization.manage") != nil {
			return set, errs.ErrForbidden
		}
		if set.Kind == "ROLE_IMAGE" && repository.requireManagedSetAccess(ctx, tx, current, set, "image.build", "organization.manage") != nil {
			return set, errs.ErrForbidden
		}
		if err := rejectShippedRoleImageMutation(ctx, tx, current.organizationID, set); err != nil {
			return set, err
		}
	}
	return set, nil
}

func (repository *Repository) writeBackCommandAuthority(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (managedSet, error) {
	payload, ok := input.Payload.(command.ConfigurationWriteBackInput)
	if !ok {
		return managedSet{}, errs.ErrInvalid
	}
	ref, kind := payload.ConfigurationRef, writeBackKind(input.Kind)
	connectionRef := ""
	if kind == "" {
		row, err := lockWriteBack(ctx, tx, current, payload.ProposalRef)
		if err != nil {
			return managedSet{}, err
		}
		ref, kind = row.proposal.ConfigurationRef, row.proposal.Kind
		connectionRef = row.proposal.ConnectionRef
	}
	set, err := repository.writeBackSetAuthority(ctx, tx, current, ref, kind, true)
	if err != nil {
		return set, err
	}
	if connectionRef == "" {
		source, err := readConfigurationSource(ctx, tx, current.organizationID, set.Ref)
		if err != nil {
			return set, errs.ErrConflict
		}
		connectionRef = source.ConnectionRef
	}
	if repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef}) != nil {
		return set, errs.ErrForbidden
	}
	return set, nil
}

func (repository *Repository) writeBackSource(ctx context.Context, tx pgx.Tx, current scope, set managedSet) (configurationSource, entity.ManagedConfigurationSourceWork, string, string, error) {
	source, err := readConfigurationSource(ctx, tx, current.organizationID, set.Ref)
	if err != nil {
		return source, entity.ManagedConfigurationSourceWork{}, "", "", errs.ErrConflict
	}
	if set.ManagedBy != "GIT" || source.State != "READY" || set.Source != source.Ref || source.AcceptedRevisionRef == "" || set.CurrentRevision == nil || set.CurrentRevision.Ref != source.AcceptedRevisionRef {
		return source, entity.ManagedConfigurationSourceWork{}, "", "", errs.ErrConflict
	}
	if repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: source.ConnectionRef}) != nil {
		return source, entity.ManagedConfigurationSourceWork{}, "", "", errs.ErrForbidden
	}
	connection, err := readConnection(ctx, tx, current, source.ConnectionRef)
	if err != nil {
		return source, entity.ManagedConfigurationSourceWork{}, "", "", err
	}
	work, connectionID, credentialID, err := repository.sourceInput(ctx, tx, current, set, command.ManagedConfigurationGitSourceInput{
		ConfigurationRef: set.Ref, ConnectionRef: source.ConnectionRef, ExpectedConnectionVersion: connection.Version,
		RepositoryRef: source.RepositoryRef, RefName: source.RefName, Path: source.Path, ContentFormat: source.format,
	})
	if err != nil {
		return source, work, "", "", err
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, connection.Ref, connection.DefinitionKey, connection.DefinitionVersion, connection.DefinitionDigest)
	if err != nil {
		return source, work, "", "", err
	}
	operations := []string{"github.branch.create", "github.repository.content.update", "github.pull_request.create"}
	if connection.DefinitionKey == "gitlab" {
		operations = []string{"gitlab.branch.create", "gitlab.commit.create", "gitlab.merge_request.create"}
	}
	for _, operation := range operations {
		capability, ok := definition.Capability(operation)
		if !ok || capability.Operation != operation || capability.Risk == "READ" || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" {
			return source, work, "", "", errs.ErrForbidden
		}
	}
	work.SourceRef, work.PreviousCommitSHA, work.Lease.SourceGeneration = source.Ref, source.AcceptedCommitSHA, source.Generation
	return source, work, connectionID, credentialID, nil
}

func (repository *Repository) changeConfigurationWriteBack(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	set, err := repository.writeBackCommandAuthority(ctx, tx, current, input)
	if err != nil {
		return commandOutcome{}, err
	}
	payload := input.Payload.(command.ConfigurationWriteBackInput)
	if input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	if writeBackKind(input.Kind) != "" {
		return repository.prepareConfigurationWriteBack(ctx, tx, current, set, input, payload)
	}
	row, err := lockWriteBack(ctx, tx, current, payload.ProposalRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if row.proposal.Version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	switch input.Kind {
	case command.ApproveManagedConfigurationGitWriteBack, command.RejectManagedConfigurationGitWriteBack:
		if row.proposal.State != entity.WriteBackWaiting || payload.ApprovalDigest != row.proposal.ApprovalDigest || !now.Before(row.proposal.ExpiresAt) {
			return commandOutcome{}, errs.ErrConflict
		}
		if input.Kind == command.ApproveManagedConfigurationGitWriteBack {
			if err := repository.writeBackEligibility(ctx, tx, current, row, true); err != nil {
				return commandOutcome{}, err
			}
			row.proposal.State, row.approverRef, row.proposal.ApprovedAt = entity.WriteBackQueued, current.actorRef, &now
			deadline := now.Add(15 * time.Minute)
			row.deadline = &deadline
		} else {
			row.proposal.State, row.proposal.CompletedAt = entity.WriteBackRejected, &now
		}
	case command.CancelManagedConfigurationGitWriteBack:
		if writeBackTerminal(row.proposal.State) || row.proposal.State == entity.WriteBackUnknown {
			return commandOutcome{}, errs.ErrConflict
		}
		if row.started != nil {
			row.proposal.State, row.proposal.FailureCode = entity.WriteBackUnknown, "OUTCOME_UNCONFIRMED"
		} else {
			row.proposal.State, row.proposal.CompletedAt = entity.WriteBackCancelled, &now
		}
		row.lease.Fence, row.lease.Claimant, row.lease.ExpiresAt = "", "", time.Time{}
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	if err := saveWriteBack(ctx, tx, current, &row); err != nil {
		return commandOutcome{}, err
	}
	repository.writeBackActions(ctx, tx, current, &row)
	return writeBackOutcome(set, row.proposal), nil
}

func (repository *Repository) prepareConfigurationWriteBack(ctx context.Context, tx pgx.Tx, current scope, set managedSet, input command.Command, payload command.ConfigurationWriteBackInput) (commandOutcome, error) {
	if set.Version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if len(payload.Content) == 0 || len(payload.Content) > 262144 || !utf8.ValidString(payload.Content) || strings.ContainsRune(payload.Content, 0) {
		return commandOutcome{}, errs.ErrInvalid
	}
	source, work, connectionID, credentialID, err := repository.writeBackSource(ctx, tx, current, set)
	if err != nil {
		return commandOutcome{}, err
	}
	if source.Version != payload.ExpectedSourceVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var base *string
	if tx.QueryRow(ctx, queryWriteBackBase, current.organizationID, source.id, source.Version, source.AcceptedCommitSHA, source.AcceptedContentSHA256).Scan(&base) != nil || base == nil || writeBackDigest([]byte(*base)) != source.AcceptedContentSHA256 {
		return commandOutcome{}, errs.ErrConflict
	}
	if *base == payload.Content {
		return commandOutcome{}, errs.ErrConflict
	}
	if set.Kind == "ROLE_IMAGE" {
		if err := repository.validateSourceRoleImage(set, source.format, payload.Content); err != nil {
			return commandOutcome{}, err
		}
	} else if _, _, err := integrationpackage.NormalizeManagedRevision([]byte(payload.Content), "GIT", repository.integrationDefinitions); err != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	ref, err := newRef("mcwb")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	proposal := entity.ConfigurationWriteBack{Ref: ref, Version: 1, ConfigurationRef: set.Ref, Kind: set.Kind, ConfigurationVersion: set.Version,
		SourceRef: source.Ref, SourceVersion: source.Version, ConnectionRef: work.ConnectionRef, ConnectionVersion: work.ConnectionVersion,
		RepositoryRef: source.RepositoryRef, SourceRefName: source.RefName, Path: source.Path, BaseCommitSHA: source.AcceptedCommitSHA,
		BaseContentSHA256: source.AcceptedContentSHA256, ProposedContentSHA256: writeBackDigest([]byte(payload.Content)), ContentFormat: source.format,
		ProposalBranch: "kodex/writeback/" + ref, State: entity.WriteBackWaiting}
	snapshot := writeBackSnapshot{Proposal: proposal, Source: work, BaseContent: *base, ProposedContent: payload.Content}
	proposal.ApprovalDigest = writeBackDigest(asJSON(snapshot))
	snapshot.Proposal = proposal
	raw := asJSON(snapshot)
	var id string
	if err := tx.QueryRow(ctx, queryWriteBackInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID, "configuration_id": set.id,
		"source_id": source.id, "actor_id": current.actorID, "connection_id": connectionID, "credential_id": credentialID,
		"snapshot": raw, "digest": writeBackDigest(raw), "approval_digest": proposal.ApprovalDigest}).Scan(&id, &proposal.CreatedAt, &proposal.ExpiresAt); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	row := writeBackRow{proposal: proposal, snapshot: snapshot, rootRef: current.actorRef, organizationRef: current.organizationRef}
	repository.writeBackActions(ctx, tx, current, &row)
	return writeBackOutcome(set, row.proposal), nil
}

func writeBackOutcome(set managedSet, proposal entity.ConfigurationWriteBack) commandOutcome {
	return commandOutcome{result: command.Result{ConfigurationWriteBack: &proposal}, projectID: set.projectID, projectRef: set.ProjectRef,
		resourceKind: "MANAGED_CONFIGURATION_WRITEBACK", resourceRef: proposal.Ref, summary: "i18n:MANAGED_CONFIGURATION_CHANGED"}
}
