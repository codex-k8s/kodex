package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	sharedhttp "github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	controlowner "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/controlplane"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/sshidentity"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/sshtarget"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	httptransport "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/transport/http"
	"golang.org/x/net/netutil"
)

const (
	// sshUploadUnavailable conceals key paths, remote addresses and TLS/RPC diagnostics from runtime output.
	// sshUploadUnavailable скрывает key paths, remote addresses и TLS/RPC diagnostics в runtime output.
	sshUploadUnavailable = "SSH identity upload is unavailable"
	// sshUploadUnjoined distinguishes a failed positive join from successful shutdown or remote quiescence.
	// sshUploadUnjoined отличает failed positive join от successful shutdown либо remote quiescence.
	sshUploadUnjoined = "SSH identity upload handlers did not join"
)

// sshIdentityUploadComponent owns only the separate browser/CLI upload listener and its local readiness dependencies; it never registers a generic TOKEN route or certifies SSH host access.
// sshIdentityUploadComponent владеет лишь отдельным browser/CLI upload listener и local readiness dependencies, не регистрируя generic TOKEN route и не подтверждая SSH host access.
type sshIdentityUploadComponent struct {
	server   *http.Server
	listener net.Listener
	gate     *sharedhttp.RequestGate
	owner    *controlowner.Owner
	store    *kubernetesstore.Store
	shutdown time.Duration
	started  atomic.Bool
}

// newSSHIdentityUpload composes the existing owner, actual key parser and private purpose store only when the full explicit configuration is present. Construction performs no Kubernetes mutation or SSH dial.
// newSSHIdentityUpload связывает existing owner, actual key parser и private purpose store только при полной explicit configuration. Construction не выполняет Kubernetes mutation либо SSH dial.
func newSSHIdentityUpload(config Config, owner *controlowner.Owner, runtimeStore *kubernetesstore.Store, logger *slog.Logger) (*sshIdentityUploadComponent, error) {
	if !config.sshIdentityUploadEnabled() {
		return nil, nil
	}
	if config.validateSSHIdentityUpload() != nil || owner == nil || runtimeStore == nil || logger == nil {
		return nil, errors.New(sshUploadConfigurationInvalid)
	}
	store, err := runtimeStore.SSHIdentityStore()
	if err != nil {
		return nil, errors.New(sshUploadUnavailable)
	}
	service, err := sshidentity.New(owner, sshtarget.KeyPreparer{}, store, config.ClaimantID, config.SSHIdentityNamespace)
	if err != nil {
		return nil, errors.New(sshUploadUnavailable)
	}
	origin, _ := url.Parse(config.SSHUploadOrigin)
	handler, err := httptransport.NewSSHIdentityUpload(service, managedssh.IdentityUploadEndpoint{Origin: config.SSHUploadOrigin, TLSServerName: origin.Hostname()}, config.SSHUploadBrowserOrigin, logger)
	if err != nil {
		return nil, errors.New(sshUploadConfigurationInvalid)
	}
	server, gate, err := newSSHUploadServer(config, handler)
	if err != nil {
		return nil, err
	}
	return &sshIdentityUploadComponent{server: server, gate: gate, owner: owner, store: store, shutdown: config.ShutdownTimeout}, nil
}

