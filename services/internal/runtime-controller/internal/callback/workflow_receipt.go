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
	workflowReceiptKind         = "workflow_catalog_page"
	workflowModeDiscovery       = "DISCOVERY"
	workflowModePublication     = "PUBLICATION"
	workflowModeActiveRuns      = "ACTIVE_RUNS"
	maximumWorkflowReceiptBytes = 2000
)

var errWorkflowReceipt = errors.New("workflow catalog receipt invalid")

type workflowPageReceipt struct {
	OffsetBytes         int64  `json:"offset_bytes"`
	NextOffsetBytes     int64  `json:"next_offset_bytes"`
	SizeBytes           int64  `json:"size_bytes"`
	EOF                 bool   `json:"eof"`
	ConfigurationSHA256 string `json:"configuration_sha256"`
	PageSHA256          string `json:"page_sha256"`
	SpecDigest          string `json:"spec_digest"`
}
type workflowListReceipt struct {
	ItemsCount int  `json:"items_count"`
	HasNext    bool `json:"has_next"`
	Advisory   bool `json:"advisory"`
}
type workflowCatalogReceipt struct {
	Version     int                  `json:"version"`
	Kind        string               `json:"kind"`
	Mode        string               `json:"mode"`
	Publication *workflowPageReceipt `json:"publication,omitempty"`
	List        *workflowListReceipt `json:"list,omitempty"`
}
type workflowCatalogEvidence struct {
	inputSHA256, requestSHA256, wireSHA256, receiptSHA256 string
	receipt                                               workflowCatalogReceipt
}
type workflowCatalogToolResult struct {
	wire     map[string]any
	evidence *workflowCatalogEvidence
}

// Закрытое доказательство не сериализуется; JSON-байты для агента остаются прежними.
func (result workflowCatalogToolResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(result.wire)
}

func workflowReceiptDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Только execution binding, без prompt/configuration/credential values.
func workflowReceiptInputDigest(input runtimecontract.RunnerInput) string {
	raw, _ := json.Marshal(struct {
		Organization, Project, Run, Node, Session, Turn, Agent, Revision, RevisionDigest, InputDigest, Lease, Fence string
		Attempt                                                                                                     int32
		Generation, RevisionVersion                                                                                 int64
	}{input.OrganizationRef, input.ProjectRef, input.RunRef, input.NodeRef, input.SessionRef, input.TurnRef, input.AgentRef, input.RuntimeRevisionRef, input.RuntimeRevisionDigest, input.InputDigest, input.LeaseRef, input.LeaseFence, input.Attempt, input.LeaseGeneration, input.RuntimeRevisionVersion})
	return workflowReceiptDigest(raw)
}

// Проекция координат запроса не назначает authority. Invalid shape не выдаётся за mode.
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

// Единственные production call sites находятся после полного owner-response validation.
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
