package app

import "errors"

// brokerDependencyJoin reports positive local completion for a component that can retain shared dependencies.
// brokerDependencyJoin сообщает подтверждённое завершение компонента, использующего общие зависимости.
type brokerDependencyJoin interface{ joined() bool }

// closeBrokerDependency requires every dependent component to join before closing the shared resource.
// Refusal keeps dependencies available to unfinished cleanup and surfaces a fatal process result.
// closeBrokerDependency требует завершения всех потребителей перед закрытием общего ресурса.
// Отказ сохраняет зависимости для незавершённой очистки и приводит к ошибочному завершению процесса.
func closeBrokerDependency(closeDependency func() error, consumers ...brokerDependencyJoin) error {
	for _, consumer := range consumers {
		if consumer == nil || !consumer.joined() {
			return errors.New(brokerRPCUnjoined)
		}
	}
	if closeDependency == nil {
		return errors.New(brokerRPCUnavailable)
	}
	return closeDependency()
}
