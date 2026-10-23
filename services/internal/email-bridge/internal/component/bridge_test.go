package component

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
	repository "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/repository/postgres/receipt"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
	"github.com/emersion/go-sasl"
)

// providerFixture owns loopback SMTP/POP listeners, synthetic MIME bytes and
// fault controls; counters observe fixture effects, not delivery to a real mailbox.
// providerFixture содержит локальные SMTP/POP-серверы, искусственное MIME-письмо
// и переключатели сбоев; счётчики отражают эффекты стенда, а не реальную доставку.
type providerFixture struct {
	oauthAuth                atomic.Int32
	mu                       sync.Mutex
	raw                      string
	uidlLines, listLines     string
	retrievals               atomic.Int32
	sent                     [][]byte
	deletes                  int
	dropSMTP, dropPOP, stall bool
	rejectUpgrade            atomic.Bool
	rejectPOPAuth            atomic.Bool
	rejectSMTPAuth           atomic.Bool
	insecureAuth             atomic.Int32
	cert                     tls.Certificate
	ca                       []byte
	smtp, pop                string
	wg                       sync.WaitGroup
}

// certificate creates a short-lived self-signed P-256 fixture certificate for
// local client/server TLS, including the expected caller URI and loopback names.
// certificate создаёт краткоживущий самоподписанный сертификат P-256 для
// локального клиентского и серверного TLS с ожидаемым URI и локальными именами.
func certificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	uri, _ := url.Parse(httptransport.CallerSPIFFE)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture"}, DNSNames: []string{"mail.example.test", "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	k, _ := x509.MarshalPKCS8PrivateKey(key)
	cert, e := tls.X509KeyPair(ca, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: k}))
	if e != nil {
		t.Fatal(e)
	}
	return cert, ca
}

// newFixture starts ephemeral loopback mail servers with bounded connections;
// implicit mode wraps TLS immediately, and cleanup closes listeners and joins handlers.
// newFixture запускает временные локальные почтовые серверы с ограниченным
// временем соединений; implicit сразу включает TLS, очистка закрывает и ждёт обработчики.
func newFixture(t *testing.T, mode string) *providerFixture {
	t.Helper()
	f := &providerFixture{}
	f.cert, f.ca = certificate(t)
	f.raw = "From: Sender <recipient@example.test>\r\nTo: sender@example.test\r\nCc: copy@example.test\r\nSubject: Fixture search\r\nMessage-ID: <source@example.test>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=fixture\r\n\r\n--fixture\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nOriginal body\r\n--fixture\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=note.txt\r\nContent-Transfer-Encoding: base64\r\n\r\nYXR0YWNobWVudA==\r\n--fixture--\r\n"
	var listeners []net.Listener
	t.Cleanup(func() {
		for _, listener := range listeners {
			listener.Close()
		}
		f.wg.Wait()
	})
	for _, protocol := range []string{"smtp", "pop"} {
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		if protocol == "smtp" {
			f.smtp = listener.Addr().String()
		} else {
			f.pop = listener.Addr().String()
		}
		listeners = append(listeners, listener)
		f.wg.Add(1)
		go func(protocol string) {
			defer f.wg.Done()
			for {
				c, e := listener.Accept()
				if e != nil {
					return
				}
				f.wg.Add(1)
				go func() {
					defer f.wg.Done()
					defer c.Close()
					_ = c.SetDeadline(time.Now().Add(5 * time.Second))
					if mode == "implicit" {
						c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12})
					}
					if protocol == "smtp" {
						f.serveSMTP(c)
					} else {
						f.servePOP(c)
					}
				}()
			}
		}(protocol)
	}
	return f
}

// serveSMTP handles the fixture SMTP subset, records DATA before its acknowledgement
// and can lose or delay that acknowledgement to simulate an uncertain send outcome.
// serveSMTP обслуживает тестовое подмножество SMTP, сохраняет DATA до подтверждения
// и умеет потерять или задержать его для воспроизведения неопределённого исхода отправки.
func (f *providerFixture) serveSMTP(c net.Conn) {
	tp := textproto.NewConn(c)
	_ = tp.PrintfLine("220 fixture ESMTP")
	for {
		line, e := tp.ReadLine()
		if e != nil {
			return
		}
		switch strings.ToUpper(strings.Fields(line)[0]) {
		case "EHLO":
			_ = tp.PrintfLine("250-fixture\r\n250-STARTTLS\r\n250 AUTH PLAIN OAUTHBEARER")
		case "STARTTLS":
			if f.rejectUpgrade.Load() {
				_ = tp.PrintfLine("454 TLS unavailable")
				continue
			}
			_ = tp.PrintfLine("220 Ready")
			c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12})
			tp = textproto.NewConn(c)
		case "AUTH":
			if f.rejectSMTPAuth.Load() {
				_ = tp.PrintfLine("535 fixture-private-text")
				continue
			}
			fields := strings.Fields(line)
			if len(fields) > 1 && fields[1] == "OAUTHBEARER" {
				if len(fields) != 3 {
					_ = tp.PrintfLine("535 Authentication failed")
					continue
				}
				raw, err := base64.StdEncoding.DecodeString(fields[2])
				if err != nil {
					_ = tp.PrintfLine("535 Authentication failed")
					continue
				}
				valid := false
				auth := sasl.NewOAuthBearerServer(func(o sasl.OAuthBearerOptions) *sasl.OAuthBearerError {
					valid = o.Username == "fixture-value" && o.Token == "fixture-value"
					if !valid {
						return &sasl.OAuthBearerError{Status: "invalid_token"}
					}
					return nil
				})
				if _, done, err := auth.Next(raw); err != nil || !done || !valid {
					_ = tp.PrintfLine("535 Authentication failed")
					continue
				}
				f.oauthAuth.Add(1)
			}
			if _, secured := c.(*tls.Conn); !secured {
				f.insecureAuth.Add(1)
			}
			_ = tp.PrintfLine("235 Authenticated")
		case "MAIL", "RCPT", "RSET", "NOOP":
			_ = tp.PrintfLine("250 OK")
		case "DATA":
			_ = tp.PrintfLine("354 Send data")
			raw, e := tp.ReadDotBytes()
			if e != nil {
				return
			}
			f.mu.Lock()
			f.sent = append(f.sent, raw)
			drop, stall := f.dropSMTP, f.stall
			f.mu.Unlock()
			if drop {
				return
			}
			if stall {
				time.Sleep(1500 * time.Millisecond)
				return
			}
			_ = tp.PrintfLine("250 Accepted")
		case "QUIT":
			_ = tp.PrintfLine("221 Bye")
			return
		default:
			_ = tp.PrintfLine("500 Unsupported")
		}
	}
}

