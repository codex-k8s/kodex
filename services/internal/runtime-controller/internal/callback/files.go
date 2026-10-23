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

const (
	// maximumFileToolReplyBytes bounds generated file-RPC receive messages and encoded
	// file-list tool output at 512 KiB; individual previews/pages retain smaller limits.
	// maximumFileToolReplyBytes ограничивает generated file-RPC receive messages и encoded
	// file-list tool output 512 KiB; previews/pages имеют отдельные меньшие limits.
	maximumFileToolReplyBytes = 512 << 10
	// runtimeFileInputFailureClass is the closed diagnostic label for rejected local
	// file-tool arguments, distinct from remote permission or integrity failures.
	// runtimeFileInputFailureClass — closed diagnostic label неверных local file-tool
	// arguments отдельно от remote permission и integrity failures.
	runtimeFileInputFailureClass = "file_input_invalid"
	// runtimeFileReplyFailureClass classifies mismatched owner file responses without
	// using raw metadata, content or remote error text as a diagnostic label.
	// runtimeFileReplyFailureClass классифицирует mismatched owner file responses без raw
	// metadata,content и remote error text в diagnostic label.
	runtimeFileReplyFailureClass = "file_reply_binding_invalid"
	// runtimeFileInputInvalidCode identifies one locally correctable file input failure;
	// it does not authorize retrying a denied owner read or changing execution scope.
	// runtimeFileInputInvalidCode обозначает один locally correctable file input failure,
	// не разрешая повтор denied owner read и изменение execution scope.
	runtimeFileInputInvalidCode = "FILE_INPUT_INVALID"
	// runtimeFileInputGuidance instructs one exact own-catalog correction, canonical
	// entry refs and bounded read offsets; it explicitly distinguishes input rejection
	// from authority denial and never supplies missing pin bytes.
	// runtimeFileInputGuidance задаёт одно exact own-catalog correction,canonical entry
	// refs/bounded read offsets,отделяя input rejection от authority denial
	// и не подставляя missing pin bytes.
	runtimeFileInputGuidance = "Correct the arguments once using the current tool schema and exact entry_ref, artifact_ref, revision and sha256: digest from your own runtime catalog. entry_ref must be vfe_ followed by 32 lowercase hexadecimal characters; copy the exact entry_ref without editing it. For read_file use offset_bytes=0 first, then the returned next_offset_bytes; maximum_bytes must be 4..16384. This is not an authority denial."
	// runtimeFileHandoffGuidance limits cross-agent handoff to immutable artifact pins
	// and file name; the receiving child must resolve its own catalog-local entry.
	// runtimeFileHandoffGuidance ограничивает cross-agent handoff immutable artifact pins
	// и file name; receiving child должен получить собственную catalog-local entry.
	runtimeFileHandoffGuidance = "Immutable file pins: artifact_ref/revision/digest+file_name only; child resolves own entry."
	// runtimeFileCatalogGuidance explains local entry/cursor ownership, complete paginated
	// RUN_RESULT discovery and provenance-aware exact pin matching. Missing local mounted
	// attachments alone do not prove absence; remote denial remains blocked, not replayable.
	// runtimeFileCatalogGuidance объясняет local entry/cursor ownership,полный paginated
	// RUN_RESULT discovery и provenance-aware exact pin matching. Отсутствие local mounted
	// attachments само не доказывает отсутствие; remote denial остаётся blocked без replay.
	runtimeFileCatalogGuidance = "entry_ref, catalog and cursors are runtime-local: never copy from parent, sibling or another turn/attempt. Local manifests describe mounted attachments, not the complete RUN_RESULT catalog; a missing handoff tuple alone does not prove a required result unavailable. Resolve artifact_ref/revision/digest via your own search_files by file name (not artifact_ref) or get_file_manifest pages; use the matched own entry_ref for metadata/preview/read_file. For unresolved required results, finish independent authorized discovery through all next_cursor pages of own RUN_RESULT manifest. Follow next_cursor with the same purpose/query; page_size=100 is not the whole catalog. Match current root/run/dependency provenance using run_ref/source and exact artifact pins; a filename hit alone does not establish origin. Read matched mandatory content to EOF. Missing exact pins or remote denial stay blocked; actual absence after discovery remains blocked; no automatic remote-error retry, replay of a denied read or invented pins."
)

