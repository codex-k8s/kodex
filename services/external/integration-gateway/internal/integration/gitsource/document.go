package gitsource

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
)

type (
	Document struct {
		Kind, Name, StableKey, ResourceID                             string
		ExpectedVersion                                               uint64
		Role                                                          *controlplanev1.RoleDefinitionSpec
		Agent                                                         *controlplanev1.AgentSpec
		InstructionSet                                                *controlplanev1.InstructionSetSpec
		ProviderPool                                                  *controlplanev1.ProviderPoolSpec
		RoleStableKey, InstructionSetStableKey, ProviderPoolStableKey string
	}
	documentEnvelope struct {
		APIVersion string `json:"api_version"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name            string `json:"name"`
			StableKey       string `json:"stable_key"`
			ResourceID      string `json:"resource_id"`
			ExpectedVersion uint64 `json:"expected_version"`
		} `json:"metadata"`
		Spec     json.RawMessage `json:"spec"`
		Bindings struct {
			RoleDefinitionStableKey string `json:"role_definition_stable_key"`
			InstructionSetStableKey string `json:"instruction_set_stable_key"`
			ProviderPoolStableKey   string `json:"provider_pool_stable_key"`
		} `json:"bindings,omitempty"`
	}
)

func ParseDocument(raw []byte, expectedKind, expectedStableKey, sourceRef string, sourceRevision uint64, sourceDigest string) (Document, error) {
	if len(raw) == 0 || len(raw) > 8<<20 {
		return Document{}, errors.New("Git source document size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope documentEnvelope
	if decoder.Decode(&envelope) != nil {
		return Document{}, errors.New("Git source document is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Document{}, errors.New("Git source document has trailing data")
	}
	if envelope.APIVersion != "mattercodex.io/v1" || envelope.Kind != expectedKind || envelope.Metadata.StableKey != expectedStableKey || envelope.Metadata.Name == "" || (envelope.Metadata.ResourceID != "" && uuid.Validate(envelope.Metadata.ResourceID) != nil) {
		return Document{}, errors.New("Git source document target is invalid")
	}
	document := Document{Kind: envelope.Kind, Name: envelope.Metadata.Name, StableKey: envelope.Metadata.StableKey, ResourceID: envelope.Metadata.ResourceID, ExpectedVersion: envelope.Metadata.ExpectedVersion, RoleStableKey: envelope.Bindings.RoleDefinitionStableKey, InstructionSetStableKey: envelope.Bindings.InstructionSetStableKey, ProviderPoolStableKey: envelope.Bindings.ProviderPoolStableKey}
	ownership := &controlplanev1.ConfigurationOwnership{ManagedBy: controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_GIT, SourceRef: sourceRef, SourceRevision: sourceRevision, SourceSha256: sourceDigest}
	options := protojson.UnmarshalOptions{DiscardUnknown: false}
	switch expectedKind {
	case "ROLE_DEFINITION":
		document.Role = &controlplanev1.RoleDefinitionSpec{}
		if options.Unmarshal(envelope.Spec, document.Role) != nil {
			return Document{}, errors.New("Git role definition spec is invalid")
		}
		document.Role.Ownership = ownership
	case "AGENT":
		document.Agent = &controlplanev1.AgentSpec{}
		if options.Unmarshal(envelope.Spec, document.Agent) != nil {
			return Document{}, errors.New("Git agent spec is invalid")
		}
		document.Agent.Ownership = ownership
		if document.RoleStableKey == "" || document.InstructionSetStableKey == "" || document.ProviderPoolStableKey == "" {
			return Document{}, errors.New("Git agent bindings are incomplete")
		}
	case "INSTRUCTION_SET":
		document.InstructionSet = &controlplanev1.InstructionSetSpec{}
		if options.Unmarshal(envelope.Spec, document.InstructionSet) != nil {
			return Document{}, errors.New("Git instruction set spec is invalid")
		}
		document.InstructionSet.Ownership = ownership
	case "PROVIDER_POOL":
		document.ProviderPool = &controlplanev1.ProviderPoolSpec{}
		if options.Unmarshal(envelope.Spec, document.ProviderPool) != nil {
			return Document{}, errors.New("Git provider pool spec is invalid")
		}
		document.ProviderPool.Ownership = ownership
	default:
		return Document{}, errors.New("Git source kind is unsupported")
	}
	return document, nil
}
