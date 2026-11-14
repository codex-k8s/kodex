package modeldiscovery

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// maximumCatalogAssetBytes bounds every decompressed document before parsing or schema validation.
// maximumCatalogAssetBytes ограничивает каждый распакованный документ до разбора и schema validation.
const maximumCatalogAssetBytes = 16 << 20

// upstreamSchemaSourceSHA256 identifies the inspected upstream TypeScript schema source; it is provenance rather than a claim that the upstream API was validated against Zod.
// upstreamSchemaSourceSHA256 обозначает проверенный исходник TypeScript schema upstream; это provenance, а не заявление о проверке upstream API через Zod.
const upstreamSchemaSourceSHA256 = "311d8d6fd567011417344ca66ea0e823564044673acdc16702070025e442deb5"

// readCatalogAsset rejects oversized or nonregular files before reading immutable local asset bytes.
// readCatalogAsset отклоняет слишком большие и не обычные файлы до чтения immutable local assets.
func readCatalogAsset(files fs.FS, name string) ([]byte, error) {
	file, err := files.Open("data/" + name)
	if err != nil {
		return nil, ErrCatalog
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximumCatalogAssetBytes {
		return nil, ErrCatalog
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumCatalogAssetBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumCatalogAssetBytes || !utf8.Valid(raw) && !strings.HasSuffix(name, ".gz") {
		return nil, ErrCatalog
	}
	return raw, nil
}

// catalogSchema compiles only the supplied local closed schema, with all remote schema loading denied.
// catalogSchema компилирует только переданную локальную закрытую схему и запрещает любую сетевую загрузку схем.
func catalogSchema(raw []byte, name string) (*jsonschema.Schema, error) {
	var document any
	if internalrpcauth.DecodeStrictJSON(raw, &document) != nil {
		return nil, ErrCatalog
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	compiler.AssertFormat()
	url := "https://kodex.invalid/modeldiscovery/" + name
	if compiler.AddResource(url, document) != nil {
		return nil, ErrCatalog
	}
	schema, err := compiler.Compile(url)
	if err != nil {
		return nil, ErrCatalog
	}
	return schema, nil
}

// decodeCatalogDocument validates duplicate-free JSON against the local schema before decoding the typed model, preserving required fields, explicit UNKNOWN and null/type distinctions.
// decodeCatalogDocument проверяет JSON без повторных ключей по локальной схеме до typed decode, сохраняя обязательные поля, явный UNKNOWN и различие null/types.
func decodeCatalogDocument(raw []byte, schema *jsonschema.Schema, target any) error {
	if len(raw) == 0 || len(raw) > maximumCatalogAssetBytes || !utf8.Valid(raw) {
		return ErrCatalog
	}
	var document any
	if internalrpcauth.DecodeStrictJSON(raw, &document) != nil || schema.Validate(document) != nil || internalrpcauth.DecodeStrictJSON(raw, target) != nil {
		return ErrCatalog
	}
	return nil
}

// validateCatalogRecords enforces unique provider/model and protocol identities and semantic capability consistency after full shape validation.
// validateCatalogRecords проверяет уникальные provider/model и protocol identities и согласованность capabilities после полной проверки формы.
func validateCatalogRecords(records []entity.ModelDiscoveryRecord) error {
	if records == nil || len(records) > 50000 {
		return ErrCatalog
	}
	seen := map[string]bool{}
	for _, record := range records {
		identity := record.ProviderID + "\x00" + record.ModelID
		if seen[identity] {
			return ErrCatalog
		}
		seen[identity] = true
		for _, field := range []struct {
			value   string
			maximum int
		}{{record.ProviderID, 128}, {record.ProviderName, 512}, {record.ModelID, 512}, {record.Name, 512}, {record.Family, 128}, {record.Type, 64}, {record.UpstreamStatus, 32}} {
			if field.value == "" || len(field.value) > field.maximum || !utf8.ValidString(field.value) || strings.ContainsAny(field.value, "\x00\r\n\t") {
				return ErrCatalog
			}
		}
		protocols := map[string]bool{}
		for _, api := range record.APIs {
			if protocols[api.Protocol] || len(api.Protocol) > 128 || len(api.BaseURL) > 2048 || len(api.SDK) > 256 || len(api.Path) > 512 {
				return ErrCatalog
			}
			protocols[api.Protocol] = true
		}
		for _, effort := range record.ReasoningEfforts {
			if len(effort) > 32 {
				return ErrCatalog
			}
		}
		for _, modality := range append(append([]string{}, record.InputModalities...), record.OutputModalities...) {
			if len(modality) > 128 {
				return ErrCatalog
			}
		}
		if record.Reasoning != "SUPPORTED" && len(record.ReasoningEfforts) > 0 || !record.ContextKnown && record.ContextTokens != 0 || !record.OutputKnown && record.OutputTokens != 0 {
			return ErrCatalog
		}
		switch record.Cache {
		case "NONE":
			if record.CacheImplicit != "UNSUPPORTED" || record.CacheExplicit != "UNSUPPORTED" {
				return ErrCatalog
			}
		case "UNKNOWN":
			if record.CacheImplicit != "UNKNOWN" || record.CacheExplicit != "UNKNOWN" {
				return ErrCatalog
			}
		case "IMPLICIT":
			if record.CacheImplicit != "SUPPORTED" || record.CacheExplicit != "UNKNOWN" {
				return ErrCatalog
			}
		case "EXPLICIT":
			if record.CacheExplicit != "SUPPORTED" || record.CacheImplicit != "UNKNOWN" {
				return ErrCatalog
			}
		case "BOTH":
			if record.CacheImplicit != "SUPPORTED" || record.CacheExplicit != "SUPPORTED" {
				return ErrCatalog
			}
		default:
			return ErrCatalog
		}
	}
	return nil
}

// loadCatalogAssets publishes no partial model on any malformed manifest, schema, hash, compressed stream, record or overlay; custom overrides may replace one upstream identity only after each input set is independently unique.
// loadCatalogAssets не выдаёт partial model при ошибке manifest, schema, hash, compressed stream, record либо overlay; custom overrides заменяют upstream identity только после отдельной проверки уникальности каждого входного набора.
func loadCatalogAssets(files fs.FS) ([]entity.ModelDiscoveryRecord, entity.ModelDiscoverySource, error) {
	var source entity.ModelDiscoverySource
	manifestRaw, err := readCatalogAsset(files, "manifest.json")
	if err != nil {
		return nil, source, err
	}
	manifestSchemaRaw, err := readCatalogAsset(files, "manifest.schema.json")
	if err != nil {
		return nil, source, err
	}
	manifestSchema, err := catalogSchema(manifestSchemaRaw, "manifest.schema.json")
	if err != nil {
		return nil, source, err
	}
	if decodeCatalogDocument(manifestRaw, manifestSchema, &source) != nil {
		return nil, entity.ModelDiscoverySource{}, ErrCatalog
	}
	if _, err := time.Parse(time.RFC3339Nano, source.RetrievedAt); err != nil || !strings.HasSuffix(source.RetrievedAt, "Z") {
		return nil, entity.ModelDiscoverySource{}, ErrCatalog
	}
	upstreamSchemaRaw, err := readCatalogAsset(files, "upstream-schema-v2.ts")
	if err != nil {
		return nil, entity.ModelDiscoverySource{}, err
	}
	upstreamDigest := sha256.Sum256(upstreamSchemaRaw)
	if hex.EncodeToString(upstreamDigest[:]) != source.SchemaSHA256 || source.SchemaSHA256 != upstreamSchemaSourceSHA256 {
		return nil, entity.ModelDiscoverySource{}, ErrCatalog
	}
	recordSchemaRaw, err := readCatalogAsset(files, "snapshot.schema.json")
	if err != nil {
		return nil, entity.ModelDiscoverySource{}, err
	}
	schemaDigest := sha256.Sum256(recordSchemaRaw)
	if hex.EncodeToString(schemaDigest[:]) != source.ProjectionSchemaSHA256 {
		return nil, entity.ModelDiscoverySource{}, ErrCatalog
	}
	recordSchema, err := catalogSchema(recordSchemaRaw, "snapshot.schema.json")
	if err != nil {
		return nil, entity.ModelDiscoverySource{}, err
	}
	compressed, err := readCatalogAsset(files, "snapshot.json.gz")
	if err != nil {
		return nil, entity.ModelDiscoverySource{}, err
	}
	compressedInput := bytes.NewReader(compressed)
	reader, err := gzip.NewReader(compressedInput)
	if err != nil {
		return nil, entity.ModelDiscoverySource{}, ErrCatalog
	}
	reader.Multistream(false)
	raw, readErr := io.ReadAll(io.LimitReader(reader, maximumCatalogAssetBytes+1))
	closeErr := reader.Close()
	digest := sha256.Sum256(raw)
	if readErr != nil || closeErr != nil || compressedInput.Len() != 0 || len(raw) > maximumCatalogAssetBytes || hex.EncodeToString(digest[:]) != source.SnapshotSHA256 {
		return nil, entity.ModelDiscoverySource{}, ErrCatalog
	}
	var records []entity.ModelDiscoveryRecord
	if decodeCatalogDocument(raw, recordSchema, &records) != nil || validateCatalogRecords(records) != nil || len(records) != source.ModelCount {
		return nil, entity.ModelDiscoverySource{}, ErrCatalog
	}
	customRaw, err := readCatalogAsset(files, "custom-overrides.json")
	if err != nil {
		return nil, entity.ModelDiscoverySource{}, err
	}
	retiredRaw, err := readCatalogAsset(files, "retired-models.json")
	if err != nil {
		return nil, entity.ModelDiscoverySource{}, err
	}
	var custom, retired []entity.ModelDiscoveryRecord
	if decodeCatalogDocument(customRaw, recordSchema, &custom) != nil || validateCatalogRecords(custom) != nil || len(custom) > 1024 || decodeCatalogDocument(retiredRaw, recordSchema, &retired) != nil || validateCatalogRecords(retired) != nil {
		return nil, entity.ModelDiscoverySource{}, ErrCatalog
	}
	index := make(map[string]int, len(records))
	for position, record := range records {
		index[record.ProviderID+"\x00"+record.ModelID] = position
	}
	for _, record := range retired {
		identity := record.ProviderID + "\x00" + record.ModelID
		if _, found := index[identity]; found {
			return nil, entity.ModelDiscoverySource{}, ErrCatalog
		}
		record.Known = "REMOVED"
		index[identity] = len(records)
		records = append(records, record)
	}
	for _, record := range custom {
		identity := record.ProviderID + "\x00" + record.ModelID
		record.Known = "CUSTOM"
		if position, found := index[identity]; found {
			records[position] = record
		} else {
			index[identity] = len(records)
			records = append(records, record)
		}
	}
	if len(records) > 50000 {
		return nil, entity.ModelDiscoverySource{}, ErrCatalog
	}
	overlay := sha256.Sum256(append(append(append([]byte{}, customRaw...), 0), retiredRaw...))
	source.OverlaySHA256 = hex.EncodeToString(overlay[:])
	manifestDigest := sha256.Sum256(manifestRaw)
	effective := sha256.Sum256([]byte(hex.EncodeToString(manifestDigest[:]) + ":" + source.OverlaySHA256))
	source.CatalogSHA256 = hex.EncodeToString(effective[:])
	return records, source, nil
}
