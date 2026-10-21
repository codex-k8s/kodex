// Package tlshello validates the observed TLS ClientHello without terminating TLS.
// Package tlshello проверяет фактический TLS ClientHello без завершения TLS.
package tlshello

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"time"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

const (
	// tlsRecordHeaderBytes is the fixed TLS record header length.
	// tlsRecordHeaderBytes — фиксированная длина заголовка TLS-записи.
	tlsRecordHeaderBytes = 5
	// handshakeHeaderBytes is the fixed handshake message header length.
	// handshakeHeaderBytes — фиксированная длина заголовка handshake-сообщения.
	handshakeHeaderBytes = 4
	// clientHelloType identifies the first handshake message accepted here.
	// clientHelloType обозначает первое handshake-сообщение, допускаемое здесь.
	clientHelloType = 1
	// handshakeRecordType excludes application data before SNI validation.
	// handshakeRecordType исключает прикладные данные до проверки SNI.
	handshakeRecordType = 22
	// serverNameExtension carries the public SNI name to compare with the destination.
	// serverNameExtension содержит открытое имя SNI для сравнения с адресатом.
	serverNameExtension = 0
	// encryptedClientHello identifies ECH, whose hidden destination cannot be verified.
	// encryptedClientHello обозначает ECH со скрытым адресатом, который нельзя проверить.
	encryptedClientHello = 0xfe0d
	// maximumTLSRecords bounds fragmentation of the initial ClientHello.
	// maximumTLSRecords ограничивает фрагментацию начального ClientHello.
	maximumTLSRecords = 16
)

// Reason enumerates closed rejection categories for ClientHello validation.
// Reason перечисляет закрытые категории отказа при проверке ClientHello.
type Reason string

const (
	// ReasonMalformed identifies invalid TLS record or handshake structure.
	// ReasonMalformed обозначает неверную структуру TLS-записи или handshake.
	ReasonMalformed Reason = "malformed"
	// ReasonOversized identifies a byte or record count above the configured bound.
	// ReasonOversized обозначает превышение заданного лимита байтов или записей.
	ReasonOversized Reason = "oversized"
	// ReasonMissing identifies a ClientHello without an SNI host name.
	// ReasonMissing обозначает ClientHello без имени узла SNI.
	ReasonMissing Reason = "missing_sni"
	// ReasonDuplicate identifies repeated SNI extensions or names.
	// ReasonDuplicate обозначает повторяющиеся расширения или имена SNI.
	ReasonDuplicate Reason = "duplicate_sni"
	// ReasonMismatch identifies SNI different from the authorized destination.
	// ReasonMismatch обозначает SNI, отличный от разрешённого адресата.
	ReasonMismatch Reason = "sni_mismatch"
	// ReasonECH identifies encrypted SNI that prevents exact destination verification.
	// ReasonECH обозначает шифрованный SNI, мешающий точной проверке адресата.
	ReasonECH Reason = "ech"
)

// Error exposes only a closed reason, never untrusted TLS bytes or host names.
// Error раскрывает только закрытую причину, без недоверенных TLS-байтов и имён узлов.
type Error struct{ Reason Reason }

// Error returns a stable diagnostic assembled from the closed reason.
// Error возвращает устойчивую диагностику из закрытой причины.
func (err *Error) Error() string { return "TLS ClientHello rejected: " + string(err.Reason) }

// ReadAndVerify buffers a bounded initial ClientHello and verifies its exact SNI.
// ReadAndVerify буферизует ограниченный начальный ClientHello и проверяет точный SNI.
func ReadAndVerify(connection net.Conn, reader *bufio.Reader, maximumBytes int, timeout time.Duration, expectedHostname string) ([]byte, error) {
	if connection == nil || reader == nil || maximumBytes < 1024 || timeout <= 0 || expectedHostname == "" {
		return nil, &Error{Reason: ReasonMalformed}
	}
	if err := connection.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, &Error{Reason: ReasonMalformed}
	}
	defer connection.SetReadDeadline(time.Time{})
	buffered := make([]byte, 0, min(maximumBytes, 16<<10))
	handshake := make([]byte, 0, min(maximumBytes, 16<<10))
	expectedLength := -1
	for record := 0; record < maximumTLSRecords; record++ {
		header := make([]byte, tlsRecordHeaderBytes)
		if _, err := io.ReadFull(reader, header); err != nil {
			return nil, &Error{Reason: ReasonMalformed}
		}
		recordLength := int(binary.BigEndian.Uint16(header[3:5]))
		if header[0] != handshakeRecordType || header[1] != 3 || header[2] < 1 || header[2] > 3 || recordLength == 0 || recordLength > 1<<14 {
			return nil, &Error{Reason: ReasonMalformed}
		}
		if len(buffered)+tlsRecordHeaderBytes+recordLength > maximumBytes {
			return nil, &Error{Reason: ReasonOversized}
		}
		payload := make([]byte, recordLength)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, &Error{Reason: ReasonMalformed}
		}
		buffered = append(buffered, header...)
		buffered = append(buffered, payload...)
		handshake = append(handshake, payload...)
		if len(handshake) >= handshakeHeaderBytes && expectedLength < 0 {
			if handshake[0] != clientHelloType {
				return nil, &Error{Reason: ReasonMalformed}
			}
			expectedLength = handshakeHeaderBytes + (int(handshake[1]) << 16) + (int(handshake[2]) << 8) + int(handshake[3])
			if expectedLength > maximumBytes {
				return nil, &Error{Reason: ReasonOversized}
			}
			if expectedLength < handshakeHeaderBytes+34 {
				return nil, &Error{Reason: ReasonMalformed}
			}
		}
		if expectedLength >= 0 && len(handshake) >= expectedLength {
			hostname, err := parseClientHello(handshake[handshakeHeaderBytes:expectedLength])
			if err != nil {
				return nil, err
			}
			if hostname != expectedHostname {
				return nil, &Error{Reason: ReasonMismatch}
			}
			return buffered, nil
		}
	}
	return nil, &Error{Reason: ReasonOversized}
}

