package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

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

const (
	// runtimeFileFixtureText is synthetic preview/stream content used as a disclosure
	// canary and exact byte/digest fixture, never real user file data.
	// runtimeFileFixtureText — synthetic preview/stream content для disclosure canary
	// и exact byte/digest fixture,не real user file data.
	runtimeFileFixtureText = "synthetic file preview"
	// runtimeFileFixtureEntry is one canonical captured-entry-format fixture with
	// exactly thirty-two lowercase hexadecimal suffix characters.
	// runtimeFileFixtureEntry — canonical captured-entry-format fixture с ровно
	// тридцатью двумя lowercase hexadecimal suffix characters.
	runtimeFileFixtureEntry = "vfe_0123456789abcdef0123456789abcdef"
	// runtimeFileForeignEntry is a distinct canonical entry-format locator for foreign
	// selection tests; syntactic validity does not make it a granted catalog entry.
	// runtimeFileForeignEntry — отличный canonical entry-format locator foreign selection
	// tests; syntactic validity не делает его granted catalog entry.
	runtimeFileForeignEntry = "vfe_fedcba9876543210fedcba9876543210"
)

// runtimeFilesOwnerFixture is a generated-RPC in-memory owner double with exact
// execution/file pins, read/audit/transfer counters and injected failure flags.
// Its checks are test assertions, not production transport authentication or storage.
// runtimeFilesOwnerFixture — generated-RPC in-memory owner double с exact execution/file
// pins,read/audit/transfer counters и injected failures. Проверки — test assertions,
// не production transport authentication и не storage.
type runtimeFilesOwnerFixture struct {
	cp.UnimplementedRuntimeWorkServiceServer
	t             *testing.T
	input         runtimecontract.RunnerInput
	catalog       *cp.RuntimeFileCatalog
	file          *cp.ExecutionFileDescriptor
	mu            sync.Mutex
	reads, audits int
	failAudit     bool
	failTransfer  bool
	transfers     int
	preview       *cp.PreviewExecutionFileResponse
}

// StreamExecutionArtifact rejects mismatched lease/artifact pins, counts an accepted
// transfer and emits synthetic metadata/chunk/Complete frames, optionally failing
// before completion to test that private partial bytes never become HTTP success.
// StreamExecutionArtifact отвергает mismatched lease/artifact pins,считает accepted
// transfer и выдаёт synthetic metadata/chunk/Complete frames,при настройке падая
// до completion для проверки запрета HTTP success с private partial bytes.
func (fixture *runtimeFilesOwnerFixture) StreamExecutionArtifact(request *cp.StreamExecutionArtifactRequest, stream cp.RuntimeWorkService_StreamExecutionArtifactServer) error {
	if request.GetLeaseRef() != fixture.input.LeaseRef || request.GetFence() != fixture.input.LeaseFence || request.GetGeneration() != fixture.input.LeaseGeneration || request.GetArtifactRef() != fixture.file.ArtifactRef {
		return status.Error(codes.PermissionDenied, "synthetic transfer binding denied")
	}
	fixture.mu.Lock()
	fixture.transfers++
	fail := fixture.failTransfer
	fixture.mu.Unlock()
	metadata := &cp.Artifact{Ref: fixture.file.ArtifactRef, ProjectRef: fixture.file.ProjectRef, Revision: int32(fixture.file.Revision), Version: fixture.file.Version,
		FileName: fixture.file.Name, MediaType: fixture.file.MediaType, SizeBytes: fixture.file.SizeBytes, Digest: fixture.file.Digest}
	if err := stream.Send(&cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Metadata{Metadata: metadata}}); err != nil {
		return err
	}
	if err := stream.Send(&cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Chunk{Chunk: []byte(runtimeFileFixtureText)}}); err != nil {
		return err
	}
	if fail {
		return status.Error(codes.Unavailable, "synthetic final owner read unavailable")
	}
	return stream.Send(&cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Complete{Complete: &cp.RuntimeArtifactTransferComplete{SizeBytes: fixture.file.SizeBytes, Digest: fixture.file.Digest}}})
}

