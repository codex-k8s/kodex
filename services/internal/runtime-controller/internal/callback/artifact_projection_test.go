package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

type artifactProjectionClient struct {
	controlplanev1.RuntimeWorkServiceClient
	response *controlplanev1.ReadExecutionArtifactResponse
	requests []*controlplanev1.StreamExecutionArtifactRequest
	err      error
}

func (client *artifactProjectionClient) StreamExecutionArtifact(_ context.Context, request *controlplanev1.StreamExecutionArtifactRequest, _ ...grpc.CallOption) (controlplanev1.RuntimeWorkService_StreamExecutionArtifactClient, error) {
	client.requests = append(client.requests, request)
	if client.err != nil {
		return nil, client.err
	}
	return &fixtureArtifactStream{frames: []*controlplanev1.StreamExecutionArtifactResponse{
		{Part: &controlplanev1.StreamExecutionArtifactResponse_Metadata{Metadata: client.response.GetArtifact()}},
		{Part: &controlplanev1.StreamExecutionArtifactResponse_Chunk{Chunk: client.response.GetContent()}},
		{Part: &controlplanev1.StreamExecutionArtifactResponse_Complete{Complete: &controlplanev1.RuntimeArtifactTransferComplete{
			SizeBytes: client.response.GetArtifact().GetSizeBytes(), Digest: client.response.GetArtifact().GetDigest()}}},
	}}, nil
}

type fixtureArtifactStream struct {
	grpc.ClientStream
	frames []*controlplanev1.StreamExecutionArtifactResponse
}

func (stream *fixtureArtifactStream) Recv() (*controlplanev1.StreamExecutionArtifactResponse, error) {
	if len(stream.frames) == 0 {
		return nil, io.EOF
	}
	frame := stream.frames[0]
	stream.frames = stream.frames[1:]
	return frame, nil
}

func fixtureArtifactSpool(t *testing.T) *artifactSpool {
	t.Helper()
	spool, err := openArtifactSpool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := spool.close(); err != nil {
			t.Error(err)
		}
	})
	return spool
}

func TestArtifactProjectionChecksExactOwnerResponseBeforeExposingBytes(t *testing.T) {
	content := []byte("synthetic immutable knowledge")
	sum := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	for _, scenario := range []string{"exact", "project", "version", "digest", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t, func(input *runtimecontract.RunnerInput) {
				input.Capabilities = append(input.Capabilities, runtimecontract.ArtifactCapability)
				input.CodexSandbox = "workspace-write"
				input.InputArtifacts = []runtimecontract.RunnerInputArtifact{{
					Ref: "artifact_abcdefgh", FileName: "knowledge.txt", MediaType: "text/plain",
					Digest: digest, SizeBytes: int64(len(content)), Revision: 1, Version: 2,
					Scope: runtimecontract.AttachmentScopeKnowledge, Position: 1, Source: "KNOWLEDGE_SOURCE",
					AttachmentPurpose: "PROJECT_KNOWLEDGE", Provenance: "PROJECT_BINDING",
				}}
			})
			response := &controlplanev1.ReadExecutionArtifactResponse{Artifact: &controlplanev1.Artifact{
				Ref: "artifact_abcdefgh", ProjectRef: input.ProjectRef, FileName: "knowledge.txt", MediaType: "text/plain",
				Digest: digest, SizeBytes: int64(len(content)), Revision: 1, Version: 2,
			}, Content: content}
			ref := response.Artifact.Ref
			switch scenario {
			case "project":
				response.Artifact.ProjectRef = "prj_foreign1"
			case "version":
				response.Artifact.Version++
			case "digest":
				response.Content = []byte("different content")
			case "unknown":
				ref = "artifact_foreign1"
			}
			client := &artifactProjectionClient{response: response}
			server := &Server{config: Config{RequestTimeout: time.Second, FileTransferTimeout: time.Second}, manager: manager, spool: fixtureArtifactSpool(t),
				control: &controlplaneclient.Client{Runtime: client}}
			request := httptest.NewRequest(http.MethodGet, "/v1/executions/"+input.LeaseRef+"/artifacts/"+ref, nil)
			request.Header.Set("Authorization", "Bearer "+ticket)
			bindTestExecutionHeaders(request, input, "artifact")
			writer := httptest.NewRecorder()
			server.route(writer, request)
			if scenario == "exact" {
				if writer.Code != http.StatusOK || writer.Body.String() != string(content) {
					t.Fatalf("exact download status=%d", writer.Code)
				}
			} else if writer.Code != http.StatusConflict && writer.Code != http.StatusNotFound || strings.Contains(writer.Body.String(), string(content)) {
				t.Fatalf("mismatch exposed bytes or succeeded: status=%d", writer.Code)
			}
			if scenario == "unknown" {
				if len(client.requests) != 0 {
					t.Fatal("unknown projection reached owner")
				}
			} else if len(client.requests) != 1 || client.requests[0].GetLeaseRef() != input.LeaseRef ||
				client.requests[0].GetFence() != input.LeaseFence || client.requests[0].GetGeneration() != input.LeaseGeneration {
				t.Fatal("download lost execution authority")
			}
		})
	}
}
