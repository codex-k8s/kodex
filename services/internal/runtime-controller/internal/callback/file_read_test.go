package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestReadFileAdvertisedForExactCatalog checks an eligible immutable catalog
// advertises the full paginated read tool, without executing it.
// TestReadFileAdvertisedForExactCatalog проверяет discovery full paginated read tool
// у eligible immutable catalog без его исполнения.
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

// TestReadFileInputCorrectionDoesNotMaskAuthorityOrAuditFailure checks only malformed
// local bounds receive one corrected-input path; owner denial, late authority/version,
// checksum and terminal-audit failures expose no text or corrective retry authority.
// TestReadFileInputCorrectionDoesNotMaskAuthorityOrAuditFailure проверяет one corrected-
// input path лишь malformed local bounds; owner denial,late authority/version,checksum
// и terminal-audit failures не дают text и corrective retry authority.
func TestReadFileInputCorrectionDoesNotMaskAuthorityOrAuditFailure(t *testing.T) {
	for _, scenario := range []string{"invalid input", "terminal audit", "owner denied", "final authority", "final version", "checksum"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newFileReadFixture(t, []byte(runtimeFileFixtureText), scenario)
			arguments := fixture.arguments()
			if scenario == "invalid input" || scenario == "terminal audit" {
				arguments["maximum_bytes"] = float64(32768)
			}
			result, failed := fixture.call(t, t.Context(), arguments, "invalid-or-denied")
			if !failed || result["text"] != nil {
				t.Fatal("failed file read exposed content")
			}
			if scenario == "invalid input" {
				if result["error_code"] != runtimeFileInputInvalidCode || result["retryable"] != true || result["guidance"] != runtimeFileInputGuidance {
					t.Fatal("invalid arguments omitted bounded correction guidance")
				}
				fixture.owner.mu.Lock()
				reads, transfers := fixture.owner.reads, fixture.owner.transfers
				fixture.owner.mu.Unlock()
				if reads != 0 || transfers != 0 {
					t.Fatal("invalid arguments reached file owner")
				}
				delete(arguments, "maximum_bytes")
				corrected, rejected := fixture.call(t, t.Context(), arguments, "corrected-once")
				if rejected || corrected["text"] != runtimeFileFixtureText || corrected["eof"] != true {
					t.Fatal("corrected exact file read failed")
				}
			} else if result["error_code"] != "TOOL_UNAVAILABLE" || result["retryable"] != false || result["guidance"] != nil {
				t.Fatal("authority, integrity or audit failure was made retryable")
			}
		})
	}
}

// fileReadOwnerFixture extends the generated owner double with arbitrary synthetic
// body frames, transfer completion tracking and initial/terminal/late-read failures.
// Its stored request projections are cloned for deterministic test observations.
// fileReadOwnerFixture дополняет generated owner double synthetic body frames,transfer
// completion tracking и initial/terminal/late-read failures. Stored projections
// клонируются для deterministic test observations.
type fileReadOwnerFixture struct {
	*runtimeFilesOwnerFixture
	body             []byte
	scenario         string
	transferComplete bool
	readBaseline     int
	records          []cp.RunToolCallState
	projections      []*cp.RecordRunToolCallRequest
}

// readFixtureDigest computes sha256:-prefixed lowercase SHA-256 of exact fixture
// bytes, including an empty source; it does not label a security scan result.
// readFixtureDigest считает sha256:-prefixed lowercase SHA-256 exact fixture bytes,
// включая empty source,не обозначая security scan result.
func readFixtureDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// GetExecutionFileMetadata delegates exact-pin assertions and injects denial or
// a cloned version drift on subsequent metadata reads to exercise final revalidation.
// GetExecutionFileMetadata делегирует exact-pin assertions и подставляет denial либо
// cloned version drift последующих metadata reads для final revalidation.
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

// StreamExecutionArtifact asserts execution/deadline pins and emits bounded chunks
// for the configured body, with scenario-specific stalls, corruption, missing/extra
// terminal frames or owner denial. It marks completion only after the intended clean
// path; it does not provide production permission or storage enforcement.
// StreamExecutionArtifact проверяет execution/deadline pins и выдаёт bounded chunks
// configured body со scenario-specific stalls,corruption,missing/extra terminal frames
// и owner denial. Completion отмечается лишь после intended clean path; production
// permission/storage enforcement не реализуются.
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