// TestCatalogBodyUsesMetadataPinAndGeneratedStreamBeforeExposingBytes traverses local
// HTTP and generated bufconn RPC for seven scenarios, checking exact metadata then
// stream, no content on invalid query/ticket/owner/partial result, and released spool
// capacity. It does not verify real mTLS, live owner storage or antivirus scanning.
// TestCatalogBodyUsesMetadataPinAndGeneratedStreamBeforeExposingBytes проходит local
// HTTP/generated bufconn RPC семи scenarios: exact metadata затем stream,без content
// при invalid query/ticket/owner/partial result,с освобождением spool capacity.
// Real mTLS,live owner storage и antivirus scanning не проверяются.
func TestCatalogBodyUsesMetadataPinAndGeneratedStreamBeforeExposingBytes(t *testing.T) {
	for _, scenario := range []string{"exact", "foreign project query", "duplicate purpose", "wrong purpose", "foreign metadata", "partial stream", "missing ticket"} {
		t.Run(scenario, func(t *testing.T) {
			manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t, func(input *runtimecontract.RunnerInput) {
				input.Capabilities = append(input.Capabilities, runtimecontract.ArtifactCapability)
				input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeProject}}
			})
			catalog, file := fileFixture(input)
			owner := &runtimeFilesOwnerFixture{t: t, input: input, catalog: catalog, file: file, failTransfer: scenario == "partial stream"}
			connection := runtimeFilesOwnerConnection(t, owner, "passthrough:///catalog-transfer-fixture")
			descriptor, ok := fileDescriptor(input, file.Purpose, file)
			if !ok {
				t.Fatal("download descriptor missing")
			}
			download := descriptor["download"].(map[string]any)
			endpoint, err := url.Parse(download["relative_path"].(string))
			if err != nil || endpoint.IsAbs() || download["method"] != "GET" || download["requires_execution_context"] != true {
				t.Fatal("unsafe download descriptor")
			}
			query := endpoint.Query()
			switch scenario {
			case "foreign project query":
				query.Set("project_ref", "prj_other")
			case "duplicate purpose":
				query.Add("purpose", runtimecontract.FilePurposeProject)
			case "wrong purpose":
				query.Set("purpose", runtimecontract.FilePurposeRunResult)
			case "foreign metadata":
				file.ProjectRef = "prj_other"
			}
			endpoint.RawQuery = query.Encode()
			server := &Server{manager: manager, config: Config{RequestTimeout: time.Second, FileTransferTimeout: time.Second}, spool: fixtureArtifactSpool(t), control: &controlplaneclient.Client{Runtime: cp.NewRuntimeWorkServiceClient(connection)}}
			request := httptest.NewRequest(http.MethodGet, endpoint.String(), nil)
			if scenario != "missing ticket" {
				request.Header.Set("Authorization", "Bearer "+ticket)
			}
			bindTestExecutionHeaders(request, input, "artifact")
			writer := httptest.NewRecorder()
			server.route(writer, request)
			if scenario == "exact" {
				if writer.Code != http.StatusOK || writer.Body.String() != runtimeFileFixtureText || writer.Header().Get("X-Kodex-Artifact-Digest") != file.Digest {
					t.Fatalf("exact catalog body rejected: %d", writer.Code)
				}
			} else if writer.Code < 400 || strings.Contains(writer.Body.String(), runtimeFileFixtureText) {
				t.Fatalf("invalid catalog transfer exposed bytes: %d", writer.Code)
			}
			owner.mu.Lock()
			reads, transfers := owner.reads, owner.transfers
			owner.mu.Unlock()
			if scenario == "exact" || scenario == "partial stream" {
				if reads != 1 || transfers != 1 {
					t.Fatal("catalog body did not use both exact owner paths")
				}
			} else if scenario == "foreign metadata" {
				if reads != 1 || transfers != 0 {
					t.Fatal("foreign metadata opened a body stream")
				}
			} else if reads != 0 || transfers != 0 {
				t.Fatal("invalid selector or authentication reached owner")
			}
			if len(server.spool.slots) != 0 {
				t.Fatal("HTTP response retained a spool slot")
			}
		})
	}
}

// check asserts a short request deadline and exact lease/fence/generation/catalog/
// purpose context, then increments the read count. Assertion failure records a test
// error rather than implementing a real server authorization decision.
// check проверяет short request deadline и exact lease/fence/generation/catalog/purpose
// context,затем увеличивает read count. Ошибка записывается как test error,
// не как реальное server authorization decision.
func (fixture *runtimeFilesOwnerFixture) check(ctx context.Context, execution *cp.ExecutionFileContext) {
	fixture.t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 3*time.Second || execution.GetLeaseRef() != fixture.input.LeaseRef || execution.GetFence() != fixture.input.LeaseFence ||
		execution.GetGeneration() != fixture.input.LeaseGeneration || execution.GetCatalogRef() != fixture.catalog.Ref || execution.GetCatalogDigest() != fixture.catalog.Digest || execution.GetPurpose() != fixture.file.Purpose {
		fixture.t.Error("generated file RPC lost exact execution authority or deadline")
	}
	fixture.mu.Lock()
	fixture.reads++
	fixture.mu.Unlock()
}

// SearchExecutionFiles asserts exact execution context and the expected search
// query/default page size, then returns one synthetic descriptor and empty cursor.
// SearchExecutionFiles проверяет exact execution context и expected search query/default
// page size,возвращая synthetic descriptor с empty cursor.
func (fixture *runtimeFilesOwnerFixture) SearchExecutionFiles(ctx context.Context, request *cp.SearchExecutionFilesRequest) (*cp.SearchExecutionFilesResponse, error) {
	fixture.check(ctx, request.GetContext())
	if request.GetQuery() != "synthetic" || request.GetPage().GetPageSize() != 20 {
		fixture.t.Error("search query or page was not forwarded")
	}
	return &cp.SearchExecutionFilesResponse{Catalog: fixture.catalog, Items: []*cp.ExecutionFileDescriptor{fixture.file}, Total: 1, Page: &cp.PageInfo{}}, nil
}

// GetExecutionFileManifest checks execution pins and returns the fixed catalog
// and one synthetic descriptor without enumerating any real repository or mailbox.
// GetExecutionFileManifest проверяет execution pins и возвращает fixed catalog
// с synthetic descriptor без обхода реального repository и mailbox.
func (fixture *runtimeFilesOwnerFixture) GetExecutionFileManifest(ctx context.Context, request *cp.GetExecutionFileManifestRequest) (*cp.GetExecutionFileManifestResponse, error) {
	fixture.check(ctx, request.GetContext())
	return &cp.GetExecutionFileManifestResponse{Catalog: fixture.catalog, Items: []*cp.ExecutionFileDescriptor{fixture.file}, Total: 1, Page: &cp.PageInfo{}}, nil
}

// GetExecutionFileMetadata checks exact execution and selected file pins before
// returning fixture pointers; it does not make defensive copies or current domain reads.
// GetExecutionFileMetadata проверяет exact execution/selected file pins до fixture
// pointers без defensive copies и current domain reads.
func (fixture *runtimeFilesOwnerFixture) GetExecutionFileMetadata(ctx context.Context, request *cp.GetExecutionFileMetadataRequest) (*cp.GetExecutionFileMetadataResponse, error) {
	fixture.check(ctx, request.GetContext())
	fixture.checkFile(request.GetFile())
	return &cp.GetExecutionFileMetadataResponse{Catalog: fixture.catalog, File: fixture.file}, nil
}