var (
	// errRuntimeFileInput is a comparable fixed local-input rejection without raw
	// selector, file path, credential or payload details.
	// errRuntimeFileInput — comparable fixed local-input отказ без raw selector,file path,
	// credential и payload details.
	errRuntimeFileInput = errors.New("runtime file tool input is invalid")
	// errRuntimeFileReply rejects response binding/content-integrity mismatch with fixed
	// text; it must not be treated as permission to repair input or retry remotely.
	// errRuntimeFileReply отвергает response binding/content-integrity mismatch с fixed
	// text; это не разрешение исправлять input и повторять remote read.
	errRuntimeFileReply = errors.New("runtime file tool response binding is invalid")
	// runtimeFileRefPattern bounds opaque artifact/run/catalog-style refs to eight
	// through 96 ASCII identifier characters; prefix and ownership are separate checks.
	// runtimeFileRefPattern ограничивает opaque artifact/run/catalog-style refs восемью–96
	// ASCII identifier characters; prefix и владение проверяются отдельно.
	runtimeFileRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)
	// runtimeFileEntryPattern matches the owner-captured vfe_ plus thirty-two lowercase
	// hex format exactly; it must not be generalized from unrelated opaque ref grammars.
	// runtimeFileEntryPattern точно принимает owner-captured vfe_ и тридцать два lowercase
	// hex characters; grammar не обобщается по другим opaque refs.
	runtimeFileEntryPattern = regexp.MustCompile(`^vfe_[a-f0-9]{32}$`)
	// runtimeFileDigestPattern accepts sha256: followed by sixty-four lowercase hex
	// characters, describing a commitment locator rather than a verified scan or grant.
	// runtimeFileDigestPattern принимает sha256: и шестьдесят четыре lowercase hex characters
	// как commitment locator, не verified scan и не grant.
	runtimeFileDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

// runtimeFilesAvailable reuses the shared exact execution/catalog eligibility
// predicate; current owner permissions and lifecycle still govern each RPC.
// runtimeFilesAvailable переиспользует shared exact execution/catalog eligibility
// predicate; текущие owner permissions/lifecycle по-прежнему проверяются каждым RPC.
func runtimeFilesAvailable(input runtimecontract.RunnerInput) bool {
	return runtimecontract.RuntimeFileToolsAvailable(input)
}

// runtimeFilePurpose requires eligible file tools and purpose membership in the
// immutable catalog, then maps exactly four supported purpose names to wire enums.
// Unknown or unadvertised purposes return no enum authority.
// runtimeFilePurpose требует eligible file tools/purpose membership immutable catalog
// и переводит ровно четыре поддерживаемых purpose names в wire enums.
// Unknown/unadvertised purpose не получает enum authority.
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

