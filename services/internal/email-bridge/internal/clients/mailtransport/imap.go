package mailtransport

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/mail"
	"slices"
	"strconv"
	"strings"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
)

func (p *Provider) imap(ctx context.Context, m api.Mailbox) (*imapclient.Client, func(), error) {
	if m.Imap == nil {
		return nil, nil, errs.Unsupported
	}
	t, user, secret, err := p.material(ctx, *m.Imap)
	if err != nil {
		return nil, nil, err
	}
	c, closeConn, err := p.connect(ctx, *m.Imap, t, 32<<20)
	if err != nil {
		return nil, nil, err
	}
	opts := &imapclient.Options{TLSConfig: t}
	var client *imapclient.Client
	if m.Imap.TlsMode == "starttls" {
		client, err = imapclient.NewStartTLS(c, opts)
	} else {
		client = imapclient.New(c, opts)
	}
	if err != nil {
		closeConn()
		return nil, nil, errs.Unavailable
	}
	cleanup := func() { closeConn(); _ = client.Close() }
	if m.Imap.AuthMethod == "oauthbearer" {
		err = client.Authenticate(sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{Username: user, Token: secret}))
	} else {
		err = client.Login(user, secret).Wait()
	}
	if err != nil {
		cleanup()
		return nil, nil, errs.Unavailable
	}
	return client, cleanup, nil
}

func selectIMAP(c *imapclient.Client, folder string, validity uint32, readOnly bool) (*imap.SelectData, error) {
	s, err := c.Select(folder, &imap.SelectOptions{ReadOnly: readOnly}).Wait()
	if err != nil {
		return nil, errs.Unavailable
	}
	if s.UIDValidity == 0 || s.UIDNext == 0 {
		return nil, errs.Unavailable
	}
	if validity != 0 && validity != s.UIDValidity {
		return nil, errs.Conflict
	}
	return s, nil
}

func parseUID(value string) (imap.UID, error) {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != value {
		return 0, errs.Invalid
	}
	return imap.UID(n), nil
}

func (p *Provider) sourceRaw(ctx context.Context, m api.Mailbox, cmd api.Command) ([]byte, error) {
	if m.ReceiveProtocol != "imap" {
		c, done, err := p.pop(ctx, m)
		if err != nil {
			return nil, err
		}
		defer done()
		return readRaw(c, m, cmd.Message.SourceUid)
	}
	uid, err := parseUID(cmd.Message.SourceUid)
	if err != nil || cmd.Message.SourceUidValidity == 0 {
		return nil, errs.Invalid
	}
	c, done, err := p.imap(ctx, m)
	if err != nil {
		return nil, err
	}
	defer done()
	if _, err = selectIMAP(c, cmd.Folder, cmd.Message.SourceUidValidity, true); err != nil {
		return nil, err
	}
	raw, _, err := imapRaw(c, m, uid)
	return raw, err
}

// UID FETCH BODY.PEEK не меняет \Seen. Размер проверяется до загрузки literal.
func imapRaw(c *imapclient.Client, m api.Mailbox, uid imap.UID) ([]byte, *imapclient.FetchMessageBuffer, error) {
	set := imap.UIDSetNum(uid)
	meta, err := c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true, RFC822Size: true, Envelope: true}).Collect()
	if err != nil {
		return nil, nil, errs.Unavailable
	}
	if len(meta) == 0 {
		return nil, nil, errs.NotFound
	}
	if len(meta) != 1 || meta[0].UID != uid || meta[0].RFC822Size > int64(m.Limits.MessageBytes) {
		return nil, nil, errs.Invalid
	}
	section := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Size: int64(m.Limits.MessageBytes) + 1}}
	data, err := c.Fetch(set, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
	if err != nil {
		return nil, nil, errs.Unavailable
	}
	if len(data) != 1 || data[0].UID != uid {
		return nil, nil, errs.NotFound
	}
	raw := data[0].FindBodySection(section)
	if len(raw) == 0 || len(raw) > m.Limits.MessageBytes || int64(len(raw)) != meta[0].RFC822Size {
		return nil, nil, errs.Invalid
	}
	return raw, meta[0], nil
}

