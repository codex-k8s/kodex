package app

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	controlowner "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/controlplane"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/kubernetesoperation"
	target "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/kubernetestarget"
)

// maximumKubernetesStreams bounds process-local retained buffers independently of owner effect authority.
// maximumKubernetesStreams ограничивает process-local retained buffers независимо от owner effect authority.
const maximumKubernetesStreams = 16

// configureOperations creates private stream consumers after shared dependencies and before the startup barrier.
// The caller registers Close immediately; constructors perform no target I/O or external effects.
// configureOperations создаёт private stream consumers после shared dependencies и до startup barrier.
// Caller немедленно регистрирует Close; constructors не выполняют target I/O либо external effects.
func (upload *kubernetesIdentityUpload) configureOperations(lifecycle context.Context, owner *controlowner.Owner, namespace string) error {
	if upload == nil || upload.streams != nil || upload.operations != nil || upload.resources == nil {
		return errors.New(kubernetesUploadUnavailable)
	}
	native, err := target.NewPodStreamPreparer(upload.store, namespace)
	if err != nil {
		return err
	}
	streams, err := kubernetesoperation.NewStreamOperations(lifecycle, owner, native, maximumKubernetesStreams)
	if err != nil {
		return err
	}
	upload.streams = streams
	operations, err := kubernetesoperation.NewOperations(owner, upload.resources, streams)
	if err != nil {
		stop, cancel := context.WithTimeout(context.WithoutCancel(lifecycle), upload.shutdown)
		defer cancel()
		return errors.Join(err, streams.Close(stop))
	}
	upload.streams, upload.operations = streams, operations
	return nil
}

// streamWorker joins private transports and completion workers when the common worker group stops.
// The independent shutdown budget never turns a timeout into dependency-release permission.
// streamWorker присоединяет private transports/completion workers при остановке общей worker group.
// Независимый shutdown budget не превращает timeout в разрешение закрыть dependencies.
func (upload *kubernetesIdentityUpload) streamWorker(shutdownBase context.Context) serviceruntime.Worker {
	return func(ctx context.Context) error {
		if upload == nil || upload.streams == nil || shutdownBase == nil {
			return errors.New(kubernetesUploadUnavailable)
		}
		<-ctx.Done()
		stop, cancel := context.WithTimeout(shutdownBase, upload.shutdown)
		defer cancel()
		return upload.streams.Close(stop)
	}
}
