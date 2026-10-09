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
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

var errWorkflowCatalogRead = errors.New("workflow catalog read is invalid")

func workflowCatalogReadTool() map[string]any {
	pins := map[string]any{"workflow_ref": opaqueRefSchema(), "published_ref": opaqueRefSchema(), "spec_digest": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}, "workflow_version": map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991}}
	publication := map[string]any{}
	for key, v := range pins {
		publication[key] = v
	}
	publication["offset_bytes"] = map[string]any{"type": "integer", "minimum": 0, "maximum": controlplaneapi.WorkflowPublicationMaximumBytes}
	publication["maximum_bytes"] = map[string]any{"type": "integer", "minimum": 4, "maximum": maximumAssistantConfigurationPageBytes}
	publication["configuration_sha256"] = map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}
	required := []string{"workflow_ref", "published_ref", "spec_digest", "workflow_version"}
	discovery := objectSchema(nil, map[string]any{"query": stringSchema(0, 200), "page_token": stringSchema(0, 512)})
	full := objectSchema([]string{"publication_read"}, map[string]any{"publication_read": objectSchema(required, publication)})
	active := objectSchema([]string{"active_runs_read"}, map[string]any{"active_runs_read": objectSchema(required, pins), "page_token": stringSchema(0, 512)})
	return map[string]any{"name": "get_workflow_catalog", "description": "Discover published project Workflows with query/page_token; follow next_page_token to EOF. Then use exactly one selector: publication_read with discovered workflow_ref/published_ref/spec_digest/workflow_version reads the full published configuration, steps/instructions/DAG/capabilities/defaults/result schema in UTF-8 pages; continue offset_bytes=next_offset_bytes with unchanged configuration_sha256 and pins until eof=true. active_runs_read with those pins lists actor-visible nonterminal roots of that Workflow; follow next_page_token. Project/actor come from the server. Active-run discovery is advisory, not a uniqueness guarantee. Never guess refs/schema or repeat an accepted launch; launch_workflow revalidates authority and exact pins.", "inputSchema": map[string]any{"type": "object", "oneOf": []any{discovery, full, active}}}
}

func workflowCatalogReadPins(selector map[string]any) (*cp.ExecutionWorkflowReadPins, error) {
	workflow, ok := selector["workflow_ref"].(string)
	published, pubOK := selector["published_ref"].(string)
	digest, digestOK := selector["spec_digest"].(string)
	version, versionOK := exactJSONInt64(selector["workflow_version"])
	if !ok || !pubOK || !digestOK || !versionOK || !runtimeFileRefPattern.MatchString(workflow) || !runtimeFileRefPattern.MatchString(published) || !workflowCatalogDigestPattern.MatchString(digest) || version < 1 || version > 9007199254740991 {
		return nil, errWorkflowCatalogRead
	}
	return &cp.ExecutionWorkflowReadPins{WorkflowRef: workflow, PublishedRef: published, SpecDigest: digest, WorkflowVersion: version}, nil
}