type imapCursor struct {
	Binding  string `json:"binding"`
	Validity uint32 `json:"validity"`
	Upper    uint32 `json:"upper"`
}

func imapAddresses(a []imap.Address) string {
	values := make([]string, 0, len(a))
	for _, address := range a {
		if value := address.Addr(); value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, ", ")
}

func (p *Provider) readIMAP(ctx context.Context, m api.Mailbox, cmd api.Command) (api.Result, error) {
	c, cleanup, err := p.imap(ctx, m)
	if err != nil {
		return api.Result{}, err
	}
	defer cleanup()
	if cmd.Operation == api.OperationMailboxes {
		r := api.Result{Status: "ok", Mailboxes: []string{}}
		for _, folder := range m.AllowedFolders {
			items, err := c.List("", folder, nil).Collect()
			if err != nil {
				return api.Result{}, errs.Unavailable
			}
			for _, item := range items {
				if item.Mailbox == folder && !slices.Contains(item.Attrs, imap.MailboxAttrNoSelect) {
					r.Mailboxes = append(r.Mailboxes, folder)
				}
			}
		}
		return r, nil
	}
	s, err := selectIMAP(c, cmd.Folder, cmd.UidValidity, true)
	if err != nil {
		return api.Result{}, err
	}
	if cmd.Operation == api.OperationList || cmd.Operation == api.OperationSearch || cmd.Operation == api.OperationThread {
		return imapPage(c, m, cmd, s)
	}
	uid, err := parseUID(cmd.Uid)
	if err != nil || cmd.UidValidity == 0 {
		return api.Result{}, errs.Invalid
	}
	raw, _, err := imapRaw(c, m, uid)
	if err != nil {
		return api.Result{}, err
	}
	r, err := parseMessage(raw, m)
	if err != nil {
		return api.Result{}, err
	}
	r.Uid, r.UidValidity, r.Folder, r.ContentDigest = cmd.Uid, s.UIDValidity, cmd.Folder, api.Digest(raw)
	switch cmd.Operation {
	case api.OperationFetch:
	case api.OperationAttachments:
		r.BodyText = ""
		for i := range r.Attachments {
			r.Attachments[i].ContentBase64 = ""
		}
	case api.OperationDownload:
		if cmd.AttachmentIndex < 0 || cmd.AttachmentIndex >= len(r.Attachments) {
			return api.Result{}, errs.NotFound
		}
		r.BodyText = ""
		r.Attachments = []api.Attachment{r.Attachments[cmd.AttachmentIndex]}
	default:
		return api.Result{}, errs.Unsupported
	}
	return r, nil
}

