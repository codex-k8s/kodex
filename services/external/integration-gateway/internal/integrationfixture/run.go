package integrationfixture

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultListenAddress supplies the pod-facing HTTP listener only when no explicit local override is configured.
	// defaultListenAddress задаёт HTTP-listener для Pod только при отсутствии явного локального переопределения.
	defaultListenAddress = ":8080"
	// listenAddressEnv selects an explicit loopback-only HTTP override; the empty value uses defaultListenAddress.
	// listenAddressEnv выбирает явное HTTP-переопределение только для loopback; пустое значение использует defaultListenAddress.
	listenAddressEnv = "KODEX_INTEGRATION_SYNTHETIC_LISTEN_ADDRESS"
	// tlsListenAddressEnv enables an optional TLS listener only together with both identity files and a bearer digest.
	// tlsListenAddressEnv включает необязательный TLS-listener только вместе с обоими файлами идентичности и bearer-дайджестом.
	tlsListenAddressEnv = "KODEX_INTEGRATION_SYNTHETIC_TLS_LISTEN_ADDRESS"
	// tlsCertificateFileEnv names the canonical absolute certificate path loaded by Run for the optional TLS listener.
	// tlsCertificateFileEnv задаёт нормализованный абсолютный путь сертификата, загружаемого Run для необязательного TLS-listener.
	tlsCertificateFileEnv = "KODEX_INTEGRATION_SYNTHETIC_TLS_CERTIFICATE_FILE"
	// tlsPrivateKeyFileEnv names the paired TLS private-key path; configuration stores the path rather than key bytes.
	// tlsPrivateKeyFileEnv задаёт путь парного закрытого TLS-ключа; конфигурация хранит путь, а не байты ключа.
	tlsPrivateKeyFileEnv = "KODEX_INTEGRATION_SYNTHETIC_TLS_PRIVATE_KEY_FILE"
	// bearerTokenSHA256Env supplies only the expected lowercase token digest for prefixed fixture authentication, not the token itself.
	// bearerTokenSHA256Env передаёт только ожидаемый дайджест токена нижнего регистра для аутентификации префикса, а не сам токен.
	bearerTokenSHA256Env = "KODEX_INTEGRATION_SYNTHETIC_BEARER_SHA256"
	// fixtureShutdownTimeout bounds the concurrent shutdown attempt across all fixture listeners with one shared deadline.
	// fixtureShutdownTimeout ограничивает конкурентную попытку остановки всех listeners оснастки одним общим сроком.
	fixtureShutdownTimeout = 5 * time.Second
	// fixtureReadHeaderTimeout limits header acquisition independently from the five-second request read/write budgets.
	// fixtureReadHeaderTimeout ограничивает получение заголовков отдельно от пятисекундных бюджетов чтения и записи запроса.
	fixtureReadHeaderTimeout = 2 * time.Second
	// invalidBearerDigestDiagnostic is the stable closed error shared by digest shape and hex-decoding rejection.
	// invalidBearerDigestDiagnostic задаёт устойчивую закрытую ошибку отклонения формы дайджеста и его hex-декодирования.
	invalidBearerDigestDiagnostic = "synthetic integration bearer digest is invalid"
)

// tlsServerConfig holds an optional listener, identity-file paths and an already decoded bearer digest without starting TLS.
// tlsServerConfig хранит необязательный listener, пути файлов идентичности и уже декодированный bearer-дайджест без запуска TLS.
type tlsServerConfig struct {
	listenAddress, certificateFile, privateKeyFile string
	bearerDigest                                   [sha256.Size]byte
}

// Run allocates one disposable store and starts HTTP plus an optional TLS1.3 listener sharing that store.
// It marks probes ready after binding, waits for lifecycle cancellation or a serving result, then attempts bounded concurrent shutdown.
// Startup failures close the initial listener; credentials remain local files and the fixture performs no platform/provider calls.
// Run создаёт одно одноразовое хранилище и запускает HTTP и необязательный TLS1.3-listener с общим состоянием.
// Probes становятся готовыми после привязки; затем ожидается отмена lifecycle либо результат обслуживания и предпринимается ограниченная конкурентная остановка.
// При ошибках запуска исходный listener закрывается; реквизиты остаются локальными файлами, а вызовы платформы и провайдера не выполняются.
func Run(lifecycleCtx, shutdownBaseCtx context.Context, output io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(output, nil))
	store := NewStore()
	handler := NewHandler(store)
	listenAddress, err := configuredListenAddress()
	if err != nil {
		return err
	}
	tlsConfig, err := configuredTLSServer()
	if err != nil {
		return err
	}
	server := newFixtureServer(listenAddress, handler, logger)
	listener, err := (&net.ListenConfig{}).Listen(lifecycleCtx, "tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("listen for synthetic integration requests: %w", err)
	}
	servers := []*http.Server{server}
	listeners := []net.Listener{listener}
	handlers := []*Handler{handler}
	if tlsConfig != nil {
		certificate, loadErr := tls.LoadX509KeyPair(tlsConfig.certificateFile, tlsConfig.privateKeyFile)
		if loadErr != nil {
			_ = listener.Close()
			return fmt.Errorf("load synthetic integration TLS identity: %w", loadErr)
		}
		tlsListener, listenErr := (&net.ListenConfig{}).Listen(lifecycleCtx, "tcp", tlsConfig.listenAddress)
		if listenErr != nil {
			_ = listener.Close()
			return fmt.Errorf("listen for synthetic OpenAPI requests: %w", listenErr)
		}
		tlsHandler := NewBearerHandler(store, tlsConfig.bearerDigest)
		tlsServer := newFixtureServer(tlsConfig.listenAddress, tlsHandler, logger)
		servers = append(servers, tlsServer)
		listeners = append(listeners, tls.NewListener(tlsListener, &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{certificate},
		}))
		handlers = append(handlers, tlsHandler)
	}
	for _, current := range handlers {
		current.SetReady(true)
	}
	logger.InfoContext(
		lifecycleCtx,
		"integration synthetic fixture started",
		"listeners", len(listeners),
		"address", listener.Addr().String(),
	)
	serveResult := make(chan error, len(servers))
	for index := range servers {
		currentServer, currentListener := servers[index], listeners[index]
		go func() { serveResult <- currentServer.Serve(currentListener) }()
	}

	var serveErr error
	select {
	case <-lifecycleCtx.Done():
	case serveErr = <-serveResult:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	}
	for _, current := range handlers {
		current.SetReady(false)
	}
	shutdownErr := shutdownFixtureServers(shutdownBaseCtx, servers)
	logger.InfoContext(shutdownBaseCtx, "integration synthetic fixture stopped")
	return errors.Join(serveErr, shutdownErr)
}

