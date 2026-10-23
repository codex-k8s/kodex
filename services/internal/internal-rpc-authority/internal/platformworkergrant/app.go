// Package platformworkergrant maintains a short-lived local credential for one
// registered worker without Kubernetes access or other workloads' signing keys.
// Пакет platformworkergrant поддерживает короткоживущие локальные учётные данные
// одной зарегистрированной нагрузки без доступа к Kubernetes и ключам других.
package platformworkergrant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	sharedhttpserver "github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/google/uuid"
)

const (
	// grantType pins the protected JWS typ used for local worker credentials; matching it alone does not
	// verify a producer or authorize an operation.
	// grantType закрепляет защищённый JWS typ локальных worker credentials; одно совпадение не проверяет
	// производителя и не разрешает операцию.
	grantType = "kodex-application-grant+jws"
	// grantTTL caps issued credential lifetime and the readback time validator at four minutes; periodic
	// refresh does not extend an existing signed envelope.
	// grantTTL ограничивает срок выпуска и проверку прочитанного credential четырьмя минутами;
	// периодическое обновление не продлевает прежний подписанный конверт.
	grantTTL = 4 * time.Minute
	// maximumKeyBytes limits accepted signing-key input after os.ReadFile has completed; it is not a
	// streaming allocation limit.
	// maximumKeyBytes ограничивает принимаемый ключ после завершения os.ReadFile; это не потоковое
	// ограничение выделения памяти.
	maximumKeyBytes = 16 << 10
)

// supportedWorkloads is the closed local issuance allowlist; membership still requires an exact
// workload/generation signing-key binding and valid instance configuration.
// supportedWorkloads — закрытый локальный список выпуска; членство дополнительно требует точной
// привязки ключа к нагрузке и поколению и корректной настройки экземпляра.
var supportedWorkloads = map[string]struct{}{
	"automation-scheduler": {},
	"image-admission":      {},
	"image-promotion":      {},
	"integration-gateway":  {},
	"interaction-gateway":  {},
	"email-bridge":         {},
	"role-image-builder":   {},
	"runtime-controller":   {},
	"session-archive":      {},
	"secret-broker":        {},
	"control-plane":        {},
}

// config holds one worker's key/output paths, optional instance ID and bounded refresh/shutdown
// settings. loadConfig validates configuration; fields do not establish observed workload identity.
// config хранит пути ключа и результата одной нагрузки, необязательный ID экземпляра и ограниченные
// настройки обновления и остановки. loadConfig проверяет настройки; сами поля не доказывают
// наблюдаемую личность нагрузки.
type config struct {
	WorkloadID      string        `env:"PLATFORM_WORKER_GRANT_WORKLOAD_ID"`
	InstanceID      string        `env:"PLATFORM_WORKER_GRANT_INSTANCE_ID"`
	PrivateJWKFile  string        `env:"PLATFORM_WORKER_GRANT_PRIVATE_JWK_FILE"`
	OutputFile      string        `env:"PLATFORM_WORKER_GRANT_OUTPUT_FILE"`
	TechnicalListen string        `env:"PLATFORM_WORKER_GRANT_TECHNICAL_LISTEN"`
	RefreshInterval time.Duration `env:"PLATFORM_WORKER_GRANT_REFRESH_INTERVAL"`
	ShutdownTimeout time.Duration `env:"PLATFORM_WORKER_GRANT_SHUTDOWN_TIMEOUT"`
}

// claims keeps this issuer's nominal credential type over the shared JSON shape;
// rotate and readGrant still own producer, lifetime and readback validation.
// claims сохраняет отдельный тип выпуска на общей форме JSON; rotate и readGrant
// по-прежнему проверяют производителя, сроки и результат записи.
type claims internalrpcauth.PlatformWorkerGrantClaims

// Run loads configuration/key and writes a verified initial grant before starting technical HTTP and
// rotation workers. It marks shutdown unready, stops and joins workers through bounded cleanup using
// the supplied independent shutdown base.
// Run читает настройки и ключ и записывает проверенный первый grant до запуска технического HTTP и
// обновления. При остановке снимает готовность, останавливает и ожидает workers ограниченными
// cleanup-операциями на переданном независимом базовом контексте.
func Run(lifecycle, shutdownBase context.Context) error {
	configuration, err := loadConfig()
	if err != nil {
		return err
	}
	key, err := loadKey(configuration.PrivateJWKFile)
	if err != nil {
		return err
	}
	readiness := serviceruntime.NewReadiness()
	if err := rotate(configuration, key, time.Now); err != nil {
		return fmt.Errorf("materialize initial platform worker grant: %w", err)
	}
	readiness.Set(true, "ready")
	server := technicalServer(configuration.TechnicalListen, readiness, func() bool {
		return readCurrent(configuration, key, time.Now()) == nil
	})
	workers := serviceruntime.StartWorkers(lifecycle,
		sharedhttpserver.ShutdownWorker(server.ListenAndServe, server.Shutdown, configuration.ShutdownTimeout),
		rotationWorker(configuration, key, readiness, slog.Default()),
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	readiness.Set(false, "shutting_down")
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "technical HTTP server", Timeout: configuration.ShutdownTimeout, Run: server.Shutdown},
		serviceruntime.ShutdownOperation{Name: "platform worker grant workers", Timeout: configuration.ShutdownTimeout, Run: workers.Wait},
	)
	return errors.Join(err, shutdownErr)
}

