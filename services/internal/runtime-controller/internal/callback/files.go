package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

const maximumFileToolReplyBytes = 512 << 10

var (
	errRuntimeFileInput      = errors.New("runtime file tool input is invalid")
	errRuntimeFileReply      = errors.New("runtime file tool response binding is invalid")
	runtimeFileRefPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)
	runtimeFileDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

func runtimeFilesAvailable(input runtimecontract.RunnerInput) bool {
	return input.Mode == runtimecontract.RunnerModeTurn && input.ProjectRef != "" && input.LeaseRef != "" &&
		input.LeaseFence != "" && input.LeaseGeneration > 0 && input.FileCatalog != nil && input.FileCatalog.Validate() == nil
}

func runtimeFilePurpose(input runtimecontract.RunnerInput, purpose string) (cp.RuntimeFilePurpose, bool) {
	if !runtimeFilesAvailable(input) || !slices.Contains(input.FileCatalog.Purposes, purpose) {
		return 0, false
	}
	switch purpose {
	case runtimecontract.FilePurposeProject:
		return cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_PROJECT, true
	case runtimecontract.FilePurposeWorkspaceInput:
		return cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_WORKSPACE_INPUT, true
	case runtimecontract.FilePurposeRunResult:
		return cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_RUN_RESULT, true
	case runtimecontract.FilePurposeSkill:
		return cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_SKILL, true
	default:
		return 0, false
	}
}

func runtimeFileTools(input runtimecontract.RunnerInput) []map[string]any {
	if !runtimeFilesAvailable(input) {
		return nil
	}
	result := make([]map[string]any, 0, 4)
	for _, tool := range []struct{ name, description string }{
		{runtimecontract.FileToolSearch, "Search authorized files in this exact runtime catalog. Follow next_cursor within the same purpose and query."},
		{runtimecontract.FileToolMetadata, "Read exact metadata using an entry_ref, artifact_ref, revision and digest returned by this runtime catalog."},
		{runtimecontract.FileToolPreview, "Read a bounded UTF-8 text preview of an exact catalog entry. Binary files do not have text previews."},
		{runtimecontract.FileToolManifest, "Read one page of the immutable runtime file manifest, filtered by current permissions and lifecycle."},
	} {
		properties := map[string]any{"purpose": enumSchema(input.FileCatalog.Purposes...)}
		required := []string{"purpose"}
		if tool.name == runtimecontract.FileToolSearch || tool.name == runtimecontract.FileToolManifest {
			properties["page_size"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 20}
			properties["cursor"] = stringSchema(0, 512)
			if tool.name == runtimecontract.FileToolSearch {
				properties["query"] = stringSchema(0, 200)
			}
		} else {
			properties["entry_ref"], properties["artifact_ref"] = opaqueRefSchema(), opaqueRefSchema()
			properties["revision"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991}
			properties["digest"] = map[string]any{"type": "string", "pattern": runtimeFileDigestPattern.String()}
			required = append(required, "entry_ref", "artifact_ref", "revision", "digest")
			if tool.name == runtimecontract.FileToolPreview {
				properties["maximum_bytes"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 16384, "default": 4096}
			}
		}
		result = append(result, map[string]any{"name": tool.name, "description": tool.description,
			"inputSchema": objectSchema(required, properties)})
	}
	return result
}

func fileInteger(arguments map[string]any, key string, fallback, maximum int64) (int64, bool) {
	raw, exists := arguments[key]
	if !exists {
		return fallback, fallback > 0
	}
	number, ok := raw.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 1 || number > float64(maximum) || math.Trunc(number) != number {
		return 0, false
	}
	return int64(number), true
}

func fileText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum && !strings.ContainsRune(value, '\x00')
}

