package service

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
)

type publisherCapabilityRepository struct {
	mu           sync.Mutex
	capabilities map[string]securityrepo.Capability
	inputs       map[string]securityrepo.IssueCapabilityInput
	issueErr     error
}

func newPublisherCapabilityRepository() *publisherCapabilityRepository {
	return &publisherCapabilityRepository{capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}}
}

func (repository *publisherCapabilityRepository) IssueInteractionCapability(_ context.Context, input securityrepo.IssueCapabilityInput) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.issueErr != nil {
		return repository.issueErr
	}
	key := string(input.TokenHash)
	repository.inputs[key] = input
	repository.capabilities[key] = securityrepo.Capability{
		State: input.State,
		Kind:  input.Kind, Operation: input.Operation, ResourceType: input.ResourceType, ResourceID: input.ResourceID,
		ChannelID: input.ChannelID, PostBinding: input.PostBinding, ActorUserID: input.ActorUserID, ActorUserName: input.ActorUserName,
		InstallationScope: input.InstallationScope, WorkspaceScope: input.WorkspaceScope, SessionScope: input.SessionScope,
		IssuedAt: input.IssuedAt, ExpiresAt: input.ExpiresAt,
	}
	return nil
}

func (repository *publisherCapabilityRepository) CheckInteractionCapability(_ context.Context, input securityrepo.ConsumeCapabilityInput) (securityrepo.Capability, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.checkInteractionCapability(input)
}

func (repository *publisherCapabilityRepository) ConsumeInteractionCapability(_ context.Context, input securityrepo.ConsumeCapabilityInput) (securityrepo.Capability, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	capability, err := repository.checkInteractionCapability(input)
	if err != nil {
		return securityrepo.Capability{}, err
	}
	capability.State = securityrepo.CapabilityStateConsumed
	capability.ConsumedAt = input.Now
	repository.capabilities[string(input.TokenHash)] = capability
	return capability, nil
}

func (repository *publisherCapabilityRepository) checkInteractionCapability(input securityrepo.ConsumeCapabilityInput) (securityrepo.Capability, error) {
	key := string(input.TokenHash)
	capability, ok := repository.capabilities[key]
	if !ok {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityNotFound
	}
	issued := repository.inputs[key]
	if capability.State == securityrepo.CapabilityStateConsumed || !capability.ConsumedAt.IsZero() {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityConsumed
	}
	if capability.State != securityrepo.CapabilityStateUnused {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityInactive
	}
	if !capability.ExpiresAt.After(input.Now) {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityExpired
	}
	if capability.Kind != input.Kind || capability.Operation != input.Operation || capability.ResourceType != input.ResourceType || capability.ResourceID != input.ResourceID || capability.ChannelID != input.ChannelID || capability.PostBinding != input.PostBinding || capability.ActorUserID != input.ActorUserID || !bytes.Equal(issued.ContextHash, input.ContextHash) {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityBinding
	}
	return capability, nil
}

func (repository *publisherCapabilityRepository) TransitionInteractionCapabilities(_ context.Context, input securityrepo.TransitionCapabilitiesInput) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, tokenHash := range input.TokenHashes {
		capability, ok := repository.capabilities[string(tokenHash)]
		if !ok || capability.State != input.From {
			return securityrepo.ErrCapabilityInactive
		}
	}
	for _, tokenHash := range input.TokenHashes {
		key := string(tokenHash)
		capability := repository.capabilities[key]
		capability.State = input.To
		repository.capabilities[key] = capability
	}
	return nil
}

func (*publisherCapabilityRepository) AdmitExistingClusterAdmin(context.Context, securityrepo.ClusterAdminAdmissionInput) (bool, error) {
	return false, nil
}

type publisherBackend struct {
	postRef     MattermostPostRef
	postErr     error
	updateErr   error
	findRef     MattermostPostRef
	findFound   bool
	findErr     error
	finds       int
	posts       []MattermostCard
	updates     []MattermostCard
	updateStart chan struct{}
	updateGate  chan struct{}
}

func (backend *publisherBackend) FindExactThreadCard(_ context.Context, _ MattermostCard) (MattermostPostRef, bool, error) {
	backend.finds++
	return backend.findRef, backend.findFound, backend.findErr
}

func (*publisherBackend) PostThreadMessage(_ context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: "message-1"}, nil
}

func (backend *publisherBackend) PostThreadMessageWithToken(ctx context.Context, _ string, input MattermostThreadPostInput) (MattermostPostRef, error) {
	return backend.PostThreadMessage(ctx, input)
}

func (*publisherBackend) UpdateThreadMessage(_ context.Context, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: input.PostID}, nil
}

