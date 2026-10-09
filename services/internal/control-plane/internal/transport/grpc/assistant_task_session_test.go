package grpc

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAssistantTaskSessionSelectorClosedMode(t *testing.T) {
	for name, mutate := range map[string]func(*controlplanev1.SearchAssistantResourcesRequest){
		"query":            func(r *controlplanev1.SearchAssistantResourcesRequest) { r.Query = "previous task" },
		"definitions":      func(r *controlplanev1.SearchAssistantResourcesRequest) { r.IntegrationDefinitionCatalog = true },
		"definition-query": func(r *controlplanev1.SearchAssistantResourcesRequest) { r.DefinitionQuery = "value" },
		"offset":           func(r *controlplanev1.SearchAssistantResourcesRequest) { r.DefinitionOffset = 1 },
		"configuration": func(r *controlplanev1.SearchAssistantResourcesRequest) {
			r.AssistantConfigurationCatalog = &controlplanev1.AssistantConfigurationCatalogRequest{}
		},
		"unknown-request": func(r *controlplanev1.SearchAssistantResourcesRequest) {
			r.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
		"unknown-selector": func(r *controlplanev1.SearchAssistantResourcesRequest) {
			r.AssistantTaskSessionRead.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
		"missing": func(r *controlplanev1.SearchAssistantResourcesRequest) { r.AssistantTaskSessionRead = nil },
	} {
		t.Run(name, func(t *testing.T) {
			request := &controlplanev1.SearchAssistantResourcesRequest{AssistantTaskSessionRead: &controlplanev1.AssistantTaskSessionReadRequest{RunRef: "run_abcdefgh"}}
			mutate(request)
			// Никакой domain call не достигается до проверки режима.
			_, err := (&Server{}).readAssistantTaskSession(t.Context(), value.Principal{}, request)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatal("mixed or unknown selector reached the domain")
			}
		})
	}
}
