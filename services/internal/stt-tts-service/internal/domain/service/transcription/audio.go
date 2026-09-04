package transcription

import (
	"encoding/binary"
	"mime"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

type audioFormat struct {
	mediaType string
	fileName  string
	duration  func([]byte) (time.Duration, bool)
}

// ValidateAudio связывает заявленный media type, сигнатуру и вычисленную
// длительность до любого provider effect. MVP принимает форматы с локально
// проверяемой длительностью: MP3, WAV и FLAC.
func ValidateAudio(raw []byte, declaredMediaType string, maximumBytes int, maximumDuration time.Duration) (value.Audio, error) {
	if len(raw) == 0 {
		return value.Audio{}, errs.ErrInvalidRequest
	}
	if maximumBytes < 1 || len(raw) > maximumBytes || len(raw) > value.MaximumAbsoluteBytes {
		return value.Audio{}, errs.ErrAudioTooLarge
	}
	mediaType, _, err := mime.ParseMediaType(declaredMediaType)
	if err != nil {
		return value.Audio{}, errs.ErrUnsupportedAudio
	}
	mediaType = strings.ToLower(mediaType)
	format, ok := supportedAudioFormat(mediaType, raw)
	if !ok {
		return value.Audio{}, errs.ErrUnsupportedAudio
	}
	duration, ok := format.duration(raw)
	if !ok || duration <= 0 {
		return value.Audio{}, errs.ErrUnsupportedAudio
	}
	if maximumDuration <= 0 || duration > maximumDuration {
		return value.Audio{}, errs.ErrAudioTooLong
	}
	return value.Audio{Bytes: append([]byte(nil), raw...), MediaType: format.mediaType, FileName: format.fileName, Duration: duration}, nil
}

func supportedAudioFormat(mediaType string, raw []byte) (audioFormat, bool) {
	switch mediaType {
	case "audio/mpeg", "audio/mp3":
		if hasMP3Signature(raw) {
			return audioFormat{mediaType: "audio/mpeg", fileName: "audio.mp3", duration: mp3Duration}, true
		}
	case "audio/wav", "audio/x-wav", "audio/wave":
		if len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WAVE" {
			return audioFormat{mediaType: "audio/wav", fileName: "audio.wav", duration: wavDuration}, true
		}
	case "audio/flac", "audio/x-flac":
		if len(raw) >= 4 && string(raw[:4]) == "fLaC" {
			return audioFormat{mediaType: "audio/flac", fileName: "audio.flac", duration: flacDuration}, true
		}
	}
	return audioFormat{}, false
}

func hasMP3Signature(raw []byte) bool {
	offset, ok := mp3Start(raw)
	return ok && offset+4 <= len(raw) && raw[offset] == 0xff && raw[offset+1]&0xe0 == 0xe0
}

func mp3Start(raw []byte) (int, bool) {
	if len(raw) >= 10 && string(raw[:3]) == "ID3" {
		for _, value := range raw[6:10] {
			if value&0x80 != 0 {
				return 0, false
			}
		}
		size := int(raw[6])<<21 | int(raw[7])<<14 | int(raw[8])<<7 | int(raw[9])
		offset := 10 + size
		if raw[5]&0x10 != 0 {
			offset += 10
		}
		return offset, offset+4 <= len(raw)
	}
	return 0, len(raw) >= 4
}

func mp3Duration(raw []byte) (time.Duration, bool) {
	offset, ok := mp3Start(raw)
	if !ok {
		return 0, false
	}
	var samples int64
	var sampleRate int
	frames := 0
	for offset+4 <= len(raw) {
		if len(raw)-offset == 128 && string(raw[offset:offset+3]) == "TAG" {
			offset += 128
			break
		}
		header := binary.BigEndian.Uint32(raw[offset : offset+4])
		frameLength, frameSamples, rate, valid := mp3Frame(header)
		if !valid || offset+frameLength > len(raw) {
			return 0, false
		}
		if sampleRate != 0 && sampleRate != rate {
			return 0, false
		}
		sampleRate = rate
		samples += int64(frameSamples)
		frames++
		offset += frameLength
	}
	if offset != len(raw) || frames == 0 || sampleRate == 0 {
		return 0, false
	}
	return time.Duration(samples) * time.Second / time.Duration(sampleRate), true
}

