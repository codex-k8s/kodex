package mailtransport

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"slices"
	"strings"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

func (p *Provider) smtp(ctx context.Context, m api.Mailbox) (*smtp.Client, func(), error) {
	t, u, pw, e := p.material(ctx, m.Smtp)
	if e != nil {
		return nil, nil, e
	}
	c, cleanup, e := p.connect(ctx, m.Smtp, t, 1<<20)
	if e != nil {
		return nil, nil, e
	}
	client := smtp.NewClient(c)
	if m.Smtp.TlsMode == "starttls" {
		client, e = smtp.NewClientStartTLS(c, t)
		if e != nil {
			cleanup()
			return nil, nil, errs.Unavailable
		}
	}
	client.CommandTimeout = time.Duration(m.Limits.TimeoutSeconds) * time.Second
	client.SubmissionTimeout = client.CommandTimeout
	if client.Hello(m.HelloName) != nil || client.Auth(sasl.NewPlainClient("", u, pw)) != nil {
		cleanup()
		return nil, nil, errs.Unavailable
	}
	return client, cleanup, nil
}
func (p *Provider) Ready(ctx context.Context, m api.Mailbox) error {
	c, done, e := p.smtp(ctx, m)
	if e != nil {
		return e
	}
	e = c.Noop()
	done()
	if e != nil {
		return errs.Unavailable
	}
	pc, done, e := p.pop(ctx, m)
	if e != nil {
		return e
	}
	defer done()
	if _, _, e = snapshot(pc, m); e != nil {
		return e
	}
	return nil
}
func (p *Provider) Send(ctx context.Context, m api.Mailbox, cmd api.Command, id string) (string, error) {
	v := cmd.Message
	inReplyTo := ""
	if cmd.Operation != api.OperationSend {
		pc, done, e := p.pop(ctx, m)
		if e != nil {
			return "unknown", e
		}
		raw, e := readRaw(pc, m, v.SourceUid)
		done()
		if e != nil {
			return "unknown", e
		}
		source, e := mail.ReadMessage(bytes.NewReader(raw))
		if e != nil {
			return "unknown", errs.Invalid
		}
		if cmd.Operation == api.OperationForward {
			v.Attachments = append(v.Attachments, api.Attachment{Filename: "forwarded.eml", ContentType: "message/rfc822", ContentBase64: base64.StdEncoding.EncodeToString(raw)})
		} else {
			reply := source.Header.Get("Reply-To")
			if reply == "" {
				reply = source.Header.Get("From")
			}
			addresses, e := mail.ParseAddressList(reply)
			if e != nil {
				return "unknown", errs.Invalid
			}
			if cmd.Operation == api.OperationReplyAll {
				for _, field := range []string{"To", "Cc"} {
					if value := source.Header.Get(field); value != "" {
						a, e := mail.ParseAddressList(value)
						if e != nil {
							return "unknown", errs.Invalid
						}
						addresses = append(addresses, a...)
					}
				}
			}
			expected := []string{}
			for _, a := range addresses {
				if a.Address != m.Sender && !slices.Contains(expected, a.Address) {
					expected = append(expected, a.Address)
				}
			}
			actual := append([]string{v.To}, v.Cc...)
			actual = append(actual, v.Bcc...)
			slices.Sort(actual)
			slices.Sort(expected)
			if !slices.Equal(actual, expected) {
				return "unknown", errs.Denied
			}
			inReplyTo = source.Header.Get("Message-ID")
			if strings.ContainsAny(inReplyTo, "\r\n\x00") || len(inReplyTo) > 998 {
				return "unknown", errs.Invalid
			}
		}
	}
	raw, e := compose(m, v, id, inReplyTo)
	if e != nil {
		return "unknown", e
	}
	c, done, e := p.smtp(ctx, m)
	if e != nil {
		return "unknown", e
	}
	defer done()
	if c.Mail(m.EnvelopeFrom, nil) != nil {
		return "failed", nil
	}
	to := append([]string{v.To}, v.Cc...)
	to = append(to, v.Bcc...)
	for _, recipient := range to {
		if c.Rcpt(recipient, nil) != nil {
			return "failed", nil
		}
	}
	data, e := c.Data()
	if e != nil {
		return "failed", nil
	}
	if _, e = data.Write(raw); e != nil {
		return "unknown", errs.Unavailable
	}
	if e = data.Close(); e != nil {
		return "unknown", errs.Unavailable
	}
	return "accepted", nil
}
func compose(m api.Mailbox, v api.MessageInput, id, inReplyTo string) ([]byte, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{"Content-Type": []string{"text/plain; charset=utf-8"}, "Content-Transfer-Encoding": []string{"quoted-printable"}}
	part, e := w.CreatePart(h)
	if e != nil {
		return nil, errs.Invalid
	}
	qp := quotedprintable.NewWriter(part)
	if _, e = qp.Write([]byte(v.BodyText)); e != nil {
		return nil, errs.Invalid
	}
	if qp.Close() != nil {
		return nil, errs.Invalid
	}
	if len(v.Attachments) > m.Limits.MaxAttachments {
		return nil, errs.Invalid
	}
	for _, a := range v.Attachments {
		raw, e := base64.StdEncoding.DecodeString(a.ContentBase64)
		if e != nil || len(raw) > m.Limits.AttachmentBytes {
			return nil, errs.Invalid
		}
		h := textproto.MIMEHeader{"Content-Type": []string{a.ContentType}, "Content-Disposition": []string{mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename})}, "Content-Transfer-Encoding": []string{"base64"}}
		part, e := w.CreatePart(h)
		if e != nil {
			return nil, errs.Invalid
		}
		encoded := base64.StdEncoding.EncodeToString(raw)
		for len(encoded) > 0 {
			n := min(76, len(encoded))
			if _, e = io.WriteString(part, encoded[:n]+"\r\n"); e != nil {
				return nil, errs.Invalid
			}
			encoded = encoded[n:]
		}
	}
	if w.Close() != nil {
		return nil, errs.Invalid
	}
	var out bytes.Buffer
	for _, line := range []string{"From: " + v.From, "To: " + v.To, "Subject: " + mime.QEncoding.Encode("utf-8", v.Subject), "Date: " + time.Now().UTC().Format(time.RFC1123Z), "Message-ID: <" + id + "@" + m.HelloName + ">", "MIME-Version: 1.0", "Content-Type: multipart/mixed; boundary=" + w.Boundary()} {
		out.WriteString(line + "\r\n")
	}
	if len(v.Cc) > 0 {
		out.WriteString("Cc: " + strings.Join(v.Cc, ", ") + "\r\n")
	}
	if inReplyTo != "" {
		out.WriteString("In-Reply-To: " + inReplyTo + "\r\nReferences: " + inReplyTo + "\r\n")
	}
	out.WriteString("\r\n")
	out.Write(body.Bytes())
	if out.Len() > m.Limits.MessageBytes {
		return nil, errs.Invalid
	}
	return out.Bytes(), nil
}

