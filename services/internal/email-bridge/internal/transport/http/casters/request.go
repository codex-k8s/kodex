package casters

import (
	"io"
	"net/http"
	"strings"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
)

// Command decodes a bounded mailbox operation or a sender-scoped health/receipt/send request; it rejects malformed inputs before a domain command is executed.
// Command разбирает ограниченную по размеру почтовую команду либо запрос здоровья, квитанции или отправки в области отправителя; ошибочный ввод отклоняется до исполнения доменной команды.
// The boolean reports whether the request used the sender-scoped route; mailbox ownership and send permission are enforced by the downstream service.
// Булево значение показывает, использован ли маршрут с областью отправителя; владение ящиком и право отправки проверяет нижележащий сервис.
func Command(r *http.Request, c api.Configuration) (api.Command, bool, error) {
	var cmd api.Command
	if r.URL.Path == "/v1/mailbox-operations" && r.Method == http.MethodPost {
		if r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" {
			return cmd, false, errs.Invalid
		}
		b, e := io.ReadAll(io.LimitReader(r.Body, 24<<20+1))
		if e != nil || len(b) > 24<<20 || api.Decode(b, &cmd) != nil {
			return cmd, false, errs.Invalid
		}
		return cmd, false, nil
	}
	q := r.URL.Query()
	if len(q) != 1 || len(q["sender"]) != 1 {
		return cmd, true, errs.Invalid
	}
	for _, m := range c.Mailboxes {
		if m.Sender == q.Get("sender") {
			if cmd.MailboxId != "" {
				return cmd, true, errs.Invalid
			}
			cmd.MailboxId = m.Id
		}
	}
	if cmd.MailboxId == "" {
		return cmd, true, errs.NotFound
	}
	switch {
	case r.URL.Path == "/v1/health" && r.Method == http.MethodGet:
		cmd.Operation = api.OperationHealth
	case r.URL.Path == "/v1/messages" && r.Method == http.MethodPost:
		if r.Header.Get("Content-Type") != "application/json" {
			return cmd, true, errs.Invalid
		}
		cmd.Operation = api.OperationSend
		cmd.EffectKey = r.Header.Get("Idempotency-Key")
		b, e := io.ReadAll(io.LimitReader(r.Body, 24<<20+1))
		if e != nil || len(b) > 24<<20 || api.Decode(b, &cmd.Message) != nil {
			return cmd, true, errs.Invalid
		}
	case strings.HasPrefix(r.URL.Path, "/v1/messages/by-idempotency-key/") && r.Method == http.MethodGet:
		cmd.Operation = api.OperationReceipt
		cmd.EffectKey = strings.TrimPrefix(r.URL.Path, "/v1/messages/by-idempotency-key/")
	case strings.HasPrefix(r.URL.Path, "/v1/messages/") && r.Method == http.MethodGet:
		cmd.Operation = api.OperationReceipt
		cmd.ReceiptId = strings.TrimPrefix(r.URL.Path, "/v1/messages/")
	default:
		return cmd, true, errs.NotFound
	}
	if strings.Contains(cmd.ReceiptId, "/") || strings.Contains(cmd.EffectKey, "/") {
		return cmd, true, errs.Invalid
	}
	return cmd, true, nil
}
