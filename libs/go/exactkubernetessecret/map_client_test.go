package exactkubernetessecret

import (
	"encoding/json"
	"testing"
)

// TestMapGeneration parses valid decimal annotation generations and rejects malformed JSON, negative values and nonnumeric text.
// TestMapGeneration разбирает допустимые десятичные поколения аннотаций и отклоняет некорректный JSON, отрицательные значения и нечисловой текст.
func TestMapGeneration(t *testing.T) {
	t.Parallel()

	for raw, expected := range map[string]uint64{
		`{"kodex.dev/secret-generation":"0"}`: 0,
		`{"kodex.dev/secret-generation":"7"}`: 7,
	} {
		generation, err := mapGeneration(json.RawMessage(raw))
		if err != nil || generation != expected {
			t.Fatalf("mapGeneration(%s) = %d, %v; want %d, nil", raw, generation, err, expected)
		}
	}
	for _, invalid := range []json.RawMessage{
		json.RawMessage(`{"kodex.dev/secret-generation":"invalid"}`),
		json.RawMessage(`{"kodex.dev/secret-generation":"-1"}`),
		json.RawMessage(`[]`),
	} {
		if _, err := mapGeneration(invalid); err == nil {
			t.Fatalf("mapGeneration() accepted %s", invalid)
		}
	}
}

// TestDataGenerationRejectsBootstrapMarkerInsideData prevents nonempty stored data from masquerading as an empty generation-zero bootstrap.
// TestDataGenerationRejectsBootstrapMarkerInsideData запрещает непустым хранимым данным маскироваться под пустое начальное состояние нулевого поколения.
func TestDataGenerationRejectsBootstrapMarkerInsideData(t *testing.T) {
	t.Parallel()

	if _, err := dataGeneration(map[string][]byte{generationDataKey: []byte("0")}); err == nil {
		t.Fatal("dataGeneration() accepted bootstrap generation inside non-empty data")
	}
}

// TestPublicMapDataExcludesInternalGenerationMarker returns only application entries while proving marker removal does not mutate the stored map.
// TestPublicMapDataExcludesInternalGenerationMarker возвращает лишь прикладные записи и подтверждает, что удаление маркера не меняет хранимый словарь.
func TestPublicMapDataExcludesInternalGenerationMarker(t *testing.T) {
	t.Parallel()

	stored := map[string][]byte{
		"private.jwk":     []byte("private"),
		generationDataKey: []byte("3"),
	}
	public := publicMapData(stored)
	if len(public) != 1 || string(public["private.jwk"]) != "private" {
		t.Fatalf("publicMapData() = %#v; want only private.jwk", public)
	}
	if string(stored[generationDataKey]) != "3" {
		t.Fatal("publicMapData() mutated stored generation marker")
	}
}

// TestMapClientValidDataUsesExactKeys accepts registered optional subsets and rejects empty maps, unknown keys and empty values.
// TestMapClientValidDataUsesExactKeys принимает зарегистрированные необязательные подмножества и отклоняет пустые словари, неизвестные ключи и пустые значения.
func TestMapClientValidDataUsesExactKeys(t *testing.T) {
	t.Parallel()

	client := &MapClient{allowed: map[string]struct{}{"private.jwk": {}, "bundle.jws": {}}}
	if !client.validData(map[string][]byte{
		"private.jwk": []byte("private"),
		"bundle.jws":  []byte("bundle"),
	}) {
		t.Fatal("exact data set was rejected")
	}
	if !client.validData(map[string][]byte{"private.jwk": []byte("private")}) {
		t.Fatal("allowed optional data subset was rejected")
	}
	for _, invalid := range []map[string][]byte{
		{},
		{"private.jwk": []byte("private"), "unknown": []byte("value")},
		{"private.jwk": nil, "bundle.jws": []byte("bundle")},
	} {
		if client.validData(invalid) {
			t.Fatalf("invalid data set was accepted: %#v", invalid)
		}
	}
}