// servePOP serves synthetic UIDL/LIST/TOP/RETR data, commits a deletion on QUIT
// and supports TLS/authentication failures or a lost final acknowledgement.
// servePOP выдаёт искусственные UIDL/LIST/TOP/RETR, фиксирует удаление при QUIT
// и воспроизводит ошибки TLS/аутентификации либо потерю итогового подтверждения.
func (f *providerFixture) servePOP(c net.Conn) {
	tp := textproto.NewConn(c)
	_ = tp.PrintfLine("+OK fixture")
	deleted := false
	for {
		line, e := tp.ReadLine()
		if e != nil {
			return
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			return
		}
		f.mu.Lock()
		gone := f.deletes > 0
		raw, uidlLines, listLines := f.raw, f.uidlLines, f.listLines
		f.mu.Unlock()
		switch parts[0] {
		case "STLS":
			if f.rejectUpgrade.Load() {
				_ = tp.PrintfLine("-ERR TLS unavailable")
				continue
			}
			_ = tp.PrintfLine("+OK TLS")
			c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12})
			tp = textproto.NewConn(c)
		case "USER", "PASS", "NOOP":
			if parts[0] == "PASS" && f.rejectPOPAuth.Load() {
				_ = tp.PrintfLine("-ERR fixture-private-text")
				continue
			}
			if parts[0] != "NOOP" {
				if _, secured := c.(*tls.Conn); !secured {
					f.insecureAuth.Add(1)
				}
			}
			_ = tp.PrintfLine("+OK")
		case "UIDL":
			_ = tp.PrintfLine("+OK")
			w := tp.DotWriter()
			if uidlLines != "" {
				fmt.Fprint(w, uidlLines)
			} else if !gone {
				fmt.Fprintln(w, "1 uid-one\r\n2 uid-two")
			}
			w.Close()
		case "LIST":
			_ = tp.PrintfLine("+OK")
			w := tp.DotWriter()
			if listLines != "" {
				fmt.Fprint(w, listLines)
			} else if !gone {
				fmt.Fprintf(w, "1 %d\r\n2 %d\r\n", len(raw), len(raw))
			}
			w.Close()
		case "TOP", "RETR":
			if parts[0] == "RETR" {
				f.retrievals.Add(1)
			}
			_ = tp.PrintfLine("+OK")
			w := tp.DotWriter()
			value := raw
			if parts[0] == "TOP" {
				value = strings.Split(raw, "\r\n\r\n")[0] + "\r\n\r\n"
			}
			io.WriteString(w, value)
			w.Close()
		case "DELE":
			deleted = true
			_ = tp.PrintfLine("+OK")
		case "QUIT":
			f.mu.Lock()
			if deleted {
				f.deletes++
			}
			drop := f.dropPOP
			f.mu.Unlock()
			if drop {
				return
			}
			_ = tp.PrintfLine("+OK")
			return
		default:
			_ = tp.PrintfLine("-ERR")
		}
	}
}

// dialFixture maps configured SMTP ports to the local SMTP listener and other
// targets to the POP listener; it is a test route, not an egress-policy verifier.
// dialFixture направляет настроенные SMTP-порты в локальный SMTP, остальные
// адреса — в POP; это тестовый маршрут, а не проверка сетевой политики.
type dialFixture struct{ smtp, pop string }

