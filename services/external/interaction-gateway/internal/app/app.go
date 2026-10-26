// Package app composes the optional interaction gateway, its owner RPC client, adapter and managed worker lifecycle.
// RU: Пакет app собирает необязательный interaction gateway, его owner RPC клиент, адаптер и управляемый жизненный цикл worker.
package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/mattermost"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/usertext"
	"github.com/google/uuid"
)

const (
	// issuerUID pins the workload grant issuer's expected file owner, not the identity of an external chat user.
	// RU: issuerUID закрепляет ожидаемого владельца файла издателя workload grant, а не идентичность внешнего пользователя чата.
	issuerUID = 29001
	// issuerGID pins the projected application grant's expected issuer group for the protected RPC client.
	// RU: issuerGID закрепляет ожидаемую группу издателя спроецированного application grant для защищённого RPC клиента.
	issuerGID = 29000
)

// Run initializes telemetry, protected owner RPC, the adapter and technical HTTP before starting delivery, invocation and source workers.
// RU: Run создаёт telemetry, защищённый owner RPC, адаптер и технический HTTP до запуска worker доставки, вызовов и источников.
// It marks readiness around the worker lifetime and uses the independent shutdown base for bounded joins and telemetry cleanup.
// RU: Функция меняет readiness на границах жизни worker и использует независимую базу shutdown для ограниченного ожидания и очистки telemetry.
func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	telemetry, err := sharedobservability.NewRuntimeFromEnv(startup, serviceName, buildVersion)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, serviceruntime.RunShutdown(shutdownBase,
			serviceruntime.ShutdownOperation{Name: "tracing", Timeout: 5 * time.Second, Run: telemetry.ShutdownTracing},
			serviceruntime.ShutdownOperation{Name: "error reporting", Timeout: 5 * time.Second, Run: telemetry.FlushSentry},
		))
	}()
	logger := telemetry.Logger(os.Stdout)
	metrics := sharedobservability.NewMetrics(metricsSubsystem, buildVersion, map[string]string{})
	readiness := serviceruntime.NewReadiness()
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{ServiceIdentity: true,
		RPCProfile: config.RPCProfile, CallerWorkload: serviceName,
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName, CAFile: config.ControlPlaneCAFile,
		ClientCertificateFile: config.ControlPlaneCertificateFile, ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile, ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID,
		DialTimeout: config.RequestTimeout, Operations: controlplaneclient.InteractionGatewayOperations(),
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	text, err := usertext.New()
	if err != nil {
		return err
	}
	adapter, err := mattermost.New(mattermost.Config{
		CredentialDirectory: config.CredentialDirectory, ProxyURL: config.EgressProxyURL,
		AllowedHosts: config.AllowedHosts, Timeout: config.OperationTimeout,
	}, text)
	if err != nil {
		return err
	}
	technical, err := httpserver.New(httpserver.StandardProcessConfig(config.TechnicalListen), readiness, metrics.PrometheusHandler())
	if err != nil {
		return err
	}
	if err := technical.Listen(); err != nil {
		return err
	}
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	sources := newSourceManager(control.Interaction, adapter, logger, config)
	workers := serviceruntime.StartWorkers(lifecycle,
		technical.Worker(),
		runDeliveryLoop(control, adapter, logger, config),
		runInvocationLoop(control.Runtime, adapter, logger, config),
		runSourceRefresh(sources, control, logger, config),
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	readiness.Set(false, "stopping")
	metrics.SetReady(false)
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "interaction workers", Timeout: config.ShutdownTimeout / 3, Run: workers.Wait},
		serviceruntime.ShutdownOperation{Name: "interaction sources", Timeout: config.ShutdownTimeout / 3, Run: sources.Close},
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: config.ShutdownTimeout / 6, Run: technical.Shutdown},
	)
	return errors.Join(err, shutdownErr)
}

// runDeliveryLoop polls sequential delivery work within an operation deadline and logs only transitions into or out of degradation.
// RU: runDeliveryLoop опрашивает последовательную доставку в пределах срока операции и журналирует только переходы деградации и восстановления.
func runDeliveryLoop(control *controlplaneclient.Client, adapter *mattermost.Adapter, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.PollInterval)
		defer ticker.Stop()
		degraded := false
		for {
			cycle, cancel := context.WithTimeout(ctx, config.OperationTimeout)
			err := processDeliveries(cycle, control.Interaction, adapter, config)
			cancel()
			if err != nil && !degraded {
				degraded = true
				logger.WarnContext(ctx, "interaction delivery degraded", "error_class", "control_plane_or_adapter")
			} else if err == nil && degraded {
				degraded = false
				logger.InfoContext(ctx, "interaction delivery restored")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

// stableKey derives a deterministic UUID from two NUL-separated components; it supplies idempotency identity, not authorization.
// RU: stableKey выводит детерминированный UUID из двух компонентов с разделителем NUL; это идентичность идемпотентности, а не полномочие.
func stableKey(left, right string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(left+"\x00"+right)).String()
}
