package mattermost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/mattermost/mattermost/server/public/model"
)

const (
	// egressProxyHost fixes the only supported transport proxy; connection configuration cannot select another route.
	// RU: egressProxyHost фиксирует единственный поддерживаемый proxy; настройки подключения не выбирают иной маршрут.
	egressProxyHost = "egress-gateway.kodex-system.svc.cluster.local:8080"
	// maximumResponseBytes bounds buffered SDK response bodies, including responses without Content-Length.
	// RU: maximumResponseBytes ограничивает буферизуемые ответы SDK, включая ответы без Content-Length.
	maximumResponseBytes = 4 << 20
	// maximumWebsocketBytes caps the SDK socket's individual incoming message size before event decoding.
	// RU: maximumWebsocketBytes ограничивает размер отдельного входящего сообщения SDK сокета до декодирования события.
	maximumWebsocketBytes = 1 << 20
	// maximumMessageBytes bounds accepted inbound and operation message content in bytes, not Unicode code points.
	// RU: maximumMessageBytes ограничивает текст входящих сообщений и операций в байтах, а не Unicode codepoints.
	maximumMessageBytes = 16 << 10
)

// noEffectError wraps a failure proven to precede dispatch or a provider response classified as rejecting the write.
// RU: noEffectError оборачивает доказанный отказ до отправки либо ответ провайдера, классифицированный как отклонение записи.
type noEffectError struct{ cause error }

// Error retains the wrapped internal diagnostic; public outcome mapping must still replace it with a safe code.
// RU: Error сохраняет внутреннюю диагностику причины; публичное отображение исхода обязано заменить её безопасным кодом.
func (err *noEffectError) Error() string { return err.cause.Error() }

// Unwrap preserves sentinel classification without removing the explicit no-effect marker.
// RU: Unwrap сохраняет классификацию исходной ошибки, не удаляя явный маркер отсутствия эффекта.
func (err *noEffectError) Unwrap() error { return err.cause }

// ConfirmedNoEffect recognizes only an explicit no-effect wrapper; absence of the marker never proves successful delivery.
// RU: ConfirmedNoEffect распознаёт только явную обёртку отсутствия эффекта; отсутствие маркера никогда не доказывает успешную доставку.
func ConfirmedNoEffect(err error) bool {
	var noEffect *noEffectError
	return errors.As(err, &noEffect)
}

// deliveryRoot rejects thread coordinates on ordinary delivery and resolves an acknowledgement's exact accepted team/channel/root post.
// RU: deliveryRoot отклоняет координаты ветки у обычной доставки и разрешает точные принятые команду/канал/корневое сообщение подтверждения.
func deliveryRoot(ctx context.Context, client *model.Client4, channel *model.Channel, claim *controlplanev1.InteractionDeliveryClaim) (string, error) {
	ack := claim.GetCapabilityKey() == "mattermost.acknowledgements"
	if !ack {
		if claim.GetAcceptanceReceiptRef() != "" || claim.GetExternalRootPostRef() != "" || claim.GetExternalTeamRef() != "" || claim.GetExternalChannelRef() != "" {
			return "", errConfiguration
		}
		return "", nil
	}
	if !boundedReference(claim.GetAcceptanceReceiptRef()) || claim.GetExternalTeamRef() != channel.TeamId || claim.GetExternalChannelRef() != channel.Id {
		return "", errConfiguration
	}
	root, err := scopedPost(ctx, client, channel.Id, claim.GetExternalRootPostRef())
	if err != nil {
		return "", err
	}
	if root.RootId != "" {
		return "", errResponse
	}
	return root.Id, nil
}

// createPost submits one post and checks created status, exact channel/root and optional gate tuple before returning post/thread coordinates.
// RU: createPost отправляет одно сообщение и сверяет статус создания, точные канал/корень и необязательную связку gate до возврата координат сообщения/ветки.
// Only defined provider rejections carry no-effect proof; transport failure or malformed success remains potentially effective.
// RU: Только заданные отказы провайдера доказывают отсутствие эффекта; транспортный отказ или неверный успешный ответ сохраняют возможность эффекта.
func createPost(ctx context.Context, client *model.Client4, channelID, rootID, message string, gate *gateContext) (string, string, error) {
	input := &model.Post{ChannelId: channelID, RootId: rootID, Message: message}
	if gate != nil {
		gate.addToPost(input)
	}
	post, response, err := client.CreatePost(ctx, input)
	if err != nil {
		classified := classify(err)
		if response != nil {
			switch response.StatusCode {
			case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests:
				return "", "", &noEffectError{cause: classified}
			}
		}
		return "", "", classified
	}
	if response == nil || response.StatusCode != http.StatusCreated || post == nil || !model.IsValidId(post.Id) || post.ChannelId != channelID || post.RootId != rootID || post.DeleteAt != 0 {
		return "", "", errResponse
	}
	if gate != nil {
		readback, err := gateFromPost(post)
		if err != nil || readback == nil || *readback != *gate {
			return "", "", errResponse
		}
	}
	if rootID == "" {
		rootID = post.Id
	}
	return post.Id, rootID, nil
}