func (server *Server) callFileTool(ctx context.Context, input runtimecontract.RunnerInput, tool string, arguments map[string]any) (map[string]any, error) {
	purpose, _ := arguments["purpose"].(string)
	purposeValue, ok := runtimeFilePurpose(input, purpose)
	if !ok || !runtimecontract.IsRuntimeFileTool(tool) {
		return nil, errRuntimeFileInput
	}
	// Lease, generation и catalog берутся только из authenticated execution input.
	execution := &cp.ExecutionFileContext{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		CatalogRef: input.FileCatalog.Ref, CatalogDigest: input.FileCatalog.Digest, Purpose: purposeValue}
	ctx, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	options := []grpc.CallOption{grpc.MaxCallRecvMsgSize(maximumFileToolReplyBytes)}
	if tool == runtimecontract.FileToolSearch || tool == runtimecontract.FileToolManifest {
		allowed := []string{"purpose", "page_size", "cursor"}
		if tool == runtimecontract.FileToolSearch {
			allowed = append(allowed, "query")
		}
		pageSize, valid := fileInteger(arguments, "page_size", 20, 100)
		cursor, cursorOK := optionalFileString(arguments, "cursor", 512)
		query, queryOK := optionalFileString(arguments, "query", 200)
		if !onlyKeys(arguments, allowed...) || !valid || !cursorOK || !queryOK {
			return nil, errRuntimeFileInput
		}
		page := &cp.PageRequest{PageSize: int32(pageSize), PageToken: cursor}
		if tool == runtimecontract.FileToolSearch {
			response, err := server.control.Runtime.SearchExecutionFiles(ctx, &cp.SearchExecutionFilesRequest{Context: execution, Query: query, Page: page}, options...)
			if err != nil {
				return nil, err
			}
			return filePageResult(input, purposeValue, response.GetCatalog(), response.GetItems(), response.GetTotal(), response.GetPage(), int(pageSize))
		}
		response, err := server.control.Runtime.GetExecutionFileManifest(ctx, &cp.GetExecutionFileManifestRequest{Context: execution, Page: page}, options...)
		if err != nil {
			return nil, err
		}
		return filePageResult(input, purposeValue, response.GetCatalog(), response.GetItems(), response.GetTotal(), response.GetPage(), int(pageSize))
	}
	allowed := []string{"purpose", "entry_ref", "artifact_ref", "revision", "digest"}
	if tool == runtimecontract.FileToolPreview {
		allowed = append(allowed, "maximum_bytes")
	}
	entry, _ := arguments["entry_ref"].(string)
	artifact, _ := arguments["artifact_ref"].(string)
	digest, _ := arguments["digest"].(string)
	revision, valid := fileInteger(arguments, "revision", 0, 9007199254740991)
	if !onlyKeys(arguments, allowed...) || !valid || !validFileRef(entry, "vfe_") || !validFileRef(artifact, "art_") || !runtimeFileDigestPattern.MatchString(digest) {
		return nil, errRuntimeFileInput
	}
	exact := &cp.ExecutionFileRef{EntryRef: entry, ArtifactRef: artifact, Revision: revision, Digest: digest}
	if tool == runtimecontract.FileToolMetadata {
		response, err := server.control.Runtime.GetExecutionFileMetadata(ctx, &cp.GetExecutionFileMetadataRequest{Context: execution, File: exact}, options...)
		if err != nil {
			return nil, err
		}
		return exactFileResult(input, purposeValue, response.GetCatalog(), response.GetFile(), exact)
	}
	maximum, valid := fileInteger(arguments, "maximum_bytes", 4096, 16384)
	if !valid {
		return nil, errRuntimeFileInput
	}
	response, err := server.control.Runtime.PreviewExecutionFile(ctx, &cp.PreviewExecutionFileRequest{Context: execution, File: exact, MaximumBytes: int32(maximum)}, options...)
	if err != nil {
		return nil, err
	}
	result, err := exactFileResult(input, purposeValue, response.GetCatalog(), response.GetFile(), exact)
	if err != nil {
		return nil, err
	}
	text := response.GetText()
	sum := sha256.Sum256([]byte(text))
	previewDigest := "sha256:" + hex.EncodeToString(sum[:])
	if len(text) > int(maximum) || !fileText(text, int(maximum)) || response.GetPreviewDigest() != previewDigest ||
		!response.GetTruncated() && (int64(len(text)) != response.GetFile().GetSizeBytes() || previewDigest != digest) ||
		response.GetTruncated() && int64(len(text)) >= response.GetFile().GetSizeBytes() {
		return nil, errRuntimeFileReply
	}
	result["text"], result["truncated"], result["preview_digest"] = text, response.GetTruncated(), previewDigest
	return result, nil
}

func optionalFileString(arguments map[string]any, key string, maximum int) (string, bool) {
	raw, present := arguments[key]
	if !present {
		return "", true
	}
	value, ok := raw.(string)
	return value, ok && fileText(value, maximum)
}

func validFileRef(ref, prefix string) bool {
	return strings.HasPrefix(ref, prefix) && runtimeFileRefPattern.MatchString(ref)
}

func validFileCatalog(input runtimecontract.RunnerInput, catalog *cp.RuntimeFileCatalog) bool {
	if catalog == nil || !runtimeFilesAvailable(input) || catalog.GetRef() != input.FileCatalog.Ref || catalog.GetDigest() != input.FileCatalog.Digest ||
		catalog.GetTotal() != input.FileCatalog.Total || len(catalog.GetPurposes()) != len(input.FileCatalog.Purposes) {
		return false
	}
	for index, purpose := range input.FileCatalog.Purposes {
		expected, ok := runtimeFilePurpose(input, purpose)
		if !ok || catalog.GetPurposes()[index] != expected {
			return false
		}
	}
	return true
}

