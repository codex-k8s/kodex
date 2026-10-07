package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

func TestReadFileAdvertisedForExactCatalog(t *testing.T) {
	input := validWarmExecutionInput()
	input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeProject}}
	for _, tool := range runtimeFileTools(input) {
		if tool["name"] == "read_file" {
			return
		}
	}
	t.Fatal("exact runtime catalog omitted full paged file read")
}

type fileReadOwnerFixture struct {
	*runtimeFilesOwnerFixture
	body             []byte
	scenario         string
	transferComplete bool
	readBaseline     int
	records          []cp.RunToolCallState
}

func readFixtureDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (fixture *fileReadOwnerFixture) GetExecutionFileMetadata(ctx context.Context, request *cp.GetExecutionFileMetadataRequest) (*cp.GetExecutionFileMetadataResponse, error) {
	response, err := fixture.runtimeFilesOwnerFixture.GetExecutionFileMetadata(ctx, request)
	fixture.mu.Lock()
	reads := fixture.reads
	fixture.mu.Unlock()
	if reads > 1 {
		if fixture.scenario == "final authority" {
			return nil, status.Error(codes.PermissionDenied, "synthetic current file authority denied")
		}
		if fixture.scenario == "final version" {
			response = proto.Clone(response).(*cp.GetExecutionFileMetadataResponse)
			response.File.Version++
		}
	}
	return response, err
}

func (fixture *fileReadOwnerFixture) StreamExecutionArtifact(request *cp.StreamExecutionArtifactRequest, stream cp.RuntimeWorkService_StreamExecutionArtifactServer) error {
	if request.GetLeaseRef() != fixture.input.LeaseRef || request.GetFence() != fixture.input.LeaseFence || request.GetGeneration() != fixture.input.LeaseGeneration || request.GetArtifactRef() != fixture.file.ArtifactRef {
		fixture.t.Error("full source transfer lost exact lease binding")
		return status.Error(codes.PermissionDenied, "synthetic transfer denied")
	}
	deadline, ok := stream.Context().Deadline()
	if !ok || time.Until(deadline) > maximumFileReadDuration {
		fixture.t.Error("full source transfer lost bounded read deadline")
	}
	fixture.mu.Lock()
	fixture.transfers++
	fixture.mu.Unlock()
	metadata := &cp.Artifact{Ref: fixture.file.ArtifactRef, ProjectRef: fixture.file.ProjectRef, Revision: int32(fixture.file.Revision), Version: fixture.file.Version,
		FileName: fixture.file.Name, MediaType: fixture.file.MediaType, SizeBytes: fixture.file.SizeBytes, Digest: fixture.file.Digest}
	if fixture.scenario == "stream pin" {
		metadata.Version++
	}
	if err := stream.Send(&cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Metadata{Metadata: metadata}}); err != nil {
		return err
	}
	if fixture.scenario == "deadline" {
		<-stream.Context().Done()
		return stream.Context().Err()
	}
	body := fixture.body
	if fixture.scenario == "checksum" {
		body = bytes.Clone(body)
		body[len(body)-1] = 'z'
	}
	for start := 0; start < len(body); start += runtimecontract.MaximumArtifactTransferChunkBytes {
		end := min(len(body), start+runtimecontract.MaximumArtifactTransferChunkBytes)
		if err := stream.Send(&cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Chunk{Chunk: body[start:end]}}); err != nil {
			return err
		}
	}
	if fixture.scenario == "missing complete" {
		return nil
	}
	if fixture.scenario == "owner denied" {
		return status.Error(codes.PermissionDenied, "synthetic final source authority denied")
	}
	if err := stream.Send(&cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Complete{Complete: &cp.RuntimeArtifactTransferComplete{SizeBytes: fixture.file.SizeBytes, Digest: fixture.file.Digest}}}); err != nil {
		return err
	}
	if fixture.scenario == "trailing frame" {
		return stream.Send(&cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Chunk{Chunk: []byte("extra")}})
	}
	fixture.mu.Lock()
	fixture.transferComplete = true
	fixture.mu.Unlock()
	return nil
}

