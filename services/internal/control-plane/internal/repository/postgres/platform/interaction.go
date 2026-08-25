package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

const maximumInteractionDeliveryAttempts = 10

func projectInteractionIncident(incident entity.Incident, deliveryState string, attempt int) entity.Incident {
	incident.Category = "OPTIONAL_INTERACTION_DELIVERY"
	incident.CoreAffected = false
	switch {
	case deliveryState == "SUCCEEDED":
		incident.Severity = "INFO"
		incident.State = "RESOLVED"
		incident.SafeSummary = "i18n:INTERACTION_DELIVERY_RECOVERED"
		incident.SafeNextStep = "i18n:INTERACTION_DELIVERY_RECOVERY_COMPLETE"
	case attempt >= maximumInteractionDeliveryAttempts:
		incident.Severity = "ERROR"
		incident.State = "OPEN"
		incident.SafeSummary = "i18n:INTERACTION_DELIVERY_FAILED"
		incident.SafeNextStep = "i18n:INTERACTION_DELIVERY_RETRY_EXHAUSTED"
	default:
		incident.Severity = "WARNING"
		incident.State = "RECOVERING"
		incident.SafeSummary = "i18n:INTERACTION_DELIVERY_FAILED"
		incident.SafeNextStep = "i18n:INTERACTION_DELIVERY_RETRYING"
	}
	return incident
}