// PreviewExecutionFile checks context/file pins and the expected default/large byte
// bound, returning either a configured response pointer or the complete synthetic text.
// PreviewExecutionFile проверяет context/file pins и expected default/large byte bound,
// выдавая configured response pointer либо complete synthetic text.
func (fixture *runtimeFilesOwnerFixture) PreviewExecutionFile(ctx context.Context, request *cp.PreviewExecutionFileRequest) (*cp.PreviewExecutionFileResponse, error) {
	fixture.check(ctx, request.GetContext())
	fixture.checkFile(request.GetFile())
	if fixture.preview != nil {
		if request.GetMaximumBytes() != 16384 {
			fixture.t.Error("large preview bound was not forwarded")
		}
		return fixture.preview, nil
	}
	if request.GetMaximumBytes() != 4096 {
		fixture.t.Error("preview bound was not forwarded")
	}
	return &cp.PreviewExecutionFileResponse{Catalog: fixture.catalog, File: fixture.file, Text: runtimeFileFixtureText, PreviewDigest: fixture.file.Digest}, nil
}

// checkFile asserts exact captured entry/artifact/revision/digest selection without
// silently resolving an alternative file or treating a matching name as authority.
// checkFile проверяет exact captured entry/artifact/revision/digest selection без
// неявного выбора другого file и признания matching name источником authority.
func (fixture *runtimeFilesOwnerFixture) checkFile(exact *cp.ExecutionFileRef) {
	if exact.GetEntryRef() != fixture.file.EntryRef || exact.GetArtifactRef() != fixture.file.ArtifactRef || exact.GetRevision() != fixture.file.Revision || exact.GetDigest() != fixture.file.Digest {
		fixture.t.Error("file RPC lost the exact selected entry")
	}
}

// RecordRunToolCall checks lease/catalog-bound readonly activity without file content,
// counts the attempt and optionally fails audit. A successful fixed event ref is only
// fixture evidence, not a durable owner transaction.
// RecordRunToolCall проверяет lease/catalog-bound readonly activity без file content,
// считает attempt и при настройке отклоняет audit. Fixed event ref — лишь fixture
// evidence,не durable owner transaction.
func (fixture *runtimeFilesOwnerFixture) RecordRunToolCall(_ context.Context, request *cp.RecordRunToolCallRequest) (*cp.RecordRunToolCallResponse, error) {
	if request.GetLeaseRef() != fixture.input.LeaseRef || request.GetFence() != fixture.input.LeaseFence || request.GetGeneration() != fixture.input.LeaseGeneration ||
		request.GetGrantRef() != fixture.catalog.Ref || request.GetCapabilityRef() != "" || !runtimecontract.IsRuntimeFileTool(request.GetTool()) ||
		len(request.GetSafeParameters().GetFields()) != 1 || request.GetSafeParameters().GetFields()["purpose"].GetStringValue() != runtimecontract.FilePurposeProject ||
		strings.Contains(request.GetSafeResult(), runtimeFileFixtureText) {
		fixture.t.Error("file activity contains payload or lost the readonly catalog grant")
	}
	fixture.mu.Lock()
	fixture.audits++
	fail := fixture.failAudit
	fixture.mu.Unlock()
	if fail {
		return nil, status.Error(codes.Unavailable, "synthetic audit unavailable")
	}
	return &cp.RecordRunToolCallResponse{Event: &cp.RunEvent{Ref: "evt_filefixture1"}}, nil
}

// runtimeFilesOwnerConnection serves the supplied fake on a private one-MiB
// bufconn listener using the existing per-scenario passthrough target. Cleanup
// closes the client first, then stops the server, closes the listener and joins Serve; a
// construction failure still runs server cleanup. Plaintext is in-memory only,
// not a production transport profile or proof of mTLS/owner authorization.
// runtimeFilesOwnerConnection запускает fake на private one-MiB bufconn listener
// с прежним per-scenario passthrough target. Cleanup сначала закрывает client,
// затем останавливает server,закрывает listener и join Serve; при construction failure
// server cleanup сохраняется. Plaintext только in-memory,не production profile
// и не доказательство mTLS/owner authorization.
func runtimeFilesOwnerConnection(t *testing.T, owner cp.RuntimeWorkServiceServer, target string) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	upstream := grpc.NewServer()
	cp.RegisterRuntimeWorkServiceServer(upstream, owner)
	done := make(chan error, 1)
	go func() { done <- upstream.Serve(listener) }()
	t.Cleanup(func() { upstream.Stop(); _ = listener.Close(); <-done })
	connection, err := grpc.NewClient(target, grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}

// fileFixture creates one canonical synthetic project-file descriptor and matching
// catalog from the supplied runtime ref/digest; it hashes actual fixture bytes and
// does not allocate a real catalog grant or artifact storage record.
// fileFixture создаёт canonical synthetic project-file descriptor и matching catalog
// из runtime ref/digest,хешируя actual fixture bytes без создания реального
// catalog grant и artifact storage record.
func fileFixture(input runtimecontract.RunnerInput) (*cp.RuntimeFileCatalog, *cp.ExecutionFileDescriptor) {
	sum := sha256.Sum256([]byte(runtimeFileFixtureText))
	catalog := &cp.RuntimeFileCatalog{Ref: input.FileCatalog.Ref, Digest: input.FileCatalog.Digest, Total: 1, Purposes: []cp.RuntimeFilePurpose{cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_PROJECT}}
	file := &cp.ExecutionFileDescriptor{EntryRef: runtimeFileFixtureEntry, ArtifactRef: "art_filefixture1", Revision: 1, Version: 1,
		Digest: "sha256:" + hex.EncodeToString(sum[:]), Name: "synthetic.txt", MediaType: "text/plain", SizeBytes: int64(len(runtimeFileFixtureText)),
		Purpose: cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_PROJECT, ProjectRef: input.ProjectRef, Source: "CONTROL_CENTER"}
	return catalog, file
}