func (fixture *fileReadOwnerFixture) RecordRunToolCall(ctx context.Context, request *cp.RecordRunToolCallRequest) (*cp.RecordRunToolCallResponse, error) {
	fixture.mu.Lock()
	fixture.records = append(fixture.records, request.GetState())
	if request.GetRevision() == 1 {
		fixture.transferComplete = false
		fixture.readBaseline = fixture.reads
	}
	complete := fixture.transferComplete
	reads := fixture.reads - fixture.readBaseline
	fixture.mu.Unlock()
	if request.GetState() == cp.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED && (!complete || reads < 2) {
		fixture.t.Error("tool succeeded before full transfer and current exact file authority")
	}
	if fixture.scenario == "terminal audit" && request.GetRevision() == 2 {
		return nil, status.Error(codes.Unavailable, "synthetic terminal audit unavailable")
	}
	if fixture.scenario == "initial audit" {
		return nil, status.Error(codes.Unavailable, "synthetic initial audit unavailable")
	}
	return fixture.runtimeFilesOwnerFixture.RecordRunToolCall(ctx, request)
}

type fileReadFixture struct {
	server *Server
	owner  *fileReadOwnerFixture
	input  runtimecontract.RunnerInput
	ticket string
	logs   *bytes.Buffer
}

func newFileReadFixture(t *testing.T, body []byte, scenario string) fileReadFixture {
	t.Helper()
	manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t, func(input *runtimecontract.RunnerInput) {
		input.Capabilities = append(input.Capabilities, runtimecontract.ArtifactCapability)
		input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeProject}}
	})
	catalog, descriptor := fileFixture(input)
	descriptor.SizeBytes, descriptor.Digest = int64(len(body)), readFixtureDigest(body)
	owner := &fileReadOwnerFixture{runtimeFilesOwnerFixture: &runtimeFilesOwnerFixture{t: t, input: input, catalog: catalog, file: descriptor}, body: body, scenario: scenario}
	listener := bufconn.Listen(1 << 20)
	upstream := grpc.NewServer()
	cp.RegisterRuntimeWorkServiceServer(upstream, owner)
	done := make(chan error, 1)
	go func() { done <- upstream.Serve(listener) }()
	t.Cleanup(func() { upstream.Stop(); _ = listener.Close(); <-done })
	connection, err := grpc.NewClient("passthrough:///full-file-fixture", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	logs := &bytes.Buffer{}
	server := &Server{manager: manager, config: Config{RequestTimeout: time.Second, FileTransferTimeout: time.Second}, spool: fixtureArtifactSpool(t),
		logger: slog.New(slog.NewTextHandler(logs, nil)), control: &controlplaneclient.Client{Runtime: cp.NewRuntimeWorkServiceClient(connection)}}
	return fileReadFixture{server: server, owner: owner, input: input, ticket: ticket, logs: logs}
}

func (fixture fileReadFixture) arguments() map[string]any {
	return map[string]any{"purpose": runtimecontract.FilePurposeProject, "entry_ref": fixture.owner.file.EntryRef, "artifact_ref": fixture.owner.file.ArtifactRef,
		"revision": float64(fixture.owner.file.Revision), "digest": fixture.owner.file.Digest}
}

