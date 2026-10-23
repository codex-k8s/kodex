package gateway

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// webSocketMagic is the RFC 6455 GUID used to validate the server's accept challenge.
// webSocketMagic — GUID RFC 6455 для проверки ответа сервера на challenge.
const webSocketMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// errProviderWebSocketHeaderBound signals that the serialized upgrade exceeded its fixed limit.
// errProviderWebSocketHeaderBound сообщает о превышении лимита сериализованного upgrade-ответа.
var errProviderWebSocketHeaderBound = errors.New("websocket response headers exceed bounded serialization limit")

// providerWebSocketHeaderBuffer bounds serialized headers before any downstream bytes are written.
// providerWebSocketHeaderBuffer ограничивает сериализованные заголовки до записи байтов клиенту.
type providerWebSocketHeaderBuffer struct {
	data    []byte
	maximum int
}

// Write appends a complete fragment only when it fits the remaining header budget.
// Write добавляет целый фрагмент лишь при наличии места в оставшемся лимите заголовка.
func (buffer *providerWebSocketHeaderBuffer) Write(data []byte) (int, error) {
	if len(data) > buffer.maximum-len(buffer.data) {
		return 0, errProviderWebSocketHeaderBound
	}
	buffer.data = append(buffer.data, data...)
	return len(data), nil
}

// writeProviderWebSocketHandshake serializes a bodyless 101 into one bounded downstream write.
// writeProviderWebSocketHandshake сериализует 101 без тела в одну ограниченную запись клиенту.
// It preserves header bytes, avoids tiny TLS records, and clears buffered credentials afterward.
// Функция сохраняет байты заголовков, избегает мелких TLS-пакетов и затем очищает буфер реквизитов.
func writeProviderWebSocketHandshake(response *http.Response, writer io.Writer, maximum int) error {
	if maximum <= 0 {
		return errProviderWebSocketHeaderBound
	}
	if maximum > 64<<10 {
		maximum = 64 << 10
	}
	buffer := providerWebSocketHeaderBuffer{data: make([]byte, 0, maximum), maximum: maximum}
	defer func() { clear(buffer.data) }()
	if err := response.Write(&buffer); err != nil {
		return err
	}
	n, err := writer.Write(buffer.data)
	if err != nil {
		return err
	}
	if n != len(buffer.data) {
		return io.ErrShortWrite
	}
	return nil
}

// headerToken compares comma-separated header tokens case-insensitively without matching substrings.
// headerToken сравнивает разделённые запятыми токены заголовка без учёта регистра и ложных подстрок.
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

// hasWebSocketUpgrade detects an upgrade attempt in either required HTTP header.
// hasWebSocketUpgrade обнаруживает попытку upgrade в любом из обязательных HTTP-заголовков.
func hasWebSocketUpgrade(header http.Header) bool {
	return len(header.Values("Upgrade")) != 0 || headerToken(header, "Connection", "upgrade")
}

// validWebSocketRequest accepts only the narrow RFC 6455 handshake shape and allowed extension offer.
// validWebSocketRequest принимает только строгую форму RFC 6455 и разрешённое предложение расширения.
func validWebSocketRequest(request *http.Request) bool {
	key, err := base64.StdEncoding.DecodeString(request.Header.Get("Sec-WebSocket-Key"))
	return request.Method == http.MethodGet && request.ContentLength == 0 && len(request.TransferEncoding) == 0 &&
		len(request.Header.Values("Upgrade")) == 1 && strings.EqualFold(request.Header.Get("Upgrade"), "websocket") &&
		len(request.Header.Values("Connection")) == 1 && strings.EqualFold(strings.TrimSpace(request.Header.Get("Connection")), "upgrade") &&
		len(request.Header.Values("Sec-WebSocket-Version")) == 1 && request.Header.Get("Sec-WebSocket-Version") == "13" &&
		len(request.Header.Values("Sec-WebSocket-Key")) == 1 && err == nil && len(key) == 16 &&
		len(request.Header.Values("Sec-WebSocket-Protocol")) == 0 && validWebSocketExtensionOffer(request.Header)
}

// validWebSocketResponse checks the exact accept challenge, writable body, and permitted extensions.
// validWebSocketResponse проверяет точный accept challenge, записываемое тело и разрешённые расширения.
func validWebSocketResponse(response *http.Response, request *http.Request) bool {
	digest := sha1.Sum([]byte(request.Header.Get("Sec-WebSocket-Key") + webSocketMagic))
	_, writable := response.Body.(io.ReadWriteCloser)
	return writable && len(response.Header.Values("Upgrade")) == 1 && strings.EqualFold(response.Header.Get("Upgrade"), "websocket") &&
		len(response.Header.Values("Connection")) == 1 && strings.EqualFold(strings.TrimSpace(response.Header.Get("Connection")), "upgrade") &&
		len(response.Header.Values("Sec-WebSocket-Accept")) == 1 &&
		response.Header.Get("Sec-WebSocket-Accept") == base64.StdEncoding.EncodeToString(digest[:]) &&
		len(response.Header.Values("Sec-WebSocket-Protocol")) == 0 && validWebSocketExtensionResponse(response.Header, request.Header)
}

// forwardWebSocket pumps an already authorized upgraded stream until idle, shutdown, EOF, or write failure.
// forwardWebSocket передаёт уже разрешённый upgraded-поток до простоя, остановки, EOF или ошибки записи.
// CONNECT, host, SNI, and CA checks precede this call; both pump goroutines join before reporting completion.
// Проверки CONNECT, host, SNI и CA предшествуют вызову; обе pump-горутины завершаются до отчёта.
func (server *Server) forwardWebSocket(client net.Conn, reader *bufio.Reader, response *http.Response, limits policy.Limits, diagnostic providerResponsesDiagnostic) {
	upstream := response.Body.(io.ReadWriteCloser)
	defer upstream.Close()
	response.Body = nil
	response.ContentLength = 0
	response.Header.Del("Proxy-Authorization")
	response.Header.Del("Proxy-Authenticate")
	handshakeWriter := &providerWebSocketHandshakeWriter{Writer: client}
	if client.SetWriteDeadline(time.Now().Add(duration(limits.WriteTimeoutMilliseconds))) != nil || writeProviderWebSocketHandshake(response, handshakeWriter, limits.MaximumHeaderBytes) != nil {
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
