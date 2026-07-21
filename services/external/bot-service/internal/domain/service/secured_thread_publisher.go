package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type securedMattermostThreadPublisher struct {
	next     MattermostThreadPublisher
	security *InteractionSecurityService
}

var _ MattermostThreadPublisher = (*securedMattermostThreadPublisher)(nil)
var _ MattermostIdempotentThreadPublisher = (*securedMattermostThreadPublisher)(nil)

func NewSecuredMattermostThreadPublisher(next MattermostThreadPublisher, security *InteractionSecurityService) MattermostThreadPublisher {
	if next == nil {
		return nil
	}
	return &securedMattermostThreadPublisher{next: next, security: security}
}

func (publisher *securedMattermostThreadPublisher) PostThreadMessage(ctx context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	return publisher.next.PostThreadMessage(ctx, input)
}

func (publisher *securedMattermostThreadPublisher) PostThreadMessageWithToken(ctx context.Context, token string, input MattermostThreadPostInput) (MattermostPostRef, error) {
	return publisher.next.PostThreadMessageWithToken(ctx, token, input)
}

func (publisher *securedMattermostThreadPublisher) ReconcileOrPostThreadMessage(ctx context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	next, ok := publisher.next.(MattermostIdempotentThreadPublisher)
	if !ok {
		return MattermostPostRef{}, fmt.Errorf("idempotent Mattermost thread publication is not configured")
	}
	return next.ReconcileOrPostThreadMessage(ctx, input)
}

func (publisher *securedMattermostThreadPublisher) UpdateThreadMessage(ctx context.Context, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	return publisher.next.UpdateThreadMessage(ctx, input)
}

func (publisher *securedMattermostThreadPublisher) UpdateThreadMessageWithToken(ctx context.Context, token string, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	return publisher.next.UpdateThreadMessageWithToken(ctx, token, input)
}

func (publisher *securedMattermostThreadPublisher) PostThreadCard(ctx context.Context, card MattermostCard) (MattermostPostRef, error) {
	if len(card.Actions) == 0 {
		return publisher.postOrReconcileThreadCard(ctx, card)
	}
	if publisher.security == nil {
		return MattermostPostRef{}, fmt.Errorf("interaction security is not configured")
	}
	placeholder := card
	placeholder.Actions = nil
	ref, err := publisher.postOrReconcileThreadCard(ctx, placeholder)
	if err != nil {
		return MattermostPostRef{}, err
	}
	if strings.TrimSpace(ref.PostID) == "" || strings.TrimSpace(ref.ChannelID) == "" || ref.ChannelID != strings.TrimSpace(card.ChannelID) {
		return MattermostPostRef{}, fmt.Errorf("mattermost card publication did not return an exact post binding")
	}
	card.PostID = ref.PostID
	card.ChannelID = ref.ChannelID
	if err := publisher.security.SealCardPending(ctx, &card, card.Interaction.Actor, card.Interaction.Scope); err != nil {
		return MattermostPostRef{}, err
	}
	updated, err := publisher.next.UpdateThreadCard(ctx, card)
	if err != nil {
		updated, err = publisher.reconcileThreadCardUpdate(ctx, card, err)
		if err != nil {
			return MattermostPostRef{}, err
		}
	}
	if updated.PostID != ref.PostID || updated.ChannelID != ref.ChannelID {
		bindingErr := fmt.Errorf("mattermost card update changed the exact post binding")
		return MattermostPostRef{}, errors.Join(bindingErr, publisher.security.RevokeCard(ctx, card))
	}
	if err := publisher.security.ActivateCard(ctx, card); err != nil {
		return MattermostPostRef{}, err
	}
	return updated, nil
}

func (publisher *securedMattermostThreadPublisher) postOrReconcileThreadCard(ctx context.Context, card MattermostCard) (MattermostPostRef, error) {
	if deliveryID := strings.TrimSpace(contextStringValue(card.Props, agentStatusDeliveryIDProp)); deliveryID != "" {
		next, ok := publisher.next.(MattermostIdempotentCardPublisher)
		if !ok {
			return MattermostPostRef{}, fmt.Errorf("idempotent Mattermost card publisher is not configured")
		}
		return next.ReconcileOrPostThreadCard(ctx, card)
	}
	return publisher.next.PostThreadCard(ctx, card)
}

func (publisher *securedMattermostThreadPublisher) UpdateThreadCard(ctx context.Context, card MattermostCard) (MattermostPostRef, error) {
	if len(card.Actions) > 0 {
		if publisher.security == nil || strings.TrimSpace(card.PostID) == "" {
			return MattermostPostRef{}, fmt.Errorf("interaction security requires an exact Mattermost post binding")
		}
		if err := publisher.security.SealCardPending(ctx, &card, card.Interaction.Actor, card.Interaction.Scope); err != nil {
			return MattermostPostRef{}, err
		}
	}
	updated, err := publisher.next.UpdateThreadCard(ctx, card)
	if err != nil {
		if len(card.Actions) == 0 {
			return MattermostPostRef{}, err
		}
		updated, err = publisher.reconcileThreadCardUpdate(ctx, card, err)
		if err != nil {
			return MattermostPostRef{}, err
		}
	}
	if strings.TrimSpace(card.PostID) != "" && (updated.PostID != card.PostID || updated.ChannelID != strings.TrimSpace(card.ChannelID)) {
		bindingErr := fmt.Errorf("mattermost card update changed the exact post binding")
		if len(card.Actions) == 0 {
			return MattermostPostRef{}, bindingErr
		}
		return MattermostPostRef{}, errors.Join(bindingErr, publisher.security.RevokeCard(ctx, card))
	}
	if len(card.Actions) > 0 {
		if err := publisher.security.ActivateCard(ctx, card); err != nil {
			return MattermostPostRef{}, err
		}
	}
	return updated, nil
}

func (publisher *securedMattermostThreadPublisher) reconcileThreadCardUpdate(ctx context.Context, card MattermostCard, updateErr error) (MattermostPostRef, error) {
	reconciler, ok := publisher.next.(MattermostThreadCardUpdateReconciler)
	if !ok {
		return MattermostPostRef{}, updateErr
	}
	ref, applied, err := reconciler.ReconcileThreadCardUpdate(ctx, card)
	if err != nil {
		return MattermostPostRef{}, errors.Join(updateErr, err)
	}
	if applied {
		return ref, nil
	}
	return MattermostPostRef{}, errors.Join(updateErr, publisher.security.RevokeCard(ctx, card))
}

func (publisher *securedMattermostThreadPublisher) AddPostReactionWithToken(ctx context.Context, token string, input MattermostPostReactionInput) error {
	return publisher.next.AddPostReactionWithToken(ctx, token, input)
}