func (fixture fileReadFixture) call(t *testing.T, ctx context.Context, arguments map[string]any, id string) (map[string]any, bool) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": "read_file", "arguments": arguments}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+fixture.input.LeaseRef+"/mcp", bytes.NewReader(body)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+fixture.ticket)
	bindTestExecutionHeaders(request, fixture.input, "mcp")
	writer := httptest.NewRecorder()
	fixture.server.route(writer, request)
	var response struct {
		Error  any `json:"error"`
		Result struct {
			IsError    bool           `json:"isError"`
			Structured map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if writer.Code != http.StatusOK || json.Unmarshal(writer.Body.Bytes(), &response) != nil {
		t.Fatal("full file MCP reply is invalid")
	}
	if len(fixture.server.spool.slots) != 0 {
		t.Fatal("file read retained a private spool slot")
	}
	return response.Result.Structured, response.Error != nil || response.Result.IsError
}

func TestReadFileConsumesWholeVerifiedSourceInContiguousPages(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(strings.Repeat("x", 26276)), []byte(strings.Repeat("x", 39301)), []byte(strings.Repeat("x", 16383) + "я😀" + strings.Repeat("x", 49145) + "😀tail")} {
		t.Run(strconv.Itoa(len(body)), func(t *testing.T) {
			fixture := newFileReadFixture(t, body, "")
			arguments := fixture.arguments()
			var collected []byte
			var offset int64
			for page := 0; ; page++ {
				arguments["offset_bytes"] = float64(offset)
				result, failed := fixture.call(t, t.Context(), arguments, strings.Repeat("p", page+1))
				if failed {
					t.Fatal("exact full file page failed")
				}
				text := result["text"].(string)
				next := int64(result["next_offset_bytes"].(float64))
				if len(text) > 16384 || !utf8.ValidString(text) || int64(result["offset_bytes"].(float64)) != offset || next != offset+int64(len(text)) ||
					result["chunk_digest"] != readFixtureDigest([]byte(text)) || result["source_digest"] != readFixtureDigest(body) {
					t.Fatal("page bytes, progress, or verified commitment differ")
				}
				collected = append(collected, text...)
				if result["eof"] == true {
					if next != int64(len(body)) || !bytes.Equal(collected, body) {
						t.Fatal("EOF did not cover the entire exact source")
					}
					break
				}
				if next <= offset || page > 10 {
					t.Fatal("paged read failed to make progress")
				}
				offset = next
			}
			arguments["offset_bytes"] = float64(len(body))
			result, failed := fixture.call(t, t.Context(), arguments, "eof")
			if failed || result["text"] != "" || result["eof"] != true {
				t.Fatal("exact EOF offset was not an empty final page")
			}
		})
	}
}

func TestReadFileFailuresExposeNoTextOrPrivateAuthority(t *testing.T) {
	for _, scenario := range []string{"invalid UTF8 tail", "NUL tail", "checksum", "missing complete", "owner denied", "stream pin", "trailing frame", "final authority", "final version", "terminal audit", "initial audit", "quota", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			body := []byte("PRIVATE_FILE_READ_SENTINEL" + strings.Repeat("x", 17000))
			if scenario == "invalid UTF8 tail" {
				body = append(body, 0xff)
			}
			if scenario == "NUL tail" {
				body = append(body, 0)
			}
			fixture := newFileReadFixture(t, body, scenario)
			if scenario == "quota" {
				for range maximumConcurrentArtifactTransfers {
					_, release, err := fixture.server.spool.acquire(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(release)
				}
			}
			arguments := fixture.arguments()
			var result map[string]any
			var failed bool
			if scenario == "quota" {
				// В этом сценарии два слота намеренно остаются заняты fixtures.
				_, err := fixture.server.callFileTool(t.Context(), fixture.input, runtimecontract.FileToolRead, arguments)
				failed = err != nil
			} else {
				result, failed = fixture.call(t, t.Context(), arguments, "failure")
			}
			if !failed || result != nil && result["text"] != nil {
				t.Fatal("failed full file read exposed page text")
			}
			if strings.Contains(fixture.logs.String(), "PRIVATE_FILE_READ_SENTINEL") || strings.Contains(fixture.logs.String(), fixture.ticket) || strings.Contains(fixture.logs.String(), fixture.owner.file.ArtifactRef) {
				t.Fatal("full file failure diagnostic leaked private data")
			}
			fixture.owner.mu.Lock()
			defer fixture.owner.mu.Unlock()
			if scenario == "initial audit" && (fixture.owner.reads != 0 || fixture.owner.transfers != 0) {
				t.Fatal("initial audit rejection performed file effects")
			}
			if scenario == "quota" && fixture.owner.transfers != 0 {
				t.Fatal("spool quota failure opened a source stream")
			}
			if scenario != "terminal audit" {
				for _, state := range fixture.owner.records {
					if state == cp.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED {
						t.Fatal("failed full file operation recorded success")
					}
				}
			}
		})
	}
}

