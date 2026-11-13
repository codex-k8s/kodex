package runtimecontract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestContextWindowIsLowerOnlyAndNonAuthorizing proves native budget changes do not mutate
// catalog, route digest, cache authority or output limits, and malformed/upsize values fail closed.
// TestContextWindowIsLowerOnlyAndNonAuthorizing доказывает неизменность catalog, route digest,
// cache authority и output limits; malformed/upsize значения закрыто отклоняются.
func TestContextWindowIsLowerOnlyAndNonAuthorizing(t *testing.T) {
	input := modelProviderRunnerFixture()
	provider := input.ModelProvider
	before, _ := json.Marshal(provider)
	routeBefore, _ := provider.Route()
	cacheBefore, _ := provider.ResolveModelCache(input.Model)
	selected, _ := provider.Model(input.Model)
	for _, item := range []struct {
		raw  string
		want int
	}{{"", 32768}, {"[context]\nwindow_tokens = 16384", 16384}, {"[context]\nwindow_tokens = 32768", 32768}} {
		got, err := selected.ResolveRuntimeContext(item.raw)
		if err != nil || got.WindowTokens != item.want || got.CatalogWindowTokens != 32768 || got.MaximumOutputTokens != 1024 {
			t.Fatal("valid lower-only budget rejected", err)
		}
		if err := provider.ValidateRuntimePreferences(input.Model, item.raw, "", ReasoningUnsupported); err != nil {
			t.Fatal("runtime admission rejected valid budget", err)
		}
	}
	for _, raw := range []string{"window_tokens = 0", "window_tokens = -1", "window_tokens = 1024", "window_tokens = 32769", "window_tokens = 2000001", "window_tokens = 9223372036854775808", "window_tokens = 1.5", "window_tokens = '16384'", "window_tokens = true", "window_tokenz = 16384"} {
		if _, err := selected.ResolveRuntimeContext("[context]\n" + raw); err == nil {
			t.Fatal("invalid context budget accepted", raw)
		}
	}
	after, _ := json.Marshal(provider)
	routeAfter, _ := provider.Route()
	cacheAfter, _ := provider.ResolveModelCache(input.Model)
	a, _ := json.Marshal(cacheBefore)
	b, _ := json.Marshal(cacheAfter)
	if !bytes.Equal(before, after) || routeBefore.ConfigurationSHA256 != routeAfter.ConfigurationSHA256 || !bytes.Equal(a, b) {
		t.Fatal("context intent changed catalog/route/cache authority")
	}
}

// TestContextWindowSchemaAndPreviewUseActualModelBounds checks structural parsing is not confused
// with selected-model admission and previews show both requested and effective windows.
// TestContextWindowSchemaAndPreviewUseActualModelBounds отделяет structural parsing от selected-model
// admission и проверяет requested/effective окна в preview.
func TestContextWindowSchemaAndPreviewUseActualModelBounds(t *testing.T) {
	input := modelProviderRunnerFixture()
	model, _ := input.ModelProvider.Model(input.Model)
	schema := OverlaySchemaForModel(nil, "", model)
	for _, field := range schema.Fields {
		if field.Key == "context.window_tokens" && (field.Minimum != 1025 || field.Maximum != 32768 || field.DefaultValue != "32768") {
			t.Fatal("model range was not pinned")
		}
	}
	if len(DiagnoseConfigOverlayWithSchema("[context]\nwindow_tokens = 40000", schema)) != 1 {
		t.Fatal("editor accepted upsize")
	}
	if len(DiagnoseConfigOverlayWithSchema("[context]\nwindow_tokens = 16384", schema)) != 0 {
		t.Fatal("editor rejected budget")
	}
	for _, overlay := range []string{"", "[context]\nwindow_tokens = 16384"} {
		text, err := RenderSafeEffectiveConfig(SafeEffectiveConfigInput{ModelProvider: input.ModelProvider, Model: input.Model, Provider: input.ModelProvider.DefinitionKey(), Overlay: overlay})
		if err != nil || !strings.Contains(text, "CatalogWindowTokens = 32768") || !strings.Contains(text, "MaximumOutputTokens = 1024") {
			t.Fatal("model provenance missing from preview", err)
		}
		if overlay != "" && (!strings.Contains(text, "window_tokens = 16384") || !strings.Contains(text, "WindowTokens = 16384")) {
			t.Fatal("requested/effective budget missing")
		}
	}
}

// TestContextWindowIsPinnedByRuntimeRevision preserves a sealed retry and detects a changed
// overlay under the same revision/transport binding before any model request.
// TestContextWindowIsPinnedByRuntimeRevision сохраняет sealed retry и обнаруживает изменённый
// overlay под прежними revision/transport pins до model request.
func TestContextWindowIsPinnedByRuntimeRevision(t *testing.T) {
	input := modelProviderRunnerFixture()
	input.ConfigOverlay, input.ConfigOverlayDigest, _ = CanonicalConfigOverlay("[context]\nwindow_tokens = 16384")
	source := RuntimeRevisionCredentialSource{SecretName: "synthetic", SecretUID: "synthetic-uid", SecretResourceVersion: "1"}
	digest, err := RuntimeRevisionDigest(input, source)
	if err != nil {
		t.Fatal(err)
	}
	input.RuntimeRevisionDigest = digest
	refreshRunnerInputBindings(&input)
	raw, err := EncodeRunnerInput(input)
	if err != nil {
		t.Fatal("sealed context input rejected", err)
	}
	frozen, err := DecodeRunnerInput(raw)
	if err != nil || frozen.ConfigOverlay != input.ConfigOverlay || frozen.ConfigOverlayDigest != input.ConfigOverlayDigest {
		t.Fatal("retry lost immutable overlay", err)
	}
	changed := frozen
	changed.ConfigOverlay, changed.ConfigOverlayDigest, _ = CanonicalConfigOverlay("[context]\nwindow_tokens = 24576")
	changedDigest, err := RuntimeRevisionDigest(changed, source)
	if err != nil || changedDigest == digest {
		t.Fatal("revision digest did not bind context budget", err)
	}
	if changed.Validate() == nil {
		t.Fatal("changed overlay retained old execution binding")
	}
}
