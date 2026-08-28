package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func TestDirectRootWithoutProcessNode(t *testing.T) {
	t.Parallel()

	direct := map[string]any{"runID": "run-id", "rootRunID": "run-id"}
	if !directRootWithoutProcessNode(pgx.ErrNoRows, direct) {
		t.Fatal("direct root run must not require a separate root process node")
	}

	for name, test := range map[string]struct {
		err   error
		lease map[string]any
	}{
		"database failure": {err: errors.New("database unavailable"), lease: direct},
		"child run":        {err: pgx.ErrNoRows, lease: map[string]any{"runID": "child", "rootRunID": "root"}},
		"missing identity": {err: pgx.ErrNoRows, lease: map[string]any{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if directRootWithoutProcessNode(test.err, test.lease) {
				t.Fatal("non-direct lifecycle failure was accepted")
			}
		})
	}
}

func TestDecodeRunUsageValidatesStoredTurnBreakdown(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"total_tokens":120,"input_tokens":100,"cached_input_tokens":40,
		"cache_write_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,
		"model_context_window":200000,
		"turns":{"turn_abcdefgh":{"total_tokens":120,"input_tokens":100,"cached_input_tokens":40,
		"cache_write_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,
		"model_context_window":200000}}
	}`)
	usage, err := decodeRunUsage(raw)
	if err != nil {
		t.Fatalf("decode valid token usage: %v", err)
	}
	want := entity.TokenUsage{TotalTokens: 120, InputTokens: 100, CachedInputTokens: 40,
		CacheWriteInputTokens: 10, OutputTokens: 20, ReasoningOutputTokens: 5, ModelContextWindow: 200000}
	if usage != want {
		t.Fatalf("decoded usage = %#v, want %#v", usage, want)
	}

	for name, invalid := range map[string][]byte{
		"unknown field": []byte(`{"total_tokens":0,"input_tokens":0,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"model_context_window":0,"unexpected":1}`),
		"invalid total": []byte(`{"total_tokens":2,"input_tokens":1,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"model_context_window":0}`),
		"invalid turn":  []byte(`{"total_tokens":0,"input_tokens":0,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"model_context_window":0,"turns":{"":{"total_tokens":0,"input_tokens":0,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"model_context_window":0}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeRunUsage(invalid); err == nil {
				t.Fatal("invalid stored token usage was accepted")
			}
		})
	}
}
