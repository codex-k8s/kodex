package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	// workflowReceiptKind fixes the public safe metadata kind for all workflow catalog
	// read modes, not a reusable bearer or signed owner credential.
	// workflowReceiptKind фиксирует public safe metadata kind всех workflow catalog
	// read modes, не reusable bearer и не signed owner credential.
	workflowReceiptKind = "workflow_catalog_page"
	// workflowModeDiscovery identifies list discovery receipts with cardinality and
	// cursor presence only, excluding raw query/token or workflow names.
	// workflowModeDiscovery обозначает list discovery receipts лишь с cardinality/cursor
	// presence без raw query/token и workflow names.
	workflowModeDiscovery = "DISCOVERY"
	// workflowModePublication identifies exact published-source byte-page receipts,
	// including digests and coordinates without the source text itself.
	// workflowModePublication обозначает exact published-source byte-page receipts
	// с digests/coordinates без самого source text.
	workflowModePublication = "PUBLICATION"
	// workflowModeActiveRuns identifies actor-visible active-root list receipts,
	// whose duplicate check is explicitly advisory rather than a uniqueness reservation.
	// workflowModeActiveRuns обозначает actor-visible active-root list receipts
	// с явно advisory duplicate check, не uniqueness reservation.
	workflowModeActiveRuns = "ACTIVE_RUNS"
	// maximumWorkflowReceiptBytes caps serialized safe receipt metadata at two thousand
	// bytes separately from the public wire/source page budgets.
	// maximumWorkflowReceiptBytes ограничивает serialized safe receipt metadata двумя
	// тысячами bytes отдельно от public wire/source page budgets.
	maximumWorkflowReceiptBytes = 2000
)

// errWorkflowReceipt rejects missing, malformed or mismatched local evidence with
// fixed text and no raw wire, request, input, fence or receipt content.
// errWorkflowReceipt отвергает missing,malformed,mismatched local evidence с fixed
// text без raw wire,request,input,fence и receipt content.
var errWorkflowReceipt = errors.New("workflow catalog receipt invalid")

// workflowPageReceipt carries source/page digests, byte interval, total size and
// EOF for one validated publication page, never its text or resource locators.
// workflowPageReceipt хранит source/page digests,byte interval,total size и EOF
// одной validated publication page без text и resource locators.
type workflowPageReceipt struct {
	OffsetBytes         int64  `json:"offset_bytes"`
	NextOffsetBytes     int64  `json:"next_offset_bytes"`
	SizeBytes           int64  `json:"size_bytes"`
	EOF                 bool   `json:"eof"`
	ConfigurationSHA256 string `json:"configuration_sha256"`
	PageSHA256          string `json:"page_sha256"`
	SpecDigest          string `json:"spec_digest"`
}

// workflowListReceipt carries only bounded item count, cursor presence and the
// active-list advisory flag, not names, refs or the actual continuation token.
// workflowListReceipt хранит лишь bounded item count,cursor presence и active-list
// advisory flag, не names,refs и actual continuation token.
type workflowListReceipt struct {
	ItemsCount int  `json:"items_count"`
	HasNext    bool `json:"has_next"`
	Advisory   bool `json:"advisory"`
}

// workflowCatalogReceipt is the versioned closed safe activity projection with
// exactly the mode-appropriate page or list payload, validated before persistence.
// workflowCatalogReceipt — versioned closed safe activity projection с соответствующим
// mode page/list payload, проверяемая перед persistence.
type workflowCatalogReceipt struct {
	Version     int                  `json:"version"`
	Kind        string               `json:"kind"`
	Mode        string               `json:"mode"`
	Publication *workflowPageReceipt `json:"publication,omitempty"`
	List        *workflowListReceipt `json:"list,omitempty"`
}

