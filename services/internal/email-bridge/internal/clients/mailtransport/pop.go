package mailtransport

import (
	"bytes"
	"context"
	"encoding/base64"
	"net"
	stdmail "net/mail"
	"sort"
	"strconv"
	"strings"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	pop3 "github.com/knadh/go-pop3"
)

// fixedDial adapts a connection already established and secured by Provider to the POP library's dial interface.
// fixedDial адаптирует уже установленное и защищённое Provider соединение к dial-интерфейсу POP-библиотеки.
type fixedDial struct{ conn net.Conn }

// Dial returns only the prepared connection, ignoring the library's target arguments and never opening a direct external socket.
// Dial возвращает только подготовленное соединение, игнорируя target библиотеки и никогда не открывая прямой внешний сокет.
func (d fixedDial) Dial(_, _ string) (net.Conn, error) { return d.conn, nil }

// pop requires configured password-auth POP, establishes verified implicit/STARTTLS transport before USER/PASS and checks NOOP without enumerating mail.
// Rejections are classified from exact status prefixes; the returned cleanup closes the socket without implicitly issuing QUIT.
// pop требует настроенный POP с password-auth, устанавливает проверенный implicit/STARTTLS до USER/PASS и проверяет NOOP без перечисления почты.
// Отказ определяется по точному статусному префиксу; возвращённая очистка закрывает сокет без неявного QUIT.
func (p *Provider) pop(ctx context.Context, m api.Mailbox) (*pop3.Conn, func(), error) {
	if m.Pop == nil || m.Pop.AuthMethod != "password" {
		return nil, nil, healthFailure(api.ProtocolReadinessReasonConfigurationInvalid)
	}
	tlsConfig, u, pw, e := p.material(ctx, *m.Pop)
	if e != nil {
		return nil, nil, e
	}
	c, cleanup, e := p.connect(ctx, *m.Pop, tlsConfig, 32<<20)
	if e != nil {
		return nil, nil, e
	}
	if m.Pop.TlsMode == "starttls" {
		c, e = popStartTLS(ctx, c, tlsConfig)
		if e != nil {
			cleanup()
			return nil, nil, e
		}
	}
	observer := &popReplyObserver{Conn: c}
	client, e := pop3.New(pop3.Opt{Host: m.Pop.Host, Port: m.Pop.Port, Dialer: fixedDial{observer}}).NewConn()
	if e != nil {
		cleanup()
		return nil, nil, responseFailure(e)
	}
	for _, authenticate := range []func() error{func() error { return client.User(u) }, func() error { return client.Pass(pw) }} {
		observer.rejected = false
		if e = authenticate(); e != nil {
			cleanup()
			if observer.rejected {
				return nil, nil, healthFailure(api.ProtocolReadinessReasonAuthRejected)
			}
			return nil, nil, responseFailure(e)
		}
	}
	if e = client.Noop(); e != nil {
		cleanup()
		return nil, nil, responseFailure(e)
	}
	return client, cleanup, nil
}

// snapshot bounds UIDL/LIST cardinality, cross-checks unique positive message indexes and printable stable UIDs, then sorts by UIDL.
// Advertised sizes are validated metadata, not proof that a later RETR payload fits its content limit.
// snapshot ограничивает число UIDL/LIST, сверяет уникальные положительные индексы и печатные устойчивые UID, затем сортирует по UIDL.
// Объявленный размер является проверенными метаданными, не доказательством допустимости будущего тела RETR.
func snapshot(c *pop3.Conn, m api.Mailbox) ([]pop3.MessageID, map[int]int, error) {
	ids, e := c.Uidl(0)
	if e != nil {
		return nil, nil, responseFailure(e)
	}
	if len(ids) > m.Limits.ScanMessages {
		return nil, nil, healthFailure(api.ProtocolReadinessReasonScanLimit)
	}
	sizes, e := c.List(0)
	if e != nil {
		return nil, nil, responseFailure(e)
	}
	if len(sizes) != len(ids) {
		return nil, nil, healthFailure(api.ProtocolReadinessReasonResponseInvalid)
	}
	byID := map[int]int{}
	seen := map[string]bool{}
	seenIDs := map[int]bool{}
	for _, v := range sizes {
		if _, duplicate := byID[v.ID]; v.ID < 1 || v.Size < 0 || duplicate {
			return nil, nil, healthFailure(api.ProtocolReadinessReasonResponseInvalid)
		}
		byID[v.ID] = v.Size
	}
	for _, v := range ids {
		if v.ID < 1 || v.UID == "" || len(v.UID) > 70 || seen[v.UID] || seenIDs[v.ID] {
			return nil, nil, healthFailure(api.ProtocolReadinessReasonResponseInvalid)
		}
		for _, character := range v.UID {
			if character < 0x21 || character > 0x7e {
				return nil, nil, healthFailure(api.ProtocolReadinessReasonResponseInvalid)
			}
		}
		if _, ok := byID[v.ID]; !ok {
			return nil, nil, healthFailure(api.ProtocolReadinessReasonResponseInvalid)
		}
		seen[v.UID] = true
		seenIDs[v.ID] = true
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].UID < ids[j].UID })
	return ids, byID, nil
}

