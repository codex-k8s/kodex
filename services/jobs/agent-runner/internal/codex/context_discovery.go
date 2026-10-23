package codex

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// discoveredSkill models the bounded provider discovery descriptor, including optional opaque interface/dependency metadata and plugin provenance; parsing a descriptor alone does not admit or enable the skill.
// discoveredSkill описывает ограниченный provider discovery descriptor с optional opaque interface/dependency metadata и plugin provenance; один разбор descriptor не допускает и не включает skill.
type discoveredSkill struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Path             string          `json:"path"`
	Enabled          bool            `json:"enabled"`
	Scope            string          `json:"scope"`
	ShortDescription *string         `json:"shortDescription,omitempty"`
	Interface        json.RawMessage `json:"interface,omitempty"`
	Dependencies     json.RawMessage `json:"dependencies,omitempty"`
	PluginID         *string         `json:"pluginId,omitempty"`
}

// configureContextSkills sets only the fixed read-only extra root, reloads discovery, enables exact snapshot paths and disables unbound discoveries, then re-reads exact names/descriptions/enabled states and rejects plugin-backed expected skills or missing pins. The caller supplies the already trusted snapshot; discovery cannot broaden it.
// configureContextSkills задаёт лишь фиксированный read-only extra root, перезагружает discovery, включает точные snapshot paths и отключает unbound discoveries, затем повторно сверяет names/descriptions/enabled и отклоняет plugin-backed expected skills или missing pins. Доверенный snapshot задаёт вызывающий; discovery не расширяет его.
func (server *appServer) configureContextSkills(ctx context.Context, state *protocolState, input model.Input, snapshot runtimecontract.RuntimeContextSnapshot) error {
	raw, err := server.call(ctx, state, "skills/extraRoots/set", map[string]any{"extraRoots": []string{runtimecontract.RuntimeContextRoot + "/skills"}})
	if err != nil {
		return err
	}
	var empty struct{}
	if strictDecode(raw, &empty) != nil {
		return runtimecontract.ErrRuntimeContext
	}
	list := func() ([]discoveredSkill, error) {
		raw, err := server.call(ctx, state, "skills/list", map[string]any{"cwds": []string{workingDirectory(input)}, "forceReload": true})
		if err != nil {
			return nil, err
		}
		return decodeContextSkills(raw, workingDirectory(input))
	}
	discovered, err := list()
	if err != nil {
		return err
	}
	expected := make(map[string]runtimecontract.RuntimeSkillBundle, len(snapshot.Skills))
	for _, skill := range snapshot.Skills {
		expected[path.Join(runtimecontract.RuntimeContextRoot, "skills", skill.BundleRef, "SKILL.md")] = skill
	}
	for _, skill := range discovered {
		_, enabled := expected[skill.Path]
		if enabled == skill.Enabled {
			continue
		}
		raw, err := server.call(ctx, state, "skills/config/write", map[string]any{"path": skill.Path, "enabled": enabled})
		if err != nil {
			return err
		}
		var result struct {
			EffectiveEnabled bool `json:"effectiveEnabled"`
		}
		if strictDecode(raw, &result) != nil || result.EffectiveEnabled != enabled {
			return runtimecontract.ErrRuntimeContext
		}
	}
	discovered, err = list()
	if err != nil {
		return err
	}
	for _, skill := range discovered {
		pin, exists := expected[skill.Path]
		if !exists {
			if skill.Enabled {
				return runtimecontract.ErrRuntimeContext
			}
			continue
		}
		if !skill.Enabled || skill.Name != pin.Name || skill.Description != pin.Description || skill.PluginID != nil {
			return runtimecontract.ErrRuntimeContext
		}
		delete(expected, skill.Path)
	}
	if len(expected) != 0 {
		return runtimecontract.ErrRuntimeContext
	}
	return nil
}

// decodeContextSkills strictly decodes one bounded discovery result for the exact working directory, rejecting reported errors, excessive counts, unsafe/duplicate paths, oversized metadata and unknown scopes. It intentionally returns unbound discoveries too so reconciliation can disable them; it is not an admission decision.
// decodeContextSkills строго разбирает один ограниченный discovery result для точного working directory, отклоняя reported errors, чрезмерное число, unsafe/duplicate paths, oversized metadata и unknown scopes. Намеренно возвращает также unbound discoveries для отключения при reconciliation; не является admission decision.
func decodeContextSkills(raw json.RawMessage, cwd string) ([]discoveredSkill, error) {
	var response struct {
		Data []struct {
			CWD    string            `json:"cwd"`
			Skills []discoveredSkill `json:"skills"`
			Errors []json.RawMessage `json:"errors"`
		} `json:"data"`
	}
	if len(raw) > 1<<20 || strictDecode(raw, &response) != nil || len(response.Data) != 1 || response.Data[0].CWD != cwd ||
		len(response.Data[0].Errors) != 0 || len(response.Data[0].Skills) > 256 {
		return nil, runtimecontract.ErrRuntimeContext
	}
	seen := map[string]bool{}
	for _, skill := range response.Data[0].Skills {
		if !path.IsAbs(skill.Path) || path.Clean(skill.Path) != skill.Path || strings.ContainsAny(skill.Path, "\x00\r\n") ||
			len(skill.Path) > 1024 || skill.Name == "" || len(skill.Name) > 640 || len(skill.Description) > 8000 || seen[skill.Path] {
			return nil, runtimecontract.ErrRuntimeContext
		}
		switch skill.Scope {
		case "user", "repo", "system", "admin":
		default:
			return nil, runtimecontract.ErrRuntimeContext
		}
		seen[skill.Path] = true
	}
	return response.Data[0].Skills, nil
}
