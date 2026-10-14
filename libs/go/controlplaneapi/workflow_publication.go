package controlplaneapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

const WorkflowPublicationMaximumBytes = 1 << 20

var errWorkflowPublication = errors.New("workflow publication projection invalid")

// Версионированный wire whitelist; eligibility и owner resolution остаются в CP.
type WorkflowPublication struct {
	Version             int                        `json:"version"`
	WorkflowRef         string                     `json:"workflow_ref"`
	ProjectRef          string                     `json:"project_ref"`
	PublishedRef        string                     `json:"published_ref"`
	SpecDigest          string                     `json:"spec_digest"`
	WorkflowVersion     int64                      `json:"workflow_version,string"`
	PublishedVersion    int32                      `json:"published_version"`
	Name                string                     `json:"name"`
	Purpose             string                     `json:"purpose"`
	CoordinatorAgentRef string                     `json:"coordinator_agent_ref"`
	Instructions        string                     `json:"instructions"`
	CompletionCriteria  string                     `json:"completion_criteria"`
	InputFields         []WorkflowPublicationInput `json:"input_fields"`
	Steps               []WorkflowPublicationStep  `json:"steps"`
	Concurrency         int32                      `json:"concurrency"`
	TimeoutSeconds      int64                      `json:"timeout_seconds"`
	GateDecisions       []string                   `json:"gate_decisions"`
	ResultSchema        WorkflowPublicationSchema  `json:"result_schema"`
}
type WorkflowPublicationInput struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	ValueType    string   `json:"value_type"`
	Description  string   `json:"description"`
	DefaultValue string   `json:"default_value"`
	Required     bool     `json:"required"`
	Options      []string `json:"options"`
}
type WorkflowPublicationStep struct {
	Key                    string   `json:"key"`
	Name                   string   `json:"name"`
	AgentRef               string   `json:"agent_ref"`
	Instructions           string   `json:"instructions"`
	ExpectedResult         string   `json:"expected_result"`
	Position               int32    `json:"position"`
	ParallelGroup          int32    `json:"parallel_group"`
	TimeoutSeconds         int32    `json:"timeout_seconds"`
	Parallel               bool     `json:"parallel"`
	HumanGateAfter         bool     `json:"human_gate_after"`
	DependsOn              []string `json:"depends_on"`
	GateDecisions          []string `json:"gate_decisions"`
	RequiredCapabilityKeys []string `json:"required_capability_keys"`
}

// Только локальная структурная JSON Schema. $ref/$id/resolvers и произвольный
// payload не принимаются. Пустая schema остаётся явным {}.
type WorkflowPublicationSchema struct {
	Type                 string                                `json:"type,omitempty"`
	Title                string                                `json:"title,omitempty"`
	Description          string                                `json:"description,omitempty"`
	Properties           map[string]*WorkflowPublicationSchema `json:"properties,omitempty"`
	Required             []string                              `json:"required,omitempty"`
	Items                *WorkflowPublicationSchema            `json:"items,omitempty"`
	Enum                 []json.RawMessage                     `json:"enum,omitempty"`
	AdditionalProperties *bool                                 `json:"additionalProperties,omitempty"`
	Minimum              *float64                              `json:"minimum,omitempty"`
	Maximum              *float64                              `json:"maximum,omitempty"`
	MinLength            *int64                                `json:"minLength,omitempty"`
	MaxLength            *int64                                `json:"maxLength,omitempty"`
	MinItems             *int64                                `json:"minItems,omitempty"`
	MaxItems             *int64                                `json:"maxItems,omitempty"`
}