func parseMessage(raw []byte, m api.Mailbox) (api.Result, error) {
	msg, e := mail.ReadMessage(bytes.NewReader(raw))
	if e != nil {
		return api.Result{}, errs.Invalid
	}
	result := api.Result{Status: "ok"}
	budget := m.Limits.MessageBytes
	var visit func(textproto.MIMEHeader, io.Reader, int) error
	visit = func(h textproto.MIMEHeader, r io.Reader, depth int) error {
		if depth > 12 {
			return errs.Invalid
		}
		kind := h.Get("Content-Type")
		if kind == "" {
			kind = "text/plain"
		}
		media, params, e := mime.ParseMediaType(kind)
		if e != nil {
			return errs.Invalid
		}
		if strings.HasPrefix(media, "multipart/") {
			mr := multipart.NewReader(r, params["boundary"])
			for {
				p, e := mr.NextPart()
				if e == io.EOF {
					return nil
				}
				if e != nil {
					return errs.Invalid
				}
				if e = visit(p.Header, p, depth+1); e != nil {
					return e
				}
				p.Close()
			}
		}
		switch strings.ToLower(h.Get("Content-Transfer-Encoding")) {
		case "base64":
			r = base64.NewDecoder(base64.StdEncoding, r)
		case "quoted-printable":
			r = quotedprintable.NewReader(r)
		case "", "7bit", "8bit", "binary":
		default:
			return errs.Unsupported
		}
		b, e := io.ReadAll(io.LimitReader(r, int64(budget)+1))
		if e != nil || len(b) > budget {
			return errs.Invalid
		}
		budget -= len(b)
		disposition, dp, _ := mime.ParseMediaType(h.Get("Content-Disposition"))
		filename := dp["filename"]
		if filename == "" {
			filename = params["name"]
		}
		if disposition == "attachment" || filename != "" || media == "message/rfc822" {
			if len(b) > m.Limits.AttachmentBytes || len(result.Attachments) >= m.Limits.MaxAttachments {
				return errs.Invalid
			}
			if filename == "" {
				filename = "attachment"
			}
			if strings.ContainsAny(filename, "/\\\r\n\x00") {
				return errs.Invalid
			}
			result.Attachments = append(result.Attachments, api.Attachment{Filename: filename, ContentType: media, ContentBase64: base64.StdEncoding.EncodeToString(b)})
		} else if media == "text/plain" {
			charset := strings.ToLower(params["charset"])
			if charset != "" && charset != "utf-8" && charset != "us-ascii" {
				return errs.Unsupported
			}
			result.BodyText += string(b)
		}
		return nil
	}
	if e = visit(textproto.MIMEHeader(msg.Header), msg.Body, 0); e != nil {
		return api.Result{}, e
	}
	return result, nil
}