func (repository *Repository) ListInteractionSources(ctx context.Context, principal value.Principal) ([]map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	rows, err := repository.pool.Query(ctx, queryInteractionListSources, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var connectionRef, credentialRef, baseURL, teamName, channelName, locale string
		var capabilities []string
		if err := rows.Scan(&connectionRef, &credentialRef, &baseURL, &teamName, &channelName, &locale, &capabilities); err != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, map[string]any{
			"connectionRef": connectionRef, "credentialRef": credentialRef,
			"baseURL": baseURL, "teamName": teamName, "channelName": channelName,
			"locale": locale, "capabilities": capabilities,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) ClaimInteractionDeliveries(ctx context.Context, principal value.Principal, workloadInstance string, limit int32) ([]map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	rows, err := repository.pool.Query(ctx, queryInteractionClaimDeliveries, pgx.StrictNamedArgs{
		"organization_id":   scope.organizationID,
		"workload_instance": workloadInstance,
		"claim_limit":       limit,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var deliveryRef, connectionRef, credentialRef, baseURL, teamName, channelName, locale string
		var capabilityKey, messageKey, leaseRef, fence string
		var templateRaw []byte
		var generation int64
		var expiresAt any
		if err := rows.Scan(
			&deliveryRef, &connectionRef, &credentialRef, &baseURL, &teamName, &channelName, &locale,
			&capabilityKey, &messageKey, &templateRaw, &leaseRef, &fence, &generation, &expiresAt,
		); err != nil {
			return nil, errs.ErrUnavailable
		}
		templateData := map[string]any{}
		if json.Unmarshal(templateRaw, &templateData) != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, map[string]any{
			"deliveryRef": deliveryRef, "connectionRef": connectionRef, "credentialRef": credentialRef,
			"baseURL": baseURL, "teamName": teamName, "channelName": channelName, "locale": locale,
			"capabilityKey": capabilityKey, "messageKey": messageKey, "templateData": templateData,
			"leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expiresAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) completeInteractionDelivery(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.InteractionDeliveryInput)
	if !ok || payload.DeliveryRef == "" || payload.LeaseRef == "" || payload.Fence == "" || payload.Generation < 1 {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.Success {
		if payload.ExternalPostRef == "" || len(payload.ExternalPostRef) > 128 || len(payload.ExternalThreadRef) > 128 {
			return commandOutcome{}, errs.ErrInvalid
		}
	} else if !validInteractionErrorCode(payload.SafeErrorCode) {
		return commandOutcome{}, errs.ErrInvalid
	}
	var deliveryID, projectID, projectRef, rootRunID, runRef, gateID, capabilityKey string
	var attempt int
	var createdAt time.Time
	err := tx.QueryRow(ctx, queryInteractionCompleteDeliveryResolve, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"delivery_ref":    payload.DeliveryRef,
		"lease_ref":       payload.LeaseRef,
		"fence":           payload.Fence,
		"generation":      payload.Generation,
	}).Scan(&deliveryID, &projectID, &projectRef, &rootRunID, &runRef, &gateID, &capabilityKey, &attempt, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var deliveryRef, state string
	err = tx.QueryRow(ctx, queryInteractionCompleteDeliveryUpdate, pgx.StrictNamedArgs{
		"delivery_id":         deliveryID,
		"success":             payload.Success,
		"external_post_ref":   payload.ExternalPostRef,
		"external_thread_ref": payload.ExternalThreadRef,
		"safe_error_code":     payload.SafeErrorCode,
		"attempt":             attempt,
	}).Scan(&deliveryRef, &state)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	messageKey := "i18n:INTERACTION_DELIVERY_FAILED"
	if payload.Success {
		messageKey = "i18n:INTERACTION_DELIVERY_SUCCEEDED"
	}
	var event *entity.RunEvent
	if !payload.Success || attempt > 1 {
		incident := projectInteractionIncident(entity.Incident{
			Ref: deliveryRef, ProjectRef: projectRef, RunRef: runRef, CreatedAt: createdAt,
		}, state, attempt)
		emitted, emitErr := repository.emitRunEventWithIncident(
			ctx, tx, scope, projectID, rootRunID, deliveryRef, "INCIDENT_LINKED",
			"", "", "", "", &incident, incident.SafeSummary, "", "",
		)
		if emitErr != nil {
			return commandOutcome{}, emitErr
		}
		event = &emitted
	}
	return commandOutcome{
		result: command.Result{Runtime: map[string]any{
			"deliveryRef": deliveryRef, "state": state, "coreRunAffected": false,
		}, Event: event},
		projectID: projectID, projectRef: projectRef, resourceKind: "INTERACTION_DELIVERY",
		resourceRef: payload.DeliveryRef, summary: messageKey,
	}, nil
}

func (repository *Repository) acceptInteractionMessage(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.InteractionMessageInput)
	if !ok || payload.ConnectionRef == "" || payload.ExternalEventRef == "" || payload.ExternalPostRef == "" ||
		payload.ExternalChannelRef == "" || !lowerHexDigest(payload.ExternalUserDigest) ||
		len(payload.ExternalEventRef) > 256 || len(payload.ExternalPostRef) > 128 || len(payload.ExternalRootPostRef) > 128 ||
		len(payload.ExternalChannelRef) > 128 || len(payload.Message) > 16<<10 {
		return commandOutcome{}, errs.ErrInvalid
	}
	var connectionID string
	if err := tx.QueryRow(ctx, queryInteractionResolveConnection, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"connection_ref":  payload.ConnectionRef,
	}).Scan(&connectionID); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	eventDigest := interactionDigest(payload.ConnectionRef, payload.ExternalEventRef)
	var previousOutcome, previousRunRef, previousGateRef string
	err := tx.QueryRow(ctx, queryInteractionFindMessageReceipt, pgx.StrictNamedArgs{
		"organization_id":       scope.organizationID,
		"connection_id":         connectionID,
		"external_event_digest": eventDigest,
	}).Scan(&previousOutcome, &previousRunRef, &previousGateRef)
	if err == nil {
		acceptedRef := previousRunRef
		if acceptedRef == "" {
			acceptedRef = previousGateRef
		}
		return interactionMessageOutcome(payload.ConnectionRef, "DUPLICATE", "i18n:MATTERMOST_EVENT_ALREADY_PROCESSED", acceptedRef, "", ""), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrUnavailable
	}
	gateOutcome, gateMatched, err := repository.acceptInteractionGateDecision(ctx, tx, scope, input, connectionID, eventDigest)
	if err != nil || gateMatched {
		return gateOutcome, err
	}
	return repository.acceptInteractionInbound(ctx, tx, scope, input, connectionID, eventDigest)
}

func (repository *Repository) acceptInteractionGateDecision(ctx context.Context, tx pgx.Tx, scope scope, input command.Command, connectionID, eventDigest string) (commandOutcome, bool, error) {
	payload := input.Payload.(command.InteractionMessageInput)
	var deliveryID, grantID, gateID, gateRef, gateState, projectID, projectRef, rootRunID, runRef string
	var gateVersion int64
	var allowed []string
	err := tx.QueryRow(ctx, queryInteractionFindGateDelivery, pgx.StrictNamedArgs{
		"organization_id":        scope.organizationID,
		"connection_ref":         payload.ConnectionRef,
		"external_root_post_ref": payload.ExternalRootPostRef,
		"external_post_ref":      payload.ExternalPostRef,
	}).Scan(&deliveryID, &grantID, &gateID, &gateRef, &gateVersion, &gateState, &allowed, &projectID, &projectRef, &rootRunID, &runRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, false, nil
	}
	if err != nil {
		return commandOutcome{}, true, errs.ErrUnavailable
	}
	decision := payload.Decision
	if decision == "" || !contains([]string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}, decision) || !contains(allowed, decision) {
		if err := repository.insertInteractionReceipt(ctx, tx, scope, interactionReceipt{
			connectionID: connectionID, grantID: grantID, gateID: gateID, rootRunRef: runRef,
			projectID: projectID, eventDigest: eventDigest, userDigest: payload.ExternalUserDigest,
			outcome: "IGNORED",
		}); err != nil {
			return commandOutcome{}, true, err
		}
		return interactionMessageOutcome(gateRef, "IGNORED", "i18n:MATTERMOST_GATE_COMMAND_HELP", gateRef, projectID, projectRef), true, nil
	}
	if gateState != "OPEN" {
		if err := repository.insertInteractionReceipt(ctx, tx, scope, interactionReceipt{
			connectionID: connectionID, grantID: grantID, gateID: gateID, rootRunRef: runRef,
			projectID: projectID, eventDigest: eventDigest, userDigest: payload.ExternalUserDigest,
			outcome: "STALE", decision: decision,
		}); err != nil {
			return commandOutcome{}, true, err
		}
		return interactionMessageOutcome(gateRef, "STALE", "i18n:MATTERMOST_GATE_STALE", gateRef, projectID, projectRef), true, nil
	}
	nested := input
	nested.Kind = command.ResolveOwnerGate
	nested.Mutation.ExpectedVersion = &gateVersion
	nested.Payload = command.GateResolutionInput{GateRef: gateRef, Decision: decision, Comment: truncate(strings.TrimSpace(payload.Message), 2000)}
	outcome, err := repository.resolveGate(ctx, tx, scope, nested)
	if errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrVersionMismatch) {
		if receiptErr := repository.insertInteractionReceipt(ctx, tx, scope, interactionReceipt{
			connectionID: connectionID, grantID: grantID, gateID: gateID, rootRunRef: runRef,
			projectID: projectID, eventDigest: eventDigest, userDigest: payload.ExternalUserDigest,
			outcome: "STALE", decision: decision,
		}); receiptErr != nil {
			return commandOutcome{}, true, receiptErr
		}
		return interactionMessageOutcome(gateRef, "STALE", "i18n:MATTERMOST_GATE_STALE", gateRef, projectID, projectRef), true, nil
	}
	if err != nil {
		return commandOutcome{}, true, err
	}
	if err := repository.insertInteractionReceipt(ctx, tx, scope, interactionReceipt{
		connectionID: connectionID, grantID: grantID, gateID: gateID, rootRunRef: runRef,
		projectID: projectID, eventDigest: eventDigest, userDigest: payload.ExternalUserDigest,
		outcome: "GATE_RESOLVED", decision: decision,
	}); err != nil {
		return commandOutcome{}, true, err
	}
	outcome.result.Runtime = map[string]any{"outcome": "GATE_RESOLVED", "messageKey": "i18n:MATTERMOST_GATE_RESOLVED", "acceptedResourceRef": gateRef}
	outcome.resourceKind = "INTERACTION_DECISION"
	outcome.summary = "i18n:MATTERMOST_GATE_RESOLVED"
	return outcome, true, nil
}

func (repository *Repository) acceptInteractionInbound(ctx context.Context, tx pgx.Tx, scope scope, input command.Command, connectionID, eventDigest string) (commandOutcome, error) {
	payload := input.Payload.(command.InteractionMessageInput)
	rows, err := tx.Query(ctx, queryInteractionListInboundGrants, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"connection_ref":  payload.ConnectionRef,
	})
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	type inboundRoute struct{ grantID, grantRef, targetKind, targetRef, projectID, projectRef string }
	routes := []inboundRoute{}
	for rows.Next() {
		var item inboundRoute
		if err := rows.Scan(&item.grantID, &item.grantRef, &item.targetKind, &item.targetRef, &item.projectID, &item.projectRef); err != nil {
			rows.Close()
			return commandOutcome{}, errs.ErrUnavailable
		}
		routes = append(routes, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return commandOutcome{}, errs.ErrUnavailable
	}
	rows.Close()
	if len(routes) != 1 || strings.TrimSpace(payload.Message) == "" {
		if err := repository.insertInteractionReceipt(ctx, tx, scope, interactionReceipt{
			connectionID: connectionID, eventDigest: eventDigest, userDigest: payload.ExternalUserDigest, outcome: "IGNORED",
		}); err != nil {
			return commandOutcome{}, err
		}
		return interactionMessageOutcome(payload.ConnectionRef, "IGNORED", "i18n:MATTERMOST_INBOUND_ROUTE_UNAVAILABLE", payload.ConnectionRef, "", ""), nil
	}
	selected := routes[0]
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = command.LaunchRunInput{
		ProjectRef: selected.projectRef,
		Title:      "i18n:MATTERMOST_INBOUND_RUN",
		Task:       strings.TrimSpace(payload.Message),
		Source:     "MATTERMOST",
		Target:     entity.RunTarget{Type: selected.targetKind, Ref: selected.targetRef},
	}
	outcome, err := repository.launchRun(ctx, tx, scope, nested)
	if err != nil {
		return commandOutcome{}, err
	}
	if outcome.result.Run == nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err := repository.insertInteractionReceipt(ctx, tx, scope, interactionReceipt{
		connectionID: connectionID, grantID: selected.grantID, rootRunRef: outcome.result.Run.Ref,
		projectID: selected.projectID, eventDigest: eventDigest, userDigest: payload.ExternalUserDigest,
		outcome: "RUN_STARTED",
	}); err != nil {
		return commandOutcome{}, err
	}
	outcome.result.Runtime = map[string]any{"outcome": "RUN_STARTED", "messageKey": "i18n:MATTERMOST_RUN_ACCEPTED", "acceptedResourceRef": outcome.result.Run.Ref}
	outcome.resourceKind = "INTERACTION_MESSAGE"
	outcome.summary = "i18n:MATTERMOST_RUN_ACCEPTED"
	return outcome, nil
}

type interactionReceipt struct {
	connectionID, grantID, gateID, rootRunRef, projectID string
	eventDigest, userDigest, outcome, decision           string
}

func (repository *Repository) insertInteractionReceipt(ctx context.Context, tx pgx.Tx, scope scope, receipt interactionReceipt) error {
	ref, err := newRef("irc")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryInteractionInsertMessageReceipt, pgx.StrictNamedArgs{
		"receipt_ref":           ref,
		"organization_id":       scope.organizationID,
		"project_id":            receipt.projectID,
		"connection_id":         receipt.connectionID,
		"grant_id":              receipt.grantID,
		"root_run_ref":          receipt.rootRunRef,
		"gate_id":               receipt.gateID,
		"external_event_digest": receipt.eventDigest,
		"external_user_digest":  receipt.userDigest,
		"outcome":               receipt.outcome,
		"decision":              receipt.decision,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func interactionMessageOutcome(resourceRef, outcome, messageKey, acceptedRef, projectID, projectRef string) commandOutcome {
	return commandOutcome{
		result: command.Result{Runtime: map[string]any{
			"outcome": outcome, "messageKey": messageKey, "acceptedResourceRef": acceptedRef,
		}},
		projectID: projectID, projectRef: projectRef, resourceKind: "INTERACTION_MESSAGE",
		resourceRef: resourceRef, summary: messageKey,
	}
}

func (repository *Repository) enqueueGateInteractionDeliveries(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, gateID string) error {
	if _, err := tx.Exec(ctx, queryInteractionEnqueueGateDeliveries, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"project_id":      projectID,
		"root_run_id":     rootRunID,
		"gate_id":         gateID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) enqueueTerminalInteractionDeliveries(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID string) error {
	if _, err := tx.Exec(ctx, queryInteractionEnqueueTerminalDeliveries, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"project_id":      projectID,
		"root_run_id":     rootRunID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func interactionDigest(connectionRef, externalEventRef string) string {
	digest := sha256.Sum256([]byte(connectionRef + "\x00" + externalEventRef))
	return hex.EncodeToString(digest[:])
}

func lowerHexDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func validInteractionErrorCode(value string) bool {
	switch value {
	case "INTERACTION_CONFIGURATION_INVALID", "INTERACTION_CREDENTIAL_UNAVAILABLE", "INTERACTION_FORBIDDEN",
		"INTERACTION_RATE_LIMITED", "INTERACTION_UNAVAILABLE", "INTERACTION_RESPONSE_INVALID",
		"INTERACTION_LEASE_EXPIRED":
		return true
	default:
		return false
	}
}