// readRaw resolves a stable UIDL inside a validated bounded snapshot, checks advertised size and rejects an oversized actual RETR response.
// readRaw разрешает устойчивый UIDL в проверенном ограниченном снимке, проверяет объявленный размер и отвергает чрезмерный фактический RETR.
func readRaw(c *pop3.Conn, m api.Mailbox, uid string) ([]byte, error) {
	ids, sizes, e := snapshot(c, m)
	if e != nil {
		return nil, e
	}
	for _, id := range ids {
		if id.UID == uid {
			if sizes[id.ID] > m.Limits.MessageBytes {
				return nil, errs.Invalid
			}
			b, e := c.Cmd("RETR", true, id.ID)
			if e != nil || b.Len() > m.Limits.MessageBytes {
				return nil, errs.Unavailable
			}
			return b.Bytes(), nil
		}
	}
	return nil, errs.NotFound
}

// Read dispatches validated IMAP commands or bounded POP fetch/attachment/list/search operations and closes the chosen connection afterward.
// POP paging correlates UIDL snapshot and mailbox/query configuration; search filters page headers, while attachment bytes remain untrusted base64 data.
// Read направляет проверенные IMAP-команды либо ограниченные POP fetch/attachment/list/search и затем закрывает выбранное соединение.
// Страницы POP связаны с UIDL-снимком и настройками ящика и запроса; search фильтрует заголовки страницы, а байты вложений остаются недоверенным base64.
func (p *Provider) Read(ctx context.Context, m api.Mailbox, cmd api.Command) (api.Result, error) {
	if m.ReceiveProtocol == "imap" {
		return p.readIMAP(ctx, m, cmd)
	}
	c, cleanup, e := p.pop(ctx, m)
	if e != nil {
		return api.Result{}, e
	}
	defer cleanup()
	if cmd.Operation == api.OperationFetch || cmd.Operation == api.OperationDownload || cmd.Operation == api.OperationAttachments {
		raw, e := readRaw(c, m, cmd.Uid)
		if e != nil {
			return api.Result{}, e
		}
		parsed, e := parseMessage(raw, m)
		if e != nil {
			return api.Result{}, e
		}
		parsed.Uid, parsed.Folder = cmd.Uid, cmd.Folder
		if cmd.Operation == api.OperationDownload {
			if cmd.AttachmentIndex < 0 || cmd.AttachmentIndex >= len(parsed.Attachments) {
				return api.Result{}, errs.NotFound
			}
			parsed.Attachments = []api.Attachment{parsed.Attachments[cmd.AttachmentIndex]}
			parsed.BodyText = ""
		}
		if cmd.Operation == api.OperationAttachments {
			parsed.BodyText = ""
			for i := range parsed.Attachments {
				parsed.Attachments[i].ContentBase64 = ""
			}
		}
		return parsed, nil
	}
	if cmd.Operation != api.OperationList && cmd.Operation != api.OperationSearch {
		return api.Result{}, errs.Unsupported
	}
	ids, sizes, e := snapshot(c, m)
	if e != nil {
		return api.Result{}, e
	}
	binding := api.Digest(struct {
		Tenant, Mailbox, Connection, Query string
		Revision                           int64
		IDs                                []pop3.MessageID
	}{m.TenantId, m.Id, m.ConnectionId, cmd.Query, m.Revision, ids})
	start := 0
	if cmd.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cmd.Cursor)
		if e != nil {
			return api.Result{}, errs.Invalid
		}
		parts := strings.Split(string(b), ":")
		if len(parts) != 2 || parts[0] != binding {
			return api.Result{}, errs.Conflict
		}
		start, e = strconv.Atoi(parts[1])
		if e != nil || start < 0 || start > len(ids) {
			return api.Result{}, errs.Invalid
		}
	}
	result := api.Result{Status: "ok", Headers: []api.MailHeader{}}
	end := min(len(ids), start+m.Limits.PageSize)
	for _, id := range ids[start:end] {
		b, e := c.Cmd("TOP", true, id.ID, 0)
		if e != nil {
			if sizes[id.ID] > m.Limits.MessageBytes {
				return api.Result{}, errs.Invalid
			}
			b, e = c.Cmd("RETR", true, id.ID)
		}
		if e != nil || b.Len() > m.Limits.MessageBytes {
			return api.Result{}, errs.Unavailable
		}
		msg, e := stdmail.ReadMessage(bytes.NewReader(b.Bytes()))
		if e != nil {
			return api.Result{}, errs.Unavailable
		}
		h := api.MailHeader{Uid: id.UID, Size: sizes[id.ID], From: msg.Header.Get("From"), To: msg.Header.Get("To"), Subject: msg.Header.Get("Subject")}
		if cmd.Operation == api.OperationList || strings.Contains(strings.ToLower(h.Subject+" "+h.From+" "+h.To), strings.ToLower(cmd.Query)) {
			result.Headers = append(result.Headers, h)
		}
	}
	if end < len(ids) {
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(binding + ":" + strconv.Itoa(end)))
	}
	return result, nil
}

// Delete finds the authorized stable UIDL, issues DELE and commits it only with a positive QUIT response; failures after an attempt remain unknown.
// Delete находит разрешённый устойчивый UIDL, выполняет DELE и фиксирует его только положительным QUIT; ошибки после попытки остаются unknown.
func (p *Provider) Delete(ctx context.Context, m api.Mailbox, uid string) (string, error) {
	c, cleanup, e := p.pop(ctx, m)
	if e != nil {
		return "unknown", e
	}
	defer cleanup()
	ids, _, e := snapshot(c, m)
	if e != nil {
		return "unknown", e
	}
	for _, id := range ids {
		if id.UID == uid {
			if _, e = c.Cmd("DELE", false, id.ID); e != nil {
				return "unknown", errs.Unavailable
			}
			if _, e = c.Cmd("QUIT", false); e != nil {
				return "unknown", errs.Unavailable
			}
			return "deleted", nil
		}
	}
	return "failed", nil
}
