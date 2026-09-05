package ffmpeg_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/audio/ffmpeg"
)

func TestRealMediaRecorderContainers(t *testing.T) {
	directory := os.Getenv("KODEX_STT_MEDIARECORDER_FIXTURES")
	if directory == "" {
		t.Skip("NOT RUN: MediaRecorder captures are not configured")
	}
	raw, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var captures []struct {
		Engine, Version, Mime, File, Status string
		Size                                int64
	}
	if err := json.Unmarshal(raw, &captures); err != nil || len(captures) != 3 {
		t.Fatal("capture manifest is incomplete")
	}
	for _, capture := range captures {
		t.Run(capture.Engine, func(t *testing.T) {
			if capture.Status == "NOT RUN" {
				t.Skip("NOT RUN: browser MediaRecorder unavailable")
			}
			if filepath.Base(capture.File) != capture.File || capture.Version == "" {
				t.Fatal("invalid capture manifest")
			}
			file, err := os.Open(filepath.Join(directory, capture.File))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			audio, err := transcription.ValidateAudio(t.Context(), file, capture.Size, capture.Mime, 1<<20, 30*time.Second, ffmpeg.New(t.TempDir()))
			if err != nil || audio.Duration < time.Second {
				t.Fatalf("MediaRecorder decode failed: %v", err)
			}
		})
	}
}
