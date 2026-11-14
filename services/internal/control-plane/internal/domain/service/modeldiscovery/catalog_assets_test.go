package modeldiscovery

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// catalogAssetFixture supplies a minimal fully shaped unknown-capability record plus the exact reviewed local schemas and upstream schema-source pin.
// catalogAssetFixture задаёт минимальную полную запись с неизвестными capabilities, точными проверенными локальными схемами и upstream schema-source pin.
func catalogAssetFixture(t *testing.T) (fstest.MapFS, map[string]any, []byte) {
	t.Helper()
	files := fstest.MapFS{}
	for _, name := range []string{"manifest.json", "snapshot.schema.json", "manifest.schema.json", "upstream-schema-v2.ts"} {
		raw, err := assets.ReadFile("data/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files["data/"+name] = &fstest.MapFile{Data: raw}
	}
	record := entity.ModelDiscoveryRecord{ProviderID: "synthetic", ProviderName: "Synthetic provider", ModelID: "same", Name: "Synthetic model", Family: "UNKNOWN", Type: "chat", Tools: "UNKNOWN", Reasoning: "UNKNOWN", Cache: "UNKNOWN", CacheImplicit: "UNKNOWN", CacheExplicit: "UNKNOWN", ReasoningEfforts: []string{}, InputModalities: []string{}, OutputModalities: []string{}, UpstreamStatus: "active", APIs: []entity.ModelDiscoveryAPI{}}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if json.Unmarshal(files["data/manifest.json"].Data, &manifest) != nil {
		t.Fatal("invalid fixture provenance")
	}
	files["data/custom-overrides.json"] = &fstest.MapFile{Data: []byte("[]")}
	files["data/retired-models.json"] = &fstest.MapFile{Data: []byte("[]")}
	writeCatalogSnapshotFixture(t, files, manifest, append(append([]byte("["), raw...), ']'), 1)
	return files, manifest, raw
}

// writeCatalogSnapshotFixture recomputes hashes for deliberately malformed snapshot cases so validation is tested beyond checksum rejection.
// writeCatalogSnapshotFixture пересчитывает hashes для намеренно неверных snapshots, чтобы проверять validation после checksum boundary.
func writeCatalogSnapshotFixture(t *testing.T, files fstest.MapFS, manifest map[string]any, raw []byte, count int) {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	files["data/snapshot.json.gz"] = &fstest.MapFile{Data: compressed.Bytes()}
	digest := sha256.Sum256(raw)
	manifest["snapshot_sha256"] = hex.EncodeToString(digest[:])
	manifest["model_count"] = count
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files["data/manifest.json"] = &fstest.MapFile{Data: encoded}
}

// TestCatalogAssetsRejectMalformedDocuments exercises every input boundary with valid hashes where applicable, including duplicate identities, unknown fields, missing/null values, invalid capability states and incomplete provenance.
// TestCatalogAssetsRejectMalformedDocuments проверяет каждую input boundary с верными hashes там, где применимо: duplicate identities, unknown fields, missing/null values, неверные capability states и неполные provenance.
func TestCatalogAssetsRejectMalformedDocuments(t *testing.T) {
	for _, name := range []string{"manifest-unknown", "manifest-duplicate", "manifest-null", "manifest-time", "manifest-source", "manifest-schema", "snapshot-unknown", "snapshot-duplicate-key", "snapshot-missing", "snapshot-null", "snapshot-state", "snapshot-fraction", "snapshot-duplicate-identity", "custom-duplicate", "custom-unknown", "retired-duplicate", "retired-collision", "gzip-trailing", "gzip-corrupt"} {
		t.Run(name, func(t *testing.T) {
			files, manifest, record := catalogAssetFixture(t)
			snapshot := append(append([]byte("["), record...), ']')
			switch name {
			case "manifest-unknown":
				manifest["unknown"] = true
			case "manifest-duplicate":
				files["data/manifest.json"].Data = bytes.Replace(files["data/manifest.json"].Data, []byte("{"), []byte(`{"source":"foreign",`), 1)
			case "manifest-null":
				files["data/manifest.json"].Data = []byte("null")
			case "manifest-time":
				manifest["retrieved_at"] = "2026-99-99"
			case "manifest-source":
				manifest["source_url"] = "https://foreign.example.invalid/catalog"
			case "manifest-schema":
				manifest["projection_schema_sha256"] = string(bytes.Repeat([]byte("a"), 64))
			case "snapshot-unknown":
				snapshot = bytes.Replace(snapshot, []byte("[{"), []byte(`[{"unknown":true,`), 1)
			case "snapshot-duplicate-key":
				snapshot = bytes.Replace(snapshot, []byte("[{"), []byte(`[{"model_id":"foreign",`), 1)
			case "snapshot-missing":
				snapshot = bytes.Replace(snapshot, []byte(`"tools":"UNKNOWN",`), nil, 1)
			case "snapshot-null":
				snapshot = bytes.Replace(snapshot, []byte(`"tools":"UNKNOWN"`), []byte(`"tools":null`), 1)
			case "snapshot-state":
				snapshot = bytes.Replace(snapshot, []byte(`"tools":"UNKNOWN"`), []byte(`"tools":false`), 1)
			case "snapshot-fraction":
				snapshot = bytes.Replace(snapshot, []byte(`"context_tokens":0`), []byte(`"context_tokens":0.5`), 1)
			case "snapshot-duplicate-identity":
				snapshot = append(append(append(append([]byte("["), record...), ','), record...), ']')
			case "custom-duplicate", "retired-duplicate":
				file := "custom-overrides.json"
				if name == "retired-duplicate" {
					file = "retired-models.json"
				}
				files["data/"+file].Data = append(append(append(append([]byte("["), record...), ','), record...), ']')
			case "custom-unknown":
				files["data/custom-overrides.json"].Data = bytes.Replace(snapshot, []byte("[{"), []byte(`[{"unknown":true,`), 1)
			case "retired-collision":
				files["data/retired-models.json"].Data = snapshot
			case "gzip-trailing":
				files["data/snapshot.json.gz"].Data = append(files["data/snapshot.json.gz"].Data, 0)
			case "gzip-corrupt":
				files["data/snapshot.json.gz"].Data = []byte("not gzip")
			}
			if len(name) >= 9 && name[:9] == "snapshot-" {
				count := 1
				if name == "snapshot-duplicate-identity" {
					count = 2
				}
				writeCatalogSnapshotFixture(t, files, manifest, snapshot, count)
			} else if name != "manifest-duplicate" && name != "manifest-null" {
				raw, err := json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				files["data/manifest.json"].Data = raw
			}
			records, source, err := loadCatalogAssets(files)
			if err == nil || records != nil || source.Schema != "" {
				t.Fatal("malformed catalog yielded a usable partial model")
			}
		})
	}
}

// TestCatalogCustomOverrideIsExplicitAndUnique permits one reviewed override while preserving unknown metadata and forbidding last-value-wins behavior inside an overlay.
// TestCatalogCustomOverrideIsExplicitAndUnique допускает один проверенный override, сохраняя unknown metadata и запрещая last-value-wins внутри overlay.
func TestCatalogCustomOverrideIsExplicitAndUnique(t *testing.T) {
	files, _, record := catalogAssetFixture(t)
	files["data/custom-overrides.json"].Data = append(append([]byte("["), record...), ']')
	records, source, err := loadCatalogAssets(files)
	if err != nil || len(records) != 1 || records[0].Known != "CUSTOM" || records[0].Tools != "UNKNOWN" || source.UpstreamSchemaValidation != "NOT_PERFORMED" || source.ProjectionSchemaSHA256 == "" {
		t.Fatalf("explicit unique override rejected: %v", err)
	}
}