// workflowCatalogEvidence privately binds validated result to input, exact request,
// wire and safe-receipt hashes. This process-local provenance is not serialized,
// signed, durable or independently sufficient to authorize an owner operation.
// workflowCatalogEvidence privately связывает validated result с input,exact request,
// wire и safe-receipt hashes. Эта process-local provenance не сериализуется,
// не подписана,не durable и сама не авторизует owner operation.
type workflowCatalogEvidence struct {
	inputSHA256, requestSHA256, wireSHA256, receiptSHA256 string
	receipt                                               workflowCatalogReceipt
}

// workflowCatalogToolResult pairs public MCP wire with private local evidence;
// exporting/unmarshaling JSON cannot reconstruct that evidence. The wire map remains
// shared, and later safe-receipt checks detect mutation rather than preventing it.
// workflowCatalogToolResult связывает public MCP wire с private local evidence;
// export/unmarshal JSON не восстанавливает evidence. Wire map общая, последующая
// safe-receipt проверка обнаруживает mutation, а не предотвращает её.
type workflowCatalogToolResult struct {
	wire     map[string]any
	evidence *workflowCatalogEvidence
}

// MarshalJSON serializes only the original public wire map, retaining agent-visible
// bytes while excluding private evidence; serialization errors propagate unchanged.
// MarshalJSON сериализует лишь исходную public wire map, сохраняя agent-visible
// bytes без private evidence; ошибки сериализации возвращаются без замены.
func (result workflowCatalogToolResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(result.wire)
}

// workflowReceiptDigest returns lowercase SHA-256 of exact bytes, including an empty
// slice; it is a content commitment, not a signature or permission check.
// workflowReceiptDigest возвращает lowercase SHA-256 exact bytes,включая пустой slice;
// это content commitment, не signature и не permission check.
func workflowReceiptDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// workflowReceiptInputDigest hashes exact execution identity/revision/lease binding
// including private fence without serializing prompt/configuration/credential values.
// The hash stays in local evidence and does not establish binding authority itself.
// workflowReceiptInputDigest хеширует exact execution identity/revision/lease binding,
// включая private fence, без prompt/configuration/credential values. Hash остаётся
// local evidence и сам не устанавливает authority binding.
func workflowReceiptInputDigest(input runtimecontract.RunnerInput) string {
	raw, _ := json.Marshal(struct {
		Organization, Project, Run, Node, Session, Turn, Agent, Revision, RevisionDigest, InputDigest, Lease, Fence string
		Attempt                                                                                                     int32
		Generation, RevisionVersion                                                                                 int64
	}{input.OrganizationRef, input.ProjectRef, input.RunRef, input.NodeRef, input.SessionRef, input.TurnRef, input.AgentRef, input.RuntimeRevisionRef, input.RuntimeRevisionDigest, input.InputDigest, input.LeaseRef, input.LeaseFence, input.Attempt, input.LeaseGeneration, input.RuntimeRevisionVersion})
	return workflowReceiptDigest(raw)
}

