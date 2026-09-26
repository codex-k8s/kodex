package callback

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func (server *Server) configurationCatalog(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	result, err := configurationCatalog(input, arguments)
	if err != nil {
		return nil, err
	}
	_, requested := arguments["definition_query"]
	if !requested {
		if _, offsetOnly := arguments["definition_offset"]; offsetOnly {
			return nil, errors.New("integration definition query is required for pagination")
		}
		return result, nil
	}
	if input.LeaseRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 {
		return nil, errors.New("integration definition catalog is not available")
	}
	query, ok := arguments["definition_query"].(string)
	query = strings.TrimSpace(query)
	if !ok || utf8.RuneCountInString(query) > 80 {
		return nil, errors.New("integration definition query is invalid")
	}
	offset := 0
	if raw, supplied := arguments["definition_offset"]; supplied {
		switch value := raw.(type) {
		case int:
			offset = value
		case float64:
			if value != float64(int(value)) {
				return nil, errors.New("integration definition offset is invalid")
			}
			offset = int(value)
		default:
			return nil, errors.New("integration definition offset is invalid")
		}
	}
	if offset < 0 || offset > 10000 {
		return nil, errors.New("integration definition offset is invalid")
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.SearchAssistantResources(requestContext, &controlplanev1.SearchAssistantResourcesRequest{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		IntegrationDefinitionCatalog: true, DefinitionQuery: query, DefinitionOffset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	if len(response.GetResults()) != 0 || response.GetTruncated() || len(response.GetDefinitions()) > maximumAssistantIntegrationDefinitions ||
		response.GetNextDefinitionOffset() < 0 || response.GetNextDefinitionOffset() > 10000 ||
		(response.GetNextDefinitionOffset() != 0 && response.GetNextDefinitionOffset() <= int32(offset)) {
		return nil, errors.New("integration definition catalog response is invalid")
	}
	definitions := make([]map[string]any, 0, len(response.GetDefinitions()))
	for _, definition := range response.GetDefinitions() {
		if definition == nil || definition.GetKey() == "" || len(definition.GetConfigurationFields()) > 100 || len(definition.GetCapabilityKeys()) > 100 {
			return nil, errors.New("integration definition catalog response is invalid")
		}
		fields := make([]map[string]any, 0, len(definition.GetConfigurationFields()))
		for _, field := range definition.GetConfigurationFields() {
			if field == nil || field.GetKey() == "" {
				return nil, errors.New("integration definition catalog response is invalid")
			}
			entry := map[string]any{"key": field.GetKey(), "label": truncateRunes(field.GetLabel(), 120),
				"help": truncateRunes(field.GetHelp(), 300), "value_type": field.GetValueType(),
				"required": field.GetRequired(), "format": field.GetFormat(),
				"allowed_values": field.GetAllowedValues(), "maximum_length": field.GetMaximumLength()}
			if field.GetHasMinimum() {
				entry["minimum"] = field.GetMinimum()
			}
			if field.GetHasMaximum() {
				entry["maximum"] = field.GetMaximum()
			}
			fields = append(fields, entry)
		}
		definitions = append(definitions, map[string]any{
			"key": definition.GetKey(), "name": truncateRunes(definition.GetName(), 160),
			"description": truncateRunes(definition.GetDescription(), 500), "category": definition.GetCategory(),
			"adapter": definition.GetAdapter(), "origin": definition.GetOrigin(),
			"credential_secret_key": definition.GetCredentialSecretKey(),
			"capability_keys":       definition.GetCapabilityKeys(), "configuration_fields": fields,
		})
	}
	catalog := result.(map[string]any)
	catalog["integration_definitions"] = definitions
	if response.GetNextDefinitionOffset() > 0 {
		catalog["definition_next_offset"] = response.GetNextDefinitionOffset()
	}
	return catalog, nil
}

const maximumAssistantIntegrationDefinitions = 10
