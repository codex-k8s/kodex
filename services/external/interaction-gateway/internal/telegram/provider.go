package telegram

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// botIdentity contains only the current bot coordinates needed for service identity and supported topic checks; human profile names are discarded.
// botIdentity содержит лишь текущие координаты бота для service identity и проверки topic support; человеческие profile names отбрасываются.
type botIdentity struct {
	ID               int64  `json:"id"`
	IsBot            bool   `json:"is_bot"`
	Username         string `json:"username"`
	HasTopicsEnabled bool   `json:"has_topics_enabled"`
}

// scopedChat records the exact configured chat and text permissions without retaining its title, description, photo or member list.
// scopedChat сохраняет точный настроенный chat и text permissions без title, description, photo либо member list.
type scopedChat struct {
	ID               int64  `json:"id"`
	Type             string `json:"type"`
	IsForum          bool   `json:"is_forum"`
	IsDirectMessages bool   `json:"is_direct_messages"`
	Permissions      *struct {
		CanSendMessages bool `json:"can_send_messages"`
	} `json:"permissions"`
}

// scopedMember verifies the current bot's role and the precise text/channel permissions; another user's membership is never authority.
// scopedMember проверяет текущую роль бота и точные text/channel permissions; членство другого пользователя не становится authority.
type scopedMember struct {
	Status          string      `json:"status"`
	User            botIdentity `json:"user"`
	IsMember        bool        `json:"is_member"`
	CanSendMessages bool        `json:"can_send_messages"`
	CanPostMessages bool        `json:"can_post_messages"`
}

// scopeProof is an observation of this call's fresh authenticated lookup and never substitutes for the owner's fenced Begin or the send endpoint.
// scopeProof — наблюдение свежего аутентифицированного lookup данного вызова, не заменяющее owner fenced Begin либо send endpoint.
type scopeProof struct {
	bot  botIdentity
	chat scopedChat
}

// currentScope rechecks service identity, exact numeric chat/topic support and applicable own membership; no lookup can migrate or broaden the target.
// currentScope заново проверяет service identity, точный numeric chat/topic support и применимое собственное членство; lookup не переносит и не расширяет target.
func (client *apiClient) currentScope(ctx context.Context, scope integrationpackage.TelegramConfiguration) (scopeProof, error) {
	tokenBot := strings.Split(client.token, ":")[0]
	if strconv.FormatInt(scope.BotID, 10) != tokenBot || scope.ChatID == 0 || scope.ChatID < -maximumIdentifier || scope.ChatID > maximumIdentifier || scope.TopicID < 0 || scope.TopicID > 1<<31-1 {
		return scopeProof{}, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	raw, err := client.call(ctx, operationIdentity, struct{}{})
	if err != nil {
		return scopeProof{}, preflightFailure(err)
	}
	var bot botIdentity
	if json.Unmarshal(raw, &bot) != nil || !bot.IsBot || bot.ID != scope.BotID || len(bot.Username) > 64 {
		return scopeProof{}, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	for _, character := range bot.Username {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_') {
			return scopeProof{}, &SafeError{Code: errorConfiguration, NoEffect: true}
		}
	}
	raw, err = client.call(ctx, operationChat, struct {
		ChatID int64 `json:"chat_id"`
	}{ChatID: scope.ChatID})
	if err != nil {
		return scopeProof{}, preflightFailure(err)
	}
	var chat scopedChat
	if json.Unmarshal(raw, &chat) != nil || chat.ID != scope.ChatID || chat.IsDirectMessages {
		return scopeProof{}, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	switch chat.Type {
	case "private":
		if chat.ID < 1 || scope.TopicID > 0 && !bot.HasTopicsEnabled {
			return scopeProof{}, &SafeError{Code: errorConfiguration, NoEffect: true}
		}
	case "group", "supergroup", "channel":
		if chat.ID > 0 || scope.TopicID > 0 && (chat.Type != "supergroup" || !chat.IsForum) {
			return scopeProof{}, &SafeError{Code: errorConfiguration, NoEffect: true}
		}
		raw, err = client.call(ctx, operationMember, struct {
			ChatID int64 `json:"chat_id"`
			UserID int64 `json:"user_id"`
		}{ChatID: scope.ChatID, UserID: scope.BotID})
		if err != nil {
			return scopeProof{}, preflightFailure(err)
		}
		var member scopedMember
		if json.Unmarshal(raw, &member) != nil || member.User.ID != scope.BotID || !member.User.IsBot || !memberCanSend(chat, member) {
			return scopeProof{}, &SafeError{Code: errorProviderRejected, NoEffect: true}
		}
	default:
		return scopeProof{}, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	return scopeProof{bot: bot, chat: chat}, nil
}

// preflightFailure marks failures of read-only lookup as no-effect for this delivery while preserving only safe codes and bounded provider wait.
// preflightFailure отмечает отказы read-only lookup как no-effect данной доставки, сохраняя лишь safe codes и ограниченный provider wait.
func preflightFailure(err error) error {
	if safe, ok := err.(*SafeError); ok {
		if safe.Code == errorUnknown {
			return &SafeError{Code: errorPreflightUnavailable, NoEffect: true}
		}
		return &SafeError{Code: safe.Code, NoEffect: true, RetryAfter: safe.RetryAfter}
	}
	return &SafeError{Code: errorPreflightUnavailable, NoEffect: true}
}

// memberCanSend applies a closed role mapping and current chat text restrictions; the exact send endpoint remains final provider authority.
// memberCanSend применяет закрытую матрицу ролей и текущие chat text restrictions; точный send endpoint остаётся окончательной provider authority.
func memberCanSend(chat scopedChat, member scopedMember) bool {
	if chat.Type == "channel" {
		return member.Status == "creator" || member.Status == "administrator" && member.CanPostMessages
	}
	switch member.Status {
	case "creator", "administrator":
		return true
	case "member":
		return chat.Permissions != nil && chat.Permissions.CanSendMessages
	case "restricted":
		return member.IsMember && member.CanSendMessages
	default:
		return false
	}
}