// workflowCatalogRequestMetadata recognizes one closed request mode and validates
// its local pins/coordinates, returning no mode on malformed input. It neither checks
// owner eligibility nor assigns authority; continuation requires a source digest.
// workflowCatalogRequestMetadata распознаёт один closed request mode и проверяет
// local pins/coordinates, не выдавая mode при ошибке. Owner eligibility/authority
// не назначаются; continuation требует source digest.
func workflowCatalogRequestMetadata(arguments map[string]any) (string, int64, int64, bool) {
	_, publication := arguments["publication_read"]
	_, active := arguments["active_runs_read"]
	if !publication && !active {
		if !onlyKeys(arguments, "query", "page_token") {
			return "", 0, 0, false
		}
		for key, limit := range map[string]int{"query": 200, "page_token": 512} {
			if raw, present := arguments[key]; present {
				value, ok := raw.(string)
				if !ok || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || key == "query" && utf8.RuneCountInString(value) > limit || key == "page_token" && len(value) > limit {
					return "", 0, 0, false
				}
			}
		}
		return workflowModeDiscovery, 0, 0, true
	}
	key := "active_runs_read"
	allowed := []string{key, "page_token"}
	keys := []string{"workflow_ref", "published_ref", "spec_digest", "workflow_version"}
	if publication {
		key, allowed = "publication_read", []string{"publication_read"}
		keys = append(keys, "offset_bytes", "maximum_bytes", "configuration_sha256")
	}
	if !onlyKeys(arguments, allowed...) {
		return "", 0, 0, false
	}
	selected, ok := arguments[key].(map[string]any)
	if !ok || !onlyKeys(selected, keys...) {
		return "", 0, 0, false
	}
	if _, err := workflowCatalogReadPins(selected); err != nil {
		return "", 0, 0, false
	}
	if !publication {
		if raw, present := arguments["page_token"]; present {
			token, ok := raw.(string)
			if !ok || len(token) > 512 || !utf8.ValidString(token) || strings.ContainsRune(token, 0) {
				return "", 0, 0, false
			}
		}
		return workflowModeActiveRuns, 0, 0, true
	}
	offset, offsetOK := assistantConfigurationPageInteger(selected, "offset_bytes", 0, controlplaneapi.WorkflowPublicationMaximumBytes)
	maximum, maximumOK := assistantConfigurationPageInteger(selected, "maximum_bytes", maximumAssistantConfigurationPageBytes, maximumAssistantConfigurationPageBytes)
	digest := ""
	if raw, present := selected["configuration_sha256"]; present {
		var ok bool
		digest, ok = raw.(string)
		if !ok || !workflowCatalogDigestPattern.MatchString(digest) {
			return "", 0, 0, false
		}
	}
	if !offsetOK || !maximumOK || maximum < 4 || offset > 0 && digest == "" {
		return "", 0, 0, false
	}
	return workflowModePublication, offset, maximum, true
}

// workflowCatalogSafeParameters emits mode and publication byte coordinates only
// after metadata validation; invalid input yields an empty map, never query/token/refs.
// workflowCatalogSafeParameters выдаёт лишь mode/publication byte coordinates после
// metadata validation; invalid input даёт пустую map без query/token/refs.
func workflowCatalogSafeParameters(arguments map[string]any) map[string]any {
	result := map[string]any{}
	if mode, offset, maximum, ok := workflowCatalogRequestMetadata(arguments); ok {
		result["mode"] = mode
		if mode == workflowModePublication {
			result["offset_bytes"], result["maximum_bytes"] = offset, maximum
		}
	}
	return result
}

// workflowReceiptValid checks version/kind/mode, mutually exclusive page/list shape,
// publication progress/EOF/digest bounds or list cardinality/advisory semantics.
// It validates commitment shape and coordinates, not actual source bytes or ownership.
// workflowReceiptValid проверяет version/kind/mode,exclusive page/list shape,publication
// progress/EOF/digest bounds либо list cardinality/advisory semantics. Проверяются
// commitment shape/coordinates, не actual source bytes и не владение.
func workflowReceiptValid(receipt workflowCatalogReceipt, mode string, offset, maximum int64) bool {
	if receipt.Version != 1 || receipt.Kind != workflowReceiptKind || receipt.Mode != mode {
		return false
	}
	if mode == workflowModePublication {
		p := receipt.Publication
		return receipt.List == nil && p != nil && p.SizeBytes > 0 && p.SizeBytes <= controlplaneapi.WorkflowPublicationMaximumBytes &&
			p.OffsetBytes == offset && p.NextOffsetBytes >= p.OffsetBytes && p.NextOffsetBytes <= p.SizeBytes &&
			p.NextOffsetBytes-p.OffsetBytes <= maximum && p.EOF == (p.NextOffsetBytes == p.SizeBytes) &&
			(p.EOF || p.NextOffsetBytes > p.OffsetBytes) && workflowCatalogDigestPattern.MatchString(p.ConfigurationSHA256) &&
			workflowCatalogDigestPattern.MatchString(p.PageSHA256) && workflowCatalogDigestPattern.MatchString(p.SpecDigest)
	}
	if mode != workflowModeDiscovery && mode != workflowModeActiveRuns {
		return false
	}
	p := receipt.List
	return receipt.Publication == nil && p != nil && p.ItemsCount >= 0 && p.ItemsCount <= 10 && p.Advisory == (mode == workflowModeActiveRuns)
}