func TestReadFileRejectsOffsetAndCallerAuthorityBeforeTransfer(t *testing.T) {
	for _, mutation := range []struct {
		key   string
		value any
	}{{"offset_bytes", -1.0}, {"offset_bytes", 0.5}, {"offset_bytes", "0"}, {"offset_bytes", float64(runtimecontract.MaximumArtifactTransferBytes + 1)}, {"offset_bytes", 100.0}, {"offset_bytes", 2.0}, {"maximum_bytes", 3.0}, {"maximum_bytes", 16385.0}, {"path", "/private"}, {"lease_ref", "lease_foreign01"}, {"project_ref", "prj_foreign01"}, {"purpose", runtimecontract.FilePurposeSkill}} {
		t.Run(mutation.key, func(t *testing.T) {
			fixture := newFileReadFixture(t, []byte("a😀tail"), "")
			arguments := fixture.arguments()
			arguments[mutation.key] = mutation.value
			result, failed := fixture.call(t, t.Context(), arguments, "invalid")
			if !failed || result != nil && result["text"] != nil {
				t.Fatal("invalid read_file input exposed content")
			}
			if mutation.key != "offset_bytes" || mutation.value != 2.0 {
				fixture.owner.mu.Lock()
				defer fixture.owner.mu.Unlock()
				if fixture.owner.transfers != 0 {
					t.Fatal("invalid shape or size opened a full source stream")
				}
			}
		})
	}
}

func TestReadFileTextChecksCancellationAndRuneCarry(t *testing.T) {
	file, release, err := fixtureArtifactSpool(t).acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	body := []byte(strings.Repeat("x", (64<<10)-1) + "😀")
	if _, err := file.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := validateFileReadText(t.Context(), file, int64(len(body))); err != nil {
		t.Fatal("valid UTF8 crossing stream-sized buffer rejected")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := validateFileReadText(ctx, file, int64(len(body))); err != context.Canceled {
		t.Fatal("text scan ignored cancellation")
	}
}

func TestReadFileRetainsTerminalAuditBudgetAfterReadDeadline(t *testing.T) {
	fixture := newFileReadFixture(t, []byte("private-budget-source"), "deadline")
	fixture.server.config.RequestTimeout = 100 * time.Millisecond
	fixture.server.config.FileTransferTimeout = 2 * time.Minute
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	result, failed := fixture.call(t, ctx, fixture.arguments(), "deadline-reserve")
	if !failed || result["text"] != nil {
		t.Fatal("expired read budget exposed source bytes")
	}
	fixture.owner.mu.Lock()
	defer fixture.owner.mu.Unlock()
	if len(fixture.owner.records) != 2 || fixture.owner.records[0] != cp.RunToolCallState_RUN_TOOL_CALL_STATE_RUNNING || fixture.owner.records[1] != cp.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED {
		t.Fatal("read deadline exhausted its terminal failure audit budget")
	}
}

func TestReadFileVerifiesLargeSourceWithBoundedPageReply(t *testing.T) {
	body := []byte(strings.Repeat("x", (33<<20)+7))
	fixture := newFileReadFixture(t, body, "")
	fixture.server.config.FileTransferTimeout = 10 * time.Second
	arguments := fixture.arguments()
	result, failed := fixture.call(t, t.Context(), arguments, "large")
	if failed || len(result["text"].(string)) != 16384 || result["eof"] != false || result["source_digest"] != readFixtureDigest(body) {
		t.Fatal("large protected source was not verified before its bounded page")
	}
}
