package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/providersmoke"
)

const maximumCredentialBytes = 16 << 10

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	fixture, err := providersmoke.VerifyFixture(ctx, os.Getenv("KODEX_STT_ACCEPTANCE_FIXTURE"))
	if err != nil {
		log.Fatal(err)
	}
	defer fixture.Close()
	if len(os.Args) == 2 && os.Args[1] == "--fixture-only" {
		log.Print("STT fixture preflight passed; live provider NOT RUN")
		return
	}
	credentialFile := os.Getenv("KODEX_STT_PROVIDER_SMOKE_OPENAI_API_KEY_FILE")
	if credentialFile == "" {
		log.Fatal("STT live provider NOT RUN: separate test credential file is required")
	}
	rawKey, err := securefile.Read(credentialFile, maximumCredentialBytes)
	if err != nil {
		log.Fatal("Read STT provider smoke credential failed")
	}
	defer clear(rawKey)
	key := bytes.TrimSpace(rawKey)
	if len(key) == 0 {
		log.Fatal("STT provider smoke credential is empty")
	}
	if err := fixture.Run(ctx, key); err != nil {
		log.Fatal(err)
	}
	log.Print("STT provider smoke passed")
}
