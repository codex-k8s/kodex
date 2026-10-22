package providercredential

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

const (
	// maximumAppServerLineBytes bounds one scanned JSON-RPC line and the accepted diagnostic stream size to
	// the one-MiB budget; raw diagnostics are discarded rather than logged.
	// maximumAppServerLineBytes задаёт бюджет один МиБ для сканируемой JSON-RPC строки и принимаемого
	// diagnostic stream; сырая диагностика отбрасывается, а не логируется.
	maximumAppServerLineBytes = 1 << 20
	// maximumAppServerMessages caps one process response stream at ten thousand messages, counting
	// notifications as well as replies.
	// maximumAppServerMessages ограничивает response stream одного процесса десятью тысячами сообщений,
	// учитывая notifications наряду с replies.
	maximumAppServerMessages = 10_000
	// maximumAppServerRequest bounds marshaled outbound JSON before newline framing; an oversized request
	// fails before writing to the child stdin.
	// maximumAppServerRequest ограничивает сериализованный исходящий JSON до добавления newline; слишком
	// большой запрос отклоняется до записи в child stdin.
	maximumAppServerRequest = 1 << 20
	// maximumAuthJSONBytes limits private auth-file and catalog-auth material to one MiB, independently of
	// individual token-field validation.
	// maximumAuthJSONBytes ограничивает приватный auth-файл и catalog-auth material одним МиБ независимо от
	// проверки отдельных token полей.
	maximumAuthJSONBytes = 1 << 20
	// processShutdownTimeout supplies the five-second default for process exit/escalation and separate
	// stream-drain phases; it is not one overall deadline for every final join.
	// processShutdownTimeout задаёт default пять секунд для process exit/escalation и отдельной stream-drain
	// фазы; это не единый общий дедлайн всех итоговых join.
	processShutdownTimeout = 5 * time.Second
	// fileCredentialStoreConfig explicitly selects the CLI file credential store so isolated managed
	// material is available as auth.json instead of a host OS keyring.
	// fileCredentialStoreConfig явно выбирает CLI file credential store, сохраняя изолированный managed
	// material в auth.json вместо host OS keyring.
	fileCredentialStoreConfig = `cli_auth_credentials_store="file"`
)

// errCredentialMaterialization distinguishes failures after device login when account metadata or
// private credential bytes could not be materialized; wrapped private causes are not copied into public
// service failure codes.
// errCredentialMaterialization отличает отказы после device login при невозможности materialize account
// metadata или приватные credential байты; обёрнутые private причины не копируются в публичные service
// failure codes.
var errCredentialMaterialization = errors.New("Codex credential materialization failed")

// DeviceAuthorizationSession owns one local device login, exposes bounded verification metadata and
// yields private credential bytes only after completion; its caller must always close the session and
// clear returned bytes.
// DeviceAuthorizationSession владеет одним local device login, выдаёт ограниченные verification metadata
// и приватные credential байты только после завершения; вызывающий обязан закрыть сессию и обнулить
// возвращённые байты.
type DeviceAuthorizationSession interface {
	// MaterializerAttemptRef returns the stable server-derived reference used to correlate this local login
	// with durable attempt metadata.
	// MaterializerAttemptRef возвращает стабильную server-derived ссылку для сопоставления local login с
	// durable attempt metadata.
	MaterializerAttemptRef() string
	// VerificationURI returns the provider's validated verification metadata for the user to authorize this
	// session, not a general network-fetch capability.
	// VerificationURI возвращает проверенные verification metadata provider для авторизации этой сессии
	// пользователем, а не право произвольного сетевого чтения.
	VerificationURI() string
	// UserCode returns the short verification code for this pending login; it is not an OAuth access token
	// or stored credential JSON.
	// UserCode возвращает короткий verification код pending login; это не OAuth access token и не
	// сохранённый credential JSON.
	UserCode() string
	// Wait observes completion under the supplied context and returns private auth bytes plus a masked
	// account label; callers publish/clear those bytes and classify errors without exposing provider
	// payload.
	// Wait наблюдает завершение под переданным context и возвращает приватные auth байты с masked account
	// меткой; вызывающий публикует/обнуляет байты и классифицирует ошибки без раскрытия provider payload.
	Wait(context.Context) ([]byte, string, error)
	// Close joins session-owned process/readers and removes private state; implementations preserve the
	// first cleanup outcome for idempotent replay.
	// Close завершает принадлежащие сессии process/readers и удаляет приватное состояние; реализации
	// сохраняют первый cleanup исход для идемпотентного повтора.
	Close() error
}

