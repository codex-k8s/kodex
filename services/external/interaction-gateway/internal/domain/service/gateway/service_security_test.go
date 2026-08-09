package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	domainmattermost "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/mattermost"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
)

func TestStaleMappingStopsReclaimedInboundDeliveryAndArtifact(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	boundary := entity.Boundary{
		OrganizationID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		ProjectID:      "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		ActorID:        "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		TeamID:         "provider-team-one",
		ChannelID:      "provider-channel-one",
	}

	t.Run("reclaimed inbound", func(t *testing.T) {
		repository := &securityRepository{inbound: entity.InboundEvent{
			Kind: enum.InboundPost, Attempts: 1, OrganizationID: boundary.OrganizationID,
			ProjectID: boundary.ProjectID, TeamID: boundary.TeamID, ChannelID: boundary.ChannelID,
		}}
		guard := &rejectingMappingGuard{}
		service := securityService(repository, &securityMattermost{boundary: boundary}, guard, now)

		processed, err := service.ProcessWaiting(context.Background())
		if !processed || !errors.Is(err, domainerrs.ErrUnavailable) ||
			repository.inboundRetryCode != "MATTERMOST_MAPPING_NOT_CURRENT" || guard.calls != 1 {
			t.Fatalf("stale reclaimed inbound was not fenced: processed=%v err=%v code=%q calls=%d",
				processed, err, repository.inboundRetryCode, guard.calls)
		}
	})

	t.Run("reclaimed inbound with stale bot generation", func(t *testing.T) {
		repository := &securityRepository{inbound: entity.InboundEvent{
			Kind: enum.InboundPost, Attempts: 1, OrganizationID: boundary.OrganizationID,
			ProjectID: boundary.ProjectID, TeamID: boundary.TeamID, ChannelID: boundary.ChannelID,
			BotStableKey: "agent-primary", BotProviderUserID: "provider-user", BotProviderGeneration: 4,
		}}
		guard := &acceptingMappingGuard{}
		mattermost := &securityMattermost{boundary: boundary, validateErr: errors.New("stale generation")}
		service := securityServiceWithGuard(repository, mattermost, guard, now)

		processed, err := service.ProcessWaiting(context.Background())
		if !processed || !errors.Is(err, domainerrs.ErrUnavailable) ||
			repository.inboundRetryCode != "MATTERMOST_BOT_IDENTITY_NOT_CURRENT" ||
			mattermost.validateCalls != 1 || guard.calls != 1 {
			t.Fatalf("stale reclaimed bot generation was not fenced: processed=%v err=%v code=%q validation=%d mapping=%d",
				processed, err, repository.inboundRetryCode, mattermost.validateCalls, guard.calls)
		}
	})

	t.Run("direct delivery", func(t *testing.T) {
		repository := &securityRepository{delivery: entity.Delivery{
			State: enum.DeliveryPending, Attempts: 1, OrganizationID: boundary.OrganizationID,
			ProjectID: boundary.ProjectID, TeamID: boundary.TeamID, ChannelID: boundary.ChannelID,
		}}
		guard := &rejectingMappingGuard{}
		mattermost := &securityMattermost{boundary: boundary}
		service := securityService(repository, mattermost, guard, now)

		processed, err := service.ProcessDelivery(context.Background(), "test-worker")
		if !processed || err != nil || repository.deliveryRetryCode != "MATTERMOST_MAPPING_NOT_CURRENT" ||
			mattermost.publishCalls != 0 || guard.calls != 1 {
			t.Fatalf("stale delivery was not fenced: processed=%v err=%v code=%q publish=%d calls=%d",
				processed, err, repository.deliveryRetryCode, mattermost.publishCalls, guard.calls)
		}
	})

	t.Run("artifact download", func(t *testing.T) {
		repository := &securityRepository{grant: entity.DownloadGrant{
			ID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", OrganizationID: boundary.OrganizationID,
			ProjectID: boundary.ProjectID, TeamID: boundary.TeamID, ChannelID: boundary.ChannelID,
			ExpiresAt: now.Add(time.Minute), Artifact: entity.ArtifactBinding{ScanState: "CLEAN"},
		}}
		guard := &rejectingMappingGuard{}
		service := securityService(repository, &securityMattermost{boundary: boundary}, guard, now)

		_, _, err := service.DownloadArtifact(context.Background(), repository.grant.ID, "Bearer test")
		if !errors.Is(err, domainerrs.ErrNotFound) || guard.calls != 1 {
			t.Fatalf("stale artifact grant was not fenced: err=%v calls=%d", err, guard.calls)
		}
	})

	t.Run("readiness", func(t *testing.T) {
		guard := &rejectingMappingGuard{}
		service := securityService(&securityRepository{}, &securityMattermost{boundary: boundary}, guard, now)
		if err := service.CheckInteraction(context.Background()); err == nil || guard.calls != 1 {
			t.Fatalf("stale joined readiness was green: err=%v calls=%d", err, guard.calls)
		}
	})
}