// TestRuntimeFileToolsUseAuthenticatedCallbackAndGeneratedRPC drives four tools
// through local authenticated HTTP and generated bufconn stubs, checks exact owner
// request/audit counts and rejects malformed input without reflected payload. A failed
// RUNNING audit must prevent reads; in-memory plaintext is not live transport proof.
// TestRuntimeFileToolsUseAuthenticatedCallbackAndGeneratedRPC проводит четыре tools
// через local authenticated HTTP/generated bufconn stubs,проверяет exact owner
// request/audit counts и отказ malformed input без reflected payload. Failed RUNNING
// audit запрещает read; in-memory plaintext не доказывает live transport.
func TestRuntimeFileToolsUseAuthenticatedCallbackAndGeneratedRPC(t *testing.T) {
	manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t, func(input *runtimecontract.RunnerInput) {
		input.Capabilities = append(input.Capabilities, runtimecontract.ArtifactCapability)
		input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeProject}}
	})
	catalog, file := fileFixture(input)
	owner := &runtimeFilesOwnerFixture{t: t, input: input, catalog: catalog, file: file}
	connection := runtimeFilesOwnerConnection(t, owner, "passthrough:///runtime-files-fixture")
	var diagnostic bytes.Buffer
	server := &Server{manager: manager, config: Config{RequestTimeout: 2 * time.Second}, logger: slog.New(slog.NewTextHandler(&diagnostic, nil)),
		control: &controlplaneclient.Client{Runtime: cp.NewRuntimeWorkServiceClient(connection)}}
	invoke := func(tool string, fail bool) {
		t.Helper()
		arguments := map[string]any{"purpose": runtimecontract.FilePurposeProject}
		if tool == runtimecontract.FileToolSearch {
			arguments["query"] = "synthetic"
		}
		if tool == runtimecontract.FileToolMetadata || tool == runtimecontract.FileToolPreview {
			arguments["entry_ref"], arguments["artifact_ref"], arguments["revision"], arguments["digest"] = file.EntryRef, file.ArtifactRef, float64(file.Revision), file.Digest
		}
		raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": tool, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": arguments}})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/mcp", strings.NewReader(string(raw)))
		request.Header.Set("Authorization", "Bearer "+ticket)
		bindTestExecutionHeaders(request, input, "mcp")
		writer := httptest.NewRecorder()
		server.route(writer, request)
		var response struct {
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				IsError    bool           `json:"isError"`
				Structured map[string]any `json:"structuredContent"`
			} `json:"result"`
		}
		if writer.Code != http.StatusOK || json.Unmarshal(writer.Body.Bytes(), &response) != nil {
			t.Fatal("MCP file response is invalid")
		}
		if fail {
			if response.Error == nil || response.Error.Code != -32603 || response.Error.Message != "Tool authorization unavailable" || response.Result.Structured != nil {
				t.Fatal("failed RUNNING activity did not reject file read before effect")
			}
		} else if response.Error != nil || response.Result.IsError || response.Result.Structured == nil {
			t.Fatalf("MCP file response status=%d, isError=%t, expected failure=%t", writer.Code, response.Result.IsError, fail)
		}
		if fail && strings.Contains(writer.Body.String(), runtimeFileFixtureText) {
			t.Fatal("audit failure exposed file content")
		}
	}
	for _, tool := range []string{runtimecontract.FileToolSearch, runtimecontract.FileToolMetadata, runtimecontract.FileToolPreview, runtimecontract.FileToolManifest} {
		invoke(tool, false)
	}
	privateInput := "PRIVATE_FILE_BODY_AND_CREDENTIAL"
	badParams, err := json.Marshal(map[string]any{"name": runtimecontract.FileToolPreview, "arguments": map[string]any{
		"purpose": runtimecontract.FilePurposeProject, "entry_ref": privateInput, "artifact_ref": file.ArtifactRef,
		"revision": 1, "digest": privateInput,
	}})
	if err != nil {
		t.Fatal(err)
	}
	writer := httptest.NewRecorder()
	server.callTool(writer, httptest.NewRequest(http.MethodPost, "/mcp", nil), mcpRequest{ID: json.RawMessage(`"private-fixture"`), Params: badParams}, input)
	if !strings.Contains(diagnostic.String(), "failure_class="+runtimeFileInputFailureClass) || strings.Contains(diagnostic.String(), privateInput) ||
		strings.Contains(diagnostic.String(), file.ArtifactRef) || strings.Contains(writer.Body.String(), privateInput) || !strings.Contains(writer.Body.String(), runtimeFileInputInvalidCode) {
		t.Fatal("file input failure diagnostic leaked input or omitted correction guidance")
	}
	owner.mu.Lock()
	if owner.reads != 4 || owner.audits != 10 {
		t.Error("file calls omitted a generated RPC or durable activity")
	}
	owner.failAudit = true
	owner.mu.Unlock()
	invoke(runtimecontract.FileToolPreview, true)
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.reads != 4 {
		t.Fatal("failed RUNNING activity performed a file read")
	}
}