func DecodeWorkflowPublicationSchema(raw []byte) (WorkflowPublicationSchema, error) {
	var schema WorkflowPublicationSchema
	if len(raw) == 0 || len(raw) > WorkflowPublicationMaximumBytes || !utf8.Valid(raw) || !uniqueWorkflowJSON(raw) {
		return schema, errWorkflowPublication
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&schema) != nil || decoder.Decode(&struct{}{}) != io.EOF || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || !validWorkflowSchema(&schema, 0) {
		return WorkflowPublicationSchema{}, errWorkflowPublication
	}
	if !workflowSchemaKeywordsValid(raw, 0) {
		return WorkflowPublicationSchema{}, errWorkflowPublication
	}
	return schema, nil
}

func EncodeWorkflowPublication(value WorkflowPublication) ([]byte, string, error) {
	if !validWorkflowPublication(value) {
		return nil, "", errWorkflowPublication
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > WorkflowPublicationMaximumBytes {
		return nil, "", errWorkflowPublication
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:]), nil
}

func workflowSchemaKeywordsValid(raw []byte, depth int) bool {
	if depth > 16 {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
		if key == "type" {
			var kind string
			if json.Unmarshal(value, &kind) != nil || kind == "" {
				return false
			}
		}
		if key == "items" && !workflowSchemaKeywordsValid(value, depth+1) {
			return false
		}
		if key == "properties" {
			var children map[string]json.RawMessage
			if json.Unmarshal(value, &children) != nil {
				return false
			}
			for _, child := range children {
				if !workflowSchemaKeywordsValid(child, depth+1) {
					return false
				}
			}
		}
	}
	return true
}

func DecodeWorkflowPublication(raw []byte, digest string) (WorkflowPublication, error) {
	var value WorkflowPublication
	if len(raw) == 0 || len(raw) > WorkflowPublicationMaximumBytes || !utf8.Valid(raw) || !taskSessionDigest.MatchString(digest) || !uniqueWorkflowJSON(raw) {
		return value, errWorkflowPublication
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return WorkflowPublication{}, errWorkflowPublication
	}
	canonical, actual, err := EncodeWorkflowPublication(value)
	if err != nil || actual != digest || !bytes.Equal(raw, canonical) {
		return WorkflowPublication{}, errWorkflowPublication
	}
	return value, nil
}

func validWorkflowPublication(v WorkflowPublication) bool {
	for _, ref := range []string{v.WorkflowRef, v.ProjectRef, v.PublishedRef, v.CoordinatorAgentRef} {
		if !taskSessionRef.MatchString(ref) {
			return false
		}
	}
	if v.Version != 1 || v.WorkflowVersion < 1 || v.PublishedVersion < 1 || !taskSessionDigest.MatchString(v.SpecDigest) || len(v.InputFields) > 100 || len(v.Steps) < 1 || len(v.Steps) > 200 || v.Concurrency < 1 || v.Concurrency > 100 || v.TimeoutSeconds < 1 || v.TimeoutSeconds > 604800 || !validWorkflowSchema(&v.ResultSchema, 0) {
		return false
	}
	seen := map[string]bool{}
	for i, step := range v.Steps {
		if step.Key == "" || len(step.Key) > 96 || seen[step.Key] || step.Position != int32(i+1) || !taskSessionRef.MatchString(step.AgentRef) || step.TimeoutSeconds < 1 || step.TimeoutSeconds > 86400 || len(step.DependsOn) > 200 || len(step.RequiredCapabilityKeys) > 50 || !workflowDecisions(step.GateDecisions) {
			return false
		}
		for _, dep := range step.DependsOn {
			if !seen[dep] {
				return false
			}
		}
		seen[step.Key] = true
	}
	fields := map[string]bool{}
	for _, field := range v.InputFields {
		if field.Key == "" || len(field.Key) > 80 || fields[field.Key] || len(field.Options) > 50 {
			return false
		}
		fields[field.Key] = true
		switch field.ValueType {
		case "TEXT", "LONG_TEXT", "NUMBER", "BOOLEAN", "DATE", "SELECT":
		default:
			return false
		}
	}
	if !workflowDecisions(v.GateDecisions) {
		return false
	}
	raw, err := json.Marshal(v)
	return err == nil && workflowWireText(raw) && workflowPublicationText(v)
}

