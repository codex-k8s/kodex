package mail

import (
	"context"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

// Authority resolves the current owner decision before any mailbox operation can reach the protocol adapter.
// Authority разрешает актуальное решение владельца до допуска операции ящика к протокольному адаптеру.
type Authority interface {
	// Resolve evaluates the transport-bound caller, execution lease, exact command digest and mailbox configuration pins.
	// Resolve оценивает связанного с транспортом вызывающего, lease исполнения, точный дайджест команды и настройки ящика.
	Resolve(context.Context, api.AuthorizationRequest) (api.AuthorizationDecision, error)
}

// Provider executes already-authorized mailbox protocol operations; it cannot replace owner policy or effect journaling.
// Provider исполняет уже разрешённые операции почтового протокола; он не заменяет политику владельца или журналирование эффектов.
type Provider interface {
	// Read performs the selected bounded mailbox read using only the authorized mailbox and normalized command.
	// Read выполняет выбранное ограниченное чтение с разрешённым ящиком и нормализованной командой.
	Read(context.Context, api.Mailbox, api.Command) (api.Result, error)
	// Send attempts send/reply/forward with the reserved receipt identity; accepted means SMTP acceptance, not delivery.
	// Send пытается отправить письмо, ответ или пересылку с зарезервированной квитанцией; accepted означает принятие SMTP, не доставку.
	Send(context.Context, api.Mailbox, api.Command, string) (string, error)
	// Delete attempts the selected POP deletion and returns its protocol outcome without authorizing another effect.
	// Delete пытается удалить выбранное сообщение POP и возвращает исход протокола, не разрешая другой эффект.
	Delete(context.Context, api.Mailbox, string) (string, error)
	// Apply attempts the selected IMAP mutation and returns outcome plus observed message coordinates for journaling.
	// Apply пытается выполнить выбранное изменение IMAP и возвращает исход и наблюдаемые координаты для журнала.
	Apply(context.Context, api.Mailbox, api.Command, string) (api.Result, error)
	// Ready checks the selected mailbox transport prerequisites without creating a mail-send effect.
	// Ready проверяет предпосылки транспорта выбранного ящика без эффекта отправки письма.
	Ready(context.Context, api.Mailbox) error
	// Probe returns structured mailbox diagnostics; a successful probe is not evidence of email delivery.
	// Probe возвращает структурированную диагностику ящика; успешная проверка не доказывает доставку письма.
	Probe(context.Context, api.Mailbox) api.Result
}