// TestRuntimeFileReplyAndInputSubstitutionFailClosed mutates catalog/file selection,
// duplicate/cursor pages and caller authority fields, requiring closed rejection.
// It also checks tool advertisement depends on the complete execution/catalog snapshot.
// TestRuntimeFileReplyAndInputSubstitutionFailClosed меняет catalog/file selection,
// duplicate/cursor pages/caller authority fields,требуя closed rejection,
// и проверяет зависимость discovery от полного execution/catalog snapshot.
func TestRuntimeFileReplyAndInputSubstitutionFailClosed(t *testing.T) {
	input := validWarmExecutionInput()
	input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeProject}}
	catalog, file := fileFixture(input)
	exact := &cp.ExecutionFileRef{EntryRef: file.EntryRef, ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: file.Digest}
	for name, mutate := range map[string]func(*cp.RuntimeFileCatalog, *cp.ExecutionFileDescriptor){
		"catalog ref":    func(c *cp.RuntimeFileCatalog, _ *cp.ExecutionFileDescriptor) { c.Ref = "vfc_otherfixture" },
		"catalog digest": func(c *cp.RuntimeFileCatalog, _ *cp.ExecutionFileDescriptor) { c.Digest = strings.Repeat("f", 64) },
		"catalog count":  func(c *cp.RuntimeFileCatalog, _ *cp.ExecutionFileDescriptor) { c.Total++ },
		"catalog purpose": func(c *cp.RuntimeFileCatalog, _ *cp.ExecutionFileDescriptor) {
			c.Purposes[0] = cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_SKILL
		},
		"entry":    func(_ *cp.RuntimeFileCatalog, f *cp.ExecutionFileDescriptor) { f.EntryRef = runtimeFileForeignEntry },
		"artifact": func(_ *cp.RuntimeFileCatalog, f *cp.ExecutionFileDescriptor) { f.ArtifactRef = "art_otherfixture" },
		"revision": func(_ *cp.RuntimeFileCatalog, f *cp.ExecutionFileDescriptor) { f.Revision++ },
		"digest": func(_ *cp.RuntimeFileCatalog, f *cp.ExecutionFileDescriptor) {
			f.Digest = "sha256:" + strings.Repeat("f", 64)
		},
		"project": func(_ *cp.RuntimeFileCatalog, f *cp.ExecutionFileDescriptor) { f.ProjectRef = "prj_otherfixture" },
		"purpose": func(_ *cp.RuntimeFileCatalog, f *cp.ExecutionFileDescriptor) {
			f.Purpose = cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_SKILL
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, f := proto.Clone(catalog).(*cp.RuntimeFileCatalog), proto.Clone(file).(*cp.ExecutionFileDescriptor)
			mutate(c, f)
			if _, err := exactFileResult(input, file.Purpose, c, f, exact); err == nil {
				t.Fatal("substituted file response accepted")
			}
		})
	}
	if _, err := filePageResult(input, file.Purpose, catalog, []*cp.ExecutionFileDescriptor{file, file}, 1, &cp.PageInfo{}, 20); err == nil {
		t.Fatal("duplicate page accepted")
	}
	if _, err := filePageResult(input, file.Purpose, catalog, []*cp.ExecutionFileDescriptor{file}, 1, &cp.PageInfo{NextPageToken: "cursor"}, 20); err == nil {
		t.Fatal("inconsistent page cursor accepted")
	}
	server := &Server{config: Config{RequestTimeout: time.Second}}
	for key, value := range map[string]any{"project_ref": "prj_otherfixture", "lease_ref": "lse_otherfixture", "path": "/etc/passwd", "page_size": float64(101), "cursor": strings.Repeat("a", 513), "query": float64(5)} {
		arguments := map[string]any{"purpose": runtimecontract.FilePurposeProject, key: value}
		if _, err := server.callFileTool(t.Context(), input, runtimecontract.FileToolSearch, arguments); err == nil {
			t.Fatalf("invalid argument %s accepted", key)
		}
	}
	if _, err := server.callFileTool(t.Context(), input, runtimecontract.FileToolSearch, map[string]any{"purpose": runtimecontract.FilePurposeSkill}); err == nil {
		t.Fatal("unadvertised purpose accepted")
	}
	for _, mutation := range []func(*runtimecontract.RunnerInput){
		func(i *runtimecontract.RunnerInput) { i.FileCatalog = nil },
		func(i *runtimecontract.RunnerInput) { i.Mode = runtimecontract.RunnerModeWarm },
		func(i *runtimecontract.RunnerInput) { i.ProjectRef = "" },
		func(i *runtimecontract.RunnerInput) { i.LeaseGeneration = 0 },
	} {
		candidate := input
		mutation(&candidate)
		if len(runtimeFileTools(candidate)) != 0 {
			t.Fatal("file tools advertised without execution grant")
		}
	}
	available := runtimeFileTools(input)
	if len(available) != 5 {
		t.Fatal("exact execution omitted file tools")
	}
	for _, tool := range available {
		schema := tool["inputSchema"].(map[string]any)
		if schema["additionalProperties"] != false {
			t.Fatal("file tool allows arbitrary authority fields")
		}
	}
}

// TestRuntimeFileToolsExplainCatalogLocalFileSelection checks every file-tool
// description retains bounded own-catalog/provenance/full-discovery guidance without
// adding caller catalog/lease/path authority fields to the closed schemas.
// TestRuntimeFileToolsExplainCatalogLocalFileSelection проверяет bounded own-catalog/
// provenance/full-discovery guidance каждого file tool без добавления caller
// catalog/lease/path authority fields в closed schemas.
func TestRuntimeFileToolsExplainCatalogLocalFileSelection(t *testing.T) {
	input := validWarmExecutionInput()
	input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 216, Purposes: []string{runtimecontract.FilePurposeRunResult}}
	count := 0
	for _, tool := range tools(input) {
		name := tool["name"].(string)
		if !runtimecontract.IsRuntimeFileTool(name) {
			continue
		}
		count++
		description := tool["description"].(string)
		for _, hint := range []string{
			"never copy from parent, sibling or another turn/attempt",
			"own search_files by file name (not artifact_ref) or get_file_manifest pages", "matched own entry_ref",
			"artifact_ref/revision/digest", "Follow next_cursor with the same purpose/query",
			"page_size=100 is not the whole catalog", "Missing exact pins or remote denial stay blocked",
			"no automatic remote-error retry",
			"mounted attachments, not the complete RUN_RESULT catalog", "missing handoff tuple alone",
			"all next_cursor pages", "current root/run/dependency", "run_ref/source", "mandatory content to EOF",
			"actual absence after discovery", "independent authorized discovery",
		} {
			if !strings.Contains(description, hint) {
				t.Fatal("file tool description omitted catalog-local selection guidance")
			}
		}
		if len(description) > 2000 {
			t.Fatal("file selection guidance exceeds the runner description budget")
		}
		schema := tool["inputSchema"].(map[string]any)
		properties := schema["properties"].(map[string]any)
		if schema["additionalProperties"] != false || properties["catalog"] != nil || properties["lease_ref"] != nil || properties["file_name"] != nil {
			t.Fatal("file selection guidance changed the execution input boundary")
		}
	}
	if count != 5 || !strings.Contains(runtimeFileInputGuidance, "Correct the arguments once") ||
		!strings.Contains(runtimeFileInputGuidance, "from your own runtime catalog") {
		t.Fatal("file selection omitted a tool or bounded local input correction")
	}
}