// RecordRunToolCall clones projections and tracks per-turn read/transfer progress,
// asserting success follows full completion plus two metadata reads. It can reject
// initial/terminal audit to test fail-closed delivery without a real event commit.
// RecordRunToolCall клонирует projections и отслеживает per-turn read/transfer progress,
// проверяя success лишь после full completion/двух metadata reads. Может отклонить
// initial/terminal audit для fail-closed delivery без real event commit.
func (fixture *fileReadOwnerFixture) RecordRunToolCall(ctx context.Context, request *cp.RecordRunToolCallRequest) (*cp.RecordRunToolCallResponse, error) {
	fixture.mu.Lock()
	fixture.records = append(fixture.records, request.GetState())
	fixture.projections = append(fixture.projections, proto.Clone(request).(*cp.RecordRunToolCallRequest))
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

// fileReadFixture bundles an isolated local HTTP/MCP server, generated owner double,
// synthetic runtime ticket/input and captured logs for full-source read scenarios.
// fileReadFixture объединяет isolated local HTTP/MCP server,generated owner double,
// synthetic runtime ticket/input и captured logs full-source read scenarios.
type fileReadFixture struct {
	server *Server
	owner  *fileReadOwnerFixture
	input  runtimecontract.RunnerInput
	ticket string
	logs   *bytes.Buffer
}

// newFileReadFixture creates exact descriptor/body digests and a generated in-memory
// owner connection, then combines an isolated spool, synthetic route ticket and logger.
// Test cleanup owns each resource; no real provider or external artifact store is used.
// newFileReadFixture создаёт exact descriptor/body digests и generated in-memory owner
// connection,объединяя isolated spool,synthetic route ticket/logger. Test cleanup
// владеет ресурсами; real provider/external artifact store не используются.
func newFileReadFixture(t *testing.T, body []byte, scenario string) fileReadFixture {
	t.Helper()
	manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t, func(input *runtimecontract.RunnerInput) {
		input.Capabilities = append(input.Capabilities, runtimecontract.ArtifactCapability)
		input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeProject}}
	})
	catalog, descriptor := fileFixture(input)
	descriptor.SizeBytes, descriptor.Digest = int64(len(body)), readFixtureDigest(body)
	owner := &fileReadOwnerFixture{runtimeFilesOwnerFixture: &runtimeFilesOwnerFixture{t: t, input: input, catalog: catalog, file: descriptor}, body: body, scenario: scenario}
	connection := runtimeFilesOwnerConnection(t, owner, "passthrough:///full-file-fixture")
	logs := &bytes.Buffer{}
	server := &Server{manager: manager, config: Config{RequestTimeout: time.Second, FileTransferTimeout: time.Second}, spool: fixtureArtifactSpool(t),
		logger: slog.New(slog.NewTextHandler(logs, nil)), control: &controlplaneclient.Client{Runtime: cp.NewRuntimeWorkServiceClient(connection)}}
	return fileReadFixture{server: server, owner: owner, input: input, ticket: ticket, logs: logs}
}

// arguments returns a fresh exact own-catalog selector map with float64 revision
// matching JSON-decoded tool input, without copying another runtime's entry locator.
// arguments возвращает новую exact own-catalog selector map с float64 revision
// как JSON-decoded tool input без копирования entry locator другого runtime.
func (fixture fileReadFixture) arguments() map[string]any {
	return map[string]any{"purpose": runtimecontract.FilePurposeProject, "entry_ref": fixture.owner.file.EntryRef, "artifact_ref": fixture.owner.file.ArtifactRef,
		"revision": float64(fixture.owner.file.Revision), "digest": fixture.owner.file.Digest}
}

// call invokes the local authenticated HTTP MCP route with the supplied context/ID,
// checks a decodable response and zero retained spool slots, then returns structured
// content plus an error flag; it does not suppress private owner/audit failures.
// call вызывает local authenticated HTTP MCP route с context/ID,проверяет decodable
// response и отсутствие retained spool slots,возвращая structured content/error flag;
// private owner/audit failures не подавляются.
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

