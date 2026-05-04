package service

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v2"
)

func TestAccessEventConstantsMatchAsyncAPI(t *testing.T) {
	t.Parallel()

	rawSpec, err := os.ReadFile("../../../../../../specs/asyncapi/access-manager.v1.yaml")
	if err != nil {
		t.Fatalf("read access-manager AsyncAPI spec: %v", err)
	}
	var spec asyncAPISpec
	if err := yaml.Unmarshal(rawSpec, &spec); err != nil {
		t.Fatalf("parse access-manager AsyncAPI spec: %v", err)
	}
	schemas := spec.Components.Schemas
	envelopeSchema, ok := schemas["AccessEventEnvelope"]
	if !ok {
		t.Fatal("AccessEventEnvelope schema is missing")
	}
	envelopeEvents := refSet(envelopeSchema.OneOf)
	for _, contract := range implementedAccessEventContracts() {
		kindName, kindSchema := findEventKindSchema(t, schemas, contract.EventType)
		if got := kindSchema.Properties["aggregate_type"].Const; got != contract.AggregateType {
			t.Fatalf("event %s aggregate_type = %q, want %q", contract.EventType, got, contract.AggregateType)
		}
		payloadRef := kindSchema.Properties["payload"].Ref
		if payloadRef == "" {
			t.Fatalf("event %s payload ref is missing", contract.EventType)
		}
		payloadName := schemaRefName(payloadRef)
		if _, ok := schemas[payloadName]; !ok {
			t.Fatalf("event %s payload schema %s is missing", contract.EventType, payloadName)
		}
		eventName := findEventEnvelopeSchema(t, schemas, kindName, contract.EventType)
		if !envelopeEvents[eventName] {
			t.Fatalf("event %s schema %s is not referenced by AccessEventEnvelope", contract.EventType, eventName)
		}
	}
}

type asyncAPISpec struct {
	Components struct {
		Schemas map[string]asyncSchema `yaml:"schemas"`
	} `yaml:"components"`
}

type asyncSchema struct {
	OneOf      []asyncSchemaRef         `yaml:"oneOf"`
	AllOf      []asyncSchemaRef         `yaml:"allOf"`
	Properties map[string]asyncProperty `yaml:"properties"`
}

type asyncSchemaRef struct {
	Ref string `yaml:"$ref"`
}

type asyncProperty struct {
	Const string `yaml:"const"`
	Ref   string `yaml:"$ref"`
}

type accessEventContract struct {
	EventType     string
	AggregateType string
}

func implementedAccessEventContracts() []accessEventContract {
	return []accessEventContract{
		{EventType: accessEventOrganizationCreated, AggregateType: accessAggregateOrganization},
		{EventType: accessEventGroupCreated, AggregateType: accessAggregateGroup},
		{EventType: accessEventMembershipCreated, AggregateType: accessAggregateMembership},
		{EventType: accessEventMembershipUpdated, AggregateType: accessAggregateMembership},
		{EventType: accessEventMembershipDisabled, AggregateType: accessAggregateMembership},
		{EventType: accessEventAllowlistEntryCreated, AggregateType: accessAggregateAllowlistEntry},
		{EventType: accessEventAllowlistEntryUpdated, AggregateType: accessAggregateAllowlistEntry},
		{EventType: accessEventAllowlistEntryDisabled, AggregateType: accessAggregateAllowlistEntry},
		{EventType: accessEventUserCreated, AggregateType: accessAggregateUser},
		{EventType: accessEventUserStatusChanged, AggregateType: accessAggregateUser},
		{EventType: accessEventUserIdentityLinked, AggregateType: accessAggregateUser},
		{EventType: accessEventExternalProviderCreated, AggregateType: accessAggregateExternalProvider},
		{EventType: accessEventExternalProviderUpdated, AggregateType: accessAggregateExternalProvider},
		{EventType: accessEventExternalProviderDisabled, AggregateType: accessAggregateExternalProvider},
		{EventType: accessEventExternalAccountCreated, AggregateType: accessAggregateExternalAccount},
		{EventType: accessEventExternalAccountStatusChanged, AggregateType: accessAggregateExternalAccount},
		{EventType: accessEventExternalAccountBindingCreated, AggregateType: accessAggregateExternalAccountBinding},
		{EventType: accessEventExternalAccountBindingUpdated, AggregateType: accessAggregateExternalAccountBinding},
		{EventType: accessEventExternalAccountBindingDisabled, AggregateType: accessAggregateExternalAccountBinding},
		{EventType: accessEventAccessActionCreated, AggregateType: accessAggregateAccessAction},
		{EventType: accessEventAccessActionUpdated, AggregateType: accessAggregateAccessAction},
		{EventType: accessEventAccessRuleCreated, AggregateType: accessAggregateAccessRule},
		{EventType: accessEventAccessRuleUpdated, AggregateType: accessAggregateAccessRule},
		{EventType: accessEventAccessDecisionRecorded, AggregateType: accessAggregateAccessDecisionAudit},
	}
}

func findEventKindSchema(t *testing.T, schemas map[string]asyncSchema, eventType string) (string, asyncSchema) {
	t.Helper()

	var foundName string
	var foundSchema asyncSchema
	for name, schema := range schemas {
		if schema.Properties["event_type"].Const != eventType {
			continue
		}
		if foundName != "" {
			t.Fatalf("event %s declared by multiple AsyncAPI schemas: %s and %s", eventType, foundName, name)
		}
		foundName = name
		foundSchema = schema
	}
	if foundName == "" {
		t.Fatalf("event %s kind schema is missing", eventType)
	}
	return foundName, foundSchema
}

func findEventEnvelopeSchema(t *testing.T, schemas map[string]asyncSchema, kindName string, eventType string) string {
	t.Helper()

	var foundName string
	for name, schema := range schemas {
		for _, item := range schema.AllOf {
			if schemaRefName(item.Ref) != kindName {
				continue
			}
			if foundName != "" {
				t.Fatalf("event %s envelope declared by multiple AsyncAPI schemas: %s and %s", eventType, foundName, name)
			}
			foundName = name
		}
	}
	if foundName == "" {
		t.Fatalf("event %s envelope schema for kind %s is missing", eventType, kindName)
	}
	return foundName
}

func refSet(refs []asyncSchemaRef) map[string]bool {
	values := make(map[string]bool, len(refs))
	for _, ref := range refs {
		values[schemaRefName(ref.Ref)] = true
	}
	return values
}

func schemaRefName(ref string) string {
	_, name, ok := strings.Cut(ref, "#/components/schemas/")
	if !ok {
		return ref
	}
	return name
}
