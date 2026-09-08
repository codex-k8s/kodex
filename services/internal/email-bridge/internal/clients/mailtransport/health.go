package mailtransport

import (
	"context"
	"errors"
	"io"
	"net"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
)

// Причина содержит только enum; исходная ошибка провайдера не сохраняется.
type healthFailure api.ProtocolReadinessReason

func (e healthFailure) Error() string { return "mail protocol " + string(e) }
func (e healthFailure) Unwrap() error { return errs.Unavailable }
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

// POP3 library не типизирует -ERR. Наблюдаем только статус строки, не текст ответа.
type popReplyObserver struct {
	net.Conn
	prefix   [5]byte
	offset   int
	rejected bool
}

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