// AppServer separates runtime readiness, starting device login and observing model capabilities; owner
// authorization and durable effect reservation are supplied by higher-level services.
// AppServer разделяет runtime readiness, начало device login и наблюдение model capabilities; owner
// авторизацию и durable резервирование эффекта обеспечивают вышележащие сервисы.
type AppServer interface {
	// Check verifies the configured executable/state root and pinned CLI version using the caller's budget
	// without starting an authorization session.
	// Check проверяет настроенные executable/state root и закреплённую CLI версию с бюджетом caller без
	// запуска authorization сессии.
	Check(context.Context) error
	// StartDeviceAuthorization creates one isolated local login for server-derived attempt/home identifiers
	// and returns a session whose lifecycle must be owned and closed by the caller.
	// StartDeviceAuthorization создаёт один изолированный local login по server-derived attempt/home
	// идентификаторам и возвращает сессию, чей lifecycle и закрытие принадлежат вызывающему.
	StartDeviceAuthorization(context.Context, string, string) (DeviceAuthorizationSession, error)
	// ObserveModelCatalog consumes a private auth copy for the selected API/device route and returns only
	// safe capability metadata or a classified failure, not raw credentials.
	// ObserveModelCatalog использует приватную auth копию для выбранного API/device маршрута и возвращает
	// только безопасные capability metadata либо классифицированный отказ без сырых credentials.
	ObserveModelCatalog(context.Context, []byte, string) (ModelCatalog, error)
}

// AppServerProcess binds the configured executable and private state root to the managed catalog HTTP
// client and explicit protocol-only fallback setting; it stores configuration, not account credentials.
// AppServerProcess связывает настроенные executable и private state root с управляемым catalog HTTP
// клиентом и явным protocol-only fallback режимом; хранится конфигурация, не account credentials.
type AppServerProcess struct {
	binary                       string
	root                         string
	allowProtocolCatalogFallback bool
	catalogHTTP                  modelCatalogHTTPClient
}

// NewAppServerProcess requires absolute clean executable/root paths, creates the root when missing and
// configures the fixed catalog client. Existing root safety and executable/version readiness are checked
// later by Check.
// NewAppServerProcess требует абсолютные clean executable/root пути, создаёт отсутствующий root и
// настраивает фиксированный catalog клиент. Безопасность существующего root и executable/version
// readiness позже проверяет Check.
func NewAppServerProcess(binary, root string, allowProtocolCatalogFallback bool) (*AppServerProcess, error) {
	if !filepath.IsAbs(binary) || !filepath.IsAbs(root) || filepath.Clean(binary) != binary || filepath.Clean(root) != root {
		return nil, errors.New("Codex app-server configuration is invalid")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, errors.New("create Codex app-server state root")
	}
	return &AppServerProcess{
		binary: binary, root: root,
		allowProtocolCatalogFallback: allowProtocolCatalogFallback,
		catalogHTTP:                  newModelCatalogHTTPClient(),
	}, nil
}

// Check requires a regular executable with an execute bit and a directory root without group/other
// permissions, then verifies the exact CLI version. Stat follows existing path links; this is not an
// executable signature or path-ownership verifier.
// Check требует regular executable с execute bit и directory root без group/other прав, затем сверяет
// точную CLI версию. Stat следует существующим ссылкам пути; подпись executable и владение путями не
// проверяются.
func (process *AppServerProcess) Check(ctx context.Context) error {
	info, err := os.Stat(process.binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return errors.New("Codex app-server executable is unavailable")
	}
	info, err = os.Stat(process.root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("Codex app-server state root is unavailable")
	}
	return process.checkCatalogRuntime(ctx)
}