// encoding/json заменяет invalid UTF-8; проверяем все исходные строки до encode.
func workflowPublicationText(v WorkflowPublication) bool {
	texts := []string{v.Name, v.Purpose, v.Instructions, v.CompletionCriteria}
	for _, f := range v.InputFields {
		texts = append(texts, f.Key, f.Label, f.Description, f.DefaultValue)
		texts = append(texts, f.Options...)
	}
	for _, s := range v.Steps {
		texts = append(texts, s.Key, s.Name, s.Instructions, s.ExpectedResult)
		texts = append(texts, s.DependsOn...)
		texts = append(texts, s.RequiredCapabilityKeys...)
	}
	for _, s := range texts {
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return false
		}
	}
	return true
}
func workflowDecisions(values []string) bool {
	if len(values) > 4 {
		return false
	}
	for _, v := range values {
		switch v {
		case "APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL":
		default:
			return false
		}
	}
	return true
}
func validWorkflowSchema(v *WorkflowPublicationSchema, depth int) bool {
	if v == nil || depth > 16 || len(v.Properties) > 100 || len(v.Required) > 100 || len(v.Enum) > 100 || !utf8.ValidString(v.Title) || !utf8.ValidString(v.Description) || len(v.Title) > 1024 || len(v.Description) > 8192 {
		return false
	}
	switch v.Type {
	case "", "object", "array", "string", "number", "integer", "boolean", "null":
	default:
		return false
	}
	for key, item := range v.Properties {
		if key == "" || len(key) > 160 || !utf8.ValidString(key) || !validWorkflowSchema(item, depth+1) {
			return false
		}
	}
	required := map[string]bool{}
	for _, key := range v.Required {
		if _, ok := v.Properties[key]; !ok || required[key] {
			return false
		}
		required[key] = true
	}
	if v.Items != nil && !validWorkflowSchema(v.Items, depth+1) {
		return false
	}
	for _, value := range v.Enum {
		if len(value) > 1024 || !utf8.Valid(value) || !uniqueWorkflowJSON(value) {
			return false
		}
		var scalar any
		if json.Unmarshal(value, &scalar) != nil {
			return false
		}
		switch scalar.(type) {
		case nil, string, float64, bool:
		default:
			return false
		}
	}
	for _, n := range []*float64{v.Minimum, v.Maximum} {
		if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0)) {
			return false
		}
	}
	for _, n := range []*int64{v.MinLength, v.MaxLength, v.MinItems, v.MaxItems} {
		if n != nil && (*n < 0 || *n > WorkflowPublicationMaximumBytes) {
			return false
		}
	}
	if v.Minimum != nil && v.Maximum != nil && *v.Minimum > *v.Maximum || v.MinLength != nil && v.MaxLength != nil && *v.MinLength > *v.MaxLength || v.MinItems != nil && v.MaxItems != nil && *v.MinItems > *v.MaxItems {
		return false
	}
	return true
}
func workflowWireText(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := d.Token()
		if err == io.EOF {
			return true
		}
		if err != nil {
			return false
		}
		if s, ok := token.(string); ok && (!utf8.ValidString(s) || strings.ContainsRune(s, 0)) {
			return false
		}
	}
}
func uniqueWorkflowJSON(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func(int) bool
	walk = func(depth int) bool {
		if depth > 32 {
			return false
		}
		t, err := d.Token()
		if err != nil {
			return false
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return true
		}
		if delim != '{' && delim != '[' {
			return false
		}
		keys := map[string]bool{}
		for d.More() {
			if delim == '{' {
				token, err := d.Token()
				key, ok := token.(string)
				if err != nil || !ok || keys[key] {
					return false
				}
				keys[key] = true
			}
			if !walk(depth + 1) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && (delim == '{' && end == json.Delim('}') || delim == '[' && end == json.Delim(']'))
	}
	return walk(0) && d.Decode(&struct{}{}) == io.EOF
}
