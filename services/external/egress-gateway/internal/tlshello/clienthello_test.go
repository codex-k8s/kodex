package tlshello

import (
	"bufio"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"
)

func TestReadAndVerifyAcceptsFragmentedExactSNI(t *testing.T) {
	handshake := buildClientHello("api.openai.com", false, false)
	records := append(tlsRecord(handshake[:2]), tlsRecord(handshake[2:])...)
	buffered, err := readHello(t, records, "api.openai.com", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if string(buffered) != string(records) {
		t.Fatal("buffered TLS records were not preserved byte-for-byte")
	}
}

func TestReadAndVerifyRejectsSNIAndECHFailures(t *testing.T) {
	tests := []struct {
		name      string
		handshake []byte
		expected  string
		reason    Reason
	}{
		{"missing", buildClientHello("", false, false), "api.openai.com", ReasonMissing},
		{"duplicate", buildClientHello("api.openai.com", true, false), "api.openai.com", ReasonDuplicate},
		{"mismatch", buildClientHello("github.com", false, false), "api.openai.com", ReasonMismatch},
		{"ECH", buildClientHello("api.openai.com", false, true), "api.openai.com", ReasonECH},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := readHello(t, tlsRecord(test.handshake), test.expected, 64<<10)
			var parseErr *Error
			if !errors.As(err, &parseErr) || parseErr.Reason != test.reason {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestReadAndVerifyRejectsMalformedAndOversized(t *testing.T) {
	malformed := tlsRecord([]byte{clientHelloType, 0, 0, 50, 3})
	if _, err := readHello(t, malformed, "api.openai.com", 1024); err == nil {
		t.Fatal("expected malformed ClientHello rejection")
	}
	largePayload := make([]byte, 1500)
	largePayload[0] = clientHelloType
	largePayload[1] = 0
	largePayload[2] = 5
	largePayload[3] = 216
	_, err := readHello(t, tlsRecord(largePayload), "api.openai.com", 1024)
	var parseErr *Error
	if !errors.As(err, &parseErr) || parseErr.Reason != ReasonOversized {
		t.Fatalf("unexpected oversized error: %v", err)
	}
}

func readHello(t *testing.T, value []byte, expected string, maximum int) ([]byte, error) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { server.Close(); client.Close() })
	go func() { _, _ = client.Write(value) }()
	return ReadAndVerify(server, bufio.NewReader(server), maximum, time.Second, expected)
}

func buildClientHello(hostname string, duplicateSNI, ech bool) []byte {
	body := make([]byte, 0, 256)
	body = append(body, 3, 3)
	body = append(body, make([]byte, 32)...)
	body = append(body, 0)
	body = append(body, 0, 2, 0x13, 0x01)
	body = append(body, 1, 0)
	extensions := make([]byte, 0, 128)
	if hostname != "" {
		extensions = append(extensions, sniExtension(hostname)...)
		if duplicateSNI {
			extensions = append(extensions, sniExtension(hostname)...)
		}
	}
	if ech {
		extensions = append(extensions, 0xfe, 0x0d, 0, 1, 0)
	}
	body = append(body, byte(len(extensions)>>8), byte(len(extensions)))
	body = append(body, extensions...)
	handshake := []byte{clientHelloType, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	return append(handshake, body...)
}

func sniExtension(hostname string) []byte {
	name := []byte(hostname)
	listLength := 3 + len(name)
	value := []byte{byte(listLength >> 8), byte(listLength), 0, byte(len(name) >> 8), byte(len(name))}
	value = append(value, name...)
	extension := []byte{0, 0, byte(len(value) >> 8), byte(len(value))}
	return append(extension, value...)
}

func tlsRecord(payload []byte) []byte {
	value := []byte{handshakeRecordType, 3, 3, 0, 0}
	binary.BigEndian.PutUint16(value[3:], uint16(len(payload)))
	return append(value, payload...)
}