func securityService(repository *securityRepository, mattermost *securityMattermost,
	guard *rejectingMappingGuard, now time.Time,
) *Service {
	return securityServiceWithGuard(repository, mattermost, guard, now)
}

func securityServiceWithGuard(repository *securityRepository, mattermost *securityMattermost,
	guard MappingGuard, now time.Time,
) *Service {
	return &Service{
		repository: repository, mattermost: mattermost, mapping: guard, observer: securityObserver{},
		config: Config{
			MaximumAttempts: 3, InboundLease: time.Second, DeliveryLease: time.Second, RetryBase: time.Second,
		},
		now: func() time.Time { return now },
	}
}

type securityRepository struct {
	domainrepo.Repository
	inbound           entity.InboundEvent
	delivery          entity.Delivery
	grant             entity.DownloadGrant
	inboundRetryCode  string
	deliveryRetryCode string
}

func (repository *securityRepository) ClaimWaitingInbound(context.Context, time.Duration) (entity.InboundEvent, bool, error) {
	return repository.inbound, true, nil
}

func (repository *securityRepository) RetryInbound(_ context.Context, _ entity.InboundEvent, code, _, _ string,
	_ time.Time, _ bool,
) error {
	repository.inboundRetryCode = code
	return nil
}

func (repository *securityRepository) ClaimDelivery(context.Context, string, string, time.Duration) (entity.Delivery, bool, error) {
	return repository.delivery, true, nil
}

func (repository *securityRepository) RetryDelivery(_ context.Context, _ entity.Delivery, code string,
	_ time.Time, _ bool,
) error {
	repository.deliveryRetryCode = code
	return nil
}

func (repository *securityRepository) GetDownloadGrant(context.Context, string) (entity.DownloadGrant, error) {
	return repository.grant, nil
}

type securityMattermost struct {
	domainmattermost.Client
	boundary      entity.Boundary
	publishCalls  int
	validateCalls int
	validateErr   error
}

func (client *securityMattermost) ResolveMappedChannel(context.Context, string, string) (entity.Boundary, error) {
	return client.boundary, nil
}

func (client *securityMattermost) ReadinessBoundary(context.Context) (entity.Boundary, error) {
	return client.boundary, nil
}

func (client *securityMattermost) AuthenticateArtifactDownload(context.Context, string, entity.DownloadGrant) error {
	return nil
}

func (client *securityMattermost) ValidateRuntimeBotIdentity(context.Context, string, string, string, string,
	uint64,
) error {
	client.validateCalls++
	return client.validateErr
}

func (client *securityMattermost) Publish(context.Context, entity.Delivery, []string) (domainmattermost.Published, error) {
	client.publishCalls++
	return domainmattermost.Published{}, nil
}

type rejectingMappingGuard struct{ calls int }

func (guard *rejectingMappingGuard) RequireBoundTeam(context.Context, entity.TeamPrincipal,
	string,
) (entity.WorkspaceMattermostMapping, error) {
	guard.calls++
	return entity.WorkspaceMattermostMapping{}, domainerrs.ErrUnauthorized
}

type acceptingMappingGuard struct{ calls int }

func (guard *acceptingMappingGuard) RequireBoundTeam(context.Context, entity.TeamPrincipal,
	string,
) (entity.WorkspaceMattermostMapping, error) {
	guard.calls++
	return entity.WorkspaceMattermostMapping{State: "BOUND"}, nil
}

type securityObserver struct{}

func (securityObserver) ObserveInbound(string, string)        {}
func (securityObserver) ObserveDelivery(string, string)       {}
func (securityObserver) ObserveExternalEffect(string, string) {}
