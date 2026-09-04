package acceptance

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestLiveRussianNumberFixture(t *testing.T) {
	path := os.Getenv("KODEX_STT_ACCEPTANCE_FIXTURE")
	fixture, err := VerifyFixture(path)
	if err != nil {
		if os.Getenv("KODEX_STT_ACCEPTANCE_OPENAI_API_KEY") == "" && errors.Is(err, ErrFixtureUnavailable) {
			t.Skip("NOT RUN: external fixture and test credential are not configured")
		}
		t.Fatalf("fixture preflight: %v", err)
	}
	defer fixture.Close()
	key := []byte(os.Getenv("KODEX_STT_ACCEPTANCE_OPENAI_API_KEY"))
	defer clear(key)
	if len(key) == 0 {
		t.Skip("NOT RUN: fixture checksum is valid; KODEX_STT_ACCEPTANCE_OPENAI_API_KEY is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := fixture.Run(ctx, key); err != nil {
		t.Fatal(err)
	}
}
