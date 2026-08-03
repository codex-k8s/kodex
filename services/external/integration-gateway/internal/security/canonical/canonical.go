package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
)

const RedactedValue = "[REDACTED]"

var sensitiveNames = map[string]struct{}{
	"authorization": {}, "credential": {}, "password": {}, "secret": {},
	"token": {}, "api_key": {}, "apikey": {}, "access_key": {}, "private_key": {},
}

type Request struct {
	DefinitionID            string          `json:"definition_id"`
	DefinitionVersion       uint64          `json:"definition_version"`
	DefinitionDigest        string          `json:"definition_digest"`
	ConnectionID            string          `json:"connection_id"`
	ConnectionRevision      uint64          `json:"connection_revision"`
	ConnectionGeneration    uint64          `json:"connection_generation"`
	ConnectionBindingDigest string          `json:"connection_binding_digest"`
	Capability              string          `json:"capability"`
	ToolName                string          `json:"tool_name"`
	ToolVersion             uint64          `json:"tool_version"`
	TenantID                string          `json:"tenant_id"`
	ProjectID               string          `json:"project_id"`
	ProcessID               string          `json:"process_id"`
	SessionID               string          `json:"session_id"`
	SessionVersion          uint64          `json:"session_version"`
	ThreadID                string          `json:"thread_id"`
	TurnID                  string          `json:"turn_id"`
	TurnVersion             uint64          `json:"turn_version"`
	Attempt                 uint32          `json:"attempt"`
	InputDigest             string          `json:"input_digest"`
	RuntimeRevisionID       string          `json:"runtime_revision_id"`
	RuntimeRevisionVersion  uint64          `json:"runtime_revision_version"`
	RuntimeRevisionDigest   string          `json:"runtime_revision_digest"`
	RuntimeManifestDigest   string          `json:"runtime_manifest_digest"`
	RoleID                  string          `json:"role_id"`
	RoleVersion             uint64          `json:"role_version"`
	GrantID                 string          `json:"grant_id"`
	GrantGeneration         uint64          `json:"grant_generation"`
	Method                  string          `json:"method"`
	Path                    string          `json:"path"`
	Arguments               json.RawMessage `json:"arguments"`
}

func Normalize(raw []byte, maximumBytes int64) (json.RawMessage, error) {
	if len(raw) == 0 || int64(len(raw)) > maximumBytes {
		return nil, errors.New("JSON payload size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("JSON payload is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing JSON data is forbidden")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("canonicalize JSON payload")
	}
	return canonical, nil
}

func Hash(request Request) (string, json.RawMessage, error) {
	arguments, err := Normalize(request.Arguments, 256<<10)
	if err != nil {
		return "", nil, err
	}
	request.Arguments = arguments
	canonical, err := json.Marshal(request)
	if err != nil {
		return "", nil, errors.New("marshal canonical request")
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), canonical, nil
}

func Preview(arguments json.RawMessage, pointers []string) (json.RawMessage, error) {
	canonical, err := Normalize(arguments, 256<<10)
	if err != nil {
		return nil, err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("decode canonical preview")
	}
	redactSensitive(value)
	sorted := append([]string(nil), pointers...)
	sort.Strings(sorted)
	for _, pointer := range sorted {
		redactPointer(value, pointer)
	}
	preview, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("marshal redacted preview")
	}
	return preview, nil
}

func redactSensitive(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, sensitive := sensitiveNames[strings.ToLower(key)]; sensitive {
				typed[key] = RedactedValue
				continue
			}
			redactSensitive(child)
		}
	case []any:
		for _, child := range typed {
			redactSensitive(child)
		}
	}
}

func redactPointer(root any, pointer string) {
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	current := root
	for index, raw := range parts {
		part := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch container := current.(type) {
		case map[string]any:
			if index == len(parts)-1 {
				if _, exists := container[part]; exists {
					container[part] = RedactedValue
				}
				return
			}
			child, exists := container[part]
			if !exists {
				return
			}
			current = child
		case []any:
			item, err := strconv.Atoi(part)
			if err != nil || item < 0 || item >= len(container) || strconv.Itoa(item) != part {
				return
			}
			if index == len(parts)-1 {
				container[item] = RedactedValue
				return
			}
			current = container[item]
		default:
			return
		}
	}
}