// StartDeviceAuthorization accepts server-derived nonempty attempt/home identifiers, rejects home
// separators and checks the resulting private directory before initializing a strict file-store
// app-server and device-code login. Failed initialization closes the process and removes its home;
// higher layers must supply safe home components and fresh authority.
// StartDeviceAuthorization принимает server-derived непустые attempt/home идентификаторы, отклоняет home
// разделители и проверяет полученный private directory до инициализации strict file-store app-server и
// device-code login. При ошибке инициализации процесс закрывается, home удаляется; безопасные home
// компоненты и свежие полномочия задаются выше.
func (process *AppServerProcess) StartDeviceAuthorization(
	ctx context.Context,
	materializerAttemptRef, homeName string,
) (DeviceAuthorizationSession, error) {
	if ctx == nil || materializerAttemptRef == "" || homeName == "" || strings.ContainsAny(homeName, `/\\`) {
		return nil, errors.New("device authorization process input is invalid")
	}
	home := filepath.Join(process.root, homeName)
	if err := os.Mkdir(home, 0o700); err != nil && !os.IsExist(err) {
		return nil, errors.New("create Codex app-server state directory")
	}
	if info, err := os.Lstat(home); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("Codex app-server state directory is unsafe")
	}
	server, err := startAppServer(process.binary, home)
	if err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	initialized := false
	defer func() {
		if !initialized {
			_ = server.terminate()
			_ = os.RemoveAll(home)
		}
	}()
	initialize := map[string]any{
		"clientInfo": map[string]string{"name": "kodex-secret-broker", "title": "Kodex secret-broker", "version": "1"},
		"capabilities": map[string]any{
			"experimentalApi":           false,
			"optOutNotificationMethods": []string{"thread/started", "turn/started", "turn/completed"},
		},
	}
	if _, err := server.call(ctx, "initialize", initialize); err != nil {
		return nil, err
	}
	if err := server.write(map[string]any{"method": "initialized"}); err != nil {
		return nil, err
	}
	raw, err := server.call(ctx, "account/login/start", map[string]string{"type": "chatgptDeviceCode"})
	if err != nil {
		return nil, err
	}
	var response struct {
		Type            string `json:"type"`
		LoginID         string `json:"loginId"`
		UserCode        string `json:"userCode"`
		VerificationURL string `json:"verificationUrl"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Type != "chatgptDeviceCode" || response.LoginID == "" ||
		len(response.LoginID) > 256 || response.UserCode == "" || len(response.UserCode) > 128 ||
		response.VerificationURL == "" || len(response.VerificationURL) > 2000 ||
		!strings.HasPrefix(response.VerificationURL, "https://") {
		return nil, errors.New("Codex app-server returned invalid device authorization metadata")
	}
	initialized = true
	return &deviceSession{
		server: server, home: home, materializerAttemptRef: materializerAttemptRef,
		loginID: response.LoginID, verificationURI: response.VerificationURL, userCode: response.UserCode,
	}, nil
}

// wireMessage decodes only JSON-RPC correlation, method, params, result and error fields; higher-level
// dispatch validates permissible request/notification combinations before using payload.
// wireMessage разбирает только JSON-RPC correlation, method, params, result и error поля; вышележащий
// dispatch проверяет допустимые сочетания request/notification до использования payload.
type wireMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// streamEvent carries either one decoded protocol message or a sanitized stream error through the
// bounded reader queue, not diagnostic stderr bytes.
// streamEvent передаёт либо одно разобранное protocol сообщение, либо очищенную stream ошибку через
// ограниченную reader очередь без diagnostic stderr байтов.
type streamEvent struct {
	message wireMessage
	err     error
}

// appServer owns the child command, explicit parent stdio ends, bounded message/diagnostic readers and
// once-only cleanup state. Calls share sequential IDs and one response channel; this is not a concurrent
// RPC multiplexer.
// appServer владеет child command, явными parent stdio ends, ограниченными message/diagnostic readers и
// однократным cleanup состоянием. Calls используют последовательные ID и общий response channel;
// concurrent RPC multiplexer не реализуется.
type appServer struct {
	command         *exec.Cmd
	stdin           io.WriteCloser
	messages        <-chan streamEvent
	wait            <-chan error
	diagnostics     <-chan error
	nextID          int64
	closeOnce       sync.Once
	closeErr        error
	readerStop      chan struct{}
	readerDone      chan struct{}
	stdout          io.ReadCloser
	stderr          io.ReadCloser
	shutdownTimeout time.Duration
}

// startAppServer configures strict stdio JSON-RPC, isolated HOME/CODEX_HOME, explicit file credentials
// and the fixed managed proxy with no inherited account environment. It starts a separate process group
// with a parent-death signal through the owned-pipe constructor.
// startAppServer настраивает strict stdio JSON-RPC, изолированные HOME/CODEX_HOME, явные file
// credentials и фиксированный managed proxy без наследования account environment. Отдельная process
// group с parent-death signal запускается через owned-pipe конструктор.
func startAppServer(binary, home string) (*appServer, error) {
	// Materialization owns an isolated CODEX_HOME and must receive the managed
	// credential as a file. The default "auto" store may select an OS keyring,
	// which cannot be copied into the immutable runtime Secret.
	// Materialization владеет изолированным CODEX_HOME и получает managed credential как файл.
	// Default "auto" store может выбрать OS keyring, который нельзя скопировать в immutable runtime Secret.
	command := exec.Command(binary, "app-server", "-c", fileCredentialStoreConfig, "--strict-config", "--listen", "stdio://")
	command.WaitDelay = processShutdownTimeout
	command.Dir = home
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home, "CODEX_HOME=" + home}
	command.Env = append(command.Env, "HTTP_PROXY="+providerEgressProxyURL, "HTTPS_PROXY="+providerEgressProxyURL)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	return startAppServerCommand(command)
}

// startAppServerCommand starts the supplied command with production diagnostic draining and owned stdio;
// tests may inject a synthetic command without changing message or shutdown logic.
// startAppServerCommand запускает переданную command с production diagnostic draining и owned stdio;
// тесты могут подставить synthetic command без изменения message и shutdown логики.
func startAppServerCommand(command *exec.Cmd) (*appServer, error) {
	return startAppServerWithDiagnosticReader(command, readAppServerDiagnostics)
}

// startAppServerWithDiagnosticReader allocates explicit caller-owned stdio through the shared
// constructor but retains its own diagnostic reader, message queue, Wait and shutdown budget.
// Deferred child-end closure and parent-reader lifetime remain independent of Cmd.Wait.
// startAppServerWithDiagnosticReader выделяет явные stdio через общий конструктор, сохраняя свои
// diagnostic reader, очередь, Wait и shutdown budget. Отложенное закрытие child ends и срок
// parent readers остаются независимыми от Cmd.Wait.
func startAppServerWithDiagnosticReader(command *exec.Cmd, readDiagnostics func(io.Reader) error) (*appServer, error) {
	pipes, err := serviceruntime.NewCommandPipes(command, "Codex app-server")
	if err != nil {
		return nil, err
	}
	stdinChild, stdin := pipes.StdinChild, pipes.Stdin
	stdout, stdoutChild := pipes.Stdout, pipes.StdoutChild
	stderr, stderrChild := pipes.Stderr, pipes.StderrChild
	defer stdinChild.Close()
	// Родитель владеет read ends: Cmd.Wait не должен закрывать их под readers.
	defer stdoutChild.Close()
	defer stderrChild.Close()
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, errors.New("start Codex app-server process")
	}
	messages := make(chan streamEvent, 64)
	readerStop, readerDone := make(chan struct{}), make(chan struct{})
	go readAppServerMessages(stdout, messages, readerStop, readerDone)
	diagnostics := make(chan error, 1)
	go func() {
		diagnostics <- readDiagnostics(stderr)
	}()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	return &appServer{command: command, stdin: stdin, messages: messages, wait: wait, diagnostics: diagnostics, readerStop: readerStop, readerDone: readerDone, stdout: stdout, stderr: stderr, shutdownTimeout: processShutdownTimeout}, nil
}

// readAppServerDiagnostics discards up to the diagnostic byte budget plus one sentinel byte and returns
// a fixed error on overflow/read failure; raw stderr content is never returned.
// readAppServerDiagnostics отбрасывает diagnostic байты до бюджета плюс один контрольный байт и
// возвращает фиксированную ошибку при переполнении/ошибке чтения; сырой stderr не возвращается.
func readAppServerDiagnostics(reader io.Reader) error {
	written, err := io.Copy(io.Discard, io.LimitReader(reader, maximumAppServerLineBytes+1))
	if err != nil || written > maximumAppServerLineBytes {
		return errors.New("Codex app-server diagnostic stream exceeded its bound")
	}
	return nil
}

// readAppServerMessages bounds line/count budgets, validates basic JSON-RPC field combinations and
// delivers decoded events until EOF, failure or stop-aware queue cancellation. It closes events/done;
// ordinary JSON decoding does not reject unknown fields or duplicate keys.
// readAppServerMessages ограничивает line/count бюджеты, проверяет базовые JSON-RPC сочетания полей и
// передаёт decoded events до EOF, отказа или stop-aware отмены очереди. Events/done закрываются; обычный
// JSON decoder не запрещает неизвестные поля и duplicate keys.
func readAppServerMessages(reader io.Reader, events chan<- streamEvent, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	defer close(events)
	send := func(event streamEvent) bool {
		select {
		case events <- event:
			return true
		case <-stop:
			return false
		}
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maximumAppServerLineBytes)
	count := 0
	for scanner.Scan() {
		count++
		if count > maximumAppServerMessages || len(scanner.Bytes()) == 0 {
			send(streamEvent{err: errors.New("Codex app-server message budget exceeded")})
			return
		}
		var message wireMessage
		if json.Unmarshal(scanner.Bytes(), &message) != nil ||
			(message.Method == "" && len(message.ID) == 0) ||
			(message.Method != "" && len(message.Result) != 0) ||
			(len(message.Result) != 0 && len(message.Error) != 0) {
			send(streamEvent{err: errors.New("Codex app-server JSON-RPC message is invalid")})
			return
		}
		if !send(streamEvent{message: message}) {
			return
		}
	}
	if scanner.Err() != nil {
		send(streamEvent{err: errors.New("read Codex app-server response stream")})
	}
}

// call writes one sequentially correlated request, ignores notifications and rejects unsolicited server
// requests, refresh authority and mismatched replies. Context completion returns cancellation; callers
// must serialize access because the ID counter and shared response stream are not synchronized per call.
// call записывает один последовательно коррелированный запрос, пропускает notifications и отклоняет
// незапрошенные server requests, refresh полномочия и несовпадающие replies. Завершение context
// возвращает cancellation; вызывающие сериализуют доступ, поскольку ID counter и общий response stream
// не синхронизированы для calls.
func (server *appServer) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	server.nextID++
	id := server.nextID
	if err := server.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, context.Canceled
		case event, open := <-server.messages:
			if !open {
				return nil, errors.New("Codex app-server closed its response stream")
			}
			if event.err != nil {
				return nil, event.err
			}
			if event.message.Method != "" {
				if len(event.message.ID) != 0 {
					_ = server.write(map[string]any{"id": event.message.ID, "error": map[string]any{"code": -32000, "message": "Server requests are not authorized"}})
					if method != "account/login/start" && event.message.Method == "account/chatgptAuthTokens/refresh" {
						return nil, errModelCatalogAuthorization
					}
					return nil, errors.New("Codex app-server requested unsupported authority")
				}
				continue
			}
			var responseID int64
			if json.Unmarshal(event.message.ID, &responseID) != nil || responseID != id {
				return nil, errors.New("Codex app-server response correlation failed")
			}
			if len(event.message.Error) != 0 {
				return nil, errors.New("Codex app-server returned a protocol error")
			}
			return event.message.Result, nil
		}
	}
}

// write marshals and bounds one JSON request before adding newline framing and writing child stdin,
// returning fixed encoding/write errors. The pre-append marshaled slice is deferred-cleared; immutable
// fields or reallocated framing copies are not guaranteed erased.
// write сериализует и ограничивает JSON запрос до добавления newline и записи в child stdin, возвращая
// фиксированные encoding/write ошибки. Исходный marshaled срез обнуляется через defer; стирание
// immutable полей и перераспределённых framing копий не гарантируется.
func (server *appServer) write(message any) error {
	raw, err := json.Marshal(message)
	defer clear(raw)
	if err != nil || len(raw) == 0 || len(raw) > maximumAppServerRequest {
		return errors.New("encode Codex app-server request")
	}
	raw = append(raw, '\n')
	if _, err := server.stdin.Write(raw); err != nil {
		return errors.New("write Codex app-server request")
	}
	return nil
}

// terminate closes reader delivery/stdin once, waits for exit, escalates process-group TERM/KILL on
// phase budgets and separately drains both readers before closing owned pipes. Forced shutdown and drain
// failure remain errors; final waits are not enclosed in an independent overall context.
// terminate однократно закрывает reader delivery/stdin, ждёт exit, повышает process-group TERM/KILL по
// фазовым бюджетам и отдельно дожидается обоих readers до закрытия owned pipes. Forced shutdown и drain
// отказ остаются ошибками; итоговые ожидания не охвачены отдельным общим context.
func (server *appServer) terminate() error {
	server.closeOnce.Do(func() {
		close(server.readerStop)
		_ = server.stdin.Close()
		timer := time.NewTimer(server.shutdownTimeout)
		defer timer.Stop()
		select {
		case err := <-server.wait:
			if err != nil {
				server.closeErr = errors.New("Codex app-server exited unsuccessfully")
			}
		case <-timer.C:
			if server.command.Process != nil {
				_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGTERM)
			}
			force := time.NewTimer(server.shutdownTimeout)
			select {
			case <-server.wait:
				server.closeErr = errors.New("Codex app-server required forced shutdown")
			case <-force.C:
				if server.command.Process != nil {
					_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGKILL)
				}
				<-server.wait
				server.closeErr = errors.New("Codex app-server shutdown deadline exceeded")
			}
			force.Stop()
		}
		// Потомок может удерживать pipe после выхода лидера. Drain имеет отдельный budget.
		drain := time.NewTimer(server.shutdownTimeout)
		defer drain.Stop()
		readerDone, diagnostics := server.readerDone, server.diagnostics
		for readerDone != nil || diagnostics != nil {
			select {
			case <-readerDone:
				readerDone = nil
			case diagnosticErr := <-diagnostics:
				server.closeErr = errors.Join(server.closeErr, diagnosticErr)
				diagnostics = nil
			case <-drain.C:
				_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGKILL)
				_ = server.stdout.Close()
				_ = server.stderr.Close()
				server.closeErr = errors.Join(server.closeErr, errors.New("Codex app-server stream shutdown deadline exceeded"))
			}
		}
		_ = server.stdout.Close()
		_ = server.stderr.Close()
	})
	return server.closeErr
}

// deviceSession binds one app-server/private home and login ID to verification metadata, exposing a
// once-only process/home cleanup result; durable device state is owned by Service/store instead.
// deviceSession связывает один app-server/private home и login ID с verification metadata, выдавая
// однократный process/home cleanup исход; durable device состояние принадлежит Service/store.
type deviceSession struct {
	server                                *appServer
	home, materializerAttemptRef, loginID string
	verificationURI, userCode             string
	closeOnce                             sync.Once
	closeErr                              error
}

// MaterializerAttemptRef returns this session's assigned materializer reference without a provider or
// storage read.
// MaterializerAttemptRef возвращает назначенную materializer ссылку сессии без provider и storage
// чтения.
func (session *deviceSession) MaterializerAttemptRef() string { return session.materializerAttemptRef }

// VerificationURI returns the already validated device-login verification URL stored during
// initialization without following it.
// VerificationURI возвращает уже проверенный device-login verification URL, сохранённый при
// инициализации, без перехода по нему.
func (session *deviceSession) VerificationURI() string { return session.verificationURI }

// UserCode returns the pending session's stored verification code, without reading the auth file or
// refreshing credentials.
// UserCode возвращает сохранённый verification код pending сессии без чтения auth-файла и refresh
// credentials.
func (session *deviceSession) UserCode() string { return session.userCode }

// Wait requires matching successful login completion and a chatgpt account update before account/read
// with refreshToken=false and private auth-file reading. It rejects uncorrelated responses/server
// requests and wraps materialization-stage errors; the caller owns returned credential bytes and session
// closure.
// Wait требует соответствующее успешное login completion и chatgpt account update до account/read с
// refreshToken=false и чтения private auth-файла. Некоррелированные ответы/server requests отклоняются,
// materialization-stage ошибки оборачиваются; возвращённые credential байты и закрытие сессии
// принадлежат вызывающему.
func (session *deviceSession) Wait(ctx context.Context) ([]byte, string, error) {
	loginCompleted, accountUpdated := false, false
	for {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case event, open := <-session.server.messages:
			if !open {
				return nil, "", errors.New("Codex app-server closed before device authorization completed")
			}
			if event.err != nil {
				return nil, "", event.err
			}
			if event.message.Method == "" {
				return nil, "", errors.New("Codex app-server emitted an uncorrelated response")
			}
			if len(event.message.ID) != 0 {
				_ = session.server.write(map[string]any{"id": event.message.ID, "error": map[string]any{"code": -32000, "message": "Server requests are not authorized"}})
				return nil, "", errors.New("Codex app-server requested unsupported authority")
			}
			switch event.message.Method {
			case "account/login/completed":
				var completed struct {
					LoginID string  `json:"loginId"`
					Success bool    `json:"success"`
					Error   *string `json:"error"`
				}
				if json.Unmarshal(event.message.Params, &completed) != nil || completed.LoginID != session.loginID {
					return nil, "", errors.New("Codex app-server device authorization completion is invalid")
				}
				if !completed.Success || completed.Error != nil {
					return nil, "", errors.New("Codex device authorization failed")
				}
				loginCompleted = true
			case "account/updated":
				var updated struct {
					AuthMode *string `json:"authMode"`
				}
				if json.Unmarshal(event.message.Params, &updated) != nil || updated.AuthMode == nil || *updated.AuthMode != "chatgpt" {
					return nil, "", errors.Join(errCredentialMaterialization, errors.New("Codex account update is invalid"))
				}
				accountUpdated = true
			default:
				continue
			}
			if !loginCompleted || !accountUpdated {
				continue
			}
			raw, err := session.server.call(ctx, "account/read", map[string]bool{"refreshToken": false})
			if err != nil {
				return nil, "", errors.Join(errCredentialMaterialization, err)
			}
			masked, err := maskedAccount(raw)
			if err != nil {
				return nil, "", errors.Join(errCredentialMaterialization, err)
			}
			authJSON, err := readAuthJSON(session.home)
			if err != nil {
				return nil, "", errors.Join(errCredentialMaterialization, err)
			}
			return authJSON, masked, nil
		}
	}
}

// Close records once-only process termination plus private-home removal errors and returns the same
// cleanup outcome on repeat; both cleanup operations are attempted.
// Close однократно сохраняет ошибки завершения процесса и удаления private home и возвращает тот же
// cleanup исход при повторе; обе операции предпринимаются.
func (session *deviceSession) Close() error {
	session.closeOnce.Do(func() {
		session.closeErr = session.server.terminate()
		session.closeErr = errors.Join(session.closeErr, os.RemoveAll(session.home))
	})
	return session.closeErr
}

// maskedAccount requires chatgpt account metadata and returns ChatGPT when email is absent, otherwise
// the first two local-part runes plus masking and the unchanged domain. It performs minimal bounded
// email shape checks, not full RFC validation or ownership verification.
// maskedAccount требует chatgpt account metadata и возвращает ChatGPT при отсутствии email, иначе первые
// две руны local-part с маской и неизменённым domain. Проверяется минимальная ограниченная форма email,
// не полный RFC синтаксис и не владение.
func maskedAccount(raw json.RawMessage) (string, error) {
	var response struct {
		Account struct {
			Type  string  `json:"type"`
			Email *string `json:"email"`
		} `json:"account"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Account.Type != "chatgpt" {
		return "", errors.New("Codex account metadata is invalid")
	}
	if response.Account.Email == nil {
		return "ChatGPT", nil
	}
	email := *response.Account.Email
	if email == "" || len(email) > 320 {
		return "", errors.New("Codex account metadata is invalid")
	}
	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" || domain == "" {
		return "", errors.New("Codex account metadata is invalid")
	}
	prefix := []rune(local)
	if len(prefix) > 2 {
		prefix = prefix[:2]
	}
	return string(prefix) + "***@" + domain, nil
}

