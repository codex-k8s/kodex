package mailtransport

import (
	"context"
	"errors"
	"io"
	"net"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
)

// healthFailure retains a protocol-readiness reason instead of the original provider error or private response text.
// healthFailure сохраняет причину готовности протокола вместо исходной ошибки провайдера или закрытого текста ответа.
type healthFailure api.ProtocolReadinessReason

// Error formats the typed readiness reason with a fixed prefix, without retaining or printing upstream diagnostic content.
// Error форматирует типизированную причину готовности с фиксированным префиксом без сохранения или печати внешних диагностических данных.
func (e healthFailure) Error() string { return "mail protocol " + string(e) }

// Unwrap preserves the domain unavailable sentinel for errors.Is checks while keeping provider details discarded.
// Unwrap сохраняет доменный признак unavailable для errors.Is, оставляя подробности провайдера отброшенными.
func (e healthFailure) Unwrap() error { return errs.Unavailable }

// healthReason returns none for success, a retained typed reason when present, or a generic unavailable reason for other failures.
// healthReason возвращает none при успехе, сохранённую типизированную причину при её наличии либо общий unavailable при прочих отказах.
func healthReason(err error) *api.ProtocolReadinessReason {
	reason := api.ProtocolReadinessReasonNone
	if err != nil {
		reason = api.ProtocolReadinessReasonUnavailable
		var safe healthFailure
		if errors.As(err, &safe) {
			reason = api.ProtocolReadinessReason(safe)
		}
	}
	return &reason
}

// responseFailure distinguishes typed network/EOF/cancellation failures from malformed protocol responses and drops raw error details.
// responseFailure различает типизированные network/EOF/cancellation отказы и неверные протокольные ответы, отбрасывая сырые подробности ошибок.
func responseFailure(err error) error {
	if err == nil {
		return nil
	}
	var network net.Error
	if errors.As(err, &network) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return healthFailure(api.ProtocolReadinessReasonNetworkUnavailable)
	}
	return healthFailure(api.ProtocolReadinessReasonResponseInvalid)
}

// popReplyObserver compensates for the POP library's untyped rejection errors by tracking the last completed line's five-byte status prefix.
// It does not retain a complete response body; protocol readers still receive the original bytes unchanged.
// popReplyObserver компенсирует нетипизированные отказы POP-библиотеки, отслеживая пятибайтовый статусный префикс последней завершённой строки.
// Полное тело ответа не сохраняется; протокольный reader получает исходные байты без изменений.
type popReplyObserver struct {
	net.Conn
	prefix   [5]byte
	offset   int
	rejected bool
}

// Read forwards socket bytes and updates exact -ERR status only at newline boundaries, handling prefixes split across reads.
// Read передаёт сетевые байты и обновляет точный статус -ERR только на границе строки, учитывая префиксы, разделённые чтениями.
func (c *popReplyObserver) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	for _, b := range p[:n] {
		if c.offset < len(c.prefix) {
			c.prefix[c.offset] = b
		}
		if c.offset <= len(c.prefix) {
			c.offset++
		}
		if b == '\n' {
			c.rejected = c.prefix[0] == '-' && c.prefix[1] == 'E' && c.prefix[2] == 'R' && c.prefix[3] == 'R' && (c.prefix[4] == ' ' || c.prefix[4] == '\r' || c.prefix[4] == '\n')
			c.prefix = [5]byte{}
			c.offset = 0
		}
	}
	return n, err
}