func (backend *publisherBackend) UpdateThreadMessageWithToken(ctx context.Context, _ string, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	return backend.UpdateThreadMessage(ctx, input)
}

func (backend *publisherBackend) PostThreadCard(_ context.Context, card MattermostCard) (MattermostPostRef, error) {
	backend.posts = append(backend.posts, card)
	if backend.postErr != nil {
		return MattermostPostRef{}, backend.postErr
	}
	if backend.postRef.PostID == "" && backend.postRef.ChannelID == "" {
		return MattermostPostRef{ChannelID: card.ChannelID, PostID: "post-1"}, nil
	}
	return backend.postRef, nil
}

func (backend *publisherBackend) UpdateThreadCard(_ context.Context, card MattermostCard) (MattermostPostRef, error) {
	backend.updates = append(backend.updates, card)
	if backend.updateStart != nil {
		close(backend.updateStart)
		<-backend.updateGate
	}
	if backend.updateErr != nil {
		return MattermostPostRef{}, backend.updateErr
	}
	return MattermostPostRef{ChannelID: card.ChannelID, PostID: card.PostID}, nil
}

func (*publisherBackend) AddPostReactionWithToken(context.Context, string, MattermostPostReactionInput) error {
	return nil
}

func securedPublisherTestCard() MattermostCard {
	return MattermostCard{
		ChannelID: "channel-1", ActionURL: "http://bot-service/mattermost/actions/agents",
		Interaction: MattermostCardInteraction{Actor: AuthenticatedActor{UserID: "actor-1", UserName: "trusted"}, Scope: InteractionScope{}},
		Actions:     []MattermostCardAction{{ID: "main", Context: map[string]any{"kind": "agents_menu", "view": "main"}}},
	}
}

func TestSecuredThreadPublisherBindsActualPostAndSurvivesRestart(t *testing.T) {
	repository := newPublisherCapabilityRepository()
	security := NewInteractionSecurityService(InteractionSecurityConfig{Repository: repository, Admission: fixedServiceAdmission(AdmissionAllowed)})
	backend := &publisherBackend{}
	publisher := NewSecuredMattermostThreadPublisher(backend, security)
	ref, err := publisher.PostThreadCard(context.Background(), securedPublisherTestCard())
	if err != nil {
		t.Fatalf("PostThreadCard() error = %v", err)
	}
	if ref.PostID != "post-1" || len(backend.posts) != 1 || len(backend.posts[0].Actions) != 0 || len(backend.updates) != 1 {
		t.Fatalf("two-phase publication: ref=%#v posts=%#v updates=%#v", ref, backend.posts, backend.updates)
	}
	bound := backend.updates[0]
	if bound.PostID != "post-1" || bound.ChannelID != "channel-1" || bound.Actions[0].Context[interactionCapabilityContextKey] == nil {
		t.Fatalf("bound card = %#v", bound)
	}

	restarted := NewInteractionSecurityService(InteractionSecurityConfig{Repository: repository, Admission: fixedServiceAdmission(AdmissionAllowed)})
	callback := ActionCallback{Context: bound.Actions[0].Context, UserID: "actor-1", ChannelID: "channel-1", PostID: "post-1"}
	if _, err := restarted.AuthenticateAction(context.Background(), callback); err != nil {
		t.Fatalf("AuthenticateAction() after restart = %v", err)
	}
	if _, err := restarted.AuthenticateAction(context.Background(), callback); !errors.Is(err, ErrInteractionAuthentication) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestSecuredThreadPublisherReconcilesExactApprovalPlaceholder(t *testing.T) {
	repository := newPublisherCapabilityRepository()
	security := NewInteractionSecurityService(InteractionSecurityConfig{Repository: repository, Admission: fixedServiceAdmission(AdmissionAllowed)})
	backend := &publisherBackend{
		findRef: MattermostPostRef{ChannelID: "channel-1", PostID: "approval-placeholder"}, findFound: true,
	}
	publisher := NewSecuredMattermostThreadPublisher(backend, security).(MattermostIdempotentCardPublisher)
	card := securedPublisherTestCard()
	card.Props = map[string]any{"matter_codex_delivery_id": "apr_0123456789abcdef0123456789abcdef"}
	ref, err := publisher.ReconcileOrPostThreadCard(context.Background(), card)
	if err != nil {
		t.Fatalf("ReconcileOrPostThreadCard() error=%v", err)
	}
	if ref.PostID != "approval-placeholder" || backend.finds != 1 || len(backend.posts) != 0 || len(backend.updates) != 1 {
		t.Fatalf("approval reconciliation ref=%+v finds=%d posts=%d updates=%d", ref, backend.finds, len(backend.posts), len(backend.updates))
	}
	if backend.updates[0].Actions[0].Context[interactionCapabilityContextKey] == nil {
		t.Fatal("reconciled approval card was not sealed before activation")
	}
}

func TestSecuredThreadPublisherRequiresAuthoritativeHumanBeforeApprovalActivation(t *testing.T) {
	tests := []struct {
		name        string
		allowed     bool
		verifierErr error
		wantErr     bool
	}{
		{name: "external bot is denied", allowed: false, wantErr: true},
		{name: "Mattermost verification unavailable", verifierErr: errors.New("synthetic Mattermost failure"), wantErr: true},
		{name: "verified human is allowed", allowed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newPublisherCapabilityRepository()
			verifier := &admissionActorVerifier{allowed: test.allowed, err: test.verifierErr}
			security := NewInteractionSecurityService(InteractionSecurityConfig{
				Repository: repository, Admission: fixedServiceAdmission(AdmissionAllowed), ActorVerifier: verifier,
			})
			backend := &publisherBackend{}
			publisher := NewSecuredMattermostThreadPublisher(backend, security)
			card := securedPublisherTestCard()
			card.Actions[0].Context = map[string]any{
				"kind": "integration_approval", "action": "approve",
				"resource_type": "integration_approval", "resource_id": "apr_0123456789abcdef0123456789abcdef",
			}
			_, err := publisher.PostThreadCard(context.Background(), card)
			if (err != nil) != test.wantErr {
				t.Fatalf("PostThreadCard() error=%v, wantErr=%v", err, test.wantErr)
			}
			if verifier.calls != 1 || verifier.userID != "actor-1" || verifier.channel != "channel-1" {
				t.Fatalf("authoritative proof calls=%d subject=%q channel=%q", verifier.calls, verifier.userID, verifier.channel)
			}
			if len(backend.updates) != 1 || len(backend.updates[0].Actions) != 1 {
				t.Fatalf("bound approval card updates=%#v", backend.updates)
			}
			callback := ActionCallback{
				Context: backend.updates[0].Actions[0].Context, UserID: "actor-1", ChannelID: "channel-1", PostID: "post-1",
			}
			_, authErr := security.AuthenticateAction(context.Background(), callback)
			if test.wantErr && !errors.Is(authErr, ErrInteractionAuthentication) {
				t.Fatalf("denied activation left a usable capability: %v", authErr)
			}
			if !test.wantErr && authErr != nil {
				t.Fatalf("verified human callback error=%v", authErr)
			}
		})
	}
}