// newFixtureServer configures bounded header/read/write/idle budgets and a structured error logger without binding or serving.
// TLS wrapping belongs to Run, and the handler continues to own method/body/authentication validation.
// newFixtureServer задаёт ограниченные бюджеты заголовков, чтения, записи и простоя и структурированный журнал ошибок без привязки или обслуживания.
// TLS-обёрткой владеет Run, а проверкой метода, тела и аутентификации — обработчик.
func newFixtureServer(address string, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr: address, Handler: handler,
		ReadHeaderTimeout: fixtureReadHeaderTimeout,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

// shutdownFixtureServers attempts Shutdown for every server concurrently under one child deadline and joins all returned errors.
// It tolerates ErrServerClosed but does not force-close connections or claim successful cleanup after a timeout.
// shutdownFixtureServers конкурентно пытается остановить каждый сервер одним дочерним сроком и объединяет все возвращённые ошибки.
// ErrServerClosed принимается, но соединения принудительно не закрываются и очистка после таймаута не объявляется успешной.
func shutdownFixtureServers(base context.Context, servers []*http.Server) error {
	shutdownCtx, cancelShutdown := context.WithTimeout(base, fixtureShutdownTimeout)
	defer cancelShutdown()
	errorsByServer := make(chan error, len(servers))
	for _, server := range servers {
		current := server
		go func() {
			err := current.Shutdown(shutdownCtx)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			errorsByServer <- err
		}()
	}
	var result error
	for range servers {
		result = errors.Join(result, <-errorsByServer)
	}
	return result
}

// configuredListenAddress returns the default pod address or an explicit IPv4-loopback address with ephemeral/unprivileged port.
// It parses configuration only; no socket is bound and explicit wildcard overrides are rejected.
// configuredListenAddress возвращает обычный адрес Pod либо явный IPv4-loopback с временным или непривилегированным портом.
// Разбирается только конфигурация; сокет не привязывается, а явные wildcard-переопределения отклоняются.
func configuredListenAddress() (string, error) {
	address := os.Getenv(listenAddressEnv)
	if address == "" {
		return defaultListenAddress, nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return "", errors.New("synthetic integration listen address is invalid")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || (portNumber != 0 && portNumber < 1024) || portNumber > 65535 {
		return "", errors.New("synthetic integration listen port is invalid")
	}
	return address, nil
}

// configuredTLSServer accepts either no TLS fields or all four with a valid wildcard-empty-host/loopback address, paths and SHA-256 text.
// It validates canonical absolute paths but does not read or inspect certificate/key files until Run initializes the listener.
// configuredTLSServer принимает отсутствие TLS-полей либо все четыре с допустимым пустым wildcard-хостом или loopback-адресом, путями и SHA-256 текстом.
// Проверяются нормализованные абсолютные пути, но файлы сертификата и ключа не читаются и не исследуются до инициализации listener в Run.
func configuredTLSServer() (*tlsServerConfig, error) {
	values := []string{
		os.Getenv(tlsListenAddressEnv),
		os.Getenv(tlsCertificateFileEnv),
		os.Getenv(tlsPrivateKeyFileEnv),
		os.Getenv(bearerTokenSHA256Env),
	}
	nonEmpty := 0
	for _, value := range values {
		if value != "" {
			nonEmpty++
		}
	}
	if nonEmpty == 0 {
		return nil, nil
	}
	if nonEmpty != len(values) {
		return nil, errors.New("synthetic integration TLS configuration is incomplete")
	}
	address := values[0]
	host, port, err := net.SplitHostPort(address)
	if err != nil || (host != "" && host != "127.0.0.1") {
		return nil, errors.New("synthetic integration TLS listen address is invalid")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1024 || portNumber > 65535 {
		return nil, errors.New("synthetic integration TLS listen port is invalid")
	}
	for _, path := range values[1:3] {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, errors.New("synthetic integration TLS path is invalid")
		}
	}
	if values[3] != strings.ToLower(values[3]) || len(values[3]) != sha256.Size*2 {
		return nil, errors.New(invalidBearerDigestDiagnostic)
	}
	decoded, err := hex.DecodeString(values[3])
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New(invalidBearerDigestDiagnostic)
	}
	config := &tlsServerConfig{listenAddress: address, certificateFile: values[1], privateKeyFile: values[2]}
	copy(config.bearerDigest[:], decoded)
	return config, nil
}
