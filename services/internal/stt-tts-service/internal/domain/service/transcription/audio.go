package transcription

import (
	"encoding/binary"
	"errors"
	"io"
	"mime"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

// ValidateAudio проверяет сигнатуру, все frames/chunks и точную границу файла
// без полной копии payload. FLAC закрыто отклоняется: STREAMINFO total_samples
// не доказывает длительность фактических frames.
func ValidateAudio(reader io.ReadSeeker, sizeBytes int64, declaredMediaType string, maximumBytes int64, maximumDuration time.Duration) (value.Audio, error) {
	if reader == nil || sizeBytes <= 0 {
		return value.Audio{}, errs.ErrInvalidRequest
	}
	if maximumBytes < 1 || sizeBytes > maximumBytes || sizeBytes > value.MaximumAbsoluteBytes {
		return value.Audio{}, errs.ErrAudioTooLarge
	}
	mediaType, _, err := mime.ParseMediaType(declaredMediaType)
	if err != nil {
		return value.Audio{}, errs.ErrUnsupportedAudio
	}
	mediaType = strings.ToLower(mediaType)
	var duration time.Duration
	var fileName string
	switch mediaType {
	case "audio/mpeg", "audio/mp3":
		duration, err = mp3Duration(reader, sizeBytes)
		mediaType, fileName = "audio/mpeg", "audio.mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		duration, err = wavDuration(reader, sizeBytes)
		mediaType, fileName = "audio/wav", "audio.wav"
	case "audio/flac", "audio/x-flac":
		return value.Audio{}, errs.ErrUnsupportedAudio
	default:
		return value.Audio{}, errs.ErrUnsupportedAudio
	}
	if err != nil || duration <= 0 {
		return value.Audio{}, errs.ErrUnsupportedAudio
	}
	if maximumDuration <= 0 || duration > maximumDuration {
		return value.Audio{}, errs.ErrAudioTooLong
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return value.Audio{}, errs.ErrInvalidRequest
	}
	return value.Audio{Reader: reader, SizeBytes: sizeBytes, MediaType: mediaType, FileName: fileName, Duration: duration}, nil
}

func mp3Duration(reader io.ReadSeeker, size int64) (time.Duration, error) {
	offset, err := mp3Start(reader, size)
	if err != nil {
		return 0, err
	}
	var samples int64
	var sampleRate int64
	frames := 0
	for offset < size {
		if size-offset == 128 {
			var tag [3]byte
			if readAt(reader, offset, tag[:]) == nil && string(tag[:]) == "TAG" {
				offset += 128
				break
			}
		}
		if size-offset < 4 {
			return 0, errors.New("truncated MP3 frame")
		}
		var raw [4]byte
		if err := readAt(reader, offset, raw[:]); err != nil {
			return 0, err
		}
		frameLength, frameSamples, rate, ok := mp3Frame(binary.BigEndian.Uint32(raw[:]))
		if !ok || frameLength > size-offset {
			return 0, errors.New("invalid MP3 frame")
		}
		if sampleRate != 0 && sampleRate != int64(rate) {
			return 0, errors.New("inconsistent MP3 sample rate")
		}
		if samples > (1<<63-1)-int64(frameSamples) {
			return 0, errors.New("MP3 sample count overflows")
		}
		sampleRate = int64(rate)
		samples += int64(frameSamples)
		frames++
		offset += frameLength
	}
	if offset != size || frames == 0 || sampleRate == 0 || samples > int64((1<<63-1)/int64(time.Second)) {
		return 0, errors.New("invalid MP3 boundary")
	}
	return time.Duration(samples) * time.Second / time.Duration(sampleRate), nil
}

func mp3Start(reader io.ReadSeeker, size int64) (int64, error) {
	if size < 4 {
		return 0, errors.New("MP3 is too short")
	}
	var header [10]byte
	count := int64(len(header))
	if size < count {
		count = size
	}
	if err := readAt(reader, 0, header[:count]); err != nil {
		return 0, err
	}
	if count >= 10 && string(header[:3]) == "ID3" {
		for _, item := range header[6:10] {
			if item&0x80 != 0 {
				return 0, errors.New("invalid ID3 size")
			}
		}
		tagSize := int64(header[6])<<21 | int64(header[7])<<14 | int64(header[8])<<7 | int64(header[9])
		offset := int64(10) + tagSize
		if header[5]&0x10 != 0 {
			offset += 10
		}
		if offset < 0 || offset > size-4 {
			return 0, errors.New("ID3 boundary is invalid")
		}
		return offset, nil
	}
	return 0, nil
}

func mp3Frame(header uint32) (int64, int, int, bool) {
	if header>>21 != 0x7ff {
		return 0, 0, 0, false
	}
	version := (header >> 19) & 0x3
	layer := (header >> 17) & 0x3
	bitrateIndex := int((header >> 12) & 0xf)
	sampleRateIndex := int((header >> 10) & 0x3)
	padding := int((header >> 9) & 0x1)
	if version == 1 || layer != 1 || bitrateIndex == 0 || bitrateIndex == 15 || sampleRateIndex == 3 {
		return 0, 0, 0, false
	}
	rates := [3]int{44100, 48000, 32000}
	rate := rates[sampleRateIndex]
	bitratesMPEG1 := [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	bitratesMPEG2 := [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	bitrate, samples, coefficient := bitratesMPEG1[bitrateIndex], 1152, 144
	if version != 3 {
		rate /= 2
		if version == 0 {
			rate /= 2
		}
		bitrate, samples, coefficient = bitratesMPEG2[bitrateIndex], 576, 72
	}
	length := coefficient*bitrate*1000/rate + padding
	return int64(length), samples, rate, length >= 24
}

func wavDuration(reader io.ReadSeeker, size int64) (time.Duration, error) {
	if size < 44 || size > int64(^uint32(0))+8 {
		return 0, errors.New("WAV size is invalid")
	}
	var header [12]byte
	if err := readAt(reader, 0, header[:]); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" || int64(binary.LittleEndian.Uint32(header[4:8]))+8 != size {
		return 0, errors.New("WAV header is invalid")
	}
	var byteRate uint32
	var dataSize uint32
	var blockAlign uint16
	seenFormat, seenData := false, false
	for offset := int64(12); offset < size; {
		if size-offset < 8 {
			return 0, errors.New("trailing WAV data")
		}
		var chunk [8]byte
		if err := readAt(reader, offset, chunk[:]); err != nil {
			return 0, err
		}
		chunkSize := int64(binary.LittleEndian.Uint32(chunk[4:]))
		payload := offset + 8
		if chunkSize > size-payload {
			return 0, errors.New("WAV chunk overflows")
		}
		switch string(chunk[:4]) {
		case "fmt ":
			if seenFormat || chunkSize < 16 {
				return 0, errors.New("WAV format is invalid")
			}
			seenFormat = true
			var format [16]byte
			if err := readAt(reader, payload, format[:]); err != nil {
				return 0, err
			}
			encoding := binary.LittleEndian.Uint16(format[0:2])
			channels := binary.LittleEndian.Uint16(format[2:4])
			rate := binary.LittleEndian.Uint32(format[4:8])
			byteRate = binary.LittleEndian.Uint32(format[8:12])
			blockAlign = binary.LittleEndian.Uint16(format[12:14])
			bits := binary.LittleEndian.Uint16(format[14:16])
			validBits := encoding == 1 && (bits == 8 || bits == 16 || bits == 24 || bits == 32) || encoding == 3 && (bits == 32 || bits == 64)
			expectedAlign := uint64(channels) * uint64(bits) / 8
			expectedRate := uint64(rate) * expectedAlign
			if channels == 0 || channels > 8 || rate < 8000 || rate > 384000 || !validBits || expectedAlign == 0 ||
				expectedAlign > uint64(^uint16(0)) || uint64(blockAlign) != expectedAlign || expectedRate > uint64(^uint32(0)) || uint64(byteRate) != expectedRate {
				return 0, errors.New("WAV format values are invalid")
			}
		case "data":
			if seenData {
				return 0, errors.New("duplicate WAV data")
			}
			seenData, dataSize = true, uint32(chunkSize)
		}
		next := payload + chunkSize + chunkSize%2
		if next < payload || next > size {
			return 0, errors.New("WAV boundary overflows")
		}
		offset = next
	}
	if !seenFormat || !seenData || byteRate == 0 || dataSize == 0 || blockAlign == 0 || dataSize%uint32(blockAlign) != 0 {
		return 0, errors.New("WAV structure is incomplete")
	}
	return time.Duration(dataSize) * time.Second / time.Duration(byteRate), nil
}

func readAt(reader io.ReadSeeker, offset int64, buffer []byte) error {
	if _, err := reader.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	_, err := io.ReadFull(reader, buffer)
	return err
}
