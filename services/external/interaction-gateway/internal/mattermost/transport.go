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
	egressProxyHost       = "egress-gateway.kodex-system.svc.cluster.local:8080"
	maximumResponseBytes  = 4 << 20
	maximumWebsocketBytes = 1 << 20
	maximumMessageBytes   = 16 << 10
)

type noEffectError struct{ cause error }

func (err *noEffectError) Error() string { return err.cause.Error() }
func (err *noEffectError) Unwrap() error { return err.cause }

// ConfirmedNoEffect разрешает повтор только при доказанном отсутствии отправки.
func ConfirmedNoEffect(err error) bool {
	var noEffect *noEffectError
	return errors.As(err, &noEffect)
}

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

type scopedTransport struct {
	host string
	next http.RoundTripper
}

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

func scopedHTTPClient(base *url.URL, transport http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: scopedTransport{host: base.Host, next: transport}, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errConfiguration },
	}
}

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

func validInboundPost(post *model.Post, channelID, botID string) bool {
	return post != nil && model.IsValidId(post.Id) && model.IsValidId(post.UserId) && post.UserId != botID && post.ChannelId == channelID && (post.RootId == "" || model.IsValidId(post.RootId)) && post.DeleteAt == 0 && strings.TrimSpace(post.Message) != "" && len(post.Message) <= maximumMessageBytes
}

func sameInboundPost(event, post *model.Post, channelID, botID string) bool {
	return validInboundPost(post, channelID, botID) && post.Id == event.Id && post.UserId == event.UserId && post.RootId == event.RootId && post.Message == event.Message && post.UpdateAt == event.UpdateAt
}

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