func fileDescriptor(input runtimecontract.RunnerInput, purpose cp.RuntimeFilePurpose, file *cp.ExecutionFileDescriptor) (map[string]any, bool) {
	if file == nil || file.GetPurpose() != purpose || !validFileRef(file.GetEntryRef(), "vfe_") || !validFileRef(file.GetArtifactRef(), "art_") ||
		file.GetRevision() < 1 || file.GetVersion() < 1 || !runtimeFileDigestPattern.MatchString(file.GetDigest()) || file.GetSizeBytes() < 0 ||
		file.GetName() == "" || !fileText(file.GetName(), 1024) || file.GetMediaType() == "" || !fileText(file.GetMediaType(), 255) ||
		!fileText(file.GetSource(), 80) || !fileText(file.GetSourceRef(), 96) || !fileText(file.GetSourceRevisionRef(), 96) ||
		file.GetProjectRef() != input.ProjectRef && !(purpose == cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_WORKSPACE_INPUT && file.GetProjectRef() == "") ||
		file.GetRunRef() != "" && !validFileRef(file.GetRunRef(), "run_") {
		return nil, false
	}
	download := url.Values{"purpose": {strings.TrimPrefix(purpose.String(), "RUNTIME_FILE_PURPOSE_")}, "entry_ref": {file.GetEntryRef()},
		"revision": {strconv.FormatInt(file.GetRevision(), 10)}, "digest": {file.GetDigest()}}
	return map[string]any{"entry_ref": file.GetEntryRef(), "artifact_ref": file.GetArtifactRef(), "revision": file.GetRevision(), "version": file.GetVersion(),
		"download": map[string]any{"method": "GET", "relative_path": "/v1/executions/" + url.PathEscape(input.LeaseRef) + "/artifacts/" + url.PathEscape(file.GetArtifactRef()) + "?" + download.Encode(), "requires_execution_context": true},
		"digest":   file.GetDigest(), "name": file.GetName(), "media_type": file.GetMediaType(), "size_bytes": file.GetSizeBytes(),
		"purpose": strings.TrimPrefix(purpose.String(), "RUNTIME_FILE_PURPOSE_"), "project_ref": file.GetProjectRef(), "run_ref": file.GetRunRef(),
		"source": file.GetSource(), "source_ref": file.GetSourceRef(), "source_revision_ref": file.GetSourceRevisionRef()}, true
}

func exactFileResult(input runtimecontract.RunnerInput, purpose cp.RuntimeFilePurpose, catalog *cp.RuntimeFileCatalog, file *cp.ExecutionFileDescriptor, exact *cp.ExecutionFileRef) (map[string]any, error) {
	result, valid := fileDescriptor(input, purpose, file)
	if !validFileCatalog(input, catalog) || !valid || file.GetEntryRef() != exact.GetEntryRef() || file.GetArtifactRef() != exact.GetArtifactRef() ||
		file.GetRevision() != exact.GetRevision() || file.GetDigest() != exact.GetDigest() {
		return nil, errRuntimeFileReply
	}
	return map[string]any{"catalog": input.FileCatalog, "file": result}, nil
}

func filePageResult(input runtimecontract.RunnerInput, purpose cp.RuntimeFilePurpose, catalog *cp.RuntimeFileCatalog, files []*cp.ExecutionFileDescriptor, total int64, page *cp.PageInfo, limit int) (map[string]any, error) {
	if !validFileCatalog(input, catalog) || total < 0 || total > catalog.GetTotal() || len(files) > limit || int64(len(files)) > total ||
		len(page.GetNextPageToken()) > 512 || !fileText(page.GetNextPageToken(), 512) || page.GetNextPageToken() != "" && len(files) != limit {
		return nil, errRuntimeFileReply
	}
	items := make([]map[string]any, 0, len(files))
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		item, ok := fileDescriptor(input, purpose, file)
		if !ok || seen[file.GetEntryRef()] {
			return nil, errRuntimeFileReply
		}
		seen[file.GetEntryRef()] = true
		items = append(items, item)
	}
	result := map[string]any{"catalog": input.FileCatalog, "items": items, "total": total, "next_cursor": page.GetNextPageToken()}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maximumFileToolReplyBytes {
		return nil, errRuntimeFileReply
	}
	return result, nil
}
