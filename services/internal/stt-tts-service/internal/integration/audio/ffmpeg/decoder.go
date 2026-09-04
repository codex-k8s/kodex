// Package ffmpeg проверяет контейнеры декодером без сетевых протоколов.
package ffmpeg

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

const decodeTimeout = 5 * time.Second
const pcmBytesPerSecond = int64(16000 * 2)

type Decoder struct{ directory string }

func New(directory string) *Decoder { return &Decoder{directory: directory} }
func (decoder *Decoder) CheckLocal(ctx context.Context) error {
	if ctx == nil || !filepath.IsAbs(decoder.directory) {
		return errors.New("audio decoder configuration is invalid")
	}
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errors.New("audio decoder is unavailable")
	}
	return ctx.Err()
}

func (decoder *Decoder) Duration(ctx context.Context, reader io.ReadSeeker, size int64, format string, maximum time.Duration) (time.Duration, error) {
	if ctx == nil || reader == nil || size <= 0 || size > value.MaximumAbsoluteBytes || maximum <= 0 || maximum > 30*time.Minute {
		return 0, errs.ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return 0, errs.ErrInvalidRequest
	}
	// RIFF имеет точную внешнюю границу; FFmpeg допускает мусор за ней.
	if format == "wav" {
		var header [12]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" || int64(binary.LittleEndian.Uint32(header[4:8]))+8 != size {
			return 0, errs.ErrUnsupportedAudio
		}
		if _, err := reader.Seek(0, io.SeekStart); err != nil {
			return 0, errs.ErrInvalidRequest
		}
	}
	file, ok := reader.(*os.File)
	if !ok {
		var err error
		file, err = os.CreateTemp(decoder.directory, ".decode-*")
		if err != nil {
			return 0, errs.ErrInvalidRequest
		}
		defer file.Close()
		defer os.Remove(file.Name())
		copied, err := io.Copy(file, io.LimitReader(reader, size+1))
		if err != nil || copied != size {
			return 0, errs.ErrInvalidRequest
		}
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			return 0, errs.ErrInvalidRequest
		}
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return 0, errs.ErrInvalidRequest
	}
	decodeCtx, cancel := context.WithTimeout(ctx, decodeTimeout)
	defer cancel()
	output := &sampleCounter{limit: int64(maximum) * pcmBytesPerSecond / int64(time.Second), cancel: cancel}
	warnings := &warningCounter{}
	// Только наследованный seekable descriptor: MP4 moov в конце поддержан,
	// ссылки на файлы/URL внутри контейнера физически не открываются.
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-xerror", "-max_alloc", "33554432", "-threads", "1", "-filter_threads", "1",
		"-protocol_whitelist", "fd", "-format_whitelist", format,
		"-codec_whitelist", "mp3,mp3float,flac,opus,vorbis,aac,alac,pcm_s16le,pcm_s24le,pcm_s32le,pcm_u8,pcm_f32le,pcm_f64le",
		"-err_detect", "explode", "-guess_layout_max", "0"}
	// Matroska уже выдаёт packet boundaries. Повторный Opus parser FFmpeg
	// ошибочно разбирает codec private header; полный decoder остаётся включён.
	if format == "matroska,webm" {
		args = append(args, "-fflags", "+noparse+nofillin")
	}
	args = append(args, "-fd", "3", "-i", "fd:",
		"-map", "0:a:0", "-vn", "-sn", "-dn", "-ac", "1", "-ar", "16000", "-threads", "1",
		"-f", "s16le", "-c:a", "pcm_s16le", "pipe:1")
	command := exec.CommandContext(decodeCtx, "ffmpeg", args...)
	command.ExtraFiles = []*os.File{file}
	command.Stdout, command.Stderr = output, warnings
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C"}
	command.WaitDelay = 250 * time.Millisecond
	err = command.Run()
	if output.exceeded {
		return 0, errs.ErrAudioTooLong
	}
	if decodeCtx.Err() != nil {
		return 0, decodeCtx.Err()
	}
	if err != nil || warnings.seen || output.bytes == 0 || output.bytes%2 != 0 {
		return 0, errs.ErrUnsupportedAudio
	}
	return time.Duration(output.bytes) * time.Second / time.Duration(pcmBytesPerSecond), nil
}

type sampleCounter struct {
	bytes, limit int64
	exceeded     bool
	cancel       context.CancelFunc
}

func (counter *sampleCounter) Write(raw []byte) (int, error) {
	counter.bytes += int64(len(raw))
	if counter.bytes > counter.limit {
		counter.exceeded = true
		counter.cancel()
		return 0, errs.ErrAudioTooLong
	}
	return len(raw), nil
}

// Диагностика декодера может содержать metadata; не сохраняем даже её текст.
type warningCounter struct{ seen bool }

func (counter *warningCounter) Write(raw []byte) (int, error) {
	counter.seen = true
	return len(raw), nil
}
