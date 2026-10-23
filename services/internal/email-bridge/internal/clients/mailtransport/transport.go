package mailtransport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
)

// Secrets resolves an exact descriptor for a validated mailbox endpoint without exposing credential discovery to protocol callers.
// Secrets разрешает точный descriptor проверенного endpoint ящика, не предоставляя вызывающим протокол поиск реквизитов.
type Secrets interface {
	// Read retrieves the requested credential generation under the supplied operation context; implementations own storage-boundary checks.
	// Read читает запрошенное поколение реквизита в контексте операции; проверки границы хранения принадлежат реализации.
	Read(context.Context, api.Descriptor) ([]byte, error)
}

// Files resolves descriptor name/generation beneath one designated protected root, independently of the immutable configuration loader.
// Files разрешает имя и поколение descriptor под одним назначенным защищённым корнем отдельно от загрузчика снимка конфигурации.
type Files struct{ Root string }

// Read rejects canceled or invalid descriptors and uses a bounded confined secure read, returning only a safe unavailable error on failure.
// Read отвергает отменённый контекст или неверный descriptor и выполняет ограниченное защищённое чтение внутри корня, возвращая безопасный unavailable при отказе.
func (f Files) Read(ctx context.Context, d api.Descriptor) ([]byte, error) {
	if ctx.Err() != nil || !api.DescriptorValid(d) {
		return nil, errs.Unavailable
	}
	b, e := securefile.ReadWithin(f.Root, filepath.Join(f.Root, d.Name, strconv.FormatInt(d.Generation, 10)), 1<<20)
	if e != nil {
		return nil, errs.Unavailable
	}
	return b, nil
}

// Dialer provides a context-aware connection boundary; the production composition supplies Tunnel rather than a direct external dialer.
// Dialer задаёт границу соединения с контекстом; production-композиция передаёт Tunnel, не прямой внешний dialer.
type Dialer interface {
	// Dial opens the supplied host/port target using the configured transport boundary and preserves caller cancellation during setup.
	// Dial открывает переданный host/port через настроенную транспортную границу и сохраняет отмену вызывающего при установлении связи.
	Dial(context.Context, string) (net.Conn, error)
}

// Tunnel dials only its configured platform egress address and pins the expected mail policy/configuration generation on each CONNECT.
// The composition validates that address; header readback is correlation metadata, not a cryptographic proof or end-provider TLS.
// Tunnel соединяется лишь с настроенным platform egress и закрепляет ожидаемое поколение политики и конфигурации почты в каждом CONNECT.
// Адрес проверяет композиция; сверка заголовков является связью метаданных, не криптографическим доказательством или TLS конечного провайдера.
type Tunnel struct {
	Address, PolicyDigest, ConfigurationDigest string
	ConfigurationRevision                      int64
}

// ValidEgressDigest accepts exactly 32 bytes encoded as canonical lowercase hexadecimal, with no algorithm prefix.
// ValidEgressDigest допускает ровно 32 байта в каноническом шестнадцатеричном нижнем регистре без префикса алгоритма.
func ValidEgressDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

// readbackValid requires exactly one matching header for every policy, workload, operation and configuration pin.
// readbackValid требует ровно один совпадающий заголовок для каждой привязки политики, workload, операции и конфигурации.
func (t Tunnel) readbackValid(headers http.Header) bool {
	revision := strconv.FormatInt(t.ConfigurationRevision, 10)
	for key, expected := range map[string]string{
		"X-Kodex-Egress-Revision":               "mail-" + revision,
		"X-Kodex-Egress-Digest":                 t.PolicyDigest,
		"X-Kodex-Egress-Profile":                "email-mail",
		"X-Kodex-Egress-Workload":               "email-bridge",
		"X-Kodex-Egress-Operation":              "email.transport",
		"X-Kodex-Egress-Configuration-Revision": revision,
		"X-Kodex-Egress-Configuration-Digest":   t.ConfigurationDigest,
	} {
		values := headers.Values(key)
		if len(values) != 1 || values[0] != expected {
			return false
		}
	}
	return true
}

