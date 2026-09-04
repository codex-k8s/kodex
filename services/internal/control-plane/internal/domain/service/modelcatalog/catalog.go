// Package modelcatalog содержит закрытый каталог model capabilities control-plane.
package modelcatalog

import "slices"

type Model struct {
	ID, DefaultEffort string
	Efforts           []string
}

var catalog = []Model{
	{ID: "gpt-6-astra", DefaultEffort: "medium", Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
	{ID: "gpt-5.6-sol", DefaultEffort: "medium", Efforts: []string{"none", "low", "medium", "high", "xhigh", "max"}},
	{ID: "gpt-5.6-terra", DefaultEffort: "medium", Efforts: []string{"none", "low", "medium", "high", "xhigh", "max"}},
	{ID: "gpt-5.6-luna", DefaultEffort: "medium", Efforts: []string{"none", "low", "medium", "high", "xhigh", "max"}},
	{ID: "gpt-5.5", DefaultEffort: "medium", Efforts: []string{"low", "medium", "high", "xhigh"}},
	{ID: "gpt-5.4", DefaultEffort: "medium", Efforts: []string{"none", "low", "medium", "high", "xhigh"}},
	{ID: "gpt-5.4-mini", DefaultEffort: "medium", Efforts: []string{"none", "low", "medium", "high", "xhigh"}},
	{ID: "gpt-5.3-codex", DefaultEffort: "medium", Efforts: []string{"low", "medium", "high", "xhigh"}},
}

func Models(providerReported []string) []Model {
	result := make([]Model, len(catalog))
	for index, item := range catalog {
		result[index] = Model{ID: item.ID, DefaultEffort: item.DefaultEffort, Efforts: slices.Clone(item.Efforts)}
	}
	if slices.Contains(providerReported, "gpt-5.3-codex-spark") {
		result = append(result, Model{ID: "gpt-5.3-codex-spark", DefaultEffort: "medium", Efforts: []string{"low", "medium", "high", "xhigh"}})
	}
	return result
}

func Find(id string, providerReported []string) (Model, bool) {
	for _, model := range Models(providerReported) {
		if model.ID == id {
			return model, true
		}
	}
	return Model{}, false
}
