package mailtransport

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
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

// smtp prepares the validated mailbox's bounded TLS connection, performs configured STARTTLS before credentials and authenticates with password or OAuth bearer.
// It returns explicit connection cleanup and maps authentication rejection separately from network or malformed-response failures.
// smtp готовит ограниченное TLS-соединение проверенного ящика, выполняет настроенный STARTTLS до реквизитов и входит по password либо OAuth bearer.
// Возвращается явная очистка соединения; отказ входа отделяется от сетевых ошибок и неверных ответов.
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
			return nil, nil, healthFailure(api.ProtocolReadinessReasonTLSUnavailable)
		}
	}
	client.CommandTimeout = time.Duration(m.Limits.TimeoutSeconds) * time.Second
	client.SubmissionTimeout = client.CommandTimeout
	var auth sasl.Client = sasl.NewPlainClient("", u, pw)
	if m.Smtp.AuthMethod == "oauthbearer" {
		auth = sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{Username: u, Token: pw})
	}
	if err := client.Hello(m.HelloName); err != nil {
		cleanup()
		return nil, nil, responseFailure(err)
	}
	if err := client.Auth(auth); err != nil {
		cleanup()
		var protocol *smtp.SMTPError
		if errors.As(err, &protocol) && (protocol.Code == 535 || protocol.Code == 534 || protocol.Code == 530) {
			return nil, nil, healthFailure(api.ProtocolReadinessReasonAuthRejected)
		}
		return nil, nil, responseFailure(err)
	}
	return client, cleanup, nil
}

// Ready reduces the structured protocol probe to the domain unavailable sentinel unless SMTP and the selected receive protocol are ready.
// Ready сводит структурированную проверку протоколов к доменному unavailable, если SMTP и выбранный протокол получения не готовы.
func (p *Provider) Ready(ctx context.Context, m api.Mailbox) error {
	if p.Probe(ctx, m).Status != "ready" {
		return errs.Unavailable
	}
	return nil
}

// Probe checks SMTP authentication/NOOP and configured IMAP folder selection or POP authentication/NOOP, returning separate safe reasons.
// It sends no email, does not enumerate the POP maildrop and does not prove future provider availability or recipient delivery.
// Probe проверяет SMTP-вход и NOOP, выбор настроенных IMAP-папок либо POP-вход и NOOP, возвращая отдельные безопасные причины.
// Письмо не отправляется, POP maildrop не перечисляется; будущая доступность провайдера и доставка получателю не доказываются.
func (p *Provider) Probe(ctx context.Context, m api.Mailbox) api.Result {
	report := &api.ProtocolReadiness{Smtp: api.ProtocolReadinessSmtpNotReady, Imap: api.ProtocolReadinessImapNotConfigured, Pop3: api.ProtocolReadinessPop3NotConfigured, ImapReason: healthReason(nil), Pop3Reason: healthReason(nil)}
	c, done, err := p.smtp(ctx, m)
	if err == nil {
		err = responseFailure(c.Noop())
		done()
	}
	report.SmtpReason = healthReason(err)
	if err == nil {
		report.Smtp = api.ProtocolReadinessSmtpReady
	}
	if m.Imap != nil {
		report.Imap = api.ProtocolReadinessImapNotReady
		c, done, err := p.imap(ctx, m)
		if err == nil {
			for _, folder := range m.AllowedFolders {
				if _, err = selectIMAP(c, folder, 0, true); err != nil {
					break
				}
			}
			done()
		}
		report.ImapReason = healthReason(err)
		if err == nil {
			report.Imap = api.ProtocolReadinessImapReady
		}
	}
	if m.Pop != nil {
		report.Pop3 = api.ProtocolReadinessPop3NotReady
		_, done, err := p.pop(ctx, m)
		if err == nil {
			// pop уже подтвердил transport, authentication и NOOP. HEALTH не
			// перечисляет maildrop: scanMessages относится к LIST/SEARCH.
			done()
		}
		report.Pop3Reason = healthReason(err)
		if err == nil {
			report.Pop3 = api.ProtocolReadinessPop3Ready
		}
	}
	status := "not_ready"
	if report.Smtp == api.ProtocolReadinessSmtpReady && ((m.ReceiveProtocol == "imap" && report.Imap == api.ProtocolReadinessImapReady) || (m.ReceiveProtocol == "pop3" && report.Pop3 == api.ProtocolReadinessPop3Ready)) {
		status = "ready"
	}
	return api.Result{Status: status, ProtocolReadiness: report}
}

// Send handles an already-approved send/reply/forward command, requiring source-derived reply recipients to be present and composing Bcc-free MIME headers.
// It attempts SMTP once: pre-DATA rejections yield failed, uncertain data write/acknowledgement yields unknown, and accepted means SMTP acceptance only.
// Send обрабатывает уже одобренную send/reply/forward-команду, требуя наличия получателей из источника ответа и собирая MIME-заголовки без Bcc.
// SMTP выполняется один раз: отказ до DATA даёт failed, неопределённая запись или подтверждение данных — unknown, а accepted означает лишь приём SMTP.
func (p *Provider) Send(ctx context.Context, m api.Mailbox, cmd api.Command, id string) (string, error) {
	v := cmd.Message
	inReplyTo := ""
	if cmd.Operation != api.OperationSend {
		raw, e := p.sourceRaw(ctx, m, cmd)
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
			for _, recipient := range expected {
				if !slices.Contains(actual, recipient) {
					return "unknown", errs.Denied
				}
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

// compose encodes domain-validated message fields as multipart MIME with quoted-printable text, folded base64 attachments and a receipt-derived Message-ID.
// It enforces attachment/message byte limits and omits Bcc headers; authorization, sender policy and filename/header eligibility are caller prerequisites.
// compose кодирует проверенные доменом поля в multipart MIME с quoted-printable текстом, переносами base64-вложений и Message-ID из квитанции.
// Проверяются пределы вложений и сообщения, Bcc-заголовки не пишутся; полномочия, политика отправителя и допуск имён и заголовков — предпосылки вызывающего.
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
	out.WriteString("Reply-To: " + m.ReplyTo + "\r\n")
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

// parseMessage walks MIME to depth 12, bounds decoded aggregate/attachment bytes and count, and accepts only supported text charsets and encodings.
// It returns plain text plus base64 attachment data; syntax/size checks are not antivirus, semantic analysis or quarantine admission.
// parseMessage обходит MIME до глубины 12, ограничивает декодированные общие байты, вложения и их число, допуская только поддержанные кодировки текста.
// Возвращаются plain text и base64-вложения; проверки синтаксиса и размера не являются антивирусом, смысловым анализом или допуском из карантина.
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
			if len(filename) > 255 || strings.ContainsAny(filename, "/\\\r\n\x00") {
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