// Dial opens the selected loopback TCP endpoint with the caller's cancellation;
// remote target names never cause an external DNS lookup in this fixture.
// Dial открывает выбранный локальный TCP-адрес с отменой вызывающей стороны;
// удалённые имена в этом стенде не приводят к внешнему DNS-запросу.
func (d dialFixture) Dial(ctx context.Context, target string) (net.Conn, error) {
	address := d.pop
	if strings.HasSuffix(target, ":465") || strings.HasSuffix(target, ":587") {
		address = d.smtp
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", address)
}

// secrets counts credential reads and supplies fixture CA or dummy values;
// revocation and a missing descriptor simulate unavailable projected credentials.
// secrets считает чтения реквизитов и выдаёт тестовый CA либо условные значения;
// отзыв и отсутствующий дескриптор имитируют недоступность проецируемых реквизитов.
type secrets struct {
	ca      []byte
	reads   atomic.Int32
	revoked atomic.Bool
}

// Read counts every attempt, refuses revoked/missing data, and returns only
// synthetic values; it does not read the host's secret files or validate generations.
// Read считает каждую попытку, отклоняет отозванные/отсутствующие данные и выдаёт
// только искусственные значения; файлы секретов хоста и поколения здесь не проверяются.
func (s *secrets) Read(_ context.Context, d api.Descriptor) ([]byte, error) {
	s.reads.Add(1)
	if s.revoked.Load() || d.Name == "missing" {
		return nil, errs.Unavailable
	}
	if d.Name == "ca" {
		return s.ca, nil
	}
	return []byte("fixture-value"), nil
}

// authorityFixture supplies mutable synthetic owner decisions for negative tests;
// it does not replace transport authentication or a real control-plane authority.
// authorityFixture выдаёт изменяемые искусственные решения владельца для негативных
// тестов; он не заменяет аутентификацию транспорта и настоящий управляющий сервис.
type authorityFixture struct {
	mutate  func(*api.AuthorizationDecision)
	revoked bool
}

// Resolve binds a short-lived fixture decision to the request's operation, digest,
// effect and execution, then allows selected decision fields to be corrupted by tests.
// Resolve связывает краткоживущее тестовое решение с операцией, хешем, эффектом
// и выполнением запроса, позволяя тестам исказить выбранные поля решения.
func (a *authorityFixture) Resolve(_ context.Context, r api.AuthorizationRequest) (api.AuthorizationDecision, error) {
	if a.revoked {
		return api.AuthorizationDecision{}, errs.Denied
	}
	scope := api.Scope{Folders: []string{"INBOX"}, MailboxId: r.MailboxId, Sender: r.Sender, Operations: []api.Operation{r.Operation}, Recipients: []string{"recipient@example.test", "copy@example.test"}}
	d := api.AuthorizationDecision{Allowed: true, ActorId: "actor", AgentId: "agent", TenantId: "tenant", ConnectionId: "connection", MailboxId: r.MailboxId, Operation: r.Operation, InputSha256: r.InputSha256, EffectKey: r.EffectKey, ConfigurationRevision: r.ConfigurationRevision, CredentialGeneration: 1, GrantId: "grant", ExpiresAt: time.Now().Add(time.Minute).Unix(), Policy: api.Allow, GateApproved: true, UserScope: scope, AgentScope: scope, ConnectionScope: scope, ResourceScope: scope}
	if a.mutate != nil {
		a.mutate(&d)
	}
	d.ExecutionBinding = r.ExecutionBinding
	return d, nil
}

// memory serializes fixture receipts in a tenant/mailbox-keyed map; its no-op
// ledger/report methods cannot demonstrate durable recovery or database fencing.
// memory последовательно хранит тестовые квитанции в карте с ключом арендатора/ящика;
// заглушки журнала/отчётов не доказывают устойчивое восстановление и защиту БД.
type memory struct {
	port.ReconciliationRepository
	mu   sync.Mutex
	rows map[string]port.Record
}

// Remember accepts fixture owner bindings without persisting a reconciliation
// journal; tests requiring durable linkage must use the PostgreSQL repository.
// Remember принимает тестовые привязки владельца без сохранения журнала сверки;
// проверки устойчивой связи должны использовать PostgreSQL-репозиторий.
func (m *memory) Remember(context.Context, port.Scope, port.Record, port.OwnerReceipt, time.Time) error {
	return nil
}

// Reserve holds the fixture mutex while reserving or replaying one scoped key,
// preventing concurrent map mutations from manufacturing duplicate fixture effects.
// Reserve удерживает мьютекс при резервировании или повторном чтении ключа области,
// не позволяя конкурентным изменениям карты создавать повторные тестовые эффекты.
func (m *memory) Reserve(_ context.Context, s port.Scope, key, digest, id, resource string, audit port.Audit) (port.Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reserve(s, key, digest, id, resource, audit)
}

// reserve requires the caller to hold the mutex; it rejects changed input or a
// resource already locked by an unknown outcome and otherwise creates UNKNOWN.
// reserve требует удержания мьютекса; отклоняет изменённый ввод и ресурс, занятый
// неопределённым исходом, а в остальных случаях создаёт квитанцию UNKNOWN.
func (m *memory) reserve(s port.Scope, key, digest, id, resource string, audit port.Audit) (port.Record, bool, error) {
	k := s.Tenant + "/" + s.Mailbox + "/" + key
	if r, ok := m.rows[k]; ok {
		if r.Digest != digest {
			return r, false, errs.Conflict
		}
		return r, false, nil
	}
	for rowKey, previous := range m.rows {
		if resource != "" && previous.Resource == resource && previous.Status == "unknown" && strings.HasPrefix(rowKey, s.Tenant+"/"+s.Mailbox+"/") {
			return port.Record{}, false, errs.Conflict
		}
	}
	r := port.Record{ID: id, Key: key, Digest: digest, Status: "unknown", Resource: resource, Audit: audit}
	m.rows[k] = r
	return r, true, nil
}

// ReserveEffect checks the fixture source and lease expiry before reserving;
// only a newly created receipt receives initial report version one.
// ReserveEffect проверяет тестовый источник и срок аренды перед резервированием;
// начальная версия отчёта один назначается только новой квитанции.
func (m *memory) ReserveEffect(_ context.Context, s port.Scope, record port.Record, source port.ReportSource) (port.Record, bool, error) {
	if !source.Valid() || !source.Binding.Lease.ExpiresAt.After(time.Now()) {
		return port.Record{}, false, errs.Invalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, created, err := m.reserve(s, record.Key, record.Digest, record.ID, record.Resource, record.Audit)
	if created {
		r.ReportVersion = 1
		m.rows[s.Tenant+"/"+s.Mailbox+"/"+r.Key] = r
	}
	return r, created, err
}

// CompleteEffect atomically checks the current fixture report version, advances
// it and stores the supplied status; it does not model a durable pending-report queue.
// CompleteEffect атомарно проверяет текущую тестовую версию отчёта, увеличивает её
// и сохраняет заданный статус; устойчивая очередь отчётов здесь не моделируется.
func (m *memory) CompleteEffect(_ context.Context, s port.Scope, r port.Record, status string, source port.ReportSource) (port.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := s.Tenant + "/" + s.Mailbox + "/" + r.Key
	if !source.Valid() || r.ReportVersion < 1 || m.rows[key].ReportVersion != r.ReportVersion {
		return port.Record{}, errs.Conflict
	}
	r.ReportVersion++
	r.Status = status
	m.rows[key] = r
	return r, nil
}

// PendingReports exposes no background work in the in-memory fixture; report
// recovery assertions belong to the separately configured durable repository tests.
// PendingReports не выдаёт фоновую работу в памяти; восстановление отчётов
// проверяют отдельные тесты с явно настроенным устойчивым репозиторием.
func (*memory) PendingReports(context.Context, int) ([]port.PendingReport, error) { return nil, nil }

// ClaimReport refuses all fixture background claims, leaving delivery to direct
// service calls rather than pretending to implement durable lease ownership.
// ClaimReport отклоняет все фоновые захваты стенда, оставляя передачу прямым
// вызовам сервиса без имитации устойчивого владения арендой.
func (*memory) ClaimReport(context.Context, port.PendingReport, time.Duration) (bool, error) {
	return false, nil
}

// AcknowledgeReport is a no-op interface stub; it neither changes fixture rows
// nor proves acknowledgement persistence across a process restart.
// AcknowledgeReport — пустая заглушка интерфейса: она не меняет строки стенда
// и не доказывает сохранность подтверждения после перезапуска процесса.
func (*memory) AcknowledgeReport(context.Context, port.PendingReport) error { return nil }

// Complete replaces the scoped fixture record with the requested status under
// the mutex; unlike CompleteEffect it does not check or advance report versions.
// Complete под мьютексом заменяет квитанцию области с заданным статусом;
// в отличие от CompleteEffect версии отчётов здесь не проверяются и не увеличиваются.
func (m *memory) Complete(_ context.Context, s port.Scope, r port.Record, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.Status = status
	m.rows[s.Tenant+"/"+s.Mailbox+"/"+r.Key] = r
	return nil
}

// Get searches only the requested tenant/mailbox prefix for either receipt ID
// or effect key; an absent match returns NotFound without crossing fixture scopes.
// Get ищет идентификатор квитанции либо ключ эффекта только внутри заданного
// арендатора/ящика; отсутствие совпадения возвращает NotFound без выхода из области.
func (m *memory) Get(_ context.Context, s port.Scope, id, key string) (port.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.rows {
		if strings.HasPrefix(k, s.Tenant+"/"+s.Mailbox+"/") && (r.ID == id || r.Key == key) {
			return r, nil
		}
	}
	return port.Record{}, errs.NotFound
}

// Configuration accepts fixture snapshots without a rollback watermark; only
// the durable repository tests exercise persistent configuration monotonicity.
// Configuration принимает тестовые снимки без границы отката; устойчивую
// монотонность конфигурации проверяют только тесты настоящего репозитория.
func (m *memory) Configuration(context.Context, api.Configuration, string) error { return nil }

// Ready always reports this in-memory stub available and performs no dependency
// probe; it cannot establish readiness of PostgreSQL or a remote mail provider.
// Ready всегда объявляет заглушку памяти доступной без проверки зависимостей;
// это не доказывает готовность PostgreSQL и удалённого почтового провайдера.
func (m *memory) Ready(context.Context) error { return nil }

// configuration builds a synthetic mailbox with bounded messages, two recipients
// and all operations allowed; TLS mode chooses the fixture SMTP/POP port pair.
// configuration создаёт искусственный ящик с лимитами, двумя получателями
// и разрешёнными операциями; режим TLS выбирает пару тестовых SMTP/POP-портов.
func configuration(mode string) api.Configuration {
	d := func(name string) api.Descriptor { return api.Descriptor{Name: name, Generation: 1} }
	endpoint := api.Endpoint{Host: "mail.example.test", ServerName: "mail.example.test", Port: 465, TlsMode: api.EndpointTlsMode(mode), Ca: d("ca"), Username: d("user"), AuthMethod: "password", Secret: d("password")}
	box := api.Mailbox{Id: "mailbox", TenantId: "tenant", ConnectionId: "connection", Revision: 1, CredentialGeneration: 1, Enabled: true, ReceiveProtocol: "pop3", AllowedFolders: []string{"INBOX"}, ReplyTo: "sender@example.test", Folder: "INBOX", Sender: "sender@example.test", EnvelopeFrom: "sender@example.test", HelloName: "bridge.example.test", Recipients: []string{"recipient@example.test", "copy@example.test"}, Smtp: endpoint, Pop: &endpoint, Limits: api.Limits{TimeoutSeconds: 2, MessageBytes: 65536, AttachmentBytes: 32768, MaxAttachments: 5, MaxRecipients: 10, PageSize: 1, ScanMessages: 100}}
	box.Pop.Port = 995
	if mode == "starttls" {
		box.Smtp.Port = 587
		box.Pop.Port = 110
	}
	for _, op := range api.Operations() {
		box.Policies = append(box.Policies, api.OperationPolicy{Operation: op, Policy: api.Allow})
	}
	return api.Configuration{Version: "email-bridge/v1", Revision: 1, ManagedBy: "git", Source: "fixture", Mailboxes: []api.Mailbox{box}}
}

// receiptConfiguration supplies the same immutable IMAP-enabled configuration
// digest to tests sharing one disposable database, avoiding unrelated revision conflicts.
// receiptConfiguration задаёт одинаковый неизменяемый хеш конфигурации с IMAP
// тестам одной временной БД, исключая посторонние конфликты ревизий.
func receiptConfiguration() api.Configuration {
	c := configuration("implicit")
	c.Mailboxes[0].ReceiveProtocol = "imap"
	endpoint := c.Mailboxes[0].Smtp
	endpoint.Port = 993
	c.Mailboxes[0].Imap = &endpoint
	return c
}

// service wires the real mail service to local transports and synthetic authority;
// an optional repository must also implement report and reconciliation ports.
// service подключает настоящий почтовый сервис к локальным транспортам и тестовым
// полномочиям; переданный репозиторий также обязан поддерживать отчёты и сверку.
func service(t *testing.T, f *providerFixture, mode string, store port.Repository) (*mail.Service, *secrets, *authorityFixture) {
	t.Helper()
	sec := &secrets{ca: f.ca}
	auth := &authorityFixture{}
	if store == nil {
		store = &memory{rows: map[string]port.Record{}}
	}
	s := &mail.Service{Reports: store.(port.ReportRepository), Ledger: store.(port.ReconciliationRepository), CompletionBase: t.Context(), Config: configuration(mode), Authority: auth, Effects: effectFixture{}, Provider: &mailtransport.Provider{Secrets: sec, Dialer: dialFixture{f.smtp, f.pop}}, Receipts: store}
	return s, sec, auth
}

// executionContext preserves an existing execution binding or installs the
// synthetic fixture binding; this helper does not authenticate a real caller.
// executionContext сохраняет существующую привязку выполнения либо добавляет
// тестовую; этот помощник не аутентифицирует настоящего вызывающего клиента.
func executionContext(ctx context.Context) context.Context {
	if api.ExecutionFromContext(ctx) != nil {
		return ctx
	}
	return api.WithExecutionBinding(ctx, executionFixture())
}

// send creates a deterministic synthetic command with one text attachment;
// reply/forward tests add their source identity before invoking the service.
// send создаёт определённую искусственную команду с текстовым вложением;
// тесты ответа/пересылки добавляют идентификатор источника перед вызовом сервиса.
func send(op api.Operation, key string) api.Command {
	return api.Command{Operation: op, MailboxId: "mailbox", EffectKey: key, Message: api.MessageInput{From: "sender@example.test", To: "recipient@example.test", Subject: "Fixture", BodyText: "Message body", Attachments: []api.Attachment{{Filename: "hello.txt", ContentType: "text/plain", ContentBase64: base64.StdEncoding.EncodeToString([]byte("hello"))}}}}
}

// execute invokes the service with fixture caller credentials and an execution
// binding, failing the current test on error while leaving status assertions to callers.
// execute вызывает сервис с тестовыми реквизитами и привязкой выполнения;
// ошибка прерывает тест, а проверку статуса результата выполняет вызывающий код.
func execute(t *testing.T, s *mail.Service, c api.Command) api.Result {
	t.Helper()
	r, e := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "fixture-token", c)
	if e != nil {
		t.Fatalf("operation %s: %v", c.Operation, e)
	}
	return r
}

// TestProtocolOperations exercises synthetic SMTP/POP reads, MIME and mutation
// replay over implicit TLS and STARTTLS, including stale cursors and unsupported flags.
// TestProtocolOperations проверяет тестовые SMTP/POP-чтения, MIME и повторы изменений
// через implicit TLS и STARTTLS, включая устаревшие курсоры и неподдерживаемые флаги.
func TestProtocolOperations(t *testing.T) {
	for _, mode := range []string{"implicit", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, mode)
			s, _, _ := service(t, f, mode, nil)
			if e := api.ValidateConfiguration(s.Config); e != nil {
				t.Fatal(e)
			}
			if execute(t, s, api.Command{Operation: api.OperationHealth, MailboxId: "mailbox"}).Status != "ready" {
				t.Fatal("not ready")
			}
			if len(execute(t, s, api.Command{Operation: api.OperationMailboxes, MailboxId: "mailbox"}).Mailboxes) != 1 {
				t.Fatal("discovery")
			}
			list := execute(t, s, api.Command{Operation: api.OperationList, MailboxId: "mailbox"})
			if len(list.Headers) != 1 || list.NextCursor == "" {
				t.Fatal("pagination")
			}
			page := execute(t, s, api.Command{Operation: api.OperationList, MailboxId: "mailbox", Cursor: list.NextCursor})
			if len(page.Headers) != 1 || page.NextCursor != "" {
				t.Fatal("last page")
			}
			if len(execute(t, s, api.Command{Operation: api.OperationSearch, MailboxId: "mailbox", Query: "search"}).Headers) != 1 {
				t.Fatal("search")
			}
			fetched := execute(t, s, api.Command{Operation: api.OperationFetch, MailboxId: "mailbox", Uid: "uid-one"})
			if !strings.Contains(fetched.BodyText, "Original body") || len(fetched.Attachments) != 1 {
				t.Fatal("fetch MIME")
			}
			if len(execute(t, s, api.Command{Operation: api.OperationDownload, MailboxId: "mailbox", Uid: "uid-one"}).Attachments) != 1 {
				t.Fatal("download")
			}
			if _, e := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "token", api.Command{Operation: api.OperationMark, MailboxId: "mailbox"}); !errors.Is(e, errs.Unsupported) {
				t.Fatal("POP flags invented")
			}
			for _, op := range []api.Operation{api.OperationSend, api.OperationReply, api.OperationReplyAll, api.OperationForward} {
				cmd := send(op, string(op))
				if op != api.OperationSend {
					cmd.Message.SourceUid = "uid-one"
				}
				if op == api.OperationReplyAll {
					cmd.Message.Cc = []string{"copy@example.test"}
				}
				result := execute(t, s, cmd)
				if result.Status != "accepted" {
					t.Fatalf("%s: %s", op, result.Status)
				}
				duplicate := execute(t, s, cmd)
				if duplicate.MessageId != result.MessageId {
					t.Fatal("receipt differs")
				}
				receipt := execute(t, s, api.Command{Operation: api.OperationReceipt, MailboxId: "mailbox", EffectKey: cmd.EffectKey})
				if receipt.Status != "accepted" {
					t.Fatal("receipt read")
				}
			}
			f.mu.Lock()
			if len(f.sent) != 4 {
				t.Errorf("effects=%d", len(f.sent))
			}
			if !bytes.Contains(f.sent[1], []byte("In-Reply-To:")) || !bytes.Contains(f.sent[3], []byte("forwarded.eml")) {
				t.Error("reply/forward MIME")
			}
			f.mu.Unlock()
			if execute(t, s, api.Command{Operation: api.OperationDelete, MailboxId: "mailbox", Uid: "uid-one", EffectKey: "delete"}).Status != "deleted" {
				t.Fatal("delete")
			}
			if _, e := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "token", api.Command{Operation: api.OperationList, MailboxId: "mailbox", Cursor: list.NextCursor}); !errors.Is(e, errs.Conflict) {
				t.Fatal("stale cursor")
			}
		})
	}
}

