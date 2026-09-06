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

// ValidateRuntimeDiff повторно проверяет closed schema и digest сохранённого notice.
func ValidateRuntimeDiff(diff RuntimeDiff) error {
	previous, current := map[string][]RuntimeDescriptor{}, map[string][]RuntimeDescriptor{}
	for _, component := range runtimeComponentOrder {
		previous[component], current[component] = nil, nil
	}
	last := -1
	for _, change := range diff.Changes {
		position := slices.Index(runtimeComponentOrder, change.Component)
		if position <= last || change.Action != "USE_CURRENT_CONTEXT" || slices.Equal(change.Previous, change.Current) {
			return ErrInvalid
		}
		last = position
		previous[change.Component], current[change.Component] = change.Previous, change.Current
	}
	var checked RuntimeDiff
	var err error
	if diff.CurrentRevisionRef == "" {
		checked, err = CompareProspectiveRuntimeContexts(previous, current, diff)
	} else {
		checked, err = CompareRuntimeContexts(previous, current, diff)
	}
	if err != nil || diff.Digest != checked.Digest {
		return ErrInvalid
	}
	return nil
}

func CompareRuntimeContexts(previous, current map[string][]RuntimeDescriptor, identity RuntimeDiff) (RuntimeDiff, error) {
	if identity.PreviousRevisionRef == "" || identity.CurrentRevisionRef == "" || identity.PreviousRevisionRef == identity.CurrentRevisionRef || identity.SessionRef == "" || identity.TurnRef == "" || identity.Attempt < 1 {
		return RuntimeDiff{}, fmt.Errorf("runtime diff identity is invalid: %w", ErrInvalid)
	}
	return compareRuntimeDescriptors(previous, current, identity)
}

func CompareProspectiveRuntimeContexts(previous, current map[string][]RuntimeDescriptor, identity RuntimeDiff) (RuntimeDiff, error) {
	if identity.PreviousRevisionRef == "" || identity.SessionRef == "" || identity.CurrentRevisionRef != "" || identity.TurnRef != "" || identity.Attempt != 0 {
		return RuntimeDiff{}, fmt.Errorf("prospective runtime diff identity is invalid: %w", ErrInvalid)
	}
	return compareRuntimeDescriptors(previous, current, identity)
}

func compareRuntimeDescriptors(previous, current map[string][]RuntimeDescriptor, identity RuntimeDiff) (RuntimeDiff, error) {
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
