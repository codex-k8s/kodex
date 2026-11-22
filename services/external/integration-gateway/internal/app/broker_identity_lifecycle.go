package app

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// errIntegrationWorkUnjoined prevents dependency closure when any actual common worker or adopted purpose activity still owns it.
// errIntegrationWorkUnjoined запрещает dependency closure, пока actual common worker либо adopted purpose activity ещё владеет зависимостью.
var errIntegrationWorkUnjoined = errors.New("integration work did not join")

// brokerIdentityConsumerSet enumerates only the two trusted composition slots, preserving fixed cardinality and excluding caller registration.
// brokerIdentityConsumerSet перечисляет лишь два trusted composition slots с fixed cardinality без caller registration.
func brokerIdentityConsumerSet(consumers *brokerIdentityConsumers) []brokerIdentityConsumer {
	if consumers == nil {
		return nil
	}
	result := make([]brokerIdentityConsumer, 0, 2)
	if consumers.Kubernetes != nil {
		result = append(result, consumers.Kubernetes)
	}
	if consumers.SSH != nil {
		result = append(result, consumers.SSH)
	}
	return result
}

// monitorBrokerIdentityConsumers signals fatal lifecycle failure through the application's own cancellation path; a closed failure channel is not a join receipt.
// monitorBrokerIdentityConsumers передаёт fatal lifecycle failure в собственный cancellation path приложения; closed failure channel не является join receipt.
func monitorBrokerIdentityConsumers(consumers *brokerIdentityConsumers, stop context.CancelFunc) serviceruntime.Worker {
	return func(ctx context.Context) error {
		var kubernetes, ssh <-chan struct{}
		if consumers != nil {
			if consumers.Kubernetes != nil {
				kubernetes = consumers.Kubernetes.Failure()
			}
			if consumers.SSH != nil {
				ssh = consumers.SSH.Failure()
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-kubernetes:
			stop()
			return brokerIdentityWorkUnavailable
		case <-ssh:
			stop()
			return brokerIdentityWorkUnavailable
		}
	}
}

// joinIntegrationWork closes admission and cancels work before independent bounded observations; joined failures stay failures, while timeout never licenses dependency closure.
// joinIntegrationWork закрывает admission и отменяет работу до independent bounded observations; joined failure остаётся failure, timeout не разрешает dependency closure.
func joinIntegrationWork(base context.Context, budget time.Duration, workers *serviceruntime.WorkerGroup, consumers *brokerIdentityConsumers) error {
	if base == nil || budget <= 0 || workers == nil {
		return errIntegrationWorkUnjoined
	}
	workers.Stop()
	var result error
	for _, consumer := range brokerIdentityConsumerSet(consumers) {
		result = errors.Join(result, consumer.Close())
	}
	operations := []serviceruntime.ShutdownOperation{{Name: "integration workers", Timeout: budget, Run: func(ctx context.Context) error {
		observed := workers.Wait(ctx)
		if workers.Joined() {
			return workers.Wait(context.WithoutCancel(ctx))
		}
		return observed
	}}}
	for _, consumer := range brokerIdentityConsumerSet(consumers) {
		current := consumer
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "broker purpose activities", Timeout: budget, Run: func(ctx context.Context) error {
			observed := current.Wait(ctx)
			if current.Joined() {
				return current.Wait(context.WithoutCancel(ctx))
			}
			return observed
		}})
	}
	joinContext, cancelJoin := context.WithTimeout(base, budget)
	defer cancelJoin()
	result = errors.Join(result, serviceruntime.RunShutdown(joinContext, operations...))
	if !integrationWorkJoined(workers, consumers) {
		result = errors.Join(result, errIntegrationWorkUnjoined)
	}
	return result
}

// integrationWorkJoined checks all local consumers separately from their outcome; remote writer fencing remains owned by the broker and authoritative owner.
// integrationWorkJoined проверяет всех local consumers отдельно от outcome; remote writer fencing остаётся у broker и authoritative owner.
func integrationWorkJoined(workers *serviceruntime.WorkerGroup, consumers *brokerIdentityConsumers) bool {
	if workers == nil || !workers.Joined() {
		return false
	}
	for _, consumer := range brokerIdentityConsumerSet(consumers) {
		if !consumer.Joined() {
			return false
		}
	}
	return true
}

// shutdownJoinedIntegrationDependencies executes no dependency callback until all common workers and purpose activities positively joined; outcome failures are returned separately by their owner.
// shutdownJoinedIntegrationDependencies не вызывает dependency callbacks до positive join всех common workers и purpose activities; outcome failures отдельно возвращает их owner.
func shutdownJoinedIntegrationDependencies(base context.Context, workers *serviceruntime.WorkerGroup, consumers *brokerIdentityConsumers, operations ...serviceruntime.ShutdownOperation) error {
	if !integrationWorkJoined(workers, consumers) {
		return errIntegrationWorkUnjoined
	}
	return serviceruntime.RunShutdown(base, operations...)
}

// finishIntegrationOwnedResources unwinds only acquired startup dependencies in reverse order and closes none while workers, consumers or technical handlers remain unjoined.
// finishIntegrationOwnedResources освобождает лишь acquired startup dependencies в обратном порядке и не закрывает их при unjoined workers, consumers либо technical handlers.
func finishIntegrationOwnedResources(base context.Context, budget time.Duration, workers *serviceruntime.WorkerGroup, consumers *brokerIdentityConsumers, technicalShutdown func(context.Context) error, technicalJoined func() bool, dependencies []serviceruntime.ShutdownOperation) error {
	if (technicalShutdown == nil) != (technicalJoined == nil) {
		return errIntegrationWorkUnjoined
	}
	var result error
	if workers != nil {
		result = joinIntegrationWork(base, budget, workers, consumers)
	} else {
		waits := []serviceruntime.ShutdownOperation{}
		for _, consumer := range brokerIdentityConsumerSet(consumers) {
			current := consumer
			result = errors.Join(result, current.Close())
			waits = append(waits, serviceruntime.ShutdownOperation{Name: "startup purpose activities", Timeout: budget, Run: func(ctx context.Context) error {
				observed := current.Wait(ctx)
				if current.Joined() {
					return current.Wait(context.WithoutCancel(ctx))
				}
				return observed
			}})
		}
		joinContext, cancelJoin := context.WithTimeout(base, budget)
		result = errors.Join(result, serviceruntime.RunShutdown(joinContext, waits...))
		cancelJoin()
	}
	if technicalShutdown != nil {
		result = errors.Join(result, serviceruntime.RunShutdown(base, serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: budget / 2, Run: technicalShutdown}))
	}
	joined := workers == nil || workers.Joined()
	for _, consumer := range brokerIdentityConsumerSet(consumers) {
		joined = joined && consumer.Joined()
	}
	if technicalJoined != nil {
		joined = joined && technicalJoined()
	}
	if !joined {
		return errors.Join(result, errIntegrationWorkUnjoined)
	}
	reverse := make([]serviceruntime.ShutdownOperation, len(dependencies))
	for index := range dependencies {
		reverse[len(dependencies)-index-1] = dependencies[index]
	}
	return errors.Join(result, serviceruntime.RunShutdown(base, reverse...))
}
