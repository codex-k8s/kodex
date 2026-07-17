package service

import (
	"context"
	"fmt"
	"strings"
)

type securedMattermostThreadPublisher struct {
	next     MattermostThreadPublisher
	security *InteractionSecurityService
}

var _ MattermostThreadPublisher = (*securedMattermostThreadPublisher)(nil)

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

func (publisher *securedMattermostThreadPublisher) UpdateThreadMessage(ctx context.Context, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	return publisher.next.UpdateThreadMessage(ctx, input)
}

func (publisher *securedMattermostThreadPublisher) UpdateThreadMessageWithToken(ctx context.Context, token string, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	return publisher.next.UpdateThreadMessageWithToken(ctx, token, input)
}

func (publisher *securedMattermostThreadPublisher) PostThreadCard(ctx context.Context, card MattermostCard) (MattermostPostRef, error) {
	if len(card.Actions) == 0 {
		return publisher.next.PostThreadCard(ctx, card)
	}
	if publisher.security == nil {
		return MattermostPostRef{}, fmt.Errorf("interaction security is not configured")
	}
	placeholder := card
	placeholder.Actions = nil
	ref, err := publisher.next.PostThreadCard(ctx, placeholder)
	if err != nil {
		return MattermostPostRef{}, err
	}
	if strings.TrimSpace(ref.PostID) == "" || strings.TrimSpace(ref.ChannelID) == "" || ref.ChannelID != strings.TrimSpace(card.ChannelID) {
		return MattermostPostRef{}, fmt.Errorf("mattermost card publication did not return an exact post binding")
	}
	card.PostID = ref.PostID
	card.ChannelID = ref.ChannelID
	if err := publisher.security.SealCard(ctx, &card, card.Interaction.Actor, card.Interaction.Scope); err != nil {
		return MattermostPostRef{}, err
	}
	updated, err := publisher.next.UpdateThreadCard(ctx, card)
	if err != nil {
		return MattermostPostRef{}, err
	}
	if updated.PostID != ref.PostID || updated.ChannelID != ref.ChannelID {
		return MattermostPostRef{}, fmt.Errorf("mattermost card update changed the exact post binding")
	}
	return updated, nil
}

func (publisher *securedMattermostThreadPublisher) UpdateThreadCard(ctx context.Context, card MattermostCard) (MattermostPostRef, error) {
	if len(card.Actions) > 0 {
		if publisher.security == nil || strings.TrimSpace(card.PostID) == "" {
			return MattermostPostRef{}, fmt.Errorf("interaction security requires an exact Mattermost post binding")
		}
		if err := publisher.security.SealCard(ctx, &card, card.Interaction.Actor, card.Interaction.Scope); err != nil {
			return MattermostPostRef{}, err
		}
	}
	updated, err := publisher.next.UpdateThreadCard(ctx, card)
	if err != nil {
		return MattermostPostRef{}, err
	}
	if strings.TrimSpace(card.PostID) != "" && (updated.PostID != card.PostID || updated.ChannelID != strings.TrimSpace(card.ChannelID)) {
		return MattermostPostRef{}, fmt.Errorf("mattermost card update changed the exact post binding")
	}
	return updated, nil
}

func (publisher *securedMattermostThreadPublisher) AddPostReactionWithToken(ctx context.Context, token string, input MattermostPostReactionInput) error {
	return publisher.next.AddPostReactionWithToken(ctx, token, input)
}