// TestReadFileConsumesWholeVerifiedSourceInContiguousPages reconstructs empty, large
// and rune-crossing sources through local MCP, checking every page's bounded UTF-8,
// continuity, exact receipt digests/metadata and final empty EOF page.
// TestReadFileConsumesWholeVerifiedSourceInContiguousPages восстанавливает empty,large
// и rune-crossing sources через local MCP,проверяя bounded UTF-8,continuity,exact receipt
// digests/metadata каждой page и final empty EOF page.
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
				fixture.owner.mu.Lock()
				projection := fixture.owner.projections[len(fixture.owner.projections)-1]
				fixture.owner.mu.Unlock()
				var receipt fileReadReceipt
				if json.Unmarshal([]byte(projection.GetSafeResult()), &receipt) != nil || projection.GetRevision() != 2 ||
					receipt.Version != 1 || receipt.Kind != fileReadReceiptKind || receipt.CatalogRef != fixture.input.FileCatalog.Ref ||
					receipt.CatalogDigest != fixture.input.FileCatalog.Digest || receipt.Purpose != runtimecontract.FilePurposeProject ||
					receipt.EntryRef != fixture.owner.file.EntryRef || receipt.ArtifactRef != fixture.owner.file.ArtifactRef ||
					receipt.FileRevision != fixture.owner.file.Revision || receipt.FileVersion != fixture.owner.file.Version ||
					receipt.SizeBytes != int64(len(body)) || receipt.OffsetBytes != offset ||
					receipt.NextOffsetBytes != int64(result["next_offset_bytes"].(float64)) || receipt.EOF != result["eof"] ||
					receipt.SourceDigest != result["source_digest"] || receipt.ChunkDigest != result["chunk_digest"] {
					t.Fatal("terminal metadata receipt lost the exact verified page or execution catalog")
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

// TestReadFileTruncatedEntryAllowsOneExactOwnCatalogCorrection rejects a truncated
// entry before owner I/O, then explicitly submits the exact fixture catalog entry
// once; it checks the handler never guessed bytes or replayed the malformed selector.
// TestReadFileTruncatedEntryAllowsOneExactOwnCatalogCorrection отвергает truncated entry
// до owner I/O,затем явно передаёт exact fixture catalog entry один раз; handler
// не должен угадывать bytes и повторять malformed selector.
func TestReadFileTruncatedEntryAllowsOneExactOwnCatalogCorrection(t *testing.T) {
	fixture := newFileReadFixture(t, []byte("synthetic complete source"), "")
	fixture.owner.file.EntryRef = "vfe_897a44d8dd71438d9dd97597a7b766ee"
	arguments := fixture.arguments()
	arguments["entry_ref"] = "vfe_897a44d8dd71438d9dd975a7b766ee"
	result, failed := fixture.call(t, t.Context(), arguments, "truncated")
	if !failed || result["error_code"] != runtimeFileInputInvalidCode || result["retryable"] != true || result["guidance"] != runtimeFileInputGuidance || result["text"] != nil {
		t.Fatal("truncated entry did not preserve bounded input correction")
	}
	fixture.owner.mu.Lock()
	reads, transfers := fixture.owner.reads, fixture.owner.transfers
	fixture.owner.mu.Unlock()
	if reads != 0 || transfers != 0 || arguments["entry_ref"] != "vfe_897a44d8dd71438d9dd975a7b766ee" {
		t.Fatal("malformed selector was replayed, rewritten or read remotely")
	}
	// Исправление выбирает exact descriptor из собственного frozen каталога;
	// handler не угадывает missing bytes и не повторяет прежний input.
	arguments["entry_ref"] = fixture.owner.file.EntryRef
	result, failed = fixture.call(t, t.Context(), arguments, "corrected-once")
	if failed || result["text"] != "synthetic complete source" || result["eof"] != true || result["file"].(map[string]any)["entry_ref"] != fixture.owner.file.EntryRef {
		t.Fatal("exact own catalog correction did not consume the protected source")
	}
	fixture.owner.mu.Lock()
	defer fixture.owner.mu.Unlock()
	if fixture.owner.reads != 2 || fixture.owner.transfers != 1 {
		t.Fatal("corrected call replayed metadata or full source outside the normal proof path")
	}
}

// TestReadFileMalformedEntryWithAuditFailureHasNoCorrection checks mandatory failed
// audit suppresses local corrective guidance for a malformed entry, with no source read.
// TestReadFileMalformedEntryWithAuditFailureHasNoCorrection проверяет запрет local corrective
// guidance malformed entry при mandatory failed audit без source read.
func TestReadFileMalformedEntryWithAuditFailureHasNoCorrection(t *testing.T) {
	fixture := newFileReadFixture(t, []byte("synthetic source"), "terminal audit")
	arguments := fixture.arguments()
	arguments["entry_ref"] = "vfe_897a44d8dd71438d9dd975a7b766ee"
	result, failed := fixture.call(t, t.Context(), arguments, "malformed-audit")
	if !failed || result["error_code"] != "TOOL_UNAVAILABLE" || result["retryable"] != false || result["guidance"] != nil || result["text"] != nil {
		t.Fatal("mandatory audit failure became an input correction")
	}
	fixture.owner.mu.Lock()
	defer fixture.owner.mu.Unlock()
	if fixture.owner.reads != 0 || fixture.owner.transfers != 0 {
		t.Fatal("malformed selector with failed audit read a remote source")
	}
}

// TestReadFileFailuresExposeNoTextOrPrivateAuthority checks thirteen encoding,stream,
// authority,audit,capacity and deadline failures expose no source text or private log
// data. Initial audit/capacity failure must prevent owner effects; a refused terminal
// success write remains an attempted call, not a claimed committed success.
// TestReadFileFailuresExposeNoTextOrPrivateAuthority проверяет тринадцать encoding,stream,
// authority,audit,capacity/deadline failures без source text/private log data. Initial
// audit/capacity failure запрещают owner effects; refused terminal success write —
// лишь attempted call,не claimed committed success.
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
			for _, projection := range fixture.owner.projections {
				if strings.Contains(projection.GetSafeResult(), "PRIVATE_FILE_READ_SENTINEL") ||
					projection.GetState() != cp.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED &&
						projection.GetSafeResult() != "" && projection.GetSafeResult() != "TOOL_UNAVAILABLE" {
					t.Fatal("failed file handler published content or successful metadata")
				}
			}
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

// TestReadFileRejectsOffsetAndCallerAuthorityBeforeTransfer checks malformed bounds
// and caller authority/purpose selectors never return text or open a source stream.
// The mid-rune offset case is intentionally rejected only after full source validation.
// TestReadFileRejectsOffsetAndCallerAuthorityBeforeTransfer проверяет отсутствие text/source
// stream при malformed bounds/caller authority/purpose selectors. Mid-rune offset
// намеренно отвергается лишь после full source validation.
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

// TestReadFileTextChecksCancellationAndRuneCarry checks a four-byte rune spanning
// the scan buffer is preserved and an already canceled scan returns context.Canceled.
// TestReadFileTextChecksCancellationAndRuneCarry проверяет four-byte rune на границе
// scan buffer и context.Canceled уже отменённого scan.
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

// TestReadFileRetainsTerminalAuditBudgetAfterReadDeadline stalls the synthetic stream
// under a shorter parent deadline and verifies the reserved timeout still permits
// RUNNING/FAILED audit attempts without source text delivery.
// TestReadFileRetainsTerminalAuditBudgetAfterReadDeadline задерживает synthetic stream
// с коротким parent deadline и проверяет reserved timeout для RUNNING/FAILED audit
// attempts без source text delivery.
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

// TestReadFileVerifiesLargeSourceWithBoundedPageReply verifies an entire source above
// 33 MiB before accepting its first 16-KiB page with exact source digest and eof=false.
// TestReadFileVerifiesLargeSourceWithBoundedPageReply проверяет весь source более 33 MiB
// до принятия первой 16-KiB page с exact source digest/eof=false.
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
