package mattermost

import (
	"errors"
	"testing"

	domainartifact "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/artifact"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

func TestVerifyArtifactDeliveryPostRequiresExactFileAndProps(t *testing.T) {
	t.Parallel()
	request := domainartifact.DeliveryRequest{
		DeliveryID: "delivery-1", VersionID: "version-1", ChannelID: "channel-1", RootPostID: "root-1",
	}
	post := &mattermostmodel.Post{
		Id: "post-1", ChannelId: "channel-1", RootId: "root-1", Message: "#notrigger",
		FileIds: []string{"file-1"}, Props: artifactDeliveryProps(request),
	}
	if err := verifyArtifactDeliveryPost(post, request, "file-1"); err != nil {
		t.Fatalf("exact delivery post error = %v", err)
	}
	if err := verifyArtifactDeliveryPost(post, request, "file-other"); !errors.Is(err, domainartifact.ErrDeliveryAmbiguous) {
		t.Fatalf("foreign file error = %v", err)
	}
	post.Props["matter_codex_unexpected"] = true
	if err := verifyArtifactDeliveryPost(post, request, "file-1"); !errors.Is(err, domainartifact.ErrDeliveryAmbiguous) {
		t.Fatalf("unexpected client prop error = %v", err)
	}
}