// TestAuthorityBeforeCredentials corrupts decision bindings/scopes and requires
// refusal before credential reads; revoked fixture credentials must cause no SMTP effect.
// TestAuthorityBeforeCredentials искажает привязки/области решения и требует отказа
// до чтения реквизитов; отозванные тестовые реквизиты не должны дать SMTP-эффект.
func TestAuthorityBeforeCredentials(t *testing.T) {
	f := newFixture(t, "implicit")
	for _, name := range []string{"tenant", "user", "agent", "connection", "resource", "gate", "revocation", "generation", "digest", "expiry", "deny"} {
		t.Run(name, func(t *testing.T) {
			s, sec, a := service(t, f, "implicit", nil)
			a.mutate = func(d *api.AuthorizationDecision) {
				switch name {
				case "tenant":
					d.TenantId = "foreign"
				case "user":
					d.UserScope.Operations = nil
				case "agent":
					d.AgentScope.Recipients = nil
				case "connection":
					d.ConnectionScope.MailboxId = "foreign"
				case "resource":
					d.ResourceScope.Recipients = nil
				case "gate":
					d.Policy = api.HumanGate
					d.GateApproved = false
				case "generation":
					d.CredentialGeneration = 2
				case "digest":
					d.InputSha256 = "wrong"
				case "expiry":
					d.ExpiresAt = 1
				case "deny":
					d.Policy = api.Deny
				}
			}
			a.revoked = name == "revocation"
			_, e := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "token", send(api.OperationSend, name))
			if e == nil || sec.reads.Load() != 0 {
				t.Fatalf("denial before projection: err=%v reads=%d", e, sec.reads.Load())
			}
		})
	}
	s, sec, _ := service(t, f, "implicit", nil)
	cmd := send(api.OperationSend, "scope")
	cmd.Message.To = "foreign@example.test"
	if _, e := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "token", cmd); e == nil || sec.reads.Load() != 0 {
		t.Fatal("recipient scope")
	}
	sec.revoked.Store(true)
	result := execute(t, s, send(api.OperationSend, "credential-revoked"))
	if result.Status != "unknown" {
		t.Fatal("revoked credential effect")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) != 0 {
		t.Fatal("unexpected provider effects")
	}
}

