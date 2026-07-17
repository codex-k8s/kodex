package service

import "context"

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
	_ = publisher.security.SealCard(ctx, &card, card.Interaction.Actor, card.Interaction.Scope)
	return publisher.next.PostThreadCard(ctx, card)
}

func (publisher *securedMattermostThreadPublisher) UpdateThreadCard(ctx context.Context, card MattermostCard) (MattermostPostRef, error) {
	_ = publisher.security.SealCard(ctx, &card, card.Interaction.Actor, card.Interaction.Scope)
	return publisher.next.UpdateThreadCard(ctx, card)
}

func (publisher *securedMattermostThreadPublisher) AddPostReactionWithToken(ctx context.Context, token string, input MattermostPostReactionInput) error {
	return publisher.next.AddPostReactionWithToken(ctx, token, input)
}
