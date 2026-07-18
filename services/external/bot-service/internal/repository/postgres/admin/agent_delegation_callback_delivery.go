package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var _ adminrepo.AgentDelegationCallbackDeliveryRepository = (*Repository)(nil)

func (repo *Repository) CreateAgentDelegationCallbackDeliveries(ctx context.Context, inputs []adminrepo.CreateAgentDelegationCallbackDeliveryInput) ([]entity.AgentDelegationCallbackDelivery, error) {
	items := make([]entity.AgentDelegationCallbackDelivery, 0, len(inputs))
	for _, input := range inputs {
		item, err := scanAgentDelegationCallbackDelivery(repo.db.QueryRow(ctx, query("agent_delegation_callback_deliveries__insert.sql"),
			input.DelegationID, input.CallbackRunID, input.Destination, input.Publication,
			input.ChannelID, input.RootPostID, input.Message, input.PropsJSON,
			input.PayloadSHA256, input.ExternalID,
		))
		if errors.Is(err, pgx.ErrNoRows) {
			item, err = scanAgentDelegationCallbackDelivery(repo.db.QueryRow(ctx, query("agent_delegation_callback_deliveries__get_identity.sql"),
				input.DelegationID, input.CallbackRunID, input.Destination, input.Publication,
			))
		}
		if err != nil {
			return nil, fmt.Errorf("create agent delegation callback delivery: %w", err)
		}
		if !sameAgentDelegationCallbackDeliveryPlan(item, input) {
			return nil, fmt.Errorf("agent delegation callback delivery plan conflicts with immutable state")
		}
		items = append(items, item)
	}
	return items, nil
}

func (repo *Repository) ListAgentDelegationCallbackDeliveries(ctx context.Context, delegationID int64, callbackRunID string) ([]entity.AgentDelegationCallbackDelivery, error) {
	rows, err := repo.db.Query(ctx, query("agent_delegation_callback_deliveries__list.sql"), delegationID, callbackRunID)
	if err != nil {
		return nil, fmt.Errorf("list agent delegation callback deliveries: %w", err)
	}
	defer rows.Close()
	items := make([]entity.AgentDelegationCallbackDelivery, 0)
	for rows.Next() {
		item, scanErr := scanAgentDelegationCallbackDelivery(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan agent delegation callback delivery: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent delegation callback deliveries: %w", err)
	}
	return items, nil
}

func (repo *Repository) ClaimAgentDelegationCallbackDelivery(ctx context.Context, input adminrepo.ClaimAgentDelegationCallbackDeliveryInput) (entity.AgentDelegationCallbackDelivery, error) {
	if input.ExcludedIDs == nil {
		input.ExcludedIDs = []int64{}
	}
	item, err := scanAgentDelegationCallbackDelivery(repo.db.QueryRow(ctx, query("agent_delegation_callback_deliveries__claim.sql"),
		input.DelegationID, input.CallbackRunID, input.Now, input.LeaseOwner, input.LeaseUntil, input.ExcludedIDs,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentDelegationCallbackDelivery{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.AgentDelegationCallbackDelivery{}, fmt.Errorf("claim agent delegation callback delivery: %w", err)
	}
	return item, nil
}

func (repo *Repository) ReleaseAgentDelegationCallbackDelivery(ctx context.Context, input adminrepo.ReleaseAgentDelegationCallbackDeliveryInput) (entity.AgentDelegationCallbackDelivery, error) {
	if input.Status != "pending" && input.Status != "blocked" {
		return entity.AgentDelegationCallbackDelivery{}, fmt.Errorf("callback delivery release status is invalid")
	}
	item, err := scanAgentDelegationCallbackDelivery(repo.db.QueryRow(ctx, query("agent_delegation_callback_deliveries__release.sql"),
		input.ID, input.LeaseOwner, input.Status, input.LastErrorCode, input.Now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentDelegationCallbackDelivery{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.AgentDelegationCallbackDelivery{}, fmt.Errorf("release agent delegation callback delivery: %w", err)
	}
	return item, nil
}

func (repo *Repository) DeliverAgentDelegationCallbackDelivery(ctx context.Context, input adminrepo.DeliverAgentDelegationCallbackDeliveryInput) (entity.AgentDelegationCallbackDelivery, error) {
	item, err := scanAgentDelegationCallbackDelivery(repo.db.QueryRow(ctx, query("agent_delegation_callback_deliveries__deliver.sql"),
		input.ID, input.LeaseOwner, input.MattermostPostID, input.Now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentDelegationCallbackDelivery{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.AgentDelegationCallbackDelivery{}, fmt.Errorf("deliver agent delegation callback delivery: %w", err)
	}
	return item, nil
}

func sameAgentDelegationCallbackDeliveryPlan(item entity.AgentDelegationCallbackDelivery, input adminrepo.CreateAgentDelegationCallbackDeliveryInput) bool {
	return item.DelegationID == input.DelegationID &&
		item.CallbackRunID == input.CallbackRunID &&
		item.Destination == input.Destination &&
		item.Publication == input.Publication &&
		item.ChannelID == input.ChannelID &&
		item.RootPostID == input.RootPostID &&
		item.Message == input.Message &&
		sameAgentDelegationCallbackProps(item.PropsJSON, input.PropsJSON) &&
		bytes.Equal(item.PayloadSHA256, input.PayloadSHA256) &&
		item.ExternalID == input.ExternalID
}

func sameAgentDelegationCallbackProps(left []byte, right []byte) bool {
	var leftValue, rightValue map[string]any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func scanAgentDelegationCallbackDelivery(row pgx.Row) (entity.AgentDelegationCallbackDelivery, error) {
	var item entity.AgentDelegationCallbackDelivery
	var leaseExpiresAt *time.Time
	var lastAttemptAt *time.Time
	var deliveredAt *time.Time
	if err := row.Scan(
		&item.ID, &item.DelegationID, &item.CallbackRunID, &item.Destination,
		&item.Publication, &item.ChannelID, &item.RootPostID, &item.Message,
		&item.PropsJSON, &item.PayloadSHA256, &item.ExternalID, &item.Status,
		&item.AttemptCount, &item.LeaseOwner, &leaseExpiresAt, &lastAttemptAt,
		&item.LastErrorCode, &item.MattermostPostID, &deliveredAt,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return entity.AgentDelegationCallbackDelivery{}, err
	}
	if leaseExpiresAt != nil {
		item.LeaseExpiresAt = *leaseExpiresAt
	}
	if lastAttemptAt != nil {
		item.LastAttemptAt = *lastAttemptAt
	}
	if deliveredAt != nil {
		item.DeliveredAt = *deliveredAt
	}
	return item, nil
}
