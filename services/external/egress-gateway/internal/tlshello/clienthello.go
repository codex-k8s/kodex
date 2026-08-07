// Package tlshello проверяет фактический TLS ClientHello без завершения TLS.
package tlshello

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/policy"
)

const (
	tlsRecordHeaderBytes = 5
	handshakeHeaderBytes = 4
	clientHelloType      = 1
	handshakeRecordType  = 22
	serverNameExtension  = 0
	encryptedClientHello = 0xfe0d
	maximumTLSRecords    = 16
)

// Reason — закрытый набор причин ClientHello reject.
type Reason string

const (
	ReasonMalformed Reason = "malformed"
	ReasonOversized Reason = "oversized"
	ReasonMissing   Reason = "missing_sni"
	ReasonDuplicate Reason = "duplicate_sni"
	ReasonMismatch  Reason = "sni_mismatch"
	ReasonECH       Reason = "ech"
)

// Error не содержит недоверенные TLS bytes или hostname.
type Error struct{ Reason Reason }

func (err *Error) Error() string { return "TLS ClientHello rejected: " + string(err.Reason) }

// ReadAndVerify bounded-буферизует первый ClientHello и проверяет exact SNI.
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

type byteParser struct {
	value  []byte
	offset int
}

func (parser *byteParser) remaining() int { return len(parser.value) - parser.offset }

func (parser *byteParser) take(length int) ([]byte, bool) {
	if length < 0 || length > parser.remaining() {
		return nil, false
	}
	value := parser.value[parser.offset : parser.offset+length]
	parser.offset += length
	return value, true
}

func (parser *byteParser) skip(length int) bool {
	_, ok := parser.take(length)
	return ok
}

func (parser *byteParser) byte() (byte, bool) {
	value, ok := parser.take(1)
	if !ok {
		return 0, false
	}
	return value[0], true
}

func (parser *byteParser) uint16() (uint16, bool) {
	value, ok := parser.take(2)
	if !ok {
		return 0, false
	}
	return binary.BigEndian.Uint16(value), true
}