// TestUnknownTimeoutTLSAndDuplicate keeps lost replies, timeouts and TLS failures
// UNKNOWN across service replacement, rejects changed input and prevents repeated effects.
// TestUnknownTimeoutTLSAndDuplicate сохраняет UNKNOWN при потере ответа, тайм-ауте
// и ошибке TLS после замены сервиса, отклоняет изменённый ввод и повторные эффекты.
func TestUnknownTimeoutTLSAndDuplicate(t *testing.T) {
	for _, mode := range []string{"drop", "timeout", "hostname", "ca", "pop-drop"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, "implicit")
			s, sec, _ := service(t, f, "implicit", nil)
			f.dropSMTP = mode == "drop"
			f.stall = mode == "timeout"
			f.dropPOP = mode == "pop-drop"
			s.Config.Mailboxes[0].Limits.TimeoutSeconds = 1
			if mode == "hostname" {
				s.Config.Mailboxes[0].Smtp.ServerName = "foreign.example.test"
			}
			if mode == "ca" {
				_, sec.ca = certificate(t)
			}
			cmd := send(api.OperationSend, mode)
			if mode == "pop-drop" {
				cmd = api.Command{Operation: api.OperationDelete, MailboxId: "mailbox", Uid: "uid-one", EffectKey: mode}
			}
			first := execute(t, s, cmd)
			if first.Status != "unknown" {
				t.Fatalf("state=%s", first.Status)
			}
			replacement, _, _ := service(t, f, "implicit", s.Receipts)
			replacement.Config = s.Config
			if execute(t, replacement, cmd).MessageId != first.MessageId {
				t.Fatal("restart replay")
			}
			cmd.Message.Subject = "changed"
			if _, e := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "token", cmd); !errors.Is(e, errs.Conflict) {
				t.Fatal("input mismatch")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.sent) > 1 || f.deletes > 1 {
				t.Fatal("repeated effect")
			}
		})
	}
}

