package skillscanclient

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestScanProtocol(t *testing.T) {
	for _, test := range []struct {
		name, reply         string
		infected, wantError bool
	}{
		{"clean", "stream: OK\x00", false, false},
		{"infected", "stream: Eicar-Signature FOUND\x00", true, false},
		{"error", "stream: inspection failed ERROR\x00", false, true},
		{"unframed", "stream: OK", false, true},
		{"wrongTarget", "other: OK\x00", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "c.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			var group sync.WaitGroup
			group.Add(1)
			t.Cleanup(func() { _ = listener.Close(); group.Wait() })
			body := []byte(strings.Repeat("verified content ", 5000))
			version := "ClamAV 1.4.3/12345/" + time.Now().UTC().Format("Mon Jan _2 15:04:05 2006") + "\x00"
			go func() {
				defer group.Done()
				for {
					connection, err := listener.Accept()
					if err != nil {
						return
					}
					_ = connection.SetDeadline(time.Now().Add(time.Second))
					reader := bufio.NewReader(connection)
					command, err := reader.ReadString(0)
					if err != nil {
						_ = connection.Close()
						continue
					}
					switch command {
					case "zVERSION\x00":
						_, _ = io.WriteString(connection, version)
					case "zINSTREAM\x00":
						var received []byte
						for {
							var size uint32
							if binary.Read(reader, binary.BigEndian, &size) != nil {
								break
							}
							if size == 0 {
								break
							}
							if size > 32768 {
								t.Error("unbounded scan frame")
								break
							}
							chunk := make([]byte, size)
							if _, err := io.ReadFull(reader, chunk); err != nil {
								break
							}
							received = append(received, chunk...)
						}
						if string(received) != string(body) {
							t.Error("scanner received different content")
						}
						_, _ = io.WriteString(connection, test.reply)
					default:
						t.Error("unexpected scanner command")
					}
					_ = connection.Close()
				}
			}()
			client, err := New(socket, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			verdict, err := client.Scan(t.Context(), body)
			if (err != nil) != test.wantError {
				t.Fatalf("unexpected result: %v", err)
			}
			if err == nil && (verdict.Infected != test.infected || !strings.HasPrefix(verdict.Engine, "ClamAV ")) {
				t.Fatal("incorrect verdict provenance")
			}
		})
	}
}

func TestScannerConfiguration(t *testing.T) {
	for _, socket := range []string{"", "relative.sock", "/tmp/../scanner.sock", "/tmp/scan\x00.sock"} {
		if _, err := New(socket, time.Second); err == nil {
			t.Fatal("invalid socket accepted")
		}
	}
	client, err := New(filepath.Join(t.TempDir(), "absent.sock"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Scan(t.Context(), []byte("body")); err == nil {
		t.Fatal("missing scanner accepted")
	}
}