func imapPage(c *imapclient.Client, m api.Mailbox, cmd api.Command, selected *imap.SelectData) (api.Result, error) {
	binding := api.Digest(struct {
		Tenant, Connection, Mailbox, Folder, Query, Thread string
		Revision                                           int64
		Operation                                          api.Operation
	}{m.TenantId, m.ConnectionId, m.Id, cmd.Folder, cmd.Query, cmd.ThreadId, m.Revision, cmd.Operation})
	page := imapCursor{Binding: binding, Validity: selected.UIDValidity, Upper: uint32(selected.UIDNext) - 1}
	if cmd.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cmd.Cursor)
		if err != nil || json.Unmarshal(raw, &page) != nil {
			return api.Result{}, errs.Invalid
		}
		if page.Binding != binding || page.Validity != selected.UIDValidity || page.Upper >= uint32(selected.UIDNext) {
			return api.Result{}, errs.Conflict
		}
	}
	r := api.Result{Status: "ok", Folder: cmd.Folder, UidValidity: selected.UIDValidity, Headers: []api.MailHeader{}}
	if page.Upper == 0 {
		return r, nil
	}
	lower := uint32(1)
	if page.Upper >= uint32(m.Limits.ScanMessages) {
		lower = page.Upper - uint32(m.Limits.ScanMessages) + 1
	}
	criteria := &imap.SearchCriteria{UID: []imap.UIDSet{{{Start: imap.UID(lower), Stop: imap.UID(page.Upper)}}}}
	if cmd.Operation == api.OperationSearch {
		criteria.Text = []string{cmd.Query}
	}
	if cmd.Operation == api.OperationThread {
		if cmd.ThreadId == "" || len(cmd.ThreadId) > 998 || strings.ContainsAny(cmd.ThreadId, "\r\n\x00") {
			return api.Result{}, errs.Invalid
		}
		// Серверный поиск по Message-ID/References/In-Reply-To, не выдуманный POP thread.
		criteria.Or = [][2]imap.SearchCriteria{{{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: cmd.ThreadId}}}, {Or: [][2]imap.SearchCriteria{{{Header: []imap.SearchCriteriaHeaderField{{Key: "References", Value: cmd.ThreadId}}}, {Header: []imap.SearchCriteriaHeaderField{{Key: "In-Reply-To", Value: cmd.ThreadId}}}}}}}}
		r.ThreadId = cmd.ThreadId
	}
	search, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return api.Result{}, errs.Unavailable
	}
	set, ok := search.All.(imap.UIDSet)
	if !ok || set.Dynamic() {
		return api.Result{}, errs.Unavailable
	}
	// Проверяем диапазоны до Nums, чтобы сервер не навязал огромное выделение памяти.
	var count uint64
	for _, item := range set {
		if item.Start < imap.UID(lower) || item.Stop > imap.UID(page.Upper) || item.Start > item.Stop {
			return api.Result{}, errs.Unavailable
		}
		count += uint64(item.Stop) - uint64(item.Start) + 1
		if count > uint64(m.Limits.ScanMessages) {
			return api.Result{}, errs.Unavailable
		}
	}
	uids, ok := set.Nums()
	if !ok || len(uids) > m.Limits.ScanMessages {
		return api.Result{}, errs.Unavailable
	}
	slices.Sort(uids)
	slices.Reverse(uids)
	next := lower - 1
	if len(uids) > m.Limits.PageSize {
		uids = uids[:m.Limits.PageSize]
		next = uint32(uids[len(uids)-1]) - 1
	}
	for _, uid := range uids {
		items, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, Envelope: true, Flags: true, RFC822Size: true}).Collect()
		if err != nil {
			return api.Result{}, errs.Unavailable
		}
		if len(items) == 0 {
			continue
		}
		if len(items) != 1 || items[0].UID != uid || items[0].Envelope == nil {
			return api.Result{}, errs.Unavailable
		}
		item := items[0]
		h := api.MailHeader{Uid: strconv.FormatUint(uint64(uid), 10), UidValidity: selected.UIDValidity, Folder: cmd.Folder, Subject: item.Envelope.Subject, From: imapAddresses(item.Envelope.From), To: imapAddresses(item.Envelope.To), Size: int(item.RFC822Size), MessageId: item.Envelope.MessageID}
		for _, flag := range item.Flags {
			h.Flags = append(h.Flags, string(flag))
		}
		if cmd.Operation == api.OperationThread {
			raw, _, err := imapRaw(c, m, uid)
			if err != nil {
				return api.Result{}, err
			}
			message, err := mail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				return api.Result{}, errs.Invalid
			}
			exact := false
			target := "<" + strings.Trim(cmd.ThreadId, "<>") + ">"
			for _, field := range []string{"Message-ID", "References", "In-Reply-To"} {
				for _, id := range strings.Fields(message.Header.Get(field)) {
					exact = exact || id == target
				}
			}
			if !exact {
				continue
			}
			parsed, err := parseMessage(raw, m)
			if err != nil {
				return api.Result{}, err
			}
			r.Messages = append(r.Messages, api.MessageView{Uid: h.Uid, UidValidity: selected.UIDValidity, Folder: cmd.Folder, BodyText: parsed.BodyText, Attachments: parsed.Attachments, ContentDigest: api.Digest(raw)})
			encoded, _ := json.Marshal(r.Messages)
			if len(encoded) > m.Limits.MessageBytes {
				return api.Result{}, errs.Invalid
			}
		}
		r.Headers = append(r.Headers, h)
	}
	if next > 0 {
		page.Upper = next
		raw, _ := json.Marshal(page)
		r.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return r, nil
}