// loadConfig overlays environment values on local defaults, then requires an allowed workload,
// canonical optional instance, distinct clean absolute key/output directories and bounded
// intervals/listener syntax; it does not read the key or probe the endpoint.
// loadConfig накладывает env на локальные defaults, затем требует разрешённую нагрузку, канонический
// необязательный экземпляр, разные нормализованные абсолютные каталоги ключа и результата,
// ограниченные интервалы и допустимый адрес; ключ не читает и endpoint не проверяет.
func loadConfig() (config, error) {
	configuration := config{
		PrivateJWKFile:  "/var/run/secrets/kodex/platform-worker-grant-signer/private.jwk",
		OutputFile:      "/var/run/secrets/kodex/platform-worker-grant/application-grant.jws",
		TechnicalListen: ":9093", RefreshInterval: time.Minute, ShutdownTimeout: 5 * time.Second,
	}
	if err := env.Parse(&configuration); err != nil {
		return config{}, errors.New("parse platform worker grant environment")
	}
	if _, ok := supportedWorkloads[configuration.WorkloadID]; !ok || !validInstanceID(configuration.InstanceID) ||
		!filepath.IsAbs(configuration.PrivateJWKFile) || !filepath.IsAbs(configuration.OutputFile) ||
		filepath.Clean(configuration.PrivateJWKFile) != configuration.PrivateJWKFile || filepath.Clean(configuration.OutputFile) != configuration.OutputFile ||
		filepath.Dir(configuration.PrivateJWKFile) == filepath.Dir(configuration.OutputFile) ||
		configuration.RefreshInterval < 30*time.Second || configuration.RefreshInterval > 90*time.Second ||
		configuration.ShutdownTimeout < time.Second || configuration.ShutdownTimeout > 15*time.Second {
		return config{}, errors.New("platform worker grant configuration is invalid")
	}
	if _, _, err := net.SplitHostPort(configuration.TechnicalListen); err != nil {
		return config{}, errors.New("platform worker grant technical address is invalid")
	}
	return configuration, nil
}

// loadKey reads and bounds nonempty private JWK bytes, parses an ES256 signing key and clears the
// accepted raw buffer after parsing. Read/size failure returns before that clearing loop; no ownership
// or no-follow check is performed here.
// loadKey читает непустые ограниченные по длине байты private JWK, разбирает ключ подписи ES256 и
// очищает принятый буфер после разбора. Ошибка чтения или длины возвращается до этого цикла очистки;
// владение и no-follow здесь не проверяются.
func loadKey(path string) (internalrpcauth.ES256Key, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > maximumKeyBytes {
		return internalrpcauth.ES256Key{}, errors.New("read platform worker grant signing key")
	}
	key, err := internalrpcauth.ParsePrivateJWK(raw)
	for index := range raw {
		raw[index] = 0
	}
	if err != nil {
		return internalrpcauth.ES256Key{}, errors.New("parse platform worker grant signing key")
	}
	return key, nil
}