// TestHTTPSBoundary checks a local mTLS HTTP mutation with execution headers and
// a typed accepted receipt, then verifies that a request without a bearer is denied.
// TestHTTPSBoundary проверяет локальное изменение по mTLS HTTP с заголовками выполнения
// и типизированной квитанцией, затем отказ запросу без bearer-токена.
func TestHTTPSBoundary(t *testing.T) {
	f := newFixture(t, "implicit")
	s, _, _ := service(t, f, "implicit", nil)
	server, client := httpsFixture(t, f, httptransport.Handler{Service: s})
	raw, _ := json.Marshal(send(api.OperationSend, "https").Message)
	request, _ := http.NewRequestWithContext(t.Context(), "POST", server.URL+"/v1/messages?sender=sender%40example.test", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	binding := executionFixture()
	header, _ := api.ExecutionHeaderValue(binding)
	request.Header.Set("Authorization", "Bearer "+binding.Lease.Fence)
	request.Header.Set(api.ExecutionHeader, header)
	request.Header.Set("Idempotency-Key", "https")
	response, e := client.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 202 {
		t.Fatalf("status=%d", response.StatusCode)
	}
	var status api.MessageStatus
	if json.NewDecoder(response.Body).Decode(&status) != nil || status.Status != api.Accepted {
		t.Fatal("typed legacy receipt")
	}
	request, _ = http.NewRequestWithContext(t.Context(), "GET", server.URL+"/v1/health?sender=sender%40example.test", nil)
	response, e = client.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("missing bearer accepted")
	}
}