// Dial validates local pins, performs one bounded CONNECT and rejects malformed, non-200, body-bearing or mismatched readback before provider traffic.
// Cancellation closes setup; already-buffered opaque greeting bytes are preserved for the protocol adapter that owns subsequent TLS/authentication.
// Dial проверяет локальные привязки, делает один ограниченный CONNECT и отвергает неверный, не-200, содержащий тело или несовпавший ответ до трафика провайдера.
// Отмена закрывает установку связи; буферизированные байты приветствия сохраняются адаптеру протокола, владеющему дальнейшими TLS и аутентификацией.
func (t Tunnel) Dial(ctx context.Context, target string) (net.Conn, error) {
	if t.ConfigurationRevision < 1 || !ValidEgressDigest(t.PolicyDigest) || !ValidEgressDigest(t.ConfigurationDigest) {
		return nil, errs.Unavailable
	}
	c, e := (&net.Dialer{}).DialContext(ctx, "tcp", t.Address)
	if e != nil {
		return nil, errs.Unavailable
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	if _, e = fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); e != nil {
		c.Close()
		return nil, errs.Unavailable
	}
	reader := bufio.NewReader(io.LimitReader(c, 16384))
	resp, e := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if e != nil || resp.StatusCode != 200 || ctx.Err() != nil || !t.readbackValid(resp.Header) || resp.ContentLength > 0 || len(resp.TransferEncoding) != 0 {
		c.Close()
		return nil, errs.Unavailable
	}
	if reader.Buffered() > 0 {
		buffered, _ := reader.Peek(reader.Buffered())
		return &greetingConn{Conn: c, reader: io.MultiReader(bytes.NewReader(bytes.Clone(buffered)), c)}, nil
	}
	return c, nil
}

// Provider adapts already-authorized mailbox commands using private descriptor reads and an injected egress connection boundary.
// It does not own policy approval, effect idempotency or durable receipt completion.
// Provider адаптирует уже разрешённые команды ящика через закрытое чтение descriptor и внедрённую границу egress-соединения.
// Он не владеет подтверждением политики, идемпотентностью эффекта или устойчивой фиксацией квитанции.
type Provider struct {
	Secrets Secrets
	Dialer  Dialer
}

// material loads the exact endpoint CA/user/secret descriptors and rejects malformed trust data, oversized or control-character credential bytes.
// It creates a TLS-1.2-minimum config with exact ServerName and supplied roots; it neither dials nor authenticates the mailbox.
// material читает точные CA/user/secret descriptor endpoint и отвергает неверное доверие, чрезмерные реквизиты или управляющие символы.
// Создаётся TLS-config с минимумом 1.2, точным ServerName и переданными корнями; соединение и вход в ящик здесь не выполняются.
func (p *Provider) material(ctx context.Context, e api.Endpoint) (*tls.Config, string, string, error) {
	ca, err := p.Secrets.Read(ctx, e.Ca)
	if err != nil {
		return nil, "", "", healthFailure(api.ProtocolReadinessReasonCredentialUnavailable)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, "", "", healthFailure(api.ProtocolReadinessReasonConfigurationInvalid)
	}
	u, err := p.Secrets.Read(ctx, e.Username)
	if err != nil {
		return nil, "", "", healthFailure(api.ProtocolReadinessReasonCredentialUnavailable)
	}
	pw, err := p.Secrets.Read(ctx, e.Secret)
	if err != nil {
		return nil, "", "", healthFailure(api.ProtocolReadinessReasonCredentialUnavailable)
	}
	if len(u) == 0 || len(u) > 320 || len(pw) == 0 || len(pw) > 4096 || strings.ContainsAny(string(u)+string(pw), "\r\n\x00") {
		return nil, "", "", healthFailure(api.ProtocolReadinessReasonCredentialUnavailable)
	}
	if e.AuthMethod == "oauthbearer" && (strings.ContainsAny(string(u), ",=\x01") || strings.ContainsRune(string(pw), '\x01')) {
		return nil, "", "", healthFailure(api.ProtocolReadinessReasonCredentialUnavailable)
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: e.ServerName, RootCAs: roots}, string(u), string(pw), nil
}

// cappedConn bounds aggregate incoming wire bytes and prevents protocol libraries from removing or extending the operation deadline.
// The cap applies before higher-level parsing allocations, including TLS framing when this connection is wrapped by TLS.
// cappedConn ограничивает суммарные входящие сетевые байты и не позволяет библиотекам убрать либо продлить срок операции.
// Ограничение действует до выделений parser, включая TLS framing, когда соединение обёрнуто TLS.
type cappedConn struct {
	net.Conn
	remaining int64
	deadline  time.Time
}

// Read clips the destination to remaining wire budget, accounts actually read bytes and rejects further reads once exhausted.
// Read сокращает буфер до остатка сетевого бюджета, учитывает фактически прочитанные байты и отвергает дальнейшее чтение после исчерпания.
func (c *cappedConn) Read(b []byte) (int, error) {
	if c.remaining <= 0 {
		return 0, errs.Unavailable
	}
	if int64(len(b)) > c.remaining {
		b = b[:c.remaining]
	}
	n, e := c.Conn.Read(b)
	c.remaining -= int64(n)
	return n, e
}

