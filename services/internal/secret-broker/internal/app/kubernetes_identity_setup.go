package app

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	sharedhttp "github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	controlowner "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/controlplane"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/kubernetesidentity"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/kubernetesoperation"
	target "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/kubernetestarget"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/transport/identityupload"
	"golang.org/x/net/netutil"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// kubernetesUploadUnavailable hides listener and request details from lifecycle errors.
	// kubernetesUploadUnavailable скрывает listener и параметры запросов из ошибок lifecycle.
	kubernetesUploadUnavailable = "managed Kubernetes upload lifecycle is unavailable"
	// kubernetesUploadUnjoined marks incomplete handler cleanup without claiming remote outcome.
	// kubernetesUploadUnjoined отмечает незавершённую очистку handlers, не утверждая исход внешнего эффекта.
	kubernetesUploadUnjoined = "managed Kubernetes upload handlers did not join"
)

// kubernetesIdentityUpload owns only the direct TLS setup server and its existing owner/store checks.
// No credential resolver, background retry engine or additional authority is introduced here.
// kubernetesIdentityUpload владеет лишь direct TLS setup server и existing owner/store checks.
// Credential resolver, background retry engine и дополнительные полномочия здесь не создаются.
type kubernetesIdentityUpload struct {
	server        *http.Server
	store         *kubernetesstore.Store
	control       *controlplaneclient.Client
	health        *kubernetesoperation.Health
	resources     *kubernetesoperation.ResourceOperations
	operations    *kubernetesoperation.Operations
	streams       *kubernetesoperation.StreamOperations
	listener      net.Listener
	gate          *sharedhttp.RequestGate
	shutdown      time.Duration
	started       atomic.Bool
	serveReturned atomic.Bool
}

