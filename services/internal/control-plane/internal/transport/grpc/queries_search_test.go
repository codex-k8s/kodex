package grpc

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestSearchPageUsesRequestedLimitAndPreservesCursor(t *testing.T) {
	t.Parallel()

	for _, limit := range []int32{1, 5, 20, 50} {
		request := &controlplanev1.SearchPlatformRequest{
			Limit: limit,
			Page:  &controlplanev1.PageRequest{PageToken: "next-page"},
		}
		result := searchPage(request)
		if result.Size != limit || result.Token != "next-page" {
			t.Fatalf("limit %d: search page = %+v", limit, result)
		}
	}
	if result := searchPage(&controlplanev1.SearchPlatformRequest{}); result.Size != 50 || result.Token != "" {
		t.Fatalf("default search page = %+v", result)
	}
}