// SetDeadline clamps a zero or later requested deadline to the stored operation bound while allowing an earlier one.
// SetDeadline ограничивает нулевой либо поздний срок сохранённой границей операции, допуская более ранний.
func (c *cappedConn) SetDeadline(d time.Time) error {
	if d.IsZero() || d.After(c.deadline) {
		d = c.deadline
	}
	return c.Conn.SetDeadline(d)
}

// SetReadDeadline prevents a protocol client's read-timeout reset from extending the operation's original bound.
// SetReadDeadline не позволяет сбросу read-timeout протокольного клиента продлить исходную границу операции.
func (c *cappedConn) SetReadDeadline(d time.Time) error {
	if d.IsZero() || d.After(c.deadline) {
		d = c.deadline
	}
	return c.Conn.SetReadDeadline(d)
}

// SetWriteDeadline preserves the operation bound when a protocol client resets its write timeout.
// SetWriteDeadline сохраняет границу операции при сбросе write-timeout протокольным клиентом.
func (c *cappedConn) SetWriteDeadline(d time.Time) error {
	if d.IsZero() || d.After(c.deadline) {
		d = c.deadline
	}
	return c.Conn.SetWriteDeadline(d)
}

// connect opens the validated endpoint through Dialer, caps reads and installs context-triggered cleanup plus the caller deadline or a one-minute default.
// Implicit TLS handshakes here; STARTTLS remains the selected protocol adapter's responsibility before any credentials are sent.
// connect открывает проверенный endpoint через Dialer, ограничивает чтение и задаёт закрытие по отмене и срок вызывающего либо минутный default.
// Implicit TLS проходит handshake здесь; STARTTLS остаётся обязанностью выбранного адаптера до отправки реквизитов.
func (p *Provider) connect(ctx context.Context, e api.Endpoint, config *tls.Config, maxBytes int) (net.Conn, func(), error) {
	c, err := p.Dialer.Dial(ctx, net.JoinHostPort(e.Host, strconv.Itoa(e.Port)))
	if err != nil {
		return nil, nil, healthFailure(api.ProtocolReadinessReasonNetworkUnavailable)
	}
	d, ok := ctx.Deadline()
	if !ok {
		d = time.Now().Add(time.Minute)
	}
	bounded := &cappedConn{Conn: c, remaining: int64(maxBytes), deadline: d}
	_ = bounded.SetDeadline(d)
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	cleanup := func() { stop(); _ = c.Close() }
	if e.TlsMode == "implicit" {
		secured := tls.Client(bounded, config)
		if secured.HandshakeContext(ctx) != nil {
			cleanup()
			return nil, nil, healthFailure(api.ProtocolReadinessReasonTLSUnavailable)
		}
		return secured, cleanup, nil
	}
	return bounded, cleanup, nil
}

// popStartTLS requires positive greeting and STLS replies, completes the verified TLS handshake and replays only a synthetic greeting to the POP library.
// The caller owns connection cleanup on any failed upgrade; USER/PASS are not sent by this helper.
// popStartTLS требует положительные greeting и STLS, выполняет проверенный TLS handshake и передаёт POP-библиотеке лишь синтетическое приветствие.
// Закрытием при отказе upgrade владеет вызывающий; USER/PASS этот helper не передаёт.
func popStartTLS(ctx context.Context, c net.Conn, config *tls.Config) (net.Conn, error) {
	tp := textproto.NewConn(c)
	line, e := tp.ReadLine()
	if e != nil || !strings.HasPrefix(line, "+OK") {
		return nil, healthFailure(api.ProtocolReadinessReasonTLSUnavailable)
	}
	if tp.PrintfLine("STLS") != nil {
		return nil, healthFailure(api.ProtocolReadinessReasonTLSUnavailable)
	}
	line, e = tp.ReadLine()
	if e != nil || !strings.HasPrefix(line, "+OK") {
		return nil, healthFailure(api.ProtocolReadinessReasonTLSUnavailable)
	}
	secured := tls.Client(c, config)
	if secured.HandshakeContext(ctx) != nil {
		return nil, healthFailure(api.ProtocolReadinessReasonTLSUnavailable)
	}
	return &greetingConn{Conn: secured, reader: io.MultiReader(strings.NewReader("+OK\r\n"), secured)}, nil
}

// greetingConn preserves a prepared greeting/prefix reader while forwarding all other connection operations to the underlying socket.
// greetingConn сохраняет подготовленное чтение приветствия или префикса, передавая прочие операции нижележащему соединению.
type greetingConn struct {
	net.Conn
	reader io.Reader
}

// Read consumes the prepared reader, which may replay buffered prefix bytes before continuing on the underlying connection.
// Read читает подготовленный reader, который может вернуть сохранённый префикс до продолжения чтения нижележащего соединения.
func (c *greetingConn) Read(b []byte) (int, error) { return c.reader.Read(b) }