// validateKubernetesIdentityUpload permits either a wholly absent setup lane or its complete TLS config.
// A private purpose namespace cannot share the ordinary runtime, draft or platform service namespace.
// validateKubernetesIdentityUpload допускает полностью отсутствующую setup lane либо полный TLS config.
// Private purpose namespace не разделяется с ordinary runtime, draft либо platform service namespace.
func (config Config) validateKubernetesIdentityUpload() error {
	fields := []string{config.KubernetesIdentityNamespace, config.KubernetesUploadListen, config.KubernetesUploadOrigin, config.KubernetesUploadCertificateFile, config.KubernetesUploadPrivateKeyFile}
	count := 0
	for _, field := range fields {
		if field != "" {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	origin, err := url.Parse(config.KubernetesUploadOrigin)
	if count != len(fields) || err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawPath != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || origin.Opaque != "" ||
		len(validation.IsDNS1123Label(config.KubernetesIdentityNamespace)) != 0 || config.KubernetesIdentityNamespace == config.RuntimeNamespace || config.KubernetesIdentityNamespace == config.DraftNamespace || config.KubernetesIdentityNamespace == "kodex-system" ||
		!filepath.IsAbs(config.KubernetesUploadCertificateFile) || !filepath.IsAbs(config.KubernetesUploadPrivateKeyFile) || filepath.Clean(config.KubernetesUploadCertificateFile) != config.KubernetesUploadCertificateFile || filepath.Clean(config.KubernetesUploadPrivateKeyFile) != config.KubernetesUploadPrivateKeyFile ||
		config.KubernetesUploadListen == config.GRPCListen || config.KubernetesUploadListen == config.TechnicalListen {
		return errors.New("managed Kubernetes identity upload configuration is invalid")
	}
	_, port, err := net.SplitHostPort(config.KubernetesUploadListen)
	if err != nil || port != "9444" {
		return errors.New("managed Kubernetes identity upload listen address is invalid")
	}
	return nil
}

// newKubernetesIdentityUpload creates the opt-in purpose lane from trusted composition only.
// The existing broker service account accesses private storage, never the selected target API.
// newKubernetesIdentityUpload создаёт opt-in purpose lane только из trusted composition.
// Existing broker service account обращается к private storage, не к selected target API.
func newKubernetesIdentityUpload(config Config, owner *controlowner.Owner, control *controlplaneclient.Client) (*kubernetesIdentityUpload, error) {
	if err := config.validateKubernetesIdentityUpload(); err != nil {
		return nil, err
	}
	if config.KubernetesIdentityNamespace == "" {
		return nil, nil
	}
	store, err := kubernetesstore.InCluster(config.KubernetesIdentityNamespace)
	if err != nil {
		return nil, err
	}
	service, err := kubernetesidentity.New(owner, target.IdentitySetupVerifier{}, store, store, config.ClaimantID, config.KubernetesIdentityNamespace)
	if err != nil {
		return nil, err
	}
	handler, err := identityupload.New(service, config.KubernetesUploadOrigin)
	if err != nil {
		return nil, err
	}
	health, err := kubernetesoperation.NewHealth(owner, store, target.IdentityHealthVerifier{}, config.KubernetesIdentityNamespace)
	if err != nil {
		return nil, err
	}
	certificate, err := tls.LoadX509KeyPair(config.KubernetesUploadCertificateFile, config.KubernetesUploadPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load managed Kubernetes identity upload TLS identity")
	}
	gate, err := sharedhttp.NewRequestGate(handler, 8)
	if err != nil {
		return nil, err
	}
	server, err := identityupload.NewServer(config.KubernetesUploadListen, handler, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientAuth: tls.NoClientCert})
	if err != nil {
		return nil, err
	}
	server.Handler = gate
	native, err := target.NewResourcePreparer(store, config.KubernetesIdentityNamespace)
	if err != nil {
		return nil, err
	}
	operations, err := kubernetesoperation.NewResourceOperations(owner, native)
	if err != nil {
		return nil, err
	}
	return &kubernetesIdentityUpload{server: server, store: store, control: control, health: health, resources: operations, gate: gate, shutdown: config.ShutdownTimeout}, nil
}

// Check requires the same protected core owner route and private store used by setup/health work, independently of resource OUTPUT readiness.
// It cannot report ready while policy, owner schema or the operator artifact is unavailable.
// Check требует тот же protected core owner route и private store setup/health, независимо от resource OUTPUT readiness.
// Не сообщает ready при недоступных policy, owner schema либо operator artifact.
func (upload *kubernetesIdentityUpload) Check(ctx context.Context) error {
	if ctx == nil || upload == nil || upload.gate == nil || upload.store == nil || upload.gate.Closed() || upload.streams == nil || !upload.streams.Available() || upload.control == nil || upload.control.KubernetesOperations == nil {
		return errors.New("managed Kubernetes identity upload owner is unavailable")
	}
	response, err := upload.control.KubernetesOperations.CheckManagedKubernetesOwnerReadiness(ctx, &cp.CheckManagedKubernetesOwnerReadinessRequest{IdentityHealthOnly: true}, grpc.ForceCodec(grpcserver.StrictProtoCodec()))
	if err != nil || ctx.Err() != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !response.GetReady() || response.GetDeliveryContractRevision() != 1 || response.GetOutputContractRevision() != 0 || response.GetInputContractRevision() != 0 || response.GetFileInputContractRevision() != 0 {
		return errors.New("managed Kubernetes identity upload owner is unavailable")
	}
	return upload.store.Check(ctx)
}

// bind reserves the upload listener before readiness without touching the selected Kubernetes target.
// bind резервирует upload listener до readiness, не обращаясь к выбранному Kubernetes target.
func (upload *kubernetesIdentityUpload) bind() error {
	if upload == nil || upload.server == nil || upload.listener != nil || upload.shutdown <= 0 || upload.shutdown > time.Minute {
		return errors.New(kubernetesUploadUnavailable)
	}
	listener, err := net.Listen("tcp", upload.server.Addr)
	if err != nil {
		return errors.New(kubernetesUploadUnavailable)
	}
	upload.listener = netutil.LimitListener(listener, 16)
	return nil
}

// close releases an unused listener or aborts transports; it does not prove positive handler join.
// close освобождает неиспользованный listener либо прерывает transport; это не подтверждение завершения handlers.
func (upload *kubernetesIdentityUpload) close() {
	if upload == nil {
		return
	}
	if upload.listener != nil {
		_ = upload.listener.Close()
	}
	if upload.server != nil {
		_ = upload.server.Close()
	}
}

// Worker serves the prebound TLS listener after startup and reuses the shared shutdown worker.
// Unexpected Serve exit closes admission and joins handlers before returning a fatal component error.
// Worker обслуживает подготовленный TLS listener после startup через общий shutdown worker.
// Неожиданный выход Serve закрывает приём и ожидает handlers перед возвратом ошибки компонента.
func (upload *kubernetesIdentityUpload) Worker() serviceruntime.Worker {
	return func(ctx context.Context) error {
		if upload == nil || upload.listener == nil || !upload.started.CompareAndSwap(false, true) {
			return errors.New(kubernetesUploadUnavailable)
		}
		upload.server.BaseContext = func(net.Listener) context.Context { return ctx }
		serve := func() error {
			defer upload.serveReturned.Store(true)
			err := upload.server.ServeTLS(upload.listener, "", "")
			if ctx.Err() == nil {
				stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), upload.shutdown)
				defer cancel()
				return errors.Join(errors.New(kubernetesUploadUnavailable), upload.shutdownHTTP(stop))
			}
			return err
		}
		return sharedhttp.ShutdownWorker(serve, upload.shutdownHTTP, upload.shutdown)(ctx)
	}
}

// shutdownHTTP refuses new handlers before graceful stop, then waits independently for actual cleanup.
// A timeout keeps dependencies alive; HTTP Close and Serve return cannot substitute for the join.
// shutdownHTTP закрывает приём до graceful stop, затем отдельно ожидает фактическую очистку handlers.
// Timeout сохраняет зависимости; HTTP Close и возврат Serve не заменяют подтверждённый join.
func (upload *kubernetesIdentityUpload) shutdownHTTP(stop context.Context) error {
	_ = upload.gate.Close()
	err := upload.server.Shutdown(stop)
	if err != nil {
		err = errors.Join(err, upload.server.Close())
	}
	join, cancel := context.WithTimeout(context.WithoutCancel(stop), upload.shutdown)
	defer cancel()
	if upload.gate.Wait(join) != nil {
		return errors.Join(err, errors.New(kubernetesUploadUnjoined))
	}
	return err
}

// joined guards shared dependencies until serving and all admitted handlers actually return.
// joined сохраняет общие зависимости до фактического выхода Serve и всех допущенных handlers.
func (upload *kubernetesIdentityUpload) joined() bool {
	return upload == nil || (upload.streams == nil || upload.streams.Joined()) && (!upload.started.Load() || upload.serveReturned.Load() && upload.gate.Joined())
}