// TestPostgresEffects requires the disposable database fixture to verify concurrent
// deduplication, durable UNKNOWN/resource locks, scoped reads and configuration rollback refusal.
// TestPostgresEffects требует временную БД для проверки конкурентной дедупликации,
// сохранности UNKNOWN/блокировок, чтений в области и запрета отката конфигурации.
func TestPostgresEffects(t *testing.T) {
	store := postgresFixture(t)
	pool := store.Pool
	var e error
	f := newFixture(t, "implicit")
	s, _, _ := service(t, f, "implicit", store)
	s.Config = receiptConfiguration()
	cmd := send(api.OperationSend, "concurrent")
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "token", cmd)
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	f.mu.Lock()
	if len(f.sent) != 1 {
		t.Errorf("SMTP effects=%d", len(f.sent))
	}
	f.dropSMTP = true
	f.mu.Unlock()
	r := execute(t, s, send(api.OperationSend, "crash"))
	if r.Status != "unknown" {
		t.Fatal("unknown")
	}
	replacement, _, _ := service(t, f, "implicit", &repository.Repository{Pool: pool})
	replacement.Config = receiptConfiguration()
	if execute(t, replacement, send(api.OperationSend, "crash")).MessageId != r.MessageId {
		t.Fatal("durable unknown")
	}
	if _, e = store.Get(t.Context(), port.Scope{Tenant: "foreign", Mailbox: "mailbox"}, r.MessageId, ""); !errors.Is(e, errs.NotFound) {
		t.Fatal("cross tenant receipt")
	}
	scope := port.Scope{Tenant: "tenant", Mailbox: "mailbox"}
	audit := port.Audit{Actor: "actor", Agent: "agent", Grant: "grant", Operation: api.OperationDraftUpdate, ConfigurationRevision: 1, CredentialGeneration: 1, GateApproved: true}
	reserved, created, e := store.Reserve(t.Context(), scope, "imap-receipt", strings.Repeat("a", 64), "imap-receipt-id", strings.Repeat("b", 64), audit)
	if e != nil || !created {
		t.Fatal("IMAP resource reservation")
	}
	if _, _, e = store.Reserve(t.Context(), scope, "imap-other", strings.Repeat("c", 64), "imap-other-id", strings.Repeat("b", 64), audit); !errors.Is(e, errs.Conflict) {
		t.Fatal("unknown source admitted another effect")
	}
	reserved.UID = "7"
	reserved.UIDValidity = 4294967295
	reserved.Folder = "Drafts"
	reserved.ContentDigest = strings.Repeat("d", 64)
	if e = store.Complete(t.Context(), scope, reserved, "unknown"); e != nil {
		t.Fatal("partial metadata persistence")
	}
	persisted, e := (&repository.Repository{Pool: pool}).Get(t.Context(), scope, reserved.ID, "")
	if e != nil || persisted.UID != reserved.UID || persisted.UIDValidity != reserved.UIDValidity || persisted.Folder != reserved.Folder || persisted.ContentDigest != reserved.ContentDigest || persisted.Status != "unknown" || persisted.Audit != audit {
		t.Fatal("durable IMAP partial metadata")
	}
	if e = store.Complete(t.Context(), scope, persisted, "accepted"); e != nil {
		t.Fatal("terminal metadata persistence")
	}
	if _, created, e = store.Reserve(t.Context(), scope, "imap-other", strings.Repeat("c", 64), "imap-other-id", strings.Repeat("b", 64), audit); e != nil || !created {
		t.Fatal("known outcome did not release resource")
	}
	c := s.Config
	c.Revision = 2
	if e = store.Configuration(t.Context(), c, api.Digest(c)); e != nil {
		t.Fatal(e)
	}
	if e = store.Configuration(t.Context(), s.Config, api.Digest(s.Config)); e == nil {
		t.Fatal("rollback accepted")
	}
}