func mp3Frame(header uint32) (int, int, int, bool) {
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
	bitrate := bitratesMPEG1[bitrateIndex]
	samples := 1152
	coefficient := 144
	if version != 3 {
		rate /= 2
		if version == 0 {
			rate /= 2
		}
		bitrate = bitratesMPEG2[bitrateIndex]
		samples = 576
		coefficient = 72
	}
	length := coefficient*bitrate*1000/rate + padding
	return length, samples, rate, length >= 24
}

func wavDuration(raw []byte) (time.Duration, bool) {
	if len(raw) < 44 || int(binary.LittleEndian.Uint32(raw[4:8]))+8 != len(raw) {
		return 0, false
	}
	offset := 12
	var byteRate uint32
	var dataSize uint32
	var blockAlign uint16
	seenFormat := false
	seenData := false
	for offset+8 <= len(raw) {
		size := binary.LittleEndian.Uint32(raw[offset+4 : offset+8])
		end := offset + 8 + int(size)
		if end > len(raw) {
			return 0, false
		}
		switch string(raw[offset : offset+4]) {
		case "fmt ":
			if seenFormat || size < 16 {
				return 0, false
			}
			seenFormat = true
			format := binary.LittleEndian.Uint16(raw[offset+8 : offset+10])
			channels := binary.LittleEndian.Uint16(raw[offset+10 : offset+12])
			rate := binary.LittleEndian.Uint32(raw[offset+12 : offset+16])
			byteRate = binary.LittleEndian.Uint32(raw[offset+16 : offset+20])
			blockAlign = binary.LittleEndian.Uint16(raw[offset+20 : offset+22])
			bitsPerSample := binary.LittleEndian.Uint16(raw[offset+22 : offset+24])
			validBits := format == 1 && (bitsPerSample == 8 || bitsPerSample == 16 || bitsPerSample == 24 || bitsPerSample == 32) ||
				format == 3 && (bitsPerSample == 32 || bitsPerSample == 64)
			expectedBlockAlign := uint32(channels) * uint32(bitsPerSample) / 8
			if (format != 1 && format != 3) || channels == 0 || channels > 8 || rate < 8000 || rate > 384000 ||
				!validBits || expectedBlockAlign == 0 || uint32(blockAlign) != expectedBlockAlign || byteRate != rate*expectedBlockAlign {
				return 0, false
			}
		case "data":
			if seenData {
				return 0, false
			}
			seenData = true
			dataSize = size
		}
		offset = end + int(size&1)
	}
	if offset != len(raw) || !seenFormat || !seenData || byteRate == 0 || dataSize == 0 || dataSize%uint32(blockAlign) != 0 {
		return 0, false
	}
	return time.Duration(dataSize) * time.Second / time.Duration(byteRate), true
}

func flacDuration(raw []byte) (time.Duration, bool) {
	if len(raw) < 44 || raw[4]&0x7f != 0 || metadataLength(raw[5:8]) != 34 {
		return 0, false
	}
	streamInfo := raw[8:42]
	sampleRate := uint64(streamInfo[10])<<12 | uint64(streamInfo[11])<<4 | uint64(streamInfo[12])>>4
	totalSamples := uint64(streamInfo[13]&0x0f)<<32 | uint64(streamInfo[14])<<24 | uint64(streamInfo[15])<<16 | uint64(streamInfo[16])<<8 | uint64(streamInfo[17])
	if sampleRate < 8000 || sampleRate > 655350 || totalSamples == 0 {
		return 0, false
	}
	offset := 4
	for {
		if offset+4 > len(raw) {
			return 0, false
		}
		last := raw[offset]&0x80 != 0
		length := metadataLength(raw[offset+1 : offset+4])
		offset += 4
		if length > len(raw)-offset {
			return 0, false
		}
		offset += length
		if last {
			break
		}
	}
	if offset+2 > len(raw) || raw[offset] != 0xff || raw[offset+1]&0xfe != 0xf8 {
		return 0, false
	}
	return time.Duration(totalSamples) * time.Second / time.Duration(sampleRate), true
}

func metadataLength(raw []byte) int {
	return int(raw[0])<<16 | int(raw[1])<<8 | int(raw[2])
}