func TestSecuredThreadPublisherFailsClosedOnBindingSealAndUpdateErrors(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*publisherCapabilityRepository, *publisherBackend)
	}{
		{name: "missing post binding", configure: func(_ *publisherCapabilityRepository, backend *publisherBackend) {
			backend.postRef = MattermostPostRef{ChannelID: "channel-1"}
		}},
		{name: "wrong channel binding", configure: func(_ *publisherCapabilityRepository, backend *publisherBackend) {
			backend.postRef = MattermostPostRef{ChannelID: "channel-2", PostID: "post-1"}
		}},
		{name: "repository failure", configure: func(repository *publisherCapabilityRepository, _ *publisherBackend) {
			repository.issueErr = errors.New("synthetic issue failure")
		}},
		{name: "update failure", configure: func(_ *publisherCapabilityRepository, backend *publisherBackend) {
			backend.updateErr = errors.New("synthetic update failure")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newPublisherCapabilityRepository()
			backend := &publisherBackend{}
			test.configure(repository, backend)
			security := NewInteractionSecurityService(InteractionSecurityConfig{Repository: repository, Admission: fixedServiceAdmission(AdmissionAllowed)})
			publisher := NewSecuredMattermostThreadPublisher(backend, security)
			if _, err := publisher.PostThreadCard(context.Background(), securedPublisherTestCard()); err == nil {
				t.Fatal("PostThreadCard() error = nil")
			}
			if len(backend.posts) != 1 || len(backend.posts[0].Actions) != 0 {
				t.Fatalf("failed publication exposed actions: %#v", backend.posts)
			}
			if test.name != "update failure" && len(backend.updates) != 0 {
				t.Fatalf("failed binding reached update: %#v", backend.updates)
			}
			if test.name == "update failure" {
				if len(backend.updates) != 1 || len(backend.updates[0].Actions) != 1 {
					t.Fatalf("ambiguous update did not expose the applied card fixture: %#v", backend.updates)
				}
				callback := ActionCallback{Context: backend.updates[0].Actions[0].Context, UserID: "actor-1", ChannelID: "channel-1", PostID: "post-1"}
				if _, authErr := security.AuthenticateAction(context.Background(), callback); !errors.Is(authErr, ErrInteractionAuthentication) {
					t.Fatalf("actions from applied-then-error update remained usable: %v", authErr)
				}
			}
		})
	}
}

