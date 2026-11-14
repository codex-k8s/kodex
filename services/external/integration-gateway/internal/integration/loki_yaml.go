package integration

import (
	"bytes"
	"io"

	"go.yaml.in/yaml/v3"
)

// boundedProviderYAML parses one document without aliases, anchors, custom tags, duplicate keys, complex keys or excessive structure before any semantic decode; scalar callbacks run only on safely parsed nodes.
// boundedProviderYAML разбирает один document без aliases, anchors, custom tags, duplicate/complex keys и excessive structure до semantic decode; scalar callbacks получают только безопасно разобранные nodes.
func boundedProviderYAML(raw []byte, scalar func(string) bool) (*yaml.Node, bool) {
	if len(raw) < 1 || len(raw) > maximumResponseBytes {
		return nil, false
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var document yaml.Node
	if decoder.Decode(&document) != nil || decoder.Decode(&yaml.Node{}) != io.EOF || len(document.Content) != 1 {
		return nil, false
	}
	nodes := 0
	var visit func(*yaml.Node, int) bool
	visit = func(node *yaml.Node, depth int) bool {
		nodes++
		if nodes > 8192 || depth > 24 || node.Anchor != "" || node.Alias != nil {
			return false
		}
		switch node.Kind {
		case yaml.DocumentNode:
			if len(node.Content) != 1 {
				return false
			}
		case yaml.MappingNode:
			if node.Tag != "!!map" || len(node.Content)%2 != 0 {
				return false
			}
			seen := map[string]bool{}
			for i := 0; i < len(node.Content); i += 2 {
				key := node.Content[i]
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "" || seen[key.Value] {
					return false
				}
				seen[key.Value] = true
			}
		case yaml.SequenceNode:
			if node.Tag != "!!seq" {
				return false
			}
		case yaml.ScalarNode:
			if len(node.Value) > 8192 {
				return false
			}
			switch node.Tag {
			case "!!str", "!!int", "!!float", "!!bool", "!!null":
			default:
				return false
			}
			if scalar != nil && scalar(node.Value) {
				return false
			}
		case yaml.AliasNode:
			return false
		default:
			return false
		}
		for _, child := range node.Content {
			if !visit(child, depth+1) {
				return false
			}
		}
		return true
	}
	return &document, visit(&document, 0)
}

// decodeLokiYAML performs the bounded node validation before decoding selected native ruler fields; malformed documents never supply a recovered intermediate model.
// decodeLokiYAML проверяет bounded nodes до decode выбранных native ruler fields; malformed document не выдаёт recovered intermediate model.
func decodeLokiYAML(raw []byte, target any) error {
	document, ok := boundedProviderYAML(raw, nil)
	if !ok || document.Decode(target) != nil {
		return &SafeError{Code: providerResponseInvalidCode}
	}
	return nil
}
