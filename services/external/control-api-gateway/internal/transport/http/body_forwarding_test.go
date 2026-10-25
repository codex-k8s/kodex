package httptransport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

// forwardingReaderFunc adapts a deterministic test read step without buffering or changing its
// byte/error pair, so forwarding tests can observe the exact requested read size and precedence.
// forwardingReaderFunc адаптирует детерминированный шаг чтения без буферизации и изменения пары
// bytes/error, позволяя проверить точный размер запроса и порядок обработки ошибок.
type forwardingReaderFunc func([]byte) (int, error)

// Read invokes the configured step once and preserves its returned byte count and error.
// Read один раз вызывает настроенный шаг и сохраняет возвращённые число байт и ошибку.
func (read forwardingReaderFunc) Read(target []byte) (int, error) {
	return read(target)
}

// TestForwardBodyPreservesBoundarySemantics pins the different artifact/audio admission and read
// budgets, byte/error and send-error precedence, exact digest/count, and retained chunk isolation.
// It uses only in-memory readers and callbacks and does not open HTTP, RPC or provider sessions.
// TestForwardBodyPreservesBoundarySemantics закрепляет разные допуск и бюджет чтения artifact/audio,
// порядок bytes/error и send-error, точные digest/count и независимость сохранённых chunks.
// Используются только readers/callbacks в памяти без HTTP, RPC или сессий провайдера.
func TestForwardBodyPreservesBoundarySemantics(t *testing.T) {
	readFailure := errors.New("synthetic reader failure")
	sendFailure := errors.New("synthetic sender failure")
	profiles := []struct {
		name        string
		forward     func(io.Reader, int64, func([]byte) error) (int64, string, error)
		maximum     int64
		audio       bool
		sizeFailure error
		readFailure error
	}{
		{name: "artifact", forward: forwardArtifactBody, maximum: maximumArtifactBytes, sizeFailure: errArtifactContentLengthMismatch, readFailure: errArtifactBodyRead},
		{name: "audio", forward: forwardAudioPart, maximum: maximumAudioBytes, audio: true, sizeFailure: errAudioSizeMismatch, readFailure: errAudioBodyRead},
	}
	for _, profile := range profiles {
		cases := []struct {
			name       string
			declared   int64
			data       []byte
			terminal   error
			emptyFirst bool
			finalNil   bool
			failSend   bool
			nilReader  bool
			nilSend    bool
			tooLarge   bool
			wantCount  int64
			wantChunks int
			wantError  string
		}{
			{name: "bytes-with-eof", declared: 3, data: []byte("abc"), wantCount: 3, wantChunks: 1},
			{name: "data-then-eof", declared: 3, data: []byte("abc"), finalNil: true, wantCount: 3, wantChunks: 1},
			{name: "overrun-before-send", declared: 2, data: []byte("abc"), wantCount: 3, wantError: "size"},
			{name: "underrun-after-send", declared: 3, data: []byte("ab"), wantCount: 2, wantChunks: 1, wantError: "size"},
			{name: "read-error-before-data", declared: 3, terminal: readFailure, wantError: "read"},
			{name: "read-error-after-exact-data", declared: 3, data: []byte("abc"), terminal: readFailure, wantCount: 3, wantChunks: 1, wantError: "read"},
			{name: "read-error-before-size-check", declared: 3, data: []byte("ab"), terminal: readFailure, wantCount: 2, wantChunks: 1, wantError: "read"},
			{name: "send-error", declared: 3, data: []byte("abc"), failSend: true, wantCount: 3, wantChunks: 1, wantError: "send"},
			{name: "send-error-before-read-error", declared: 3, data: []byte("abc"), terminal: readFailure, failSend: true, wantCount: 3, wantChunks: 1, wantError: "send"},
			{name: "zero-read-is-not-eof", declared: 3, data: []byte("abc"), emptyFirst: true, wantCount: 3, wantChunks: 1},
			{name: "retained-chunks-are-independent", declared: (64 << 10) + 2, data: append(bytes.Repeat([]byte("a"), 64<<10), 'b', 'c'), wantCount: (64 << 10) + 2, wantChunks: 2},
			{name: "empty-body"},
			{name: "nil-reader", declared: 3, nilReader: true, wantError: "size"},
			{name: "nil-sender", declared: 3, nilSend: true, wantError: "size"},
			{name: "negative-size", declared: -1, wantError: "size"},
			{name: "above-profile-maximum", tooLarge: true, wantError: "size"},
		}
		for _, test := range cases {
			t.Run(profile.name+"/"+test.name, func(t *testing.T) {
				declared := test.declared
				if test.tooLarge {
					declared = profile.maximum + 1
				}
				wantError := test.wantError
				if profile.audio && test.name == "empty-body" {
					wantError = "size"
				}
				remaining := test.data
				var requests []int
				var reader io.Reader = forwardingReaderFunc(func(target []byte) (int, error) {
					requests = append(requests, len(target))
					if test.emptyFirst && len(requests) == 1 {
						return 0, nil
					}
					count := copy(target, remaining)
					remaining = remaining[count:]
					if len(remaining) > 0 || count > 0 && test.finalNil {
						return count, nil
					}
					if test.terminal != nil {
						return count, test.terminal
					}
					return count, io.EOF
				})
				if test.nilReader {
					reader = nil
				}
				var chunks [][]byte
				var send func([]byte) error = func(chunk []byte) error {
					chunks = append(chunks, chunk)
					if test.failSend {
						return sendFailure
					}
					return nil
				}
				if test.nilSend {
					send = nil
				}
				received, digest, err := profile.forward(reader, declared, send)
				var expectedError error
				switch wantError {
				case "size":
					expectedError = profile.sizeFailure
				case "read":
					expectedError = profile.readFailure
				case "send":
					expectedError = sendFailure
				}
				if err != expectedError || received != test.wantCount || len(chunks) != test.wantChunks {
					t.Fatalf("forward = count %d chunks %d error %v; want %d/%d/%v", received, len(chunks), err, test.wantCount, test.wantChunks, expectedError)
				}
				if err == nil {
					wantDigest := sha256.Sum256(test.data)
					if digest != hex.EncodeToString(wantDigest[:]) {
						t.Fatal("successful stream digest mismatch")
					}
				} else if digest != "" {
					t.Fatal("failed stream returned a digest")
				}
				if len(chunks) > 0 && !bytes.Equal(bytes.Join(chunks, nil), test.data[:int(received)]) {
					t.Fatal("retained chunks changed after later reads")
				}
				if len(requests) > 0 {
					wantRead := 64 << 10
					if profile.audio && declared+1 < int64(wantRead) {
						wantRead = int(declared + 1)
					}
					if requests[0] != wantRead {
						t.Fatalf("initial read budget = %d; want %d", requests[0], wantRead)
					}
				} else if expectedError == nil || !test.nilReader && !test.nilSend && declared >= 0 && declared <= profile.maximum && !(profile.audio && declared == 0) {
					t.Fatal("valid stream did not read its body")
				}
			})
		}
	}
}
