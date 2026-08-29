// Package platformworkergrant поддерживает короткоживущий локальный grant для
// одного заранее зарегистрированного platform worker. Агент не обращается к
// Kubernetes API и не получает ключей других workload.
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
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/google/uuid"
)

const (
	grantType       = "kodex-application-grant+jws"
	grantTTL        = 4 * time.Minute
	maximumKeyBytes = 16 << 10
)

var supportedWorkloads = map[string]struct{}{
	"automation-scheduler": {},
	"image-admission":      {},
	"image-promotion":      {},
	"integration-gateway":  {},
	"role-image-builder":   {},
	"runtime-controller":   {},
	"session-archive":      {},
	"secret-broker":        {},
}

type config struct {
	WorkloadID      string        `env:"PLATFORM_WORKER_GRANT_WORKLOAD_ID"`
	PrivateJWKFile  string        `env:"PLATFORM_WORKER_GRANT_PRIVATE_JWK_FILE"`
	OutputFile      string        `env:"PLATFORM_WORKER_GRANT_OUTPUT_FILE"`
	TechnicalListen string        `env:"PLATFORM_WORKER_GRANT_TECHNICAL_LISTEN"`
	RefreshInterval time.Duration `env:"PLATFORM_WORKER_GRANT_REFRESH_INTERVAL"`
	ShutdownTimeout time.Duration `env:"PLATFORM_WORKER_GRANT_SHUTDOWN_TIMEOUT"`
}

type claims struct {
	Version        int    `json:"v"`
	Issuer         string `json:"iss"`
	Audience       string `json:"aud"`
	Subject        string `json:"sub"`
	CallerSPIFFEID string `json:"caller_spiffe_id"`
	WorkloadID     string `json:"workload_id"`
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id"`
	TenantOwner    bool   `json:"tenant_owner"`
	Revision       uint64 `json:"revision"`
	JTI            string `json:"jti"`
	IssuedAt       int64  `json:"iat"`
	NotBefore      int64  `json:"nbf"`
	ExpiresAt      int64  `json:"exp"`
}

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
	server := technicalServer(configuration.TechnicalListen, readiness)
	workers := serviceruntime.StartWorkers(lifecycle,
		serveHTTP(server, configuration.ShutdownTimeout),
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

func loadConfig() (config, error) {
	configuration := config{
		PrivateJWKFile:  "/var/run/secrets/kodex/platform-worker-grant-signer/private.jwk",
		OutputFile:      "/var/run/secrets/kodex/platform-worker-grant/application-grant.jws",
		TechnicalListen: ":9093", RefreshInterval: time.Minute, ShutdownTimeout: 5 * time.Second,
	}
	if err := env.Parse(&configuration); err != nil {
		return config{}, errors.New("parse platform worker grant environment")
	}
	if _, ok := supportedWorkloads[configuration.WorkloadID]; !ok ||
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

func rotate(configuration config, key internalrpcauth.ES256Key, now func() time.Time) error {
	issuedAt := now().UTC().Truncate(time.Second)
	if issuedAt.Unix() <= 0 {
		return errors.New("platform worker grant issue time is invalid")
	}
	workloadSPIFFE := "spiffe://kodex.local/ns/kodex-system/sa/" + configuration.WorkloadID
	value := claims{
		Version:  1,
		Issuer:   "https://control-plane.kodex-system.svc.cluster.local/authority/platform-worker/" + configuration.WorkloadID,
		Audience: "urn:kodex:platform-worker:" + configuration.WorkloadID,
		Subject:  "kodex-system-subject", CallerSPIFFEID: workloadSPIFFE,
		WorkloadID: configuration.WorkloadID, OrganizationID: "kodex-installation",
		Revision: uint64(issuedAt.Unix()), JTI: uuid.NewString(),
		IssuedAt: issuedAt.Unix(), NotBefore: issuedAt.Unix(), ExpiresAt: issuedAt.Add(grantTTL).Unix(),
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

func readBack(configuration config, key internalrpcauth.ES256Key, now time.Time) error {
	raw, err := os.ReadFile(configuration.OutputFile)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		return errors.New("read back platform worker grant")
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(string(raw), key.PublicOnly(), internalrpcauth.ProtectedHeaderExpectation{Type: grantType, KeyID: key.KeyID})
	if err != nil {
		return errors.New("verify platform worker grant readback")
	}
	var value claims
	if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &value) != nil ||
		value.WorkloadID != configuration.WorkloadID || value.Revision != uint64(now.Unix()) || uuid.Validate(value.JTI) != nil ||
		internalrpcauth.ValidateTimes(now, time.Unix(value.IssuedAt, 0), time.Unix(value.NotBefore, 0), time.Unix(value.ExpiresAt, 0), grantTTL, 5*time.Second) != nil {
		return errors.New("platform worker grant readback binding is invalid")
	}
	return nil
}

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
					if readiness.Set(false, "local_grant_materialization_failed") {
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

func technicalServer(address string, readiness *serviceruntime.Readiness) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		ready, reason := readiness.Ready()
		if !ready {
			http.Error(writer, reason, http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	return &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8 << 10}
}

func serveHTTP(server *http.Server, shutdownTimeout time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.ListenAndServe() }()
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
			defer cancel()
			err := server.Shutdown(shutdown)
			serveErr := <-done
			if !errors.Is(serveErr, http.ErrServerClosed) {
				err = errors.Join(err, serveErr)
			}
			return err
		}
	}
}