// runtimeFileTools builds five closed file-tool schemas only for an eligible
// execution snapshot. Discovery/manifest expose bounded query/cursor pages; exact reads
// require catalog entry/artifact/revision/digest pins, with separate preview/read bounds.
// Descriptions guide provenance-aware discovery but do not execute or authorize reads.
// runtimeFileTools строит пять closed file-tool schemas лишь для eligible execution
// snapshot. Discovery/manifest дают bounded query/cursor pages; exact reads требуют
// entry/artifact/revision/digest pins с отдельными preview/read bounds. Descriptions
// направляют provenance-aware discovery,не выполняя и не авторизуя reads.
func runtimeFileTools(input runtimecontract.RunnerInput) []map[string]any {
	if !runtimeFilesAvailable(input) {
		return nil
	}
	result := make([]map[string]any, 0, 5)
	for _, tool := range []struct{ name, description string }{
		{runtimecontract.FileToolSearch, "Search authorized files in this exact runtime catalog. Follow next_cursor within the same purpose and query."},
		{runtimecontract.FileToolMetadata, "Read exact metadata using an entry_ref, artifact_ref, revision and digest returned by this runtime catalog."},
		{runtimecontract.FileToolPreview, "Read a bounded UTF-8 text preview of an exact catalog entry. Binary files do not have text previews."},
		{runtimecontract.FileToolManifest, "Read one page of the immutable runtime file manifest, filtered by current permissions and lifecycle."},
		{runtimecontract.FileToolRead, "Read an exact UTF-8 text file in bounded pages after verification of its entire source. Start at offset_bytes 0, then use next_offset_bytes with the same exact file pins until eof. A page alone is not the complete file."},
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
			properties["entry_ref"] = map[string]any{"type": "string", "minLength": 36, "maxLength": 36, "pattern": runtimeFileEntryPattern.String()}
			properties["artifact_ref"] = opaqueRefSchema()
			properties["revision"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991}
			properties["digest"] = map[string]any{"type": "string", "pattern": runtimeFileDigestPattern.String()}
			required = append(required, "entry_ref", "artifact_ref", "revision", "digest")
			if tool.name == runtimecontract.FileToolPreview {
				properties["maximum_bytes"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 16384, "default": 4096}
			}
			if tool.name == runtimecontract.FileToolRead {
				properties["maximum_bytes"] = map[string]any{"type": "integer", "minimum": 4, "maximum": 16384, "default": 16384}
				properties["offset_bytes"] = map[string]any{"type": "integer", "minimum": 0, "maximum": runtimecontract.MaximumArtifactTransferBytes, "default": 0}
			}
		}
		result = append(result, map[string]any{"name": tool.name, "description": tool.description + " " + runtimeFileCatalogGuidance,
			"inputSchema": objectSchema(required, properties)})
	}
	return result
}

// fileInteger accepts only finite positive integral float64 input within maximum.
// Absent input returns fallback with validity based solely on fallback being positive;
// it does not accept Go int/json.Number or recheck fallback against maximum.
// fileInteger принимает лишь finite positive integral float64 input до maximum.
// Absent input возвращает fallback с признаком только его положительности;
// Go int/json.Number не допускаются,fallback с maximum не сверяется.
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

// fileText checks UTF-8 validity, a Unicode-rune ceiling and absence of NUL,
// allowing empty text and imposing no separate byte or normalization policy.
// fileText проверяет UTF-8 validity,Unicode-rune ceiling и отсутствие NUL,
// допуская пустой text без отдельной byte/normalization policy.
func fileText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum && !strings.ContainsRune(value, '\x00')
}