// TestRuntimeFileEntrySchemaMatchesServerOwnedFormat checks exact vfe_ plus 32-hex
// entry schema rejects truncated/uppercase variants while independent opaque artifact/
// run refs keep their original format; syntactic checks do not grant file access.
// TestRuntimeFileEntrySchemaMatchesServerOwnedFormat проверяет exact vfe_ плюс 32-hex
// schema и отказ truncated/uppercase variants при прежней independent opaque artifact/
// run grammar; syntactic checks не дают file access.
func TestRuntimeFileEntrySchemaMatchesServerOwnedFormat(t *testing.T) {
	input := validWarmExecutionInput()
	input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeProject}}
	for _, tool := range runtimeFileTools(input) {
		properties := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		entry, exists := properties["entry_ref"]
		if !exists {
			continue
		}
		schema := entry.(map[string]any)
		if schema["type"] != "string" || schema["minLength"] != 36 || schema["maxLength"] != 36 || schema["pattern"] != `^vfe_[a-f0-9]{32}$` {
			t.Fatal("entry schema differs from the server-owned capture format")
		}
		pattern := regexp.MustCompile(schema["pattern"].(string))
		if !pattern.MatchString(runtimeFileFixtureEntry) || pattern.MatchString("vfe_897a44d8dd71438d9dd975a7b766ee") || pattern.MatchString(strings.ToUpper(runtimeFileFixtureEntry)) {
			t.Fatal("entry schema accepts a truncated or noncanonical selector")
		}
		artifact := properties["artifact_ref"].(map[string]any)
		if artifact["maxLength"] != 96 || !validFileRef("art_opaque_A-Z", "art_") || !validFileRef("run_opaque_A-Z", "run_") {
			t.Fatal("entry validation changed independent opaque artifact or run refs")
		}
	}
}

// TestRuntimeFileMalformedEntryStopsBeforeRemoteRead exercises malformed entries
// for metadata/preview/read, requiring bounded corrective failure, no owner read and
// the existing failed activity shape instead of guessed or rewritten entry bytes.
// TestRuntimeFileMalformedEntryStopsBeforeRemoteRead проверяет malformed entries
// metadata/preview/read: bounded corrective failure,нет owner read,сохраняется failed
// activity shape вместо guessed/rewritten entry bytes.
func TestRuntimeFileMalformedEntryStopsBeforeRemoteRead(t *testing.T) {
	for _, tool := range []string{runtimecontract.FileToolMetadata, runtimecontract.FileToolPreview, runtimecontract.FileToolRead} {
		for _, entry := range []string{"vfe_897a44d8dd71438d9dd975a7b766ee", "vfe_" + strings.Repeat("a", 31), "vfe_" + strings.Repeat("a", 33), "vfe_" + strings.Repeat("A", 32), "vfe_" + strings.Repeat("g", 32)} {
			t.Run(tool+"/"+entry, func(t *testing.T) {
				input := validWarmExecutionInput()
				input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeProject}}
				_, file := fileFixture(input)
				client := &runtimeFileMetadataFailureClient{code: codes.NotFound}
				server := &Server{config: Config{RequestTimeout: time.Second, FileTransferTimeout: time.Second},
					control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
				params, err := json.Marshal(map[string]any{"name": tool, "arguments": map[string]any{
					"purpose": runtimecontract.FilePurposeProject, "entry_ref": entry, "artifact_ref": file.ArtifactRef,
					"revision": file.Revision, "digest": file.Digest,
				}})
				if err != nil {
					t.Fatal(err)
				}
				writer := httptest.NewRecorder()
				server.callTool(writer, httptest.NewRequest(http.MethodPost, "/mcp", nil), mcpRequest{ID: json.RawMessage(`"malformed-entry"`), Params: params}, input)
				var response struct {
					Result struct {
						IsError    bool           `json:"isError"`
						Structured map[string]any `json:"structuredContent"`
					} `json:"result"`
				}
				if json.Unmarshal(writer.Body.Bytes(), &response) != nil || !response.Result.IsError ||
					response.Result.Structured["error_code"] != runtimeFileInputInvalidCode || response.Result.Structured["retryable"] != true ||
					response.Result.Structured["guidance"] != runtimeFileInputGuidance || len(client.requests)+len(client.previews) != 0 || len(client.projections) != 2 ||
					client.projections[1].GetState() != cp.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED {
					t.Fatal("malformed entry reached the owner or lost bounded local correction")
				}
			})
		}
	}
}

// runtimeFileMetadataFailureClient records local metadata/preview/audit calls and
// injects a selected remote status with private text; embedded unused RPCs must not run.
// runtimeFileMetadataFailureClient записывает local metadata/preview/audit calls
// и подставляет remote status с private text; embedded unused RPCs не вызываются.
type runtimeFileMetadataFailureClient struct {
	cp.RuntimeWorkServiceClient
	code        codes.Code
	requests    []*cp.GetExecutionFileMetadataRequest
	previews    []*cp.PreviewExecutionFileRequest
	projections []*cp.RecordRunToolCallRequest
}

