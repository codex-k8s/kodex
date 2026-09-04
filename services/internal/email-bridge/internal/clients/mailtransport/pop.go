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

type fixedDial struct{ conn net.Conn }

// Отдельный dialer отдаёт только уже проверенный TLS transport.
func (d fixedDial) Dial(_, _ string) (net.Conn, error) { return d.conn, nil }

func (p *Provider) pop(ctx context.Context, m api.Mailbox) (*pop3.Conn, func(), error) {
	tlsConfig, u, pw, e := p.material(ctx, m.Pop)
	if e != nil {
		return nil, nil, e
	}
	c, cleanup, e := p.connect(ctx, m.Pop, tlsConfig, 32<<20)
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
	client, e := pop3.New(pop3.Opt{Host: m.Pop.Host, Port: m.Pop.Port, Dialer: fixedDial{c}}).NewConn()
	if e != nil {
		cleanup()
		return nil, nil, errs.Unavailable
	}
	if e = client.Auth(u, pw); e != nil {
		cleanup()
		return nil, nil, errs.Unavailable
	}
	return client, cleanup, nil
}
func snapshot(c *pop3.Conn, m api.Mailbox) ([]pop3.MessageID, map[int]int, error) {
	ids, e := c.Uidl(0)
	if e != nil || len(ids) > m.Limits.ScanMessages {
		return nil, nil, errs.Unavailable
	}
	sizes, e := c.List(0)
	if e != nil || len(sizes) != len(ids) {
		return nil, nil, errs.Unavailable
	}
	byID := map[int]int{}
	seen := map[string]bool{}
	for _, v := range sizes {
		if v.ID < 1 || v.Size < 0 || byID[v.ID] != 0 {
			return nil, nil, errs.Unavailable
		}
		byID[v.ID] = v.Size
	}
	for _, v := range ids {
		if v.ID < 1 || v.UID == "" || len(v.UID) > 70 || seen[v.UID] {
			return nil, nil, errs.Unavailable
		}
		if _, ok := byID[v.ID]; !ok {
			return nil, nil, errs.Unavailable
		}
		seen[v.UID] = true
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].UID < ids[j].UID })
	return ids, byID, nil
}
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
func (p *Provider) Read(ctx context.Context, m api.Mailbox, cmd api.Command) (api.Result, error) {
	c, cleanup, e := p.pop(ctx, m)
	if e != nil {
		return api.Result{}, e
	}
	defer cleanup()
	if cmd.Operation == api.OperationFetch || cmd.Operation == api.OperationDownload {
		raw, e := readRaw(c, m, cmd.Uid)
		if e != nil {
			return api.Result{}, e
		}
		parsed, e := parseMessage(raw, m)
		if e != nil {
			return api.Result{}, e
		}
		if cmd.Operation == api.OperationDownload {
			if cmd.AttachmentIndex < 0 || cmd.AttachmentIndex >= len(parsed.Attachments) {
				return api.Result{}, errs.NotFound
			}
			parsed.Attachments = []api.Attachment{parsed.Attachments[cmd.AttachmentIndex]}
			parsed.BodyText = ""
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
		Tenant, Mailbox, Query string
		IDs                    []pop3.MessageID
	}{m.TenantId, m.Id, cmd.Query, ids})
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
