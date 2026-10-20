package skillscanclient

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func streamScannerFixture(t *testing.T, versions []string, reply string) (string, *atomic.Int64) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "stream.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	var versionCalls atomic.Int64
	var streamed atomic.Int64
	group.Add(1)
	t.Cleanup(func() { _ = listener.Close(); group.Wait() })
	go func() {
		defer group.Done()
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			reader := bufio.NewReader(connection)
			command, _ := reader.ReadString(0)
			switch command {
			case "zVERSION\x00":
				index := min(int(versionCalls.Add(1))-1, len(versions)-1)
				_, _ = io.WriteString(connection, versions[index]+"\x00")
			case "zINSTREAM\x00":
				complete := false
				for {
					var size uint32
					if binary.Read(reader, binary.BigEndian, &size) != nil {
						break
					}
					if size == 0 {
						complete = true
						break
					}
					if size > 32768 {
						t.Error("unbounded frame")
						break
					}
					n, err := io.CopyN(io.Discard, reader, int64(size))
					streamed.Add(n)
					if err != nil {
						break
					}
				}
				if complete {
					_, _ = io.WriteString(connection, reply+"\x00")
				}
			default:
				t.Error("unexpected scanner command")
			}
			_ = connection.Close()
		}
	}()
	return socket, &streamed
}

func TestArtifactStreamExactBytes(t *testing.T) {
	version := "ClamAV 1.4.3/12345/" + time.Now().UTC().Format("Mon Jan _2 15:04:05 2006")
	body := bytes.Repeat([]byte("bounded stream "), 2300000) // Больше прежних 32 MiB.
	socket, streamed := streamScannerFixture(t, []string{version}, "stream: OK")
	client, err := New(socket, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	report, err := client.ScanReader(t.Context(), bytes.NewReader(body), int64(len(body)))
	digest := sha256.Sum256(body)
	if err != nil || report.Digest != "sha256:"+hex.EncodeToString(digest[:]) || report.SizeBytes != int64(len(body)) || report.Infected || streamed.Load() != int64(len(body)) {
		t.Fatalf("exact streaming proof failed: %v", err)
	}
	if _, err := client.Scan(t.Context(), body); err == nil {
		t.Fatal("skill limit accidentally expanded")
	}
}

func TestArtifactStreamRejectsIncompleteOrUnverified(t *testing.T) {
	date := time.Now().UTC().Format("Mon Jan _2 15:04:05 2006")
	fresh := "ClamAV 1.4.3/12345/" + date
	for _, test := range []struct {
		name     string
		versions []string
		size     int64
		reply    string
	}{
		{"truncated", []string{fresh}, 4, "stream: OK"},
		{"extra bytes", []string{fresh}, 2, "stream: OK"},
		{"oversized", []string{fresh}, 512<<20 + 1, "stream: OK"},
		{"negative", []string{fresh}, -1, "stream: OK"},
		{"stale database", []string{"ClamAV 1.4.3/12345/" + time.Now().Add(-8*24*time.Hour).UTC().Format("Mon Jan _2 15:04:05 2006")}, 3, "stream: OK"},
		{"missing database", []string{"ClamAV 1.4.3/0/" + date}, 3, "stream: OK"},
		{"database rotation", []string{fresh, "ClamAV 1.4.3/12346/" + date}, 3, "stream: OK"},
		{"engine limit", []string{fresh}, 3, "INSTREAM size limit exceeded. ERROR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			socket, _ := streamScannerFixture(t, test.versions, test.reply)
			client, err := New(socket, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ScanReader(t.Context(), strings.NewReader("abc"), test.size); err == nil {
				t.Fatal("unverified stream admitted")
			}
		})
	}
	socket, streamed := streamScannerFixture(t, []string{fresh}, "stream: OK")
	client, _ := New(socket, time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.ScanReader(ctx, strings.NewReader("abc"), 3); err == nil || streamed.Load() != 0 {
		t.Fatal("cancelled scan admitted")
	}
}