// resolveChannel resolves configured names and verifies active exact team/channel identity, rejecting aliases and cross-team responses.
// RU: resolveChannel разрешает заданные имена и сверяет точные активные команду/канал, отклоняя подменённые имена и ответы другой команды.
func resolveChannel(ctx context.Context, client *model.Client4, source source) (*model.Channel, error) {
	if !model.IsValidTeamName(source.GetTeamName()) || !model.IsValidChannelIdentifier(source.GetChannelName()) {
		return nil, errConfiguration
	}
	team, _, err := client.GetTeamByName(ctx, source.GetTeamName(), "")
	if err != nil {
		return nil, classify(err)
	}
	if team == nil || !model.IsValidId(team.Id) || team.Name != source.GetTeamName() || team.DeleteAt != 0 {
		return nil, errResponse
	}
	channel, _, err := client.GetChannelByName(ctx, source.GetChannelName(), team.Id, "")
	if err != nil {
		return nil, classify(err)
	}
	if channel == nil || !model.IsValidId(channel.Id) || channel.TeamId != team.Id || channel.Name != source.GetChannelName() || channel.DeleteAt != 0 {
		return nil, errResponse
	}
	return channel, nil
}

// scopedTransport confines an underlying HTTP transport to one HTTPS origin and buffers bounded response bytes before SDK decoding.
// RU: scopedTransport ограничивает нижележащий HTTP транспорт одним HTTPS адресом и буферизует ограниченный ответ до декодирования SDK.
type scopedTransport struct {
	host string
	next http.RoundTripper
}

// RoundTrip rejects foreign origin/Host or embedded credentials, closes the upstream body and replaces it with a bounded in-memory reader.
// RU: RoundTrip отклоняет чужие адрес/Host и встроенные реквизиты, закрывает исходное тело и заменяет его ограниченным чтением из памяти.
func (transport scopedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != transport.host || request.URL.User != nil || request.URL.Fragment != "" || (request.Host != "" && request.Host != transport.host) {
		return nil, errConfiguration
	}
	response, err := transport.next.RoundTrip(request)
	if err != nil {
		return nil, errUnavailable
	}
	if response == nil || response.Body == nil {
		return nil, errResponse
	}
	defer response.Body.Close()
	if response.ContentLength > maximumResponseBytes {
		return nil, errResponse
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil || len(body) > maximumResponseBytes {
		return nil, errResponse
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

// scopedHTTPClient combines exact-origin transport, an operation timeout and unconditional redirect rejection without opening a connection.
// RU: scopedHTTPClient объединяет транспорт точного адреса, timeout операции и безусловный запрет redirect без открытия соединения.
func scopedHTTPClient(base *url.URL, transport http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: scopedTransport{host: base.Host, next: transport}, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errConfiguration },
	}
}

// validHostname accepts bounded lowercase dotted DNS names with ordinary labels, rejecting IP literals, wildcards, trailing dots and ports.
// RU: validHostname допускает ограниченные строчные DNS-имена с точками и обычными метками, отклоняя IP, wildcard, завершающую точку и порт.
func validHostname(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// validInboundPost checks non-bot author and post IDs, exact channel, valid root, live state and bounded nonempty text before forwarding.
// RU: validInboundPost проверяет идентификаторы сообщения и автора не-бота, точный канал, допустимый корень, живое состояние и ограниченный непустой текст до передачи.
func validInboundPost(post *model.Post, channelID, botID string) bool {
	return post != nil && model.IsValidId(post.Id) && model.IsValidId(post.UserId) && post.UserId != botID && post.ChannelId == channelID && (post.RootId == "" || model.IsValidId(post.RootId)) && post.DeleteAt == 0 && strings.TrimSpace(post.Message) != "" && len(post.Message) <= maximumMessageBytes
}

// sameInboundPost requires a valid readback matching the already parsed event's identity, author, root, message and update timestamp.
// RU: sameInboundPost требует допустимое перечитывание, совпадающее с разобранным событием по идентичности, автору, корню, тексту и времени изменения.
func sameInboundPost(event, post *model.Post, channelID, botID string) bool {
	return validInboundPost(post, channelID, botID) && post.Id == event.Id && post.UserId == event.UserId && post.RootId == event.RootId && post.Message == event.Message && post.UpdateAt == event.UpdateAt
}

// closeSocket closes an initialized listening SDK socket and drains both output channels until the reader closes them, including a blocked send.
// RU: closeSocket закрывает инициализированный слушающий SDK сокет и дренирует оба выходных канала до их закрытия reader, включая заблокированную отправку.
// It relies on the pinned SDK's channel-close lifecycle and has no independent timeout.
// RU: Функция опирается на жизненный цикл закрытия каналов закреплённого SDK и не имеет независимого timeout.
func closeSocket(socket *model.WebSocketClient) {
	// SDK запускает reader внутри Listen; после закрытия сокета дренируем
	// очереди до завершения reader, включая заблокированную отправку в канал.
	_ = socket.Conn.Close()
	socket.Close()
	events, responses := socket.EventChannel, socket.ResponseChannel
	for events != nil || responses != nil {
		select {
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		case _, ok := <-responses:
			if !ok {
				responses = nil
			}
		}
	}
}