// readAuthJSON opens the private auth file without following symlinks or blocking on special files,
// requires regular mode0600/effective UID/single link and bounded nonempty JSON, then rechecks inode
// identity before returning bytes. It detects path replacement, not arbitrary concurrent edits of the
// same inode; callers must clear the result.
// readAuthJSON открывает private auth-файл без следования symlink и блокировки на специальных файлах,
// требует regular mode0600/effective UID/single link и ограниченный непустой JSON, затем повторно
// сверяет inode identity до возврата байтов. Обнаруживается замена пути, а не произвольные concurrent
// правки того же inode; результат обнуляет вызывающий.
func readAuthJSON(home string) ([]byte, error) {
	path := filepath.Join(home, "auth.json")
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("read Codex authorization material")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() < 1 || info.Size() > maximumAuthJSONBytes {
		return nil, errors.New("Codex authorization material is unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("Codex authorization material identity is invalid")
	}
	value, err := io.ReadAll(io.LimitReader(file, maximumAuthJSONBytes+1))
	if err != nil || len(value) < 1 || len(value) > maximumAuthJSONBytes || !json.Valid(value) ||
		bytes.Equal(bytes.TrimSpace(value), []byte("{}")) {
		clear(value)
		return nil, errors.New("Codex authorization material is invalid")
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) {
		clear(value)
		return nil, errors.New("Codex authorization material identity changed")
	}
	return value, nil
}
