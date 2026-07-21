package artifacttype

import (
	"bytes"
	"encoding/binary"
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func validPDF(body []byte) bool {
	if len(body) < 12 || len(body) > int(MaxObjectBytes) || len(body) < 8 || body[5] < '1' || body[5] > '2' || body[6] != '.' || body[7] < '0' || body[7] > '9' {
		return false
	}
	trimmed := bytes.TrimRight(body, " \t\r\n\f\x00")
	return bytes.HasSuffix(trimmed, []byte("%%EOF"))
}

func validPNG(body []byte) bool {
	if len(body) < len(pngSignature)+12 || !bytes.Equal(body[:len(pngSignature)], pngSignature) {
		return false
	}
	position := len(pngSignature)
	seenIHDR := false
	seenIDAT := false
	seenIEND := false
	for position < len(body) {
		if len(body)-position < 12 {
			return false
		}
		length := int(binary.BigEndian.Uint32(body[position : position+4]))
		if length < 0 || length > len(body)-position-12 {
			return false
		}
		chunkType := body[position+4 : position+8]
		chunkEnd := position + 12 + length
		if checksumIEEE(body[position+4:position+8+length]) != binary.BigEndian.Uint32(body[position+8+length:chunkEnd]) {
			return false
		}
		switch string(chunkType) {
		case "IHDR":
			if seenIHDR || position != len(pngSignature) || length != 13 {
				return false
			}
			width := binary.BigEndian.Uint32(body[position+8 : position+12])
			height := binary.BigEndian.Uint32(body[position+12 : position+16])
			if width == 0 || height == 0 || uint64(width)*uint64(height) > 100_000_000 {
				return false
			}
			seenIHDR = true
		case "IDAT":
			if !seenIHDR || seenIEND || length == 0 {
				return false
			}
			seenIDAT = true
		case "IEND":
			if !seenIHDR || !seenIDAT || seenIEND || length != 0 || chunkEnd != len(body) {
				return false
			}
			seenIEND = true
		}
		position = chunkEnd
	}
	return seenIHDR && seenIDAT && seenIEND
}

func validGIF(body []byte) bool {
	if len(body) < 14 || (!bytes.HasPrefix(body, []byte("GIF87a")) && !bytes.HasPrefix(body, []byte("GIF89a"))) {
		return false
	}
	position := 13
	packed := body[10]
	if packed&0x80 != 0 {
		position += 3 * (1 << ((packed & 0x07) + 1))
	}
	if position > len(body) {
		return false
	}
	seenImage := false
	for position < len(body) {
		switch body[position] {
		case 0x3b:
			return seenImage && position == len(body)-1
		case 0x21:
			if position+2 > len(body) {
				return false
			}
			position += 2
			next, ok := skipGIFSubBlocks(body, position)
			if !ok {
				return false
			}
			position = next
		case 0x2c:
			if position+10 > len(body) {
				return false
			}
			imagePacked := body[position+9]
			position += 10
			if imagePacked&0x80 != 0 {
				position += 3 * (1 << ((imagePacked & 0x07) + 1))
			}
			if position >= len(body) {
				return false
			}
			position++
			next, ok := skipGIFSubBlocks(body, position)
			if !ok {
				return false
			}
			position = next
			seenImage = true
		default:
			return false
		}
	}
	return false
}

func skipGIFSubBlocks(body []byte, position int) (int, bool) {
	for position < len(body) {
		length := int(body[position])
		position++
		if length == 0 {
			return position, true
		}
		if length > len(body)-position {
			return 0, false
		}
		position += length
	}
	return 0, false
}

func validJPEG(body []byte) bool {
	if len(body) < 4 || body[0] != 0xff || body[1] != 0xd8 {
		return false
	}
	position := 2
	seenFrame := false
	for position < len(body) {
		if body[position] != 0xff {
			return false
		}
		for position < len(body) && body[position] == 0xff {
			position++
		}
		if position >= len(body) || body[position] == 0x00 {
			return false
		}
		marker := body[position]
		position++
		if marker == 0xd9 {
			return seenFrame && position == len(body)
		}
		if marker == 0xd8 || marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if position+2 > len(body) {
			return false
		}
		length := int(binary.BigEndian.Uint16(body[position : position+2]))
		if length < 2 || length > len(body)-position {
			return false
		}
		if isJPEGFrameMarker(marker) {
			seenFrame = true
		}
		position += length
		if marker != 0xda {
			continue
		}
		for position < len(body) {
			if body[position] != 0xff {
				position++
				continue
			}
			if position+1 >= len(body) {
				return false
			}
			next := body[position+1]
			if next == 0x00 || (next >= 0xd0 && next <= 0xd7) {
				position += 2
				continue
			}
			break
		}
	}
	return false
}

func isJPEGFrameMarker(marker byte) bool {
	return (marker >= 0xc0 && marker <= 0xc3) || (marker >= 0xc5 && marker <= 0xc7) || (marker >= 0xc9 && marker <= 0xcb) || (marker >= 0xcd && marker <= 0xcf)
}

func validWEBP(body []byte) bool {
	if len(body) < 20 || !bytes.Equal(body[:4], []byte("RIFF")) || !bytes.Equal(body[8:12], []byte("WEBP")) || uint64(binary.LittleEndian.Uint32(body[4:8]))+8 != uint64(len(body)) {
		return false
	}
	position := 12
	seenExtendedHeader := false
	seenImagePayload := false
	for position < len(body) {
		if len(body)-position < 8 {
			return false
		}
		chunkType := string(body[position : position+4])
		length := int(binary.LittleEndian.Uint32(body[position+4 : position+8]))
		position += 8
		padded := length + length%2
		if length < 0 || padded > len(body)-position {
			return false
		}
		switch chunkType {
		case "VP8X":
			if seenExtendedHeader || seenImagePayload || position != 20 || length != 10 {
				return false
			}
			seenExtendedHeader = true
		case "VP8 ", "VP8L":
			if seenImagePayload || length == 0 {
				return false
			}
			seenImagePayload = true
		case "ANMF":
			if !seenExtendedHeader || length < 16 {
				return false
			}
			seenImagePayload = true
		}
		position += padded
	}
	return seenImagePayload && position == len(body)
}
