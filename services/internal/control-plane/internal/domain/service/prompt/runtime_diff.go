package prompt

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// RuntimeDescriptor не содержит credentials, locator или произвольные metadata.
type RuntimeDescriptor struct {
	Ref     string `json:"ref,omitempty"`
	Version int64  `json:"version,omitempty"`
	Digest  string `json:"digest,omitempty"`
	Value   string `json:"value,omitempty"`
}

type RuntimeChange struct {
	Component string              `json:"component"`
	Previous  []RuntimeDescriptor `json:"previous"`
	Current   []RuntimeDescriptor `json:"current"`
	Action    string              `json:"action"`
}

type RuntimeDiff struct {
	PreviousRevisionRef string          `json:"previousRevisionRef"`
	CurrentRevisionRef  string          `json:"currentRevisionRef"`
	SessionRef          string          `json:"sessionRef"`
	TurnRef             string          `json:"turnRef"`
	Attempt             int32           `json:"attempt"`
	Changes             []RuntimeChange `json:"changes"`
	Digest              string          `json:"digest"`
}

var runtimeComponentOrder = []string{"INSTRUCTIONS", "MODEL", "REASONING", "IMAGE", "ENVIRONMENT", "FILES", "SKILLS", "MEMORY", "TOOLS", "MCP", "INTEGRATIONS", "CAPABILITIES", "POLICY"}

func CompareRuntimeContexts(previous, current map[string][]RuntimeDescriptor, identity RuntimeDiff) (RuntimeDiff, error) {
	if identity.PreviousRevisionRef == "" || identity.CurrentRevisionRef == "" || identity.PreviousRevisionRef == identity.CurrentRevisionRef || identity.SessionRef == "" || identity.TurnRef == "" || identity.Attempt < 1 {
		return RuntimeDiff{}, fmt.Errorf("runtime diff identity is invalid: %w", ErrInvalid)
	}
	for _, context := range []map[string][]RuntimeDescriptor{previous, current} {
		if len(context) != len(runtimeComponentOrder) {
			return RuntimeDiff{}, ErrInvalid
		}
		for component, descriptors := range context {
			if !slices.Contains(runtimeComponentOrder, component) || len(descriptors) > 256 {
				return RuntimeDiff{}, ErrInvalid
			}
			for _, descriptor := range descriptors {
				if descriptor.Version < 0 || descriptor.Version > 9007199254740991 || len(descriptor.Ref) > 128 || len(descriptor.Value) > 256 ||
					!utf8.ValidString(descriptor.Ref) || !utf8.ValidString(descriptor.Value) ||
					strings.ContainsFunc(descriptor.Ref+descriptor.Value, unicode.IsControl) ||
					descriptor.Digest != "" && (!validDigest(descriptor.Digest) || strings.ToLower(descriptor.Digest) != descriptor.Digest) {
					return RuntimeDiff{}, fmt.Errorf("runtime diff descriptor for %s is invalid: %w", component, ErrInvalid)
				}
			}
		}
	}
	identity.Changes = []RuntimeChange{}
	identity.Digest = ""
	for _, component := range runtimeComponentOrder {
		if slices.Equal(previous[component], current[component]) {
			continue
		}
		identity.Changes = append(identity.Changes, RuntimeChange{Component: component, Previous: append([]RuntimeDescriptor{}, previous[component]...), Current: append([]RuntimeDescriptor{}, current[component]...), Action: "USE_CURRENT_CONTEXT"})
	}
	identity.Digest = semanticDigest(identity)
	if identity.Digest == "" {
		return RuntimeDiff{}, ErrInvalid
	}
	return identity, nil
}
