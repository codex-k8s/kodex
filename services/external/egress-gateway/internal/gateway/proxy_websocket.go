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
		len(request.Header.Values("Sec-WebSocket-Protocol")) == 0 && validWebSocketExtensionOffer(request.Header)
}

func validWebSocketResponse(response *http.Response, request *http.Request) bool {
	digest := sha1.Sum([]byte(request.Header.Get("Sec-WebSocket-Key") + webSocketMagic))
	_, writable := response.Body.(io.ReadWriteCloser)
	return writable && len(response.Header.Values("Upgrade")) == 1 && strings.EqualFold(response.Header.Get("Upgrade"), "websocket") &&
		len(response.Header.Values("Connection")) == 1 && strings.EqualFold(strings.TrimSpace(response.Header.Get("Connection")), "upgrade") &&
		len(response.Header.Values("Sec-WebSocket-Accept")) == 1 &&
		response.Header.Get("Sec-WebSocket-Accept") == base64.StdEncoding.EncodeToString(digest[:]) &&
		len(response.Header.Values("Sec-WebSocket-Protocol")) == 0 && validWebSocketExtensionResponse(response.Header, request.Header)
}

// Upgraded stream имеет прежние CONNECT/host/SNI/CA проверки. Закрывается при
// idle, остановке listener либо EOF любого направления; обе pump всегда joined.
func (server *Server) forwardWebSocket(client net.Conn, reader *bufio.Reader, response *http.Response, limits policy.Limits, diagnostic providerResponsesDiagnostic) {
	upstream := response.Body.(io.ReadWriteCloser)
	defer upstream.Close()
	response.Body = nil
	response.ContentLength = 0
	response.Header.Del("Proxy-Authorization")
	response.Header.Del("Proxy-Authenticate")
	handshakeWriter := &providerWebSocketHandshakeWriter{Writer: client}
	if client.SetWriteDeadline(time.Now().Add(duration(limits.WriteTimeoutMilliseconds))) != nil || response.Write(handshakeWriter) != nil {
		diagnostic.upgrade("WRITE_FAILED", response.StatusCode)
		return
	}
	diagnostic.handshake(response, handshakeWriter)
	diagnostic.upgrade("ACCEPTED", response.StatusCode)
	_ = client.SetDeadline(time.Time{})
	activity := make(chan struct{}, 1)
	results := make(chan string, 2)
	var closeOnce sync.Once
	var clientData, upstreamData bool
	reason := "UNKNOWN"
	closeBoth := func(cause string) {
		closeOnce.Do(func() { reason = cause; _ = client.Close(); _ = upstream.Close() })
	}
	defer closeBoth("UNKNOWN")
	pump := func(destination io.Writer, source io.Reader, fromClient bool) {
		buffer := make([]byte, 32<<10)
		for {
			n, err := source.Read(buffer)
			if n > 0 {
				if fromClient {
					clientData = true
				} else {
					upstreamData = true
				}
				select {
				case activity <- struct{}{}:
				default:
				}
				writeTimer := time.AfterFunc(duration(limits.WriteTimeoutMilliseconds), func() { closeBoth("WRITE_FAILED") })
				written, writeErr := destination.Write(buffer[:n])
				writeTimer.Stop()
				if writeErr != nil || written != n {
					results <- "WRITE_FAILED"
					return
				}
			}
			if err != nil {
				results <- providerWebSocketReadFailure(err, fromClient)
				return
			}
		}
	}
	go pump(upstream, reader, true)
	go pump(client, upstream, false)
	timer := time.NewTimer(duration(limits.IdleTimeoutMilliseconds))
	defer timer.Stop()
	completed := 0
	shutdown := server.context.Done()
	for completed < 2 {
		select {
		case result := <-results:
			completed++
			closeBoth(result)
		case <-activity:
			timer.Reset(duration(limits.IdleTimeoutMilliseconds))
		case <-timer.C:
			closeBoth("IDLE")
		case <-shutdown:
			closeBoth("SHUTDOWN")
			shutdown = nil
		}
	}
	// closeOnce сохраняет причину победившего закрытия, а join синхронизирует
	// чтение: вторичный IO после closeBoth не подменяет write timeout.
	// Каждая pump пишет только свой флаг; оба результата получены после этих
	// записей. Наличие данных не доказывает успешного декодирования SDK.
	diagnostic.pump(reason, clientData, upstreamData)
	server.metrics.Connection("completed", "proxy", "none")
}
