package providersmoke

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveProviderRussianNumberFixture(t *testing.T) {
	path := os.Getenv("KODEX_STT_ACCEPTANCE_FIXTURE")
	fixture, err := VerifyFixture(path)
	if err != nil {
		t.Fatalf("fixture preflight: %v", err)
	}
	defer fixture.Close()
	key := []byte(os.Getenv("KODEX_STT_PROVIDER_SMOKE_OPENAI_API_KEY"))
	defer clear(key)
	if len(key) == 0 {
		t.Skip("NOT RUN: fixture checksum is valid; KODEX_STT_PROVIDER_SMOKE_OPENAI_API_KEY is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := fixture.Run(ctx, key); err != nil {
		t.Fatal(err)
	}
}
