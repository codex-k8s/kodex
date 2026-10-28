package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryUsageCompletenessStoredReadback observes actual native completion JSON in the disposable DB.
// queryUsageCompletenessStoredReadback читает реальный JSON штатного completion в одноразовой БД.
//
//go:embed testdata/sql/usage_completeness_stored_readback.sql
var queryUsageCompletenessStoredReadback string

// legacyUsageCounters freezes the seven-field wire shape from 4e1bc1d76 before completeness existed.
// legacyUsageCounters фиксирует формат семи полей из 4e1bc1d76 до появления completeness.
type legacyUsageCounters struct {
	TotalTokens           int64 `json:"total_tokens"`
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	ModelContextWindow    int64 `json:"model_context_window"`
}

// tokenUsage reuses unchanged numeric validation with UNKNOWN, without extending the old wire shape.
// tokenUsage использует прежнюю проверку чисел с UNKNOWN, не расширяя старый формат JSON.
func (usage legacyUsageCounters) tokenUsage() entity.TokenUsage {
	return entity.TokenUsage{TotalTokens: usage.TotalTokens, InputTokens: usage.InputTokens,
		CachedInputTokens: usage.CachedInputTokens, CacheWriteInputTokens: usage.CacheWriteInputTokens,
		OutputTokens: usage.OutputTokens, ReasoningOutputTokens: usage.ReasoningOutputTokens,
		ModelContextWindow: usage.ModelContextWindow}
}

// decodeLegacyStoredUsage mirrors the old strict reader, including nested turns and trailing data.
// decodeLegacyStoredUsage повторяет старый строгий reader, включая вложенные turns и хвост JSON.
func decodeLegacyStoredUsage(raw []byte) (entity.TokenUsage, error) {
	var stored struct {
		legacyUsageCounters
		Turns map[string]legacyUsageCounters `json:"turns,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&stored) != nil || decoder.Decode(&struct{}{}) != io.EOF || !stored.tokenUsage().Valid() {
		return entity.TokenUsage{}, errs.ErrUnavailable
	}
	for ref, usage := range stored.Turns {
		if ref == "" || !usage.tokenUsage().Valid() {
			return entity.TokenUsage{}, errs.ErrUnavailable
		}
	}
	return stored.tokenUsage(), nil
}

// assertStoredUsageLegacyCompatibility checks both SQL writers through actual child and root rows;
// UNKNOWN must omit the key, while actual known quality remains unreadable by the old reader.
// assertStoredUsageLegacyCompatibility проверяет оба SQL writer через реальные child/root строки:
// UNKNOWN обязан опускать ключ, а известное качество остаётся недоступным старому reader.
func assertStoredUsageLegacyCompatibility(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rootRef, childRef string) {
	t.Helper()
	rows, err := pool.Query(ctx, queryUsageCompletenessStoredReadback, rootRef)
	if err != nil {
		t.Fatal("read actual stored usage JSON")
	}
	defer rows.Close()
	seen := make(map[string]bool)
	for rows.Next() {
		var ref string
		var raw []byte
		if err := rows.Scan(&ref, &raw); err != nil || (ref != rootRef && ref != childRef) || seen[ref] {
			t.Fatal("stored usage readback lost exact run lineage")
		}
		seen[ref] = true
		current, err := decodeRunUsage(raw)
		if err != nil {
			t.Fatal("new reader rejected actual stored usage")
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			t.Fatal("stored usage is not a JSON object")
		}
		_, hasQuality := object["completeness"]
		legacy, legacyErr := decodeLegacyStoredUsage(raw)
		if current.Completeness == runtimecontract.UsageUnknown {
			if hasQuality || legacyErr != nil || legacy != current {
				t.Fatal("UNKNOWN writer broke old strict reader or numeric counters")
			}
		} else if !hasQuality || legacyErr == nil {
			t.Fatal("known quality was erased or old reader unexpectedly accepted it")
		}
	}
	if rows.Err() != nil || len(seen) != 2 {
		t.Fatal("stored usage readback did not cover both root and child")
	}
}

// TestLegacyStoredUsageReaderClosure pins the old reader's rejection of new root/turn fields.
// TestLegacyStoredUsageReaderClosure закрепляет отказ старого reader на новых полях root/turn.
func TestLegacyStoredUsageReaderClosure(t *testing.T) {
	for _, fixture := range []struct {
		name string
		raw  string
		ok   bool
	}{
		{"legacy-positive", `{"total_tokens":100,"input_tokens":100,"turns":{"turn":{"total_tokens":100,"input_tokens":100}}}`, true},
		{"partial", `{"completeness":"PARTIAL","total_tokens":100,"input_tokens":100}`, false},
		{"complete-zero", `{"completeness":"COMPLETE"}`, false},
		{"nested-quality", `{"turns":{"turn":{"completeness":"COMPLETE"}}}`, false},
		{"invalid", `{"completeness":"INVALID"}`, false},
		{"trailing-json", `{} {}`, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			_, err := decodeLegacyStoredUsage([]byte(fixture.raw))
			if (err == nil) != fixture.ok {
				t.Fatal("old strict reader compatibility fixture changed")
			}
		})
	}
}
