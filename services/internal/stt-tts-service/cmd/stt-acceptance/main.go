package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/acceptance"
)

const maximumCredentialBytes = 16 << 10

func main() {
	credentialFile := os.Getenv("KODEX_STT_ACCEPTANCE_OPENAI_API_KEY_FILE")
	if credentialFile == "" {
		log.Fatal("STT acceptance credential file is required")
	}
	rawKey, err := securefile.Read(credentialFile, maximumCredentialBytes)
	if err != nil {
		log.Fatal("Read STT acceptance credential failed")
	}
	defer clear(rawKey)
	key := bytes.TrimSpace(rawKey)
	if len(key) == 0 {
		log.Fatal("STT acceptance credential is empty")
	}
	fixture, err := acceptance.VerifyFixture(os.Getenv("KODEX_STT_ACCEPTANCE_FIXTURE"))
	if err != nil {
		log.Fatal(err)
	}
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := fixture.Run(ctx, key); err != nil {
		log.Fatal(err)
	}
	log.Print("STT acceptance passed")
}
