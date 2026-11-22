package kubernetestarget

import (
	"errors"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// ErrPortForwardProfileUnsupported signals only an operator-selected transport lacking full connection lifecycle, before any effect or native opening.
// The authoritative invocation owner independently verifies this condition before persisting a safe failure.
// ErrPortForwardProfileUnsupported обозначает лишь operator-selected transport без full connection lifecycle до effect либо native opening.
// Authoritative invocation owner независимо проверяет это условие до сохранения safe failure.
var ErrPortForwardProfileUnsupported = errors.New(managedkubernetes.ReasonPortForwardProfileUnsupported)