// rotate verifies workload/key generation and optional instance, reuses an exact valid same-second
// readback or signs a fresh bounded envelope, atomically replaces the local output and verifies it. A
// write may succeed before readback fails; rotation does not register durable owner acceptance.
// rotate проверяет нагрузку, поколение ключа и необязательный экземпляр, сохраняет точный действующий
// результат той же секунды либо подписывает новый ограниченный конверт, атомарно заменяет локальный
// файл и проверяет его. Запись может завершиться до ошибки проверки; устойчивое принятие владельцем
// здесь не регистрируется.
func rotate(configuration config, key internalrpcauth.ES256Key, now func() time.Time) error {
	credentialGeneration, err := internalrpcauth.KeyGeneration(key.KeyID)
	if _, ok := supportedWorkloads[configuration.WorkloadID]; !ok || !validInstanceID(configuration.InstanceID) || err != nil ||
		key.KeyID != fmt.Sprintf("%s-platform-worker-g%d", configuration.WorkloadID, credentialGeneration) {
		return errors.New("platform worker grant signing key binding is invalid")
	}
	issuedAt := now().UTC().Truncate(time.Second)
	if issuedAt.Unix() <= 0 {
		return errors.New("platform worker grant issue time is invalid")
	}
	// A same-second restart reuses the exact accepted envelope for this Pod.
	// Перезапуск в той же секунде сохраняет точный принятый конверт этого Pod.
	if readBack(configuration, key, issuedAt) == nil {
		return nil
	}
	workloadSPIFFE := "spiffe://kodex.local/ns/kodex-system/sa/" + configuration.WorkloadID
	value := claims{
		Version:  1,
		Issuer:   "https://control-plane.kodex-system.svc.cluster.local/authority/platform-worker/" + configuration.WorkloadID,
		Audience: "urn:kodex:platform-worker:" + configuration.WorkloadID,
		Subject:  "kodex-system-subject", CallerSPIFFEID: workloadSPIFFE,
		WorkloadID: configuration.WorkloadID, OrganizationID: "kodex-installation",
		Revision: uint64(issuedAt.Unix()), CredentialGeneration: credentialGeneration, JTI: uuid.NewString(),
		AuthorityABIVersion: internalrpcauth.AuthorityABIVersion,
		IssuedAt:            issuedAt.Unix(), NotBefore: issuedAt.Unix(), ExpiresAt: issuedAt.Add(grantTTL).Unix(),
	}
	if configuration.InstanceID != "" {
		value.Version = 2
		value.InstanceID = configuration.InstanceID
	}
	compact, err := internalrpcauth.SignCanonicalJSON(value, key, internalrpcauth.ProtectedHeaderExpectation{Type: grantType, KeyID: key.KeyID})
	if err != nil {
		return errors.New("sign platform worker grant")
	}
	if err := writeAtomic(configuration.OutputFile, []byte(compact)); err != nil {
		return err
	}
	return readBack(configuration, key, issuedAt)
}

// writeAtomic rejects empty/oversized/multiline output and a symlink final directory, writes a 0440
// temporary file, syncs/closes it and renames it over the target. It cleans up the temporary path but
// does not fsync the directory or secure every ancestor against races.
// writeAtomic отклоняет пустой, слишком длинный или многострочный результат и симлинк конечного
// каталога, пишет временный файл 0440, синхронизирует, закрывает и переименовывает поверх цели.
// Временный путь удаляется; fsync каталога и защита всех предков от гонок здесь не выполняются.
func writeAtomic(path string, value []byte) error {
	if len(value) == 0 || len(value) > 16<<10 || strings.ContainsAny(string(value), "\r\n") {
		return errors.New("platform worker grant output is invalid")
	}
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("platform worker grant output directory is invalid")
	}
	temporary, err := os.CreateTemp(directory, ".grant-*")
	if err != nil {
		return errors.New("create platform worker grant temporary file")
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o440); err != nil {
		_ = temporary.Close()
		return errors.New("set platform worker grant permissions")
	}
	if _, err := temporary.Write(value); err != nil {
		_ = temporary.Close()
		return errors.New("write platform worker grant")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errors.New("sync platform worker grant")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("close platform worker grant")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("replace platform worker grant")
	}
	return nil
}

// readBack requires the signed output to match this exact issue second as well as all readGrant
// bindings; it does not rewrite the file or advance an owner watermark.
// readBack требует совпадения подписанного результата с точной секундой выпуска и всеми привязками
// readGrant; файл не переписывает и watermark владельца не меняет.
func readBack(configuration config, key internalrpcauth.ES256Key, now time.Time) error {
	return readGrant(configuration, key, now, true)
}

// readCurrent accepts the still-valid configured grant between refresh ticks without requiring the
// current second as its revision; cryptographic, instance and time checks remain mandatory.
// readCurrent допускает ещё действующий настроенный grant между обновлениями, не требуя текущую
// секунду как ревизию; криптографические проверки, экземпляр и сроки остаются обязательными.
func readCurrent(configuration config, key internalrpcauth.ES256Key, now time.Time) error {
	return readGrant(configuration, key, now, false)
}

// validInstanceID accepts the existing empty-instance mode or a nonnil UUID whose canonical rendering
// equals the input; it observes no Pod or Kubernetes state.
// validInstanceID принимает существующий режим без экземпляра либо ненулевой UUID с канонической
// строкой, равной вводу; состояние Pod или Kubernetes не наблюдает.
func validInstanceID(value string) bool {
	if value == "" {
		return true
	}
	instance, err := uuid.Parse(value)
	return err == nil && instance != uuid.Nil && instance.String() == value
}