func TestSecuredThreadPublisherLegitimateRetryAfterSecurityFailure(t *testing.T) {
	repository := newPublisherCapabilityRepository()
	repository.issueErr = errors.New("synthetic issue failure")
	backend := &publisherBackend{}
	security := NewInteractionSecurityService(InteractionSecurityConfig{Repository: repository, Admission: fixedServiceAdmission(AdmissionAllowed)})
	publisher := NewSecuredMattermostThreadPublisher(backend, security)
	if _, err := publisher.PostThreadCard(context.Background(), securedPublisherTestCard()); err == nil {
		t.Fatal("first PostThreadCard() error = nil")
	}
	repository.issueErr = nil
	if _, err := publisher.PostThreadCard(context.Background(), securedPublisherTestCard()); err != nil {
		t.Fatalf("retry PostThreadCard() error = %v", err)
	}
	if len(backend.updates) != 1 || backend.updates[0].PostID != "post-1" {
		t.Fatalf("retry did not publish bound card: %#v", backend.updates)
	}
}

func TestSecuredThreadPublisherUpdatePropagatesSecurityFailureAndAllowsRetry(t *testing.T) {
	repository := newPublisherCapabilityRepository()
	repository.issueErr = errors.New("synthetic update issue failure")
	backend := &publisherBackend{}
	security := NewInteractionSecurityService(InteractionSecurityConfig{Repository: repository, Admission: fixedServiceAdmission(AdmissionAllowed)})
	publisher := NewSecuredMattermostThreadPublisher(backend, security)
	card := securedPublisherTestCard()
	card.PostID = "post-1"
	if _, err := publisher.UpdateThreadCard(context.Background(), card); err == nil {
		t.Fatal("first UpdateThreadCard() error = nil")
	}
	if len(backend.updates) != 0 {
		t.Fatalf("failed security update reached Mattermost: %#v", backend.updates)
	}
	repository.issueErr = nil
	if _, err := publisher.UpdateThreadCard(context.Background(), card); err != nil {
		t.Fatalf("retry UpdateThreadCard() error = %v", err)
	}
	if len(backend.updates) != 1 || backend.updates[0].Actions[0].Context[interactionCapabilityContextKey] == nil {
		t.Fatalf("retry update is not sealed: %#v", backend.updates)
	}
}

func TestSecuredThreadPublisherRaceKeepsPlaceholderActionless(t *testing.T) {
	repository := newPublisherCapabilityRepository()
	security := NewInteractionSecurityService(InteractionSecurityConfig{Repository: repository, Admission: fixedServiceAdmission(AdmissionAllowed)})
	backend := &publisherBackend{updateStart: make(chan struct{}), updateGate: make(chan struct{})}
	publisher := NewSecuredMattermostThreadPublisher(backend, security)
	result := make(chan error, 1)
	go func() {
		_, err := publisher.PostThreadCard(context.Background(), securedPublisherTestCard())
		result <- err
	}()
	select {
	case <-backend.updateStart:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for update barrier")
	}
	if len(backend.posts) != 1 || len(backend.posts[0].Actions) != 0 {
		t.Fatalf("publication race exposed actions: %#v", backend.posts)
	}
	close(backend.updateGate)
	if err := <-result; err != nil {
		t.Fatalf("PostThreadCard() error = %v", err)
	}
}

type fixedServiceAdmission AdmissionStatus

func (status fixedServiceAdmission) Admit(context.Context, InteractionAdmissionRequest) InteractionAdmissionDecision {
	return InteractionAdmissionDecision{Status: AdmissionStatus(status), Reason: "test"}
}

type failingCapabilityRandom struct{}

func (failingCapabilityRandom) Read([]byte) (int, error) {
	return 0, errors.New("synthetic random generator failure")
}

func TestSecuredThreadPublisherPropagatesGeneratorFailure(t *testing.T) {
	repository := newPublisherCapabilityRepository()
	security := NewInteractionSecurityService(InteractionSecurityConfig{
		Repository: repository, Admission: fixedServiceAdmission(AdmissionAllowed), Random: failingCapabilityRandom{},
	})
	backend := &publisherBackend{}
	publisher := NewSecuredMattermostThreadPublisher(backend, security)
	if _, err := publisher.PostThreadCard(context.Background(), securedPublisherTestCard()); err == nil {
		t.Fatal("PostThreadCard() error = nil")
	}
	if len(backend.posts) != 1 || len(backend.posts[0].Actions) != 0 || len(backend.updates) != 0 {
		t.Fatalf("generator failure exposed an interactive flow: posts=%#v updates=%#v", backend.posts, backend.updates)
	}
}