// GetExecutionFileMetadata captures the request pointer and returns one configured
// status with a private canary, without choosing another catalog entry.
// GetExecutionFileMetadata сохраняет request pointer и возвращает configured status
// с private canary,не выбирая другую catalog entry.
func (client *runtimeFileMetadataFailureClient) GetExecutionFileMetadata(_ context.Context, request *cp.GetExecutionFileMetadataRequest, _ ...grpc.CallOption) (*cp.GetExecutionFileMetadataResponse, error) {
	client.requests = append(client.requests, request)
	return nil, status.Error(client.code, "PRIVATE_FILE_METADATA_FAILURE")
}

// PreviewExecutionFile captures the exact request and injects the same remote failure
// used to verify that preview errors are not locally correctable input refusals.
// PreviewExecutionFile сохраняет exact request и подставляет remote failure для проверки
// отличия preview errors от locally correctable input refusals.
func (client *runtimeFileMetadataFailureClient) PreviewExecutionFile(_ context.Context, request *cp.PreviewExecutionFileRequest, _ ...grpc.CallOption) (*cp.PreviewExecutionFileResponse, error) {
	client.previews = append(client.previews, request)
	return nil, status.Error(client.code, "PRIVATE_FILE_METADATA_FAILURE")
}

// RecordRunToolCall records each safe activity attempt and returns a synthetic
// event ref without persisting a real audit record.
// RecordRunToolCall записывает safe activity attempt и возвращает synthetic event ref
// без сохранения real audit record.
func (client *runtimeFileMetadataFailureClient) RecordRunToolCall(_ context.Context, request *cp.RecordRunToolCallRequest, _ ...grpc.CallOption) (*cp.RecordRunToolCallResponse, error) {
	client.projections = append(client.projections, request)
	return &cp.RecordRunToolCallResponse{Event: &cp.RunEvent{Ref: "evt_filefixture1"}}, nil
}

// TestRuntimeFileRemoteFailureHasNoCorrectionOrRetry checks four remote status classes
// across metadata/preview/read produce one owner call, no corrective guidance/replay,
// preserved exact execution pins and no private error disclosure in wire/logs.
// TestRuntimeFileRemoteFailureHasNoCorrectionOrRetry проверяет четыре remote status classes
// metadata/preview/read: один owner call,без corrective guidance/replay,с exact execution
// pins и без private error disclosure в wire/logs.
func TestRuntimeFileRemoteFailureHasNoCorrectionOrRetry(t *testing.T) {
	for _, tool := range []string{runtimecontract.FileToolMetadata, runtimecontract.FileToolPreview, runtimecontract.FileToolRead} {
		for _, code := range []codes.Code{codes.NotFound, codes.PermissionDenied, codes.Unknown, codes.Unavailable} {
			t.Run(tool+"/"+code.String(), func(t *testing.T) {
				input := validWarmExecutionInput()
				input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 216, Purposes: []string{runtimecontract.FilePurposeRunResult}}
				_, file := fileFixture(input)
				client := &runtimeFileMetadataFailureClient{code: code}
				var diagnostic bytes.Buffer
				server := &Server{config: Config{RequestTimeout: time.Second, FileTransferTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client},
					logger: slog.New(slog.NewJSONHandler(&diagnostic, nil))}
				params, err := json.Marshal(map[string]any{"name": tool, "arguments": map[string]any{
					"purpose": runtimecontract.FilePurposeRunResult, "entry_ref": runtimeFileForeignEntry, "artifact_ref": file.ArtifactRef,
					"revision": file.Revision, "digest": file.Digest,
				}})
				if err != nil {
					t.Fatal("fixture encoding failed")
				}
				writer := httptest.NewRecorder()
				server.callTool(writer, httptest.NewRequest(http.MethodPost, "/mcp", nil), mcpRequest{ID: json.RawMessage(`"metadata-fixture"`), Params: params}, input)
				var response struct {
					Result struct {
						IsError    bool           `json:"isError"`
						Structured map[string]any `json:"structuredContent"`
					} `json:"result"`
				}
				if json.Unmarshal(writer.Body.Bytes(), &response) != nil || !response.Result.IsError ||
					response.Result.Structured["error_code"] != "TOOL_UNAVAILABLE" || response.Result.Structured["retryable"] != false ||
					response.Result.Structured["guidance"] != nil || len(client.requests)+len(client.previews) != 1 || len(client.projections) != 2 ||
					client.projections[1].State != cp.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED {
					t.Fatal("remote file failure gained correction guidance or automatic replay")
				}
				var execution *cp.ExecutionFileContext
				var exact *cp.ExecutionFileRef
				if tool == runtimecontract.FileToolPreview {
					execution, exact = client.previews[0].GetContext(), client.previews[0].GetFile()
				} else {
					execution, exact = client.requests[0].GetContext(), client.requests[0].GetFile()
				}
				if execution.GetLeaseRef() != input.LeaseRef || execution.GetFence() != input.LeaseFence || execution.GetGeneration() != input.LeaseGeneration ||
					execution.GetCatalogRef() != input.FileCatalog.Ref || execution.GetCatalogDigest() != input.FileCatalog.Digest ||
					exact.GetEntryRef() != runtimeFileForeignEntry ||
					strings.Contains(writer.Body.String(), "PRIVATE_FILE_METADATA_FAILURE") || strings.Contains(diagnostic.String(), "PRIVATE_FILE_METADATA_FAILURE") {
					t.Fatal("file handoff changed execution pins, selected for the caller or exposed private error data")
				}
			})
		}
	}
}

