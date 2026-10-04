package gateway

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

const webSocketMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func headerToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func hasWebSocketUpgrade(header http.Header) bool {
	return len(header.Values("Upgrade")) != 0 || headerToken(header, "Connection", "upgrade")
}

func validWebSocketRequest(request *http.Request) bool {
	key, err := base64.StdEncoding.DecodeString(request.Header.Get("Sec-WebSocket-Key"))
	return request.Method == http.MethodGet && request.ContentLength == 0 && len(request.TransferEncoding) == 0 &&
		len(request.Header.Values("Upgrade")) == 1 && strings.EqualFold(request.Header.Get("Upgrade"), "websocket") &&
		len(request.Header.Values("Connection")) == 1 && strings.EqualFold(strings.TrimSpace(request.Header.Get("Connection")), "upgrade") &&
		len(request.Header.Values("Sec-WebSocket-Version")) == 1 && request.Header.Get("Sec-WebSocket-Version") == "13" &&
		len(request.Header.Values("Sec-WebSocket-Key")) == 1 && err == nil && len(key) == 16 &&
		len(request.Header.Values("Sec-WebSocket-Protocol")) == 0 && len(request.Header.Values("Sec-WebSocket-Extensions")) == 0
}

func validWebSocketResponse(response *http.Response, request *http.Request) bool {
	digest := sha1.Sum([]byte(request.Header.Get("Sec-WebSocket-Key") + webSocketMagic))
	_, writable := response.Body.(io.ReadWriteCloser)
	return writable && len(response.Header.Values("Upgrade")) == 1 && strings.EqualFold(response.Header.Get("Upgrade"), "websocket") &&
		len(response.Header.Values("Connection")) == 1 && strings.EqualFold(strings.TrimSpace(response.Header.Get("Connection")), "upgrade") &&
		len(response.Header.Values("Sec-WebSocket-Accept")) == 1 &&
		response.Header.Get("Sec-WebSocket-Accept") == base64.StdEncoding.EncodeToString(digest[:]) &&
		len(response.Header.Values("Sec-WebSocket-Protocol")) == 0 && len(response.Header.Values("Sec-WebSocket-Extensions")) == 0
}

// Upgraded stream имеет прежние CONNECT/host/SNI/CA проверки. Закрывается при
// idle, остановке listener либо EOF любого направления; обе pump всегда joined.
func (server *Server) forwardWebSocket(client net.Conn, reader *bufio.Reader, response *http.Response, limits policy.Limits) {
	upstream := response.Body.(io.ReadWriteCloser)
	defer upstream.Close()
	response.Body = nil
	response.ContentLength = 0
	response.Header.Del("Proxy-Authorization")
	response.Header.Del("Proxy-Authenticate")
	if client.SetWriteDeadline(time.Now().Add(duration(limits.WriteTimeoutMilliseconds))) != nil || response.Write(client) != nil {
		return
	}
	_ = client.SetDeadline(time.Time{})
	activity := make(chan struct{}, 1)
	results := make(chan error, 2)
	var closeOnce sync.Once
	closeBoth := func() { closeOnce.Do(func() { _ = client.Close(); _ = upstream.Close() }) }
	defer closeBoth()
	pump := func(destination io.Writer, source io.Reader) {
		buffer := make([]byte, 32<<10)
		for {
			n, err := source.Read(buffer)
			if n > 0 {
				select {
				case activity <- struct{}{}:
				default:
				}
				writeTimer := time.AfterFunc(duration(limits.WriteTimeoutMilliseconds), closeBoth)
				written, writeErr := destination.Write(buffer[:n])
				writeTimer.Stop()
				if writeErr != nil || written != n {
					results <- io.ErrShortWrite
					return
				}
			}
			if err != nil {
				results <- err
				return
			}
		}
	}
	go pump(upstream, reader)
	go pump(client, upstream)
	timer := time.NewTimer(duration(limits.IdleTimeoutMilliseconds))
	defer timer.Stop()
	completed := 0
	shutdown := server.context.Done()
	for completed < 2 {
		select {
		case <-results:
			completed++
			closeBoth()
		case <-activity:
			timer.Reset(duration(limits.IdleTimeoutMilliseconds))
		case <-timer.C:
			closeBoth()
		case <-shutdown:
			closeBoth()
			shutdown = nil
		}
	}
	server.metrics.Connection("completed", "proxy", "none")
}
