package entity

import "time"

// TelegramDeliveryReceipt stores only the observed exact bot/chat/topic/message coordinates; provider descriptions, tokens and URLs are absent.
// TelegramDeliveryReceipt хранит лишь наблюдаемые точные bot/chat/topic/message coordinates; provider descriptions, tokens и URL отсутствуют.
type TelegramDeliveryReceipt struct{ BotID, ChatID, TopicID, MessageID int64 }

// TelegramSubscription is the safe project-owned configuration receipt; actual chat coordinates and credential descriptors remain private owner state.
// TelegramSubscription — безопасная квитанция project-owned настройки; точные chat coordinates и credential descriptors остаются приватным owner state.
type TelegramSubscription struct {
	Ref, ProjectRef, ConnectionRef string
	Version                        int64
	Enabled                        bool
	State                          string
	EventTypes                     []string
	ScopeDigest                    string
	BotID                          int64
	ChatDisplay, TopicDisplay      string
	ExpiresAt                      time.Time
	MaxEffects, UsedEffects        int32
	UpdatedAt                      time.Time
	CanManage                      bool
}

// InteractionDeliveryView exposes one eligible owner's delivery status without a token, private chat identifier or message body.
// InteractionDeliveryView выдаёт статус одной доступной владельцу доставки без token, private chat identifier либо message body.
type InteractionDeliveryView struct {
	Ref                                              string
	Version                                          int64
	ConnectionRef, RunRef, GateRef, EventType, State string
	Attempt                                          int32
	Retryable                                        bool
	SafeErrorCode                                    string
	CreatedAt, AvailableAt                           time.Time
	CompletedAt                                      *time.Time
	ReceiptRef                                       string
}

// InteractionDeliveryBegin records only the owner decision for the exact fenced input; a replay is explicitly distinct from new send permission.
// InteractionDeliveryBegin сохраняет лишь owner decision для точного fenced input; replay явно отделён от нового разрешения send.
type InteractionDeliveryBegin struct {
	DeliveryRef                string
	Allowed, AlreadyStarted    bool
	InputDigest, SafeErrorCode string
	RetryAfterSeconds          int32
}