// TestRuntimeRunResultPreviewBoundsAndBinding uses generated in-memory RPC to check
// complete/large/truncated Unicode previews, source/page commitments, caller bounds
// and closed failures. Invalid protobuf UTF-8 is rejected at transport, distinct from
// callback metadata/content checks; no real source bytes are fetched.
// TestRuntimeRunResultPreviewBoundsAndBinding через generated in-memory RPC проверяет
// complete/large/truncated Unicode previews,source/page commitments,caller bounds
// и closed failures. Invalid protobuf UTF-8 отвергается transport отдельно от callback
// metadata/content checks; real source bytes не запрашиваются.
func TestRuntimeRunResultPreviewBoundsAndBinding(t *testing.T) {
	for _, scenario := range []string{"full", "26276 bytes", "39301 bytes", "UTF8 boundary", "wrong digest", "wrong catalog", "wrong pin", "wrong size", "oversized", "invalid UTF8", "NUL", "wrong input digest", "string revision", "maximum exceeded"} {
		t.Run(scenario, func(t *testing.T) {
			input := validWarmExecutionInput()
			input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeRunResult}}
			catalog, file := fileFixture(input)
			catalog.Purposes = []cp.RuntimeFilePurpose{cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_RUN_RESULT}
			file.Purpose, file.Source, file.RunRef = catalog.Purposes[0], "AGENT_RESULT", "run_filefixture1"
			text := strings.Repeat("x", 39301)
			if scenario == "full" {
				text = runtimeFileFixtureText
			} else if scenario == "26276 bytes" {
				text = strings.Repeat("x", 26276)
			} else if scenario == "UTF8 boundary" {
				text = strings.Repeat("x", 16383) + "я" + strings.Repeat("x", 9900)
			}
			digest := func(value string) string {
				sum := sha256.Sum256([]byte(value))
				return "sha256:" + hex.EncodeToString(sum[:])
			}
			file.SizeBytes, file.Digest = int64(len(text)), digest(text)
			preview := text
			if len(preview) > 16384 {
				preview = preview[:16384]
				if scenario == "UTF8 boundary" {
					preview = preview[:16383]
				}
			}
			response := &cp.PreviewExecutionFileResponse{Catalog: proto.Clone(catalog).(*cp.RuntimeFileCatalog), File: proto.Clone(file).(*cp.ExecutionFileDescriptor), Text: preview, Truncated: len(text) > 16384, PreviewDigest: digest(preview)}
			arguments := map[string]any{"purpose": runtimecontract.FilePurposeRunResult, "entry_ref": file.EntryRef, "artifact_ref": file.ArtifactRef, "revision": float64(file.Revision), "digest": file.Digest, "maximum_bytes": float64(16384)}
			wantClass := runtimeFileReplyFailureClass
			switch scenario {
			case "full", "26276 bytes", "39301 bytes", "UTF8 boundary":
				wantClass = ""
			case "wrong digest":
				response.PreviewDigest = digest("different preview")
			case "wrong catalog":
				response.Catalog.Total++
			case "wrong pin":
				response.File.Revision++
			case "wrong size":
				response.File.SizeBytes = int64(len(preview))
			case "oversized":
				response.Text = strings.Repeat("x", 16385)
				response.PreviewDigest = digest(response.Text)
			case "invalid UTF8":
				response.Text = string([]byte{0xff})
				response.PreviewDigest = digest(response.Text)
				// Proto string закрыто отклоняется до callback validation.
				wantClass = "control_internal"
			case "NUL":
				response.Text = "x\x00x"
				response.PreviewDigest = digest(response.Text)
			case "wrong input digest":
				arguments["digest"] = strings.TrimPrefix(file.Digest, "sha256:")
				wantClass = runtimeFileInputFailureClass
			case "string revision":
				arguments["revision"] = "1"
				wantClass = runtimeFileInputFailureClass
			case "maximum exceeded":
				arguments["maximum_bytes"] = float64(16385)
				wantClass = runtimeFileInputFailureClass
			}
			owner := &runtimeFilesOwnerFixture{t: t, input: input, catalog: catalog, file: file, preview: response}
			connection := runtimeFilesOwnerConnection(t, owner, "passthrough:///run-result-preview-fixture")
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: cp.NewRuntimeWorkServiceClient(connection)}}
			rawResult, err := server.callFileTool(t.Context(), input, runtimecontract.FileToolPreview, arguments)
			result, _ := rawResult.(map[string]any)
			if wantClass == "" {
				if err != nil || result["text"] != preview || result["truncated"] != response.Truncated || result["preview_digest"] != digest(preview) {
					t.Fatal("exact bounded RUN_RESULT preview rejected")
				}
			} else if err == nil || result != nil || controlFailureClass(err) != wantClass {
				t.Fatal("invalid preview did not preserve its closed rejection class")
			}
			if wantClass == runtimeFileInputFailureClass && owner.reads != 0 {
				t.Fatal("invalid arguments reached the owner RPC")
			}
		})
	}
}

// TestRuntimeFileFailureClassDoesNotExposeNestedErrors checks wrapped local sentinels
// map to fixed file classes while arbitrary private nested errors remain control_unknown.
// TestRuntimeFileFailureClassDoesNotExposeNestedErrors проверяет fixed file classes
// wrapped local sentinels и control_unknown для arbitrary private nested errors.
func TestRuntimeFileFailureClassDoesNotExposeNestedErrors(t *testing.T) {
	for _, scenario := range []struct {
		err   error
		class string
	}{
		{errors.Join(errors.New("PRIVATE_FILE_BODY_AND_CREDENTIAL"), errRuntimeFileInput), runtimeFileInputFailureClass},
		{errors.Join(errors.New("PRIVATE_FILE_BODY_AND_CREDENTIAL"), errRuntimeFileReply), runtimeFileReplyFailureClass},
		{errors.New("PRIVATE_FILE_BODY_AND_CREDENTIAL"), "control_unknown"},
	} {
		if got := controlFailureClass(scenario.err); got != scenario.class || strings.Contains(got, "PRIVATE") {
			t.Fatal("file failure classification exposed an unregistered value")
		}
	}
}