// newWorkflowCatalogResult is called after owner-response validation and binds a
// bounded wire/receipt to the current request/input. It rejects canceled context or
// invalid mode/eligibility/shape, retaining the original mutable wire map; it issues
// no grants, owner decisions or persistent signature.
// newWorkflowCatalogResult вызывается после owner-response validation и связывает
// bounded wire/receipt с current request/input. Canceled context и invalid mode/
// eligibility/shape отвергаются; исходная mutable wire map сохраняется. Grants,
// owner decisions и persistent signature не выдаются.
func newWorkflowCatalogResult(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, wire map[string]any, receipt workflowCatalogReceipt) (any, error) {
	mode, offset, maximum, ok := workflowCatalogRequestMetadata(arguments)
	if ctx.Err() != nil || !workflowLaunchAvailable(input) || !ok || !workflowReceiptValid(receipt, mode, offset, maximum) {
		return nil, errWorkflowReceipt
	}
	raw, err := json.Marshal(wire)
	request, requestErr := json.Marshal(arguments)
	safe, safeErr := json.Marshal(receipt)
	if err != nil || requestErr != nil || safeErr != nil || len(raw) > 32768 || len(safe) > maximumWorkflowReceiptBytes {
		return nil, errWorkflowReceipt
	}
	return workflowCatalogToolResult{wire: wire, evidence: &workflowCatalogEvidence{inputSHA256: workflowReceiptInputDigest(input), requestSHA256: workflowReceiptDigest(request), wireSHA256: workflowReceiptDigest(raw), receiptSHA256: workflowReceiptDigest(safe), receipt: receipt}}, nil
}

// safeWorkflowCatalogReceipt requires the private typed result and rechecks current
// input/request/wire/receipt commitments and mode bounds before returning bounded
// safe JSON. A reconstructed map or mutated result cannot reuse evidence; successful
// local validation alone does not prove the subsequent owner audit write committed.
// safeWorkflowCatalogReceipt требует private typed result и перепроверяет current
// input/request/wire/receipt commitments и mode bounds до bounded safe JSON.
// Восстановленная map или изменённый result не переиспользуют evidence; local success
// сам не доказывает commit последующего owner audit write.
func safeWorkflowCatalogReceipt(input runtimecontract.RunnerInput, arguments map[string]any, result any) (string, error) {
	value, ok := result.(workflowCatalogToolResult)
	mode, offset, maximum, valid := workflowCatalogRequestMetadata(arguments)
	if !ok || value.evidence == nil || !workflowLaunchAvailable(input) || !valid {
		return "", errWorkflowReceipt
	}
	evidence := value.evidence
	request, err := json.Marshal(arguments)
	wire, wireErr := json.Marshal(value.wire)
	if err != nil || wireErr != nil || workflowReceiptInputDigest(input) != evidence.inputSHA256 || workflowReceiptDigest(request) != evidence.requestSHA256 ||
		len(wire) > 32768 || workflowReceiptDigest(wire) != evidence.wireSHA256 ||
		!workflowReceiptValid(evidence.receipt, mode, offset, maximum) {
		return "", errWorkflowReceipt
	}
	raw, err := json.Marshal(evidence.receipt)
	if err != nil || len(raw) > maximumWorkflowReceiptBytes || workflowReceiptDigest(raw) != evidence.receiptSHA256 {
		return "", errWorkflowReceipt
	}
	return string(raw), nil
}
