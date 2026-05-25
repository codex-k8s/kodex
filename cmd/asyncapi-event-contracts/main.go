package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v2"
)

type specConfig struct {
	Domain          string
	PackageName     string
	SpecPath        string
	EventTypeSchema string
	PayloadSchema   string
	OutputPath      string
}

type contract struct {
	Events     []eventConst
	Aggregates []aggregateConst
	Fields     []payloadField
}

type eventConst struct {
	Name      string
	Value     string
	Aggregate string
}

type aggregateConst struct {
	Name  string
	Value string
}

type payloadField struct {
	Name    string
	Type    string
	JSONTag string
}

func main() {
	specs := []specConfig{
		{
			Domain:          "access",
			PackageName:     "access",
			SpecPath:        "specs/asyncapi/access-manager.v1.yaml",
			EventTypeSchema: "AccessEventType",
			PayloadSchema:   "AccessEventPayload",
			OutputPath:      "libs/go/platformevents/access/events.gen.go",
		},
		{
			Domain:          "project",
			PackageName:     "project",
			SpecPath:        "specs/asyncapi/project-catalog.v1.yaml",
			EventTypeSchema: "ProjectEventType",
			PayloadSchema:   "ProjectEventPayload",
			OutputPath:      "libs/go/platformevents/project/events.gen.go",
		},
		{
			Domain:          "package",
			PackageName:     "packagehub",
			SpecPath:        "specs/asyncapi/package-hub.v1.yaml",
			EventTypeSchema: "PackageEventType",
			PayloadSchema:   "PackageEventPayload",
			OutputPath:      "libs/go/platformevents/packagehub/events.gen.go",
		},
		{
			Domain:          "provider",
			PackageName:     "provider",
			SpecPath:        "specs/asyncapi/provider-hub.v1.yaml",
			EventTypeSchema: "ProviderEventType",
			PayloadSchema:   "ProviderEventPayload",
			OutputPath:      "libs/go/platformevents/provider/events.gen.go",
		},
		{
			Domain:          "runtime",
			PackageName:     "runtimeevents",
			SpecPath:        "specs/asyncapi/runtime-manager.v1.yaml",
			EventTypeSchema: "RuntimeEventType",
			PayloadSchema:   "RuntimeEventPayload",
			OutputPath:      "libs/go/platformevents/runtime/events.gen.go",
		},
		{
			Domain:          "fleet",
			PackageName:     "fleet",
			SpecPath:        "specs/asyncapi/fleet-manager.v1.yaml",
			EventTypeSchema: "FleetEventType",
			PayloadSchema:   "FleetEventPayload",
			OutputPath:      "libs/go/platformevents/fleet/events.gen.go",
		},
		{
			Domain:          "agent",
			PackageName:     "agent",
			SpecPath:        "specs/asyncapi/agent-manager.v1.yaml",
			EventTypeSchema: "AgentEventType",
			PayloadSchema:   "AgentEventPayload",
			OutputPath:      "libs/go/platformevents/agent/events.gen.go",
		},
		{
			Domain:          "governance",
			PackageName:     "governance",
			SpecPath:        "specs/asyncapi/governance-manager.v1.yaml",
			EventTypeSchema: "GovernanceEventType",
			PayloadSchema:   "GovernanceEventPayload",
			OutputPath:      "libs/go/platformevents/governance/events.gen.go",
		},
		{
			Domain:          "interaction",
			PackageName:     "interaction",
			SpecPath:        "specs/asyncapi/interaction-hub.v1.yaml",
			EventTypeSchema: "InteractionEventType",
			PayloadSchema:   "InteractionEventPayload",
			OutputPath:      "libs/go/platformevents/interaction/events.gen.go",
		},
	}
	for _, spec := range specs {
		if err := generate(spec); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", spec.SpecPath, err)
			os.Exit(1)
		}
	}
}

func generate(spec specConfig) error {
	data, err := os.ReadFile(spec.SpecPath)
	if err != nil {
		return err
	}
	var root map[string]any
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}
	root, ok := normalize(raw).(map[string]any)
	if !ok {
		return fmt.Errorf("spec root is not a map")
	}
	contract, err := readContract(root, spec)
	if err != nil {
		return err
	}
	content, err := render(spec, contract)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(spec.OutputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(spec.OutputPath, content, 0o644)
}

func readContract(root map[string]any, spec specConfig) (contract, error) {
	components, err := childMap(root, "components")
	if err != nil {
		return contract{}, err
	}
	schemas, err := childMap(components, "schemas")
	if err != nil {
		return contract{}, err
	}
	eventSchema, err := childMap(schemas, spec.EventTypeSchema)
	if err != nil {
		return contract{}, err
	}
	payloadSchema, err := childMap(schemas, spec.PayloadSchema)
	if err != nil {
		return contract{}, err
	}

	eventValues, err := stringSlice(eventSchema, "enum")
	if err != nil {
		return contract{}, err
	}
	aggregates := eventAggregates(schemas)
	fields, err := payloadFields(payloadSchema)
	if err != nil {
		return contract{}, err
	}

	result := contract{Fields: fields}
	aggregateValues := make(map[string]struct{})
	for _, value := range eventValues {
		aggregate := aggregates[value]
		result.Events = append(result.Events, eventConst{
			Name:      "Event" + goName(stripDomain(value, spec.Domain)),
			Value:     value,
			Aggregate: aggregate,
		})
		if aggregate != "" {
			aggregateValues[aggregate] = struct{}{}
		}
	}
	for value := range aggregateValues {
		result.Aggregates = append(result.Aggregates, aggregateConst{Name: "Aggregate" + goName(value), Value: value})
	}
	sort.Slice(result.Aggregates, func(i, j int) bool { return result.Aggregates[i].Name < result.Aggregates[j].Name })
	return result, nil
}

