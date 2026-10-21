package admissioncontroller

import "testing"

// TestControllerAndRendererShareCanonicalPathBoundary pins the unchanged lexical
// path grammar in both constructors without filesystem inspection or script execution.
// A clean nonexistent absolute path is valid here; usability is not this check's job.
// TestControllerAndRendererShareCanonicalPathBoundary закрепляет неизменную
// лексическую грамматику обоих путей без проверки файлов либо исполнения скрипта.
// Чистый отсутствующий абсолютный путь здесь допустим; пригодность проверяется позже.
func TestControllerAndRendererShareCanonicalPathBoundary(t *testing.T) {
	for _, scenario := range []struct {
		name, path string
		valid      bool
	}{
		{"absolute", "/opt/kodex/render.sh", true},
		{"nonexistent", "/missing-fixture/render.sh", true},
		{"root", "/", true},
		{"empty", "", false},
		{"relative", "render.sh", false},
		{"dot", "/opt/./kodex/render.sh", false},
		{"parent", "/opt/kodex/../render.sh", false},
		{"trailing slash", "/opt/kodex/render.sh/", false},
		{"repeated separator", "/opt//kodex/render.sh", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			config := testConfig()
			config.RendererPath = scenario.path
			if (config.Validate() == nil) != scenario.valid {
				t.Fatal("controller renderer path boundary changed")
			}
			renderer, err := NewScriptRenderer(scenario.path)
			if (err == nil) != scenario.valid || (renderer != nil) != scenario.valid {
				t.Fatal("renderer path boundary differs from controller")
			}
			if renderer != nil && renderer.path != scenario.path {
				t.Fatal("renderer repaired path instead of retaining it")
			}
		})
	}
}
