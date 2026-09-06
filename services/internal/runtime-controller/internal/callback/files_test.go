package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

const runtimeFileFixtureText = "synthetic file preview"

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
}

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

func TestCatalogBodyUsesMetadataPinAndGeneratedStreamBeforeExposingBytes(t *testing.T) {
	for _, scenario := range []string{"exact", "foreign project query", "duplicate purpose", "wrong purpose", "foreign metadata", "partial stream", "missing ticket"} {
		t.Run(scenario, func(t *testing.T) {
			manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t, func(input *runtimecontract.RunnerInput) {
				input.Capabilities = append(input.Capabilities, runtimecontract.ArtifactCapability)
				input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeProject}}
			})
			catalog, file := fileFixture(input)
			owner := &runtimeFilesOwnerFixture{t: t, input: input, catalog: catalog, file: file, failTransfer: scenario == "partial stream"}
			listener := bufconn.Listen(1 << 20)
			upstream := grpc.NewServer()
			cp.RegisterRuntimeWorkServiceServer(upstream, owner)
			done := make(chan error, 1)
			go func() { done <- upstream.Serve(listener) }()
			t.Cleanup(func() { upstream.Stop(); _ = listener.Close(); <-done })
			connection, err := grpc.NewClient("passthrough:///catalog-transfer-fixture", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = connection.Close() })
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

func (fixture *runtimeFilesOwnerFixture) SearchExecutionFiles(ctx context.Context, request *cp.SearchExecutionFilesRequest) (*cp.SearchExecutionFilesResponse, error) {
	fixture.check(ctx, request.GetContext())
	if request.GetQuery() != "synthetic" || request.GetPage().GetPageSize() != 20 {
		fixture.t.Error("search query or page was not forwarded")
	}
	return &cp.SearchExecutionFilesResponse{Catalog: fixture.catalog, Items: []*cp.ExecutionFileDescriptor{fixture.file}, Total: 1, Page: &cp.PageInfo{}}, nil
}

func (fixture *runtimeFilesOwnerFixture) GetExecutionFileManifest(ctx context.Context, request *cp.GetExecutionFileManifestRequest) (*cp.GetExecutionFileManifestResponse, error) {
	fixture.check(ctx, request.GetContext())
	return &cp.GetExecutionFileManifestResponse{Catalog: fixture.catalog, Items: []*cp.ExecutionFileDescriptor{fixture.file}, Total: 1, Page: &cp.PageInfo{}}, nil
}

func (fixture *runtimeFilesOwnerFixture) GetExecutionFileMetadata(ctx context.Context, request *cp.GetExecutionFileMetadataRequest) (*cp.GetExecutionFileMetadataResponse, error) {
	fixture.check(ctx, request.GetContext())
	fixture.checkFile(request.GetFile())
	return &cp.GetExecutionFileMetadataResponse{Catalog: fixture.catalog, File: fixture.file}, nil
}

func (fixture *runtimeFilesOwnerFixture) PreviewExecutionFile(ctx context.Context, request *cp.PreviewExecutionFileRequest) (*cp.PreviewExecutionFileResponse, error) {
	fixture.check(ctx, request.GetContext())
	fixture.checkFile(request.GetFile())
	if request.GetMaximumBytes() != 4096 {
		fixture.t.Error("preview bound was not forwarded")
	}
	return &cp.PreviewExecutionFileResponse{Catalog: fixture.catalog, File: fixture.file, Text: runtimeFileFixtureText, PreviewDigest: fixture.file.Digest}, nil
}

func (fixture *runtimeFilesOwnerFixture) checkFile(exact *cp.ExecutionFileRef) {
	if exact.GetEntryRef() != fixture.file.EntryRef || exact.GetArtifactRef() != fixture.file.ArtifactRef || exact.GetRevision() != fixture.file.Revision || exact.GetDigest() != fixture.file.Digest {
		fixture.t.Error("file RPC lost the exact selected entry")
	}
}

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

func fileFixture(input runtimecontract.RunnerInput) (*cp.RuntimeFileCatalog, *cp.ExecutionFileDescriptor) {
	sum := sha256.Sum256([]byte(runtimeFileFixtureText))
	catalog := &cp.RuntimeFileCatalog{Ref: input.FileCatalog.Ref, Digest: input.FileCatalog.Digest, Total: 1, Purposes: []cp.RuntimeFilePurpose{cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_PROJECT}}
	file := &cp.ExecutionFileDescriptor{EntryRef: "vfe_filefixture1", ArtifactRef: "art_filefixture1", Revision: 1, Version: 1,
		Digest: "sha256:" + hex.EncodeToString(sum[:]), Name: "synthetic.txt", MediaType: "text/plain", SizeBytes: int64(len(runtimeFileFixtureText)),
		Purpose: cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_PROJECT, ProjectRef: input.ProjectRef, Source: "CONTROL_CENTER"}
	return catalog, file
}

func TestRuntimeFileToolsUseAuthenticatedCallbackAndGeneratedRPC(t *testing.T) {
	manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t, func(input *runtimecontract.RunnerInput) {
		input.Capabilities = append(input.Capabilities, runtimecontract.ArtifactCapability)
		input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{runtimecontract.FilePurposeProject}}
	})
	catalog, file := fileFixture(input)
	owner := &runtimeFilesOwnerFixture{t: t, input: input, catalog: catalog, file: file}
	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	cp.RegisterRuntimeWorkServiceServer(grpcServer, owner)
	serveDone := make(chan error, 1)
	go func() { serveDone <- grpcServer.Serve(listener) }()
	t.Cleanup(func() { grpcServer.Stop(); _ = listener.Close(); <-serveDone })
	connection, err := grpc.NewClient("passthrough:///runtime-files-fixture", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	server := &Server{manager: manager, config: Config{RequestTimeout: 2 * time.Second}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
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
			Result struct {
				IsError    bool           `json:"isError"`
				Structured map[string]any `json:"structuredContent"`
			} `json:"result"`
		}
		if writer.Code != http.StatusOK || json.Unmarshal(writer.Body.Bytes(), &response) != nil || response.Result.IsError != fail || response.Result.Structured == nil {
			t.Fatalf("MCP file response status=%d, isError=%t, expected failure=%t", writer.Code, response.Result.IsError, fail)
		}
		if fail && strings.Contains(writer.Body.String(), runtimeFileFixtureText) {
			t.Fatal("audit failure exposed file content")
		}
	}
	for _, tool := range []string{runtimecontract.FileToolSearch, runtimecontract.FileToolMetadata, runtimecontract.FileToolPreview, runtimecontract.FileToolManifest} {
		invoke(tool, false)
	}
	owner.mu.Lock()
	if owner.reads != 4 || owner.audits != 4 {
		t.Error("file calls omitted a generated RPC or durable activity")
	}
	owner.failAudit = true
	owner.mu.Unlock()
	invoke(runtimecontract.FileToolPreview, true)
}

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
		"entry":    func(_ *cp.RuntimeFileCatalog, f *cp.ExecutionFileDescriptor) { f.EntryRef = "vfe_otherfixture" },
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
	if len(available) != 4 {
		t.Fatal("exact execution omitted file tools")
	}
	for _, tool := range available {
		schema := tool["inputSchema"].(map[string]any)
		if schema["additionalProperties"] != false {
			t.Fatal("file tool allows arbitrary authority fields")
		}
	}
}
