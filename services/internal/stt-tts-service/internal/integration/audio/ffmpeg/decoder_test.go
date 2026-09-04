package ffmpeg_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/audio/ffmpeg"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/testdata"
)

func TestAudioContainersDecodedSamplesAndBounds(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp3")
	if err := os.WriteFile(source, testdata.RussianNumbers, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		extension, mediaType, codec string
		extra                       []string
	}{
		{"mp3", "audio/mpeg", "libmp3lame", nil},
		{"mpeg", "audio/mpeg", "libmp3lame", []string{"-f", "mp3"}},
		{"mpga", "audio/mpga", "libmp3lame", []string{"-f", "mp3"}},
		{"wav", "audio/wav", "pcm_s16le", nil},
		{"flac", "audio/flac", "flac", nil},
		{"webm", "audio/webm;codecs=opus", "libopus", nil},
		{"ogg", "audio/ogg;codecs=opus", "libopus", nil},
		{"m4a", "audio/x-m4a", "aac", nil},
		{"mp4", "audio/mp4;codecs=mp4a.40.2", "aac", []string{"-movflags", "frag_keyframe+empty_moov"}},
	} {
		t.Run(tc.extension, func(t *testing.T) {
			path := filepath.Join(dir, "audio."+tc.extension)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			args := []string{"-nostdin", "-v", "error", "-i", source, "-map_metadata", "-1", "-c:a", tc.codec}
			args = append(args, tc.extra...)
			args = append(args, path)
			if err := exec.CommandContext(ctx, "ffmpeg", args...).Run(); err != nil {
				t.Fatalf("создание %s: %v", tc.extension, err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			decoder := ffmpeg.New(dir)
			audio, err := transcription.ValidateAudio(ctx, bytes.NewReader(raw), int64(len(raw)), tc.mediaType, 1<<20, time.Minute, decoder)
			if err != nil || audio.Duration < time.Second || audio.Duration > 15*time.Second {
				t.Fatalf("decode %s: duration=%v err=%v", tc.extension, audio.Duration, err)
			}
			if _, err := transcription.ValidateAudio(ctx, bytes.NewReader(raw), int64(len(raw)), tc.mediaType, 1<<20, 100*time.Millisecond, decoder); !errors.Is(err, errs.ErrAudioTooLong) {
				t.Fatalf("duration bound: %v", err)
			}
			if _, err := transcription.ValidateAudio(ctx, bytes.NewReader(raw), int64(len(raw)), tc.mediaType, 10, time.Minute, decoder); !errors.Is(err, errs.ErrAudioTooLarge) {
				t.Fatalf("size bound: %v", err)
			}
			broken := raw[:min(len(raw), 20)]
			if _, err := transcription.ValidateAudio(ctx, bytes.NewReader(broken), int64(len(broken)), tc.mediaType, 1<<20, time.Minute, decoder); err == nil {
				t.Fatal("truncated container принят")
			}
		})
	}
	entries, err := filepath.Glob(filepath.Join(dir, ".decode-*"))
	if err != nil || len(entries) != 0 {
		t.Fatal("spool не освобождён")
	}
}

func TestAudioCancellationAndFalseFLACDuration(t *testing.T) {
	dir := t.TempDir()
	decoder := ffmpeg.New(dir)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := transcription.ValidateAudio(ctx, bytes.NewReader(testdata.RussianNumbers), int64(len(testdata.RussianNumbers)), "audio/mpeg", 1<<20, time.Minute, decoder); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel потерян")
	}
	ctx, cancel = context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	start := time.Now()
	if _, err := transcription.ValidateAudio(ctx, bytes.NewReader(testdata.RussianNumbers), int64(len(testdata.RussianNumbers)), "audio/mpeg", 1<<20, time.Minute, decoder); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline потерян")
	}
	if time.Since(start) > time.Second {
		t.Fatal("deadline не ограничен")
	}
	// STREAMINFO с ненулевым total_samples, но без единого аудиофрейма.
	raw := make([]byte, 42)
	copy(raw, "fLaC")
	raw[4] = 0x80
	raw[7] = 34
	raw[20] = 0x0b
	raw[21] = 0xb8
	raw[25] = 1
	if _, err := transcription.ValidateAudio(t.Context(), bytes.NewReader(raw), int64(len(raw)), "audio/flac", 1<<20, time.Minute, decoder); err == nil {
		t.Fatal("duration из STREAMINFO принята без frames")
	}
}

func TestRunningDecoderIsKilledAndJoinedOnDeadline(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nexec /bin/sleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ffmpeg.New(dir).Duration(ctx, bytes.NewReader(testdata.RussianNumbers), int64(len(testdata.RussianNumbers)), "mp3", time.Minute)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("running decoder deadline failed: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, ".decode-*"))
	if len(files) != 0 {
		t.Fatal("decoder spool leaked after kill/join")
	}
}