// readGrant reads bounded-length output, verifies canonical JWS with the configured public key and
// enforces exact version/instance, workload, issuer/audience/SPIFFE, system subject/tenant,
// generation, ABI, UUID and lifetime bindings. exactIssuedAt additionally pins revision to now's Unix
// second; no domain grant is accepted by this local readback.
// readGrant читает результат с проверкой длины, проверяет канонический JWS настроенным публичным
// ключом и требует точные версию, экземпляр, нагрузку, issuer/audience/SPIFFE, системные субъект и
// организацию, поколение, ABI, UUID и сроки. exactIssuedAt также привязывает ревизию к Unix-секунде
// now; локальная проверка не принимает доменный grant.
func readGrant(configuration config, key internalrpcauth.ES256Key, now time.Time, exactIssuedAt bool) error {
	raw, err := os.ReadFile(configuration.OutputFile)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		return errors.New("read back platform worker grant")
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(string(raw), key.PublicOnly(), internalrpcauth.ProtectedHeaderExpectation{Type: grantType, KeyID: key.KeyID})
	if err != nil {
		return errors.New("verify platform worker grant readback")
	}
	var value claims
	expectedVersion := 1
	if configuration.InstanceID != "" {
		expectedVersion = 2
	}
	credentialGeneration, generationErr := internalrpcauth.KeyGeneration(key.KeyID)
	if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &value) != nil ||
		generationErr != nil || !validInstanceID(configuration.InstanceID) || value.Version != expectedVersion ||
		value.InstanceID != configuration.InstanceID || value.WorkloadID != configuration.WorkloadID ||
		value.IssuedAt <= 0 || value.Revision != uint64(value.IssuedAt) ||
		(exactIssuedAt && value.Revision != uint64(now.Unix())) || now.Unix() >= value.ExpiresAt ||
		value.Issuer != "https://control-plane.kodex-system.svc.cluster.local/authority/platform-worker/"+configuration.WorkloadID ||
		value.Audience != "urn:kodex:platform-worker:"+configuration.WorkloadID ||
		value.CallerSPIFFEID != "spiffe://kodex.local/ns/kodex-system/sa/"+configuration.WorkloadID ||
		value.Subject != "kodex-system-subject" || value.OrganizationID != "kodex-installation" || value.ProjectID != "" || value.TenantOwner ||
		value.CredentialGeneration != credentialGeneration || uuid.Validate(value.JTI) != nil ||
		value.AuthorityABIVersion != internalrpcauth.AuthorityABIVersion ||
		internalrpcauth.ValidateTimes(now, time.Unix(value.IssuedAt, 0), time.Unix(value.NotBefore, 0), time.Unix(value.ExpiresAt, 0), grantTTL, 5*time.Second) != nil {
		return errors.New("platform worker grant readback binding is invalid")
	}
	return nil
}

// rotationWorker refreshes on a stopped-on-exit ticker until cancellation. On refresh failure it keeps
// readiness only while the actual previous grant verifies, logging state transitions rather than every
// failed tick.
// rotationWorker обновляет grant по останавливаемому при выходе ticker до отмены. При ошибке сохраняет
// готовность только пока прежний файл действительно проходит проверку, журналируя переходы состояния,
// а не каждый неудачный tick.
func rotationWorker(configuration config, key internalrpcauth.ES256Key, readiness *serviceruntime.Readiness, logger *slog.Logger) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(configuration.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				if err := rotate(configuration, key, time.Now); err != nil {
					if readCurrent(configuration, key, time.Now()) == nil {
						if readiness.Set(true, "grant_refresh_deferred") {
							logger.WarnContext(ctx, "platform worker grant refresh deferred", "error_class", "local_storage")
						}
					} else if readiness.Set(false, "local_grant_materialization_failed") {
						logger.ErrorContext(ctx, "platform worker grant materialization failed", "error_class", "local_storage")
					}
					continue
				}
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "platform worker grant materialization restored")
				}
			}
		}
	}
}

// technicalServer exposes bounded-time health/live probes and readiness that also verifies the actual
// current grant; failed readiness reports a fixed local reason, not signing material. Construction
// does not start a listener.
// technicalServer задаёт ограниченные по времени health/live probes и readiness с дополнительной
// проверкой действующего grant; отказ сообщает локальную причину, не материал подписи. Создание
// сервера не запускает listener.
func technicalServer(address string, readiness *serviceruntime.Readiness, grantReady func() bool) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		ready, reason := readiness.Ready()
		if ready && !grantReady() {
			ready, reason = false, "local_grant_unavailable"
		}
		if !ready {
			http.Error(writer, reason, http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	return &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8 << 10}
}