// callFileTool validates snapshot purpose/tool and closed arguments, derives lease/
// fence/generation/catalog from authenticated input, then invokes the specialized
// search/manifest/metadata/preview owner RPC or full read handler. Preview validation
// binds exact metadata and text digest with truncated/full size semantics; current
// authority remains owner-owned, and no arbitrary path/URL is accepted.
// callFileTool проверяет snapshot purpose/tool и closed arguments,берёт lease/fence/
// generation/catalog из authenticated input и вызывает специализированный owner RPC
// search/manifest/metadata/preview либо full read handler. Preview проверяет exact
// metadata/text digest и truncated/full size semantics; текущей authority владеет
// owner,произвольные path/URL не принимаются.
func (server *Server) callFileTool(ctx context.Context, input runtimecontract.RunnerInput, tool string, arguments map[string]any) (any, error) {
	purpose, _ := arguments["purpose"].(string)
	purposeValue, ok := runtimeFilePurpose(input, purpose)
	if !ok || !runtimecontract.IsRuntimeFileTool(tool) {
		return nil, errRuntimeFileInput
	}
	if tool == runtimecontract.FileToolRead {
		return server.readFile(ctx, input, purposeValue, arguments)
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

// optionalFileString permits an absent string as empty, otherwise requires an
// actual bounded valid UTF-8/NUL-free string and preserves it without trimming.
// optionalFileString допускает absent string как empty,иначе требует actual bounded
// valid UTF-8/NUL-free string,сохраняя его без trimming.
func optionalFileString(arguments map[string]any, key string, maximum int) (string, bool) {
	raw, present := arguments[key]
	if !present {
		return "", true
	}
	value, ok := raw.(string)
	return value, ok && fileText(value, maximum)
}

// validFileRef applies exact captured-entry grammar only for vfe_; other refs
// must match both their requested prefix and the independent opaque ref pattern.
// It does not resolve resource existence, tenant or read permissions.
// validFileRef применяет exact captured-entry grammar лишь для vfe_; остальные refs
// должны иметь requested prefix и independent opaque ref pattern. Существование,
// tenant и read permissions здесь не разрешаются.
func validFileRef(ref, prefix string) bool {
	// Entry назначается capture SQL как vfe_ + UUID без дефисов;
	// opaque artifact/run refs имеют другой, независимый формат.
	if prefix == "vfe_" {
		return runtimeFileEntryPattern.MatchString(ref)
	}
	return strings.HasPrefix(ref, prefix) && runtimeFileRefPattern.MatchString(ref)
}

// validFileCatalog compares ref, digest, total and ordered purpose enums against
// the eligible immutable input catalog; it does not rebuild a catalog from caller fields.
// validFileCatalog сравнивает ref,digest,total и ordered purpose enums с eligible
// immutable input catalog,не восстанавливая catalog из caller fields.
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

// fileDescriptor validates purpose, canonical pins, bounded metadata and project/
// run association, allowing projectless WORKSPACE_INPUT only. It returns a relative
// execution-context-required download locator plus provenance metadata, never a bearer
// URL or proof that a similarly named file belongs to a required dependency.
// fileDescriptor проверяет purpose,canonical pins,bounded metadata и project/run
// association,допуская projectless лишь WORKSPACE_INPUT. Возвращает relative download
// locator с execution-context requirement/provenance,не bearer URL и не доказательство
// того,что одноимённый file принадлежит нужной dependency.
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

// exactFileResult requires both immutable catalog binding and descriptor validity,
// then compares selected entry/artifact/revision/digest exactly before returning
// safe metadata. File version comes from the owner descriptor, not the caller locator.
// exactFileResult требует immutable catalog binding/descriptor validity,затем exact
// entry/artifact/revision/digest selection до safe metadata. File version берётся
// из owner descriptor,а не caller locator.
func exactFileResult(input runtimecontract.RunnerInput, purpose cp.RuntimeFilePurpose, catalog *cp.RuntimeFileCatalog, file *cp.ExecutionFileDescriptor, exact *cp.ExecutionFileRef) (map[string]any, error) {
	result, valid := fileDescriptor(input, purpose, file)
	if !validFileCatalog(input, catalog) || !valid || file.GetEntryRef() != exact.GetEntryRef() || file.GetArtifactRef() != exact.GetArtifactRef() ||
		file.GetRevision() != exact.GetRevision() || file.GetDigest() != exact.GetDigest() {
		return nil, errRuntimeFileReply
	}
	return map[string]any{"catalog": input.FileCatalog, "file": result}, nil
}

// filePageResult checks catalog/total/cardinality/cursor bounds, validates each
// descriptor and rejects duplicate entry refs before encoding a bounded list. It
// retains the owner cursor without claiming a page alone is the complete catalog.
// filePageResult проверяет catalog/total/cardinality/cursor bounds,каждый descriptor
// и duplicate entry refs до encoded bounded list. Owner cursor сохраняется;
// одна страница не объявляется полным каталогом.
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
