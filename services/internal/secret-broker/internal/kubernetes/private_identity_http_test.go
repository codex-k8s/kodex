package kubernetes

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// TestPrivateIdentityBodyOwnsAndClearsAllBytes verifies ownership and whole-buffer clearing after a partial read, with harmless repeated Close and no replayable GetBody or private formatting.
// TestPrivateIdentityBodyOwnsAndClearsAllBytes проверяет ownership и whole-buffer clearing после partial read, безопасный repeated Close, отсутствие replayable GetBody и private formatting.
func TestPrivateIdentityBodyOwnsAndClearsAllBytes(t *testing.T) {
	owned := []byte("synthetic-private-identity-http-body")
	body := newPrivateIdentityRequestBody(owned)
	defer body.Close()
	request, err := http.NewRequest(http.MethodPost, "https://example.invalid/api/v1/namespaces/synthetic/secrets", body)
	if err != nil || request.Body != body || request.GetBody != nil {
		t.Fatal("private body gained a replay closure or lost explicit ownership")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, body), "synthetic-private") {
			t.Fatal("request diagnostics exposed private bytes")
		}
	}
	first := make([]byte, 5)
	defer clear(first)
	if count, err := body.Read(first); count != 5 || err != nil || !bytes.Equal(first, []byte("synth")) {
		t.Fatal("private body read changed")
	}
	if err := request.Body.Close(); err != nil {
		t.Fatal("transport close failed")
	}
	for _, b := range owned {
		if b != 0 {
			t.Fatal("body close did not clear its complete owned buffer")
		}
	}
	if count, err := body.Read(first); count != 0 || err != io.EOF {
		t.Fatal("closed transport reread a private buffer")
	}
	if err := body.Close(); err != nil {
		t.Fatal("caller close after transport close failed")
	}
}

// TestPrivateIdentityBodySerializesAsyncReadAndClose models transport goroutines retaining the reader after Do returns; Close cannot race reads, and joined late reads never consume reused owned storage.
// TestPrivateIdentityBodySerializesAsyncReadAndClose моделирует transport goroutines, сохраняющие reader после возврата Do; Close не гоняется с reads, а joined late reads не читают reused owned storage.
func TestPrivateIdentityBodySerializesAsyncReadAndClose(t *testing.T) {
	for iteration := 0; iteration < 32; iteration++ {
		owned := bytes.Repeat([]byte("synthetic"), 4096)
		body := newPrivateIdentityRequestBody(owned)
		var group sync.WaitGroup
		start := make(chan struct{})
		for reader := 0; reader < 4; reader++ {
			group.Go(func() {
				<-start
				buffer := make([]byte, 127)
				defer clear(buffer)
				for {
					_, err := body.Read(buffer)
					if err != nil {
						if err != io.EOF {
							t.Error("unexpected private body read failure")
						}
						return
					}
				}
			})
		}
		for closer := 0; closer < 2; closer++ {
			group.Go(func() { <-start; _ = body.Close() })
		}
		close(start)
		group.Wait()
		if !bytes.Equal(owned, make([]byte, len(owned))) {
			t.Fatal("joined asynchronous cleanup retained private bytes")
		}
		copy(owned, []byte("must-never-be-reread"))
		if data, err := io.ReadAll(body); err != nil || len(data) != 0 {
			t.Fatal("closed body adopted reused caller memory")
		}
		clear(owned)
	}
}