// newSSHUploadServer loads one DNS/time-bound certificate before bind and exposes only TLS1.3 HTTP/1.1 with bounded headers/body/response lifetimes. TLS diagnostics are discarded rather than logging client addresses or uncontrolled peer text.
// newSSHUploadServer загружает DNS/time-bound certificate до bind и предоставляет только TLS1.3 HTTP/1.1 с bounded headers/body/response lifetimes. TLS diagnostics отбрасываются вместо записи client addresses либо uncontrolled peer text.
func newSSHUploadServer(config Config, handler http.Handler) (*http.Server, *sharedhttp.RequestGate, error) {
	if handler == nil || config.validateSSHIdentityUpload() != nil {
		return nil, nil, errors.New(sshUploadConfigurationInvalid)
	}
	certificate, err := tls.LoadX509KeyPair(config.SSHUploadCertificateFile, config.SSHUploadPrivateKeyFile)
	if err != nil || len(certificate.Certificate) == 0 {
		return nil, nil, errors.New(sshUploadUnavailable)
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	origin, _ := url.Parse(config.SSHUploadOrigin)
	if err != nil || leaf.VerifyHostname(origin.Hostname()) != nil {
		return nil, nil, errors.New(sshUploadUnavailable)
	}
	certificate.Leaf = leaf
	if !sshUploadCertificateCurrent(certificate, time.Now()) {
		return nil, nil, errors.New(sshUploadUnavailable)
	}
	gate, err := sharedhttp.NewRequestGate(handler, 8)
	if err != nil {
		return nil, nil, errors.New(sshUploadConfigurationInvalid)
	}
	server := &http.Server{Addr: config.SSHUploadListen, Handler: gate, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, NextProtos: []string{"http/1.1"}}, TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	return server, gate, nil
}

// sshUploadCertificateCurrent checks all presented chain lifetimes without claiming the browser trusts an issuer; issuer/DNS enrollment and certificate replacement belong to the installation/rollout protocol.
// sshUploadCertificateCurrent проверяет lifetime всей presented chain, не утверждая доверие browser к issuer; issuer/DNS enrollment и certificate replacement принадлежат installation/rollout protocol.
func sshUploadCertificateCurrent(certificate tls.Certificate, now time.Time) bool {
	if now.IsZero() || len(certificate.Certificate) == 0 {
		return false
	}
	for _, raw := range certificate.Certificate {
		parsed, err := x509.ParseCertificate(raw)
		if err != nil || now.Before(parsed.NotBefore) || !now.Before(parsed.NotAfter) {
			return false
		}
	}
	return true
}

// Check proves only local serving certificate validity, protected owner lookup availability and fixed namespace/API access; CREATE effect and SSH authentication require their real owner admission and verification paths.
// Check подтверждает лишь local certificate validity, protected owner lookup availability и fixed namespace/API access; CREATE effect и SSH authentication требуют real owner admission и verification paths.
func (component *sshIdentityUploadComponent) Check(ctx context.Context) error {
	if component == nil || component.server == nil || component.gate.Closed() || component.owner == nil || component.store == nil || component.server.TLSConfig == nil || len(component.server.TLSConfig.Certificates) != 1 || !sshUploadCertificateCurrent(component.server.TLSConfig.Certificates[0], time.Now()) {
		return errors.New(sshUploadUnavailable)
	}
	if component.owner.CheckSSHIdentitySetupReadiness(ctx) != nil || component.store.CheckSSHIdentityStorage(ctx) != nil {
		return errors.New(sshUploadUnavailable)
	}
	return nil
}

// bind reserves the exact configured listener before publishing readiness; an unstarted listener remains owned by close during any later startup refusal.
// bind резервирует exact configured listener до публикации readiness; unstarted listener закрывается close при любом последующем startup refusal.
func (component *sshIdentityUploadComponent) bind() error {
	listener, err := net.Listen("tcp", component.server.Addr)
	if err != nil {
		return errors.New(sshUploadUnavailable)
	}
	component.listener = netutil.LimitListener(listener, 16)
	return nil
}

// close releases only this component's listener and server; it does not prove remote material outcome or replace the worker's explicit request join.
// close освобождает лишь listener и server данного component, не доказывая remote material outcome и не заменяя explicit request join worker.
func (component *sshIdentityUploadComponent) close() {
	if component == nil {
		return
	}
	if component.listener != nil {
		_ = component.listener.Close()
	}
	if component.server != nil {
		_ = component.server.Close()
	}
}

// worker reuses the shared HTTP shutdown adapter and retains an independent positive handler join. A failed serve closes readiness before cleanup and becomes a fatal component error even if another goroutine closed the listener.
// worker переиспользует shared HTTP shutdown adapter и сохраняет independent positive handler join. Failed serve закрывает readiness до cleanup и становится fatal component error также при чужом закрытии listener.
func (component *sshIdentityUploadComponent) worker(shutdownBase context.Context) serviceruntime.Worker {
	return func(ctx context.Context) error {
		if component == nil || component.listener == nil {
			return errors.New(sshUploadUnavailable)
		}
		component.started.Store(true)
		component.server.BaseContext = func(net.Listener) context.Context { return ctx }
		serve := func() error {
			err := component.server.ServeTLS(component.listener, "", "")
			if ctx.Err() == nil {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(shutdownBase), component.shutdown)
				defer cancel()
				return errors.Join(errors.New(sshUploadUnavailable), component.shutdownHTTP(cleanup, shutdownBase))
			}
			return err
		}
		return sharedhttp.ShutdownWorker(serve, func(stop context.Context) error { return component.shutdownHTTP(stop, shutdownBase) }, component.shutdown)(ctx)
	}
}

// shutdownHTTP closes admission before graceful shutdown and then separately waits for actual handlers. Serve termination or timeout cannot stand in for that join.
// shutdownHTTP закрывает admission до graceful shutdown, затем отдельно ожидает actual handlers. Возврат Serve либо timeout не заменяет join.
func (component *sshIdentityUploadComponent) shutdownHTTP(stop, base context.Context) error {
	_ = component.gate.Close()
	err := component.server.Shutdown(stop)
	if err != nil {
		err = errors.Join(err, component.server.Close())
	}
	join, cancel := context.WithTimeout(context.WithoutCancel(base), component.shutdown)
	defer cancel()
	if component.gate.Wait(join) != nil {
		return errors.Join(err, errors.New(sshUploadUnjoined))
	}
	return err
}

// joined permits dependency closure only before serving began or after the real admission-closed request join; an unknown/unjoined shutdown keeps its owner transport open until process failure.
// joined разрешает closure dependencies лишь до serving либо после actual admission-closed request join; unknown/unjoined shutdown оставляет owner transport открытым до process failure.
func (component *sshIdentityUploadComponent) joined() bool {
	return component == nil || !component.started.Load() || component.gate.Joined()
}
