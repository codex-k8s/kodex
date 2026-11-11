package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
)

// Dependency describes a conservative source dependency without values, cache promises or authority.
// Dependency описывает консервативную зависимость от источника без значений, обещаний кеша и полномочий.
type Dependency struct {
	TemplateDigest, Path, Stability, Sensitivity, TrustScope, SourceDigest string
}

// templateDependencies analyzes every materialized source using the renderer AST, including branch predicates.
// Literal source is private owner content; resolved values remain data even when their source is pinned.
// templateDependencies анализирует все материализуемые исходники тем же AST, включая условия ветвлений.
// Литералы являются приватным текстом владельца; закреплённые значения по-прежнему остаются данными.
func templateDependencies(text string, snapshot Snapshot) ([]Dependency, error) {
	texts := []string{text, snapshot.StagePurposeTemplate, snapshot.StageExpectedResultTemplate}
	if snapshot.AssistantCore != nil {
		texts = append(texts, snapshot.AssistantCore.Content)
	}
	for _, extra := range snapshot.ExtraTemplates {
		texts = append(texts, extra.Content)
	}
	result := []Dependency{}
	seen := map[string]bool{}
	for _, source := range texts {
		if source == "" {
			continue
		}
		hash := sha256.Sum256([]byte(source))
		digest := hex.EncodeToString(hash[:])
		if seen[digest] {
			continue
		}
		seen[digest] = true
		parsed, err := parseTemplate(source)
		if err != nil {
			return nil, ErrInvalid
		}
		analysis := analyzeTemplate(parsed, Catalog(), validationTemplateData())
		if analysis.unknown != "" {
			return nil, ErrInvalid
		}
		result = append(result, Dependency{TemplateDigest: digest, Path: "$literal", Stability: "STATIC_LITERAL", Sensitivity: "PRIVATE", TrustScope: "OWNER_TEMPLATE", SourceDigest: digest})
		paths := map[string]bool{}
		for reference := range analysis.references {
			for path := range Catalog() {
				if path == reference || strings.HasPrefix(path, reference+".") {
					paths[path] = true
				}
			}
		}
		slots, valid := templateSlots(parsed)
		if !valid {
			return nil, ErrInvalid
		}
		for _, slot := range slots {
			paths["slot."+string(slot)] = true
		}
		names := make([]string, 0, len(paths))
		for path := range paths {
			names = append(names, path)
		}
		slices.Sort(names)
		for _, path := range names {
			entry := Dependency{TemplateDigest: digest, Path: path, Stability: "TURN_LIVE", Sensitivity: "PRIVATE", TrustScope: "DATA_ONLY"}
			pin := snapshot.ContextPin
			unavailable := false
			for name, reason := range snapshot.UnavailableVariables {
				unavailable = unavailable || reason != "" && (path == name || strings.HasPrefix(path, name+"."))
			}
			if !validDigest(pin.Digest) || unavailable {
				entry.Stability = "UNKNOWN"
			} else {
				switch path {
				case "runtime.environment.ref", "runtime.environment.image.reference", "runtime.environment.image.digest", "runtime.environment.tools", "environment.ref":
					if validDigest(pin.EnvironmentDigest) && pin.EnvironmentVersionRef != "" {
						entry.Stability, entry.SourceDigest = "VERSION_PINNED", pin.EnvironmentDigest
					}
				case "agent.ref", "agent.name":
					if pin.AgentRef != "" && pin.AgentVersion > 0 {
						entry.Stability, entry.SourceDigest = "VERSION_PINNED", pin.Digest
					}
				case "workflow.ref", "workflow.name", "workflow.purpose", "workflow.stage.key", "step.key", "step.name":
					if pin.WorkflowRevisionRef != "" && pin.WorkflowVersion > 0 {
						entry.Stability, entry.SourceDigest = "VERSION_PINNED", pin.Digest
					}
				}
			}
			result = append(result, entry)
		}
	}
	if len(result) > 512 {
		return nil, ErrInvalid
	}
	return result, nil
}