// parseClientHello validates field lengths and rejects duplicate or encrypted SNI extensions.
// parseClientHello проверяет длины полей и отклоняет повторные или зашифрованные расширения SNI.
func parseClientHello(value []byte) (string, error) {
	parser := byteParser{value: value}
	legacyVersion, ok := parser.take(2)
	if !ok || legacyVersion[0] != 3 || legacyVersion[1] != 3 {
		return "", &Error{Reason: ReasonMalformed}
	}
	if _, ok := parser.take(32); !ok {
		return "", &Error{Reason: ReasonMalformed}
	}
	sessionLength, ok := parser.byte()
	if !ok || sessionLength > 32 || !parser.skip(int(sessionLength)) {
		return "", &Error{Reason: ReasonMalformed}
	}
	cipherLength, ok := parser.uint16()
	if !ok || cipherLength < 2 || cipherLength%2 != 0 || !parser.skip(int(cipherLength)) {
		return "", &Error{Reason: ReasonMalformed}
	}
	compressionLength, ok := parser.byte()
	if !ok || compressionLength != 1 {
		return "", &Error{Reason: ReasonMalformed}
	}
	compression, ok := parser.take(1)
	if !ok || compression[0] != 0 {
		return "", &Error{Reason: ReasonMalformed}
	}
	extensionsLength, ok := parser.uint16()
	if !ok || int(extensionsLength) != parser.remaining() {
		return "", &Error{Reason: ReasonMalformed}
	}
	seen := map[uint16]struct{}{}
	hostname := ""
	for parser.remaining() > 0 {
		extensionType, typeOK := parser.uint16()
		extensionLength, lengthOK := parser.uint16()
		if !typeOK || !lengthOK {
			return "", &Error{Reason: ReasonMalformed}
		}
		extension, valueOK := parser.take(int(extensionLength))
		if !valueOK {
			return "", &Error{Reason: ReasonMalformed}
		}
		if _, duplicate := seen[extensionType]; duplicate {
			if extensionType == serverNameExtension {
				return "", &Error{Reason: ReasonDuplicate}
			}
			return "", &Error{Reason: ReasonMalformed}
		}
		seen[extensionType] = struct{}{}
		switch extensionType {
		case encryptedClientHello:
			return "", &Error{Reason: ReasonECH}
		case serverNameExtension:
			parsed, err := parseServerName(extension)
			if err != nil {
				return "", err
			}
			hostname = parsed
		}
	}
	if hostname == "" {
		return "", &Error{Reason: ReasonMissing}
	}
	return hostname, nil
}

// parseServerName accepts one normalized DNS name in an exact-length SNI list.
// parseServerName допускает одно нормализованное имя DNS в списке SNI точной длины.
func parseServerName(value []byte) (string, error) {
	parser := byteParser{value: value}
	listLength, ok := parser.uint16()
	if !ok || int(listLength) != parser.remaining() || listLength == 0 {
		return "", &Error{Reason: ReasonMalformed}
	}
	count := 0
	hostname := ""
	for parser.remaining() > 0 {
		nameType, typeOK := parser.byte()
		nameLength, lengthOK := parser.uint16()
		name, valueOK := parser.take(int(nameLength))
		if !typeOK || !lengthOK || !valueOK || nameType != 0 || len(name) == 0 {
			return "", &Error{Reason: ReasonMalformed}
		}
		count++
		if count > 1 {
			return "", &Error{Reason: ReasonDuplicate}
		}
		normalized, err := policy.NormalizeHostname(string(name))
		if err != nil {
			return "", &Error{Reason: ReasonMalformed}
		}
		hostname = normalized
	}
	if count != 1 {
		return "", &Error{Reason: ReasonMissing}
	}
	return hostname, nil
}

// byteParser tracks a cursor within the already bounded handshake bytes.
// byteParser отслеживает позицию внутри уже ограниченных байтов handshake.
type byteParser struct {
	value  []byte
	offset int
}

// remaining returns unread bytes without advancing the parser.
// remaining возвращает число непрочитанных байтов без продвижения парсера.
func (parser *byteParser) remaining() int { return len(parser.value) - parser.offset }

// take returns an exact-length slice only if it fits inside the bounded input.
// take возвращает срез точной длины, только если он помещается во входные данные.
func (parser *byteParser) take(length int) ([]byte, bool) {
	if length < 0 || length > parser.remaining() {
		return nil, false
	}
	value := parser.value[parser.offset : parser.offset+length]
	parser.offset += length
	return value, true
}

// skip advances past an exact-length field using the same bounds check as take.
// skip пропускает поле точной длины с той же проверкой границ, что и take.
func (parser *byteParser) skip(length int) bool {
	_, ok := parser.take(length)
	return ok
}

// byte reads one byte or reports a truncated field.
// byte читает один байт либо сообщает об обрезанном поле.
func (parser *byteParser) byte() (byte, bool) {
	value, ok := parser.take(1)
	if !ok {
		return 0, false
	}
	return value[0], true
}

// uint16 reads a network-order length or reports a truncated field.
// uint16 читает длину в сетевом порядке либо сообщает об обрезанном поле.
func (parser *byteParser) uint16() (uint16, bool) {
	value, ok := parser.take(2)
	if !ok {
		return 0, false
	}
	return binary.BigEndian.Uint16(value), true
}