// TestProjectedCredentialRevocation uses temporary dummy files to ensure removal
// is observed on the next read and traversal-like descriptors cannot escape the root.
// TestProjectedCredentialRevocation использует временные условные файлы, проверяя
// отзыв при следующем чтении и запрет выхода дескрипторов за корневой каталог.
func TestProjectedCredentialRevocation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "credential", "1")
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte("fixture"), 0400); e != nil {
		t.Fatal(e)
	}
	files := mailtransport.Files{Root: root}
	d := api.Descriptor{Name: "credential", Generation: 1}
	if _, e := files.Read(t.Context(), d); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if _, e := files.Read(t.Context(), d); e == nil {
		t.Fatal("removed credential remained available")
	}
	d.Name = "../escape"
	if _, e := files.Read(t.Context(), d); e == nil {
		t.Fatal("descriptor traversal")
	}
}

// TestCONNECTTransport tunnels local SMTP STARTTLS and POP STLS through a bounded
// fixture proxy with policy readback, and requires refusal for an unlisted destination.
// TestCONNECTTransport передаёт локальные SMTP STARTTLS и POP STLS через ограниченный
// тестовый прокси с данными политики и требует отказа для неразрешённого адреса.
func TestCONNECTTransport(t *testing.T) {
	f := newFixture(t, "starttls")
	s, _, _ := service(t, f, "starttls", nil)
	policyDigest := strings.Repeat("a", 64)
	readback := fmt.Sprintf("X-Kodex-Egress-Revision: mail-%d\r\nX-Kodex-Egress-Digest: %s\r\nX-Kodex-Egress-Profile: email-mail\r\nX-Kodex-Egress-Workload: email-bridge\r\nX-Kodex-Egress-Operation: email.transport\r\nX-Kodex-Egress-Configuration-Revision: %d\r\nX-Kodex-Egress-Configuration-Digest: %s\r\n", s.Config.Revision, policyDigest, s.Config.Revision, api.Digest(s.Config))
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() { listener.Close(); wg.Wait() })
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			connection, e := listener.Accept()
			if e != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(connection)
				request, e := http.ReadRequest(reader)
				if e != nil || request.Method != "CONNECT" {
					return
				}
				address := f.pop
				if request.Host == "mail.example.test:587" {
					address = f.smtp
				} else if request.Host != "mail.example.test:110" {
					io.WriteString(connection, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
					return
				}
				upstream, e := net.DialTimeout("tcp", address, time.Second)
				if e != nil {
					return
				}
				defer upstream.Close()
				io.WriteString(connection, "HTTP/1.1 200 Connection Established\r\n"+readback+"\r\n")
				done := make(chan struct{})
				go func() { io.Copy(upstream, reader); upstream.Close(); close(done) }()
				io.Copy(connection, upstream)
				connection.Close()
				<-done
			}()
		}
	}()
	tunnel := mailtransport.Tunnel{Address: listener.Addr().String(), PolicyDigest: policyDigest, ConfigurationRevision: s.Config.Revision, ConfigurationDigest: api.Digest(s.Config)}
	s.Provider = &mailtransport.Provider{Secrets: &secrets{ca: f.ca}, Dialer: tunnel}
	if execute(t, s, send(api.OperationSend, "connect")).Status != "accepted" {
		t.Fatal("STARTTLS through CONNECT")
	}
	if execute(t, s, api.Command{Operation: api.OperationFetch, MailboxId: "mailbox", Uid: "uid-one"}).Status != "ok" {
		t.Fatal("POP STLS through CONNECT")
	}
	if c, e := tunnel.Dial(t.Context(), "foreign.example.test:995"); e == nil {
		c.Close()
		t.Fatal("proxy denial bypassed")
	}
}
