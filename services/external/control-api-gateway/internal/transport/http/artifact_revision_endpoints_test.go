package httptransport

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type artifactRevisionQueryStub struct {
	controlplanev1.PlatformQueryServiceClient
	list *controlplanev1.ListArtifactRevisionsRequest
	get  *controlplanev1.GetArtifactRevisionRequest
	err  error
}

func (stub *artifactRevisionQueryStub) ListArtifactRevisions(_ context.Context, request *controlplanev1.ListArtifactRevisionsRequest, _ ...grpc.CallOption) (*controlplanev1.ListArtifactRevisionsResponse, error) {
	stub.list = request
	return &controlplanev1.ListArtifactRevisionsResponse{Page: &controlplanev1.PageInfo{}}, stub.err
}

func (stub *artifactRevisionQueryStub) GetArtifactRevision(_ context.Context, request *controlplanev1.GetArtifactRevisionRequest, _ ...grpc.CallOption) (*controlplanev1.GetArtifactRevisionResponse, error) {
	stub.get = request
	return &controlplanev1.GetArtifactRevisionResponse{Revision: &controlplanev1.ArtifactRevision{Ref: request.RevisionRef, ArtifactRef: request.ArtifactRef}}, stub.err
}

type artifactRevisionCommandStub struct {
	controlplanev1.PlatformCommandServiceClient
	request *controlplanev1.DownloadArtifactRevisionRequest
	err     error
}

func (stub *artifactRevisionCommandStub) DownloadArtifactRevision(_ context.Context, request *controlplanev1.DownloadArtifactRevisionRequest, _ ...grpc.CallOption) (controlplanev1.PlatformCommandService_DownloadArtifactRevisionClient, error) {
	stub.request = request
	return &artifactRevisionStreamStub{frames: []*controlplanev1.DownloadArtifactRevisionResponse{{FileName: "note.md", MediaType: "text/markdown", SizeBytes: 3}, {Data: []byte("old")}}}, stub.err
}

type artifactRevisionStreamStub struct {
	grpc.ClientStream
	frames []*controlplanev1.DownloadArtifactRevisionResponse
}

func (stub *artifactRevisionStreamStub) Recv() (*controlplanev1.DownloadArtifactRevisionResponse, error) {
	if len(stub.frames) == 0 {
		return nil, io.EOF
	}
	frame := stub.frames[0]
	stub.frames = stub.frames[1:]
	return frame, nil
}

func TestArtifactRevisionHTTPPreservesExactPinsAndPaging(t *testing.T) {
	t.Parallel()
	query := &artifactRevisionQueryStub{}
	server := &Server{control: &controlplaneclient.Client{Query: query}}
	size, token := generated.PageSize(5), generated.PageToken("owner-pinned-cursor")
	response := httptest.NewRecorder()
	server.ListArtifactRevisions(response, httptest.NewRequest("GET", "/", nil), "art_12345678", generated.ListArtifactRevisionsParams{PageSize: &size, PageToken: &token})
	if response.Code != 200 || query.list.GetArtifactRef() != "art_12345678" || query.list.GetPage().GetPageSize() != 5 || query.list.GetPage().GetPageToken() != string(token) {
		t.Fatalf("revision history scope or cursor changed: status=%d request=%v", response.Code, query.list)
	}
	response = httptest.NewRecorder()
	server.GetArtifactRevision(response, httptest.NewRequest("GET", "/", nil), "art_12345678", "arev_12345678")
	if response.Code != 200 || query.get.GetArtifactRef() != "art_12345678" || query.get.GetRevisionRef() != "arev_12345678" || !strings.Contains(response.Body.String(), "arev_12345678") {
		t.Fatalf("explicit revision pin was lost: status=%d request=%v", response.Code, query.get)
	}
	query.err = status.Error(codes.Aborted, "revision cursor is stale")
	response = httptest.NewRecorder()
	server.ListArtifactRevisions(response, httptest.NewRequest("GET", "/", nil), "art_12345678", generated.ListArtifactRevisionsParams{PageToken: &token})
	if response.Code != 412 || !strings.Contains(response.Body.String(), "VERSION_OR_STATE_CONFLICT") {
		t.Fatalf("stale cursor status=%d", response.Code)
	}
}

func TestArtifactRevisionOpenAPIUsesCanonicalDefaultProblem(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../../../../"))
	raw, err := os.ReadFile(filepath.Join(root, "contracts/openapi/control-api-gateway/v1/openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/v1/artifacts/{artifactRef}/revisions", "/api/v1/artifacts/{artifactRef}/revisions/{revisionRef}", "/api/v1/artifacts/{artifactRef}/revisions/{revisionRef}/content"} {
		marker := "\n  " + endpoint + ":\n"
		_, rest, found := strings.Cut(string(raw), marker)
		if !found {
			t.Fatalf("revision endpoint is absent: %s", endpoint)
		}
		section, _, _ := strings.Cut(rest, "\n  /")
		if !strings.Contains(section, `default: { $ref: "#/components/responses/Problem" }`) {
			t.Fatalf("canonical 412/404/409 Problem is not covered: %s", endpoint)
		}
	}
}

func TestArtifactRevisionHTTPDownloadUsesExplicitRevisionWithoutLatestFallback(t *testing.T) {
	t.Parallel()
	command := &artifactRevisionCommandStub{}
	server := &Server{control: &controlplaneclient.Client{Command: command}}
	response := httptest.NewRecorder()
	server.DownloadArtifactRevision(response, httptest.NewRequest("GET", "/", nil), "art_12345678", "arev_12345678", generated.DownloadArtifactRevisionParams{Purpose: generated.DownloadArtifactRevisionParamsPurposeDOWNLOAD})
	if response.Code != 200 || response.Body.String() != "old" || response.Header().Get("Cache-Control") != "private, no-store" || command.request.GetArtifactRef() != "art_12345678" || command.request.GetRevisionRef() != "arev_12345678" || command.request.GetPurpose() != controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_DOWNLOAD {
		t.Fatalf("exact revision download changed: status=%d request=%v", response.Code, command.request)
	}
	command.err = status.Error(codes.NotFound, "revision is unavailable")
	response = httptest.NewRecorder()
	server.DownloadArtifactRevision(response, httptest.NewRequest("GET", "/", nil), "art_12345678", "arev_foreign1", generated.DownloadArtifactRevisionParams{Purpose: generated.DownloadArtifactRevisionParamsPurposePREVIEW})
	if response.Code != 404 || command.request.GetRevisionRef() != "arev_foreign1" || command.request.GetPurpose() != controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_PREVIEW {
		t.Fatalf("unavailable revision did not remain closed: status=%d request=%v", response.Code, command.request)
	}
}