func eventAggregates(schemas map[string]any) map[string]string {
	result := make(map[string]string)
	for _, raw := range schemas {
		schema, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		properties, err := childMap(schema, "properties")
		if err != nil {
			continue
		}
		eventType, err := constString(properties, "event_type")
		if err != nil {
			continue
		}
		aggregateType, err := constString(properties, "aggregate_type")
		if err != nil {
			continue
		}
		result[eventType] = aggregateType
	}
	return result
}

func payloadFields(schema map[string]any) ([]payloadField, error) {
	properties, err := childMap(schema, "properties")
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := make([]payloadField, 0, len(keys))
	for _, key := range keys {
		property, err := childMap(properties, key)
		if err != nil {
			return nil, err
		}
		fields = append(fields, payloadField{Name: goName(key), Type: goType(property), JSONTag: key})
	}
	return fields, nil
}

func render(spec specConfig, contract contract) ([]byte, error) {
	var out bytes.Buffer
	fmt.Fprintf(&out, "// Code generated by cmd/asyncapi-event-contracts from %s; DO NOT EDIT.\n\n", spec.SpecPath)
	fmt.Fprintf(&out, "package %s\n\n", spec.PackageName)
	fmt.Fprintln(&out, "const SchemaVersion = 1")
	fmt.Fprintln(&out)
	fmt.Fprintln(&out, "const (")
	for _, event := range contract.Events {
		fmt.Fprintf(&out, "\t%s = %q\n", event.Name, event.Value)
	}
	fmt.Fprintln(&out, ")")
	fmt.Fprintln(&out)
	fmt.Fprintln(&out, "const (")
	for _, aggregate := range contract.Aggregates {
		fmt.Fprintf(&out, "\t%s = %q\n", aggregate.Name, aggregate.Value)
	}
	fmt.Fprintln(&out, ")")
	fmt.Fprintln(&out)
	fmt.Fprintln(&out, "// Payload is the generated common payload envelope for domain events.")
	fmt.Fprintln(&out, "type Payload struct {")
	for _, field := range contract.Fields {
		fmt.Fprintf(&out, "\t%s %s `json:\"%s,omitempty\"`\n", field.Name, field.Type, field.JSONTag)
	}
	fmt.Fprintln(&out, "}")
	return format.Source(out.Bytes())
}

func childMap(parent map[string]any, key string) (map[string]any, error) {
	raw, ok := parent[key]
	if !ok {
		return nil, fmt.Errorf("missing map %q", key)
	}
	child, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%q is not a map", key)
	}
	return child, nil
}

func stringSlice(parent map[string]any, key string) ([]string, error) {
	raw, ok := parent[key]
	if !ok {
		return nil, fmt.Errorf("missing list %q", key)
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%q is not a list", key)
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%q contains non-string value", key)
		}
		values = append(values, value)
	}
	return values, nil
}

func constString(parent map[string]any, key string) (string, error) {
	property, err := childMap(parent, key)
	if err != nil {
		return "", err
	}
	value, ok := property["const"].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%q has no string const", key)
	}
	return value, nil
}

func goType(property map[string]any) string {
	switch property["type"] {
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		items, _ := childMap(property, "items")
		return "[]" + goType(items)
	default:
		return "string"
	}
}

func stripDomain(value string, domain string) string {
	prefix := domain + "."
	return strings.TrimPrefix(value, prefix)
}

func goName(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '_' || r == '.' || r == '-' })
	var out strings.Builder
	for _, part := range parts {
		lower := strings.ToLower(part)
		if initialism, ok := initialisms[lower]; ok {
			out.WriteString(initialism)
			continue
		}
		out.WriteString(strings.ToUpper(lower[:1]))
		if len(lower) > 1 {
			out.WriteString(lower[1:])
		}
	}
	return out.String()
}

var initialisms = map[string]string{
	"api":  "API",
	"grpc": "GRPC",
	"http": "HTTP",
	"id":   "ID",
	"mr":   "MR",
	"pr":   "PR",
	"sha":  "SHA",
	"uri":  "URI",
	"url":  "URL",
}

func normalize(value any) any {
	switch typed := value.(type) {
	case map[any]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			stringKey, ok := key.(string)
			if !ok {
				continue
			}
			result[stringKey] = normalize(child)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = normalize(child)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, child := range typed {
			result = append(result, normalize(child))
		}
		return result
	default:
		return value
	}
}