func (server *Server) workflowCatalogRead(ctx context.Context, input runtimecontract.RunnerInput, args map[string]any) (any, error) {
	if !workflowLaunchAvailable(input) {
		return nil, errWorkflowCatalogRead
	}
	_, full := args["publication_read"]
	key := "active_runs_read"
	allowed := []string{"active_runs_read", "page_token"}
	if full {
		key = "publication_read"
		allowed = []string{key}
	}
	if !onlyKeys(args, allowed...) {
		return nil, errWorkflowCatalogRead
	}
	selected, ok := args[key].(map[string]any)
	if !ok {
		return nil, errWorkflowCatalogRead
	}
	keys := []string{"workflow_ref", "published_ref", "spec_digest", "workflow_version"}
	if full {
		keys = append(keys, "offset_bytes", "maximum_bytes", "configuration_sha256")
	}
	if !onlyKeys(selected, keys...) {
		return nil, errWorkflowCatalogRead
	}
	pins, err := workflowCatalogReadPins(selected)
	if err != nil {
		return nil, err
	}
	request := &cp.GetExecutionWorkflowCatalogRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration}
	var offset, maximum int64
	sourceSHA := ""
	if full {
		var valid bool
		offset, valid = assistantConfigurationPageInteger(selected, "offset_bytes", 0, controlplaneapi.WorkflowPublicationMaximumBytes)
		if !valid {
			return nil, errWorkflowCatalogRead
		}
		maximum, valid = assistantConfigurationPageInteger(selected, "maximum_bytes", maximumAssistantConfigurationPageBytes, maximumAssistantConfigurationPageBytes)
		if !valid || maximum < 4 {
			return nil, errWorkflowCatalogRead
		}
		if raw, present := selected["configuration_sha256"]; present {
			sourceSHA, valid = raw.(string)
			if !valid || !workflowCatalogDigestPattern.MatchString(sourceSHA) {
				return nil, errWorkflowCatalogRead
			}
		}
		if offset > 0 && sourceSHA == "" {
			return nil, errWorkflowCatalogRead
		}
		request.Read = &cp.GetExecutionWorkflowCatalogRequest_PublicationRead{PublicationRead: &cp.ExecutionWorkflowPublicationRead{Pins: pins}}
	} else {
		if raw, present := args["page_token"]; present {
			token, ok := raw.(string)
			if !ok || len(token) > 512 || !utf8.ValidString(token) || strings.ContainsRune(token, 0) {
				return nil, errWorkflowCatalogRead
			}
			request.PageToken = token
		}
		request.Read = &cp.GetExecutionWorkflowCatalogRequest_ActiveRunsRead{ActiveRunsRead: &cp.ExecutionWorkflowActiveRunsRead{Pins: pins}}
	}
	bounded, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.GetExecutionWorkflowCatalog(bounded, request)
	if err != nil {
		return nil, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || len(response.Items) != 0 || response.NextPageToken != "" {
		return nil, errWorkflowCatalogRead
	}
	if full {
		publication := response.GetPublication()
		if publication == nil || len(publication.ProtoReflect().GetUnknown()) != 0 {
			return nil, errWorkflowCatalogRead
		}
		raw := publication.ConfigurationJson
		digest := publication.ConfigurationSha256
		value, err := controlplaneapi.DecodeWorkflowPublication(raw, digest)
		if err != nil || value.WorkflowRef != pins.WorkflowRef || value.ProjectRef != input.ProjectRef || value.PublishedRef != pins.PublishedRef || value.SpecDigest != pins.SpecDigest || value.WorkflowVersion != pins.WorkflowVersion || sourceSHA != "" && sourceSHA != digest || offset > int64(len(raw)) {
			return nil, errWorkflowCatalogRead
		}
		end := min(offset+maximum, int64(len(raw)))
		if offset < int64(len(raw)) && !utf8.RuneStart(raw[offset]) {
			return nil, errWorkflowCatalogRead
		}
		for end < int64(len(raw)) && !utf8.RuneStart(raw[end]) {
			end--
		}
		part := raw[offset:end]
		if !utf8.Valid(part) || offset < int64(len(raw)) && len(part) == 0 {
			return nil, errWorkflowCatalogRead
		}
		hash := sha256.Sum256(part)
		result := map[string]any{"kind": "PUBLISHED_CONFIGURATION", "workflow_ref": pins.WorkflowRef, "project_ref": value.ProjectRef, "published_ref": pins.PublishedRef, "spec_digest": pins.SpecDigest, "workflow_version": pins.WorkflowVersion, "published_version": value.PublishedVersion, "configuration_sha256": digest, "configuration_page": map[string]any{"text": string(part), "size_bytes": len(raw), "offset_bytes": offset, "next_offset_bytes": end, "eof": end == int64(len(raw)), "page_sha256": hex.EncodeToString(hash[:])}}
		encoded, err := json.Marshal(result)
		if err != nil || len(encoded) > 32768 {
			return nil, errWorkflowCatalogRead
		}
		return result, nil
	}
	page := response.GetActiveRuns()
	if page == nil || len(page.ProtoReflect().GetUnknown()) != 0 || page.Pins == nil || len(page.Pins.ProtoReflect().GetUnknown()) != 0 || page.Pins.WorkflowRef != pins.WorkflowRef || page.Pins.PublishedRef != pins.PublishedRef || page.Pins.SpecDigest != pins.SpecDigest || page.Pins.WorkflowVersion != pins.WorkflowVersion || len(page.Items) > 10 || len(page.NextPageToken) > 512 || !utf8.ValidString(page.NextPageToken) || strings.ContainsRune(page.NextPageToken, 0) || page.NextPageToken != "" && (len(page.Items) != 10 || page.NextPageToken == request.PageToken) {
		return nil, errWorkflowCatalogRead
	}
	items := []map[string]any{}
	seen := map[string]bool{}
	for _, item := range page.Items {
		if item == nil || len(item.ProtoReflect().GetUnknown()) != 0 || !runtimeFileRefPattern.MatchString(item.RunRef) || seen[item.RunRef] || item.WorkflowRef != pins.WorkflowRef || !runtimeFileRefPattern.MatchString(item.PublishedRef) || !workflowCatalogDigestPattern.MatchString(item.SpecDigest) || item.PublishedVersion < 1 || item.RunVersion < 1 || !utf8.ValidString(item.Title) || utf8.RuneCountInString(item.Title) > 240 || strings.ContainsRune(item.Title, 0) || item.CreatedAt == nil || item.CreatedAt.CheckValid() != nil || len(item.CreatedAt.ProtoReflect().GetUnknown()) != 0 {
			return nil, errWorkflowCatalogRead
		}
		switch item.State {
		case "QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING":
		default:
			return nil, errWorkflowCatalogRead
		}
		seen[item.RunRef] = true
		items = append(items, map[string]any{"run_ref": item.RunRef, "workflow_ref": item.WorkflowRef, "published_ref": item.PublishedRef, "spec_digest": item.SpecDigest, "published_version": item.PublishedVersion, "run_version": item.RunVersion, "title": item.Title, "state": item.State, "created_at": item.CreatedAt.AsTime().UTC().Format("2006-01-02T15:04:05.999999999Z")})
	}
	result := map[string]any{"kind": "ACTIVE_RUNS", "workflow_ref": pins.WorkflowRef, "project_ref": input.ProjectRef, "published_ref": pins.PublishedRef, "spec_digest": pins.SpecDigest, "workflow_version": pins.WorkflowVersion, "items": items, "next_page_token": page.NextPageToken, "coverage": "ACTOR_VISIBLE_NONTERMINAL_WORKFLOW_ROOTS", "duplicate_check": "ADVISORY"}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 32768 {
		return nil, errWorkflowCatalogRead
	}
	return result, nil
}
