package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/codex-k8s/kodex/libs/go/observability/errorclass"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// natsStartupStage identifies only the fixed NATS phases owned by this gateway.
// Values do not originate in URLs, file names, server replies or credentials.
// natsStartupStage обозначает только фиксированные NATS-этапы этого gateway.
// Значения не берутся из URL, имён файлов, ответов сервера либо реквизитов.
type natsStartupStage uint8

const (
	// natsStageConnect retains the existing initial-connect phase.
	// natsStageConnect сохраняет прежний этап первого подключения.
	natsStageConnect natsStartupStage = iota
	// natsStageVerify retains the existing initial-flush phase.
	// natsStageVerify сохраняет прежний этап начального flush.
	natsStageVerify
	// natsStageLoadCA identifies the existing RootCAs option application.
	// natsStageLoadCA обозначает прежнее применение опции RootCAs.
	natsStageLoadCA
	// natsStageLoadClientCertificate identifies the existing ClientCert option.
	// natsStageLoadClientCertificate обозначает прежнюю опцию ClientCert.
	natsStageLoadClientCertificate
	// natsStageLoadCredentials identifies the existing UserCredentials option.
	// natsStageLoadCredentials обозначает прежнюю опцию UserCredentials.
	natsStageLoadCredentials
)

// natsStartupError keeps typed causes private while exposing only a closed
// stage and class through the unchanged process error-formatting boundary.
// natsStartupError сохраняет типизированную причину приватно, выдавая лишь
// закрытые этап и класс через прежнюю границу форматирования ошибки процесса.
type natsStartupError struct {
	stage natsStartupStage
	cause error
}

// Error never formats its cause, URLs, paths, server replies or JWT material.
// Error никогда не форматирует причину, URL, пути, ответы сервера либо JWT.
func (failure *natsStartupError) Error() string {
	return natsStageLabel(failure.stage) + " error_class=" + natsFailureClass(failure.stage, failure.cause)
}

// Unwrap permits private errors.Is/As classification without changing cause identity.
// Unwrap разрешает приватную errors.Is/As проверку, не меняя идентичность причины.
func (failure *natsStartupError) Unwrap() error { return failure.cause }

// Format closes every formatting verb over the same public diagnostic so even
// debug formatting cannot serialize the wrapper's private fields.
// Format замыкает все способы форматирования на той же публичной диагностике,
// чтобы даже debug-формат не сериализовал приватные поля wrapper.
func (failure *natsStartupError) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, failure.Error())
}

// natsStartupFailure retains the first precise option-stage marker instead of
// hiding it under the outer connect stage. It adds no retry or logging effect.
// natsStartupFailure сохраняет первый точный marker этапа опции, не скрывая его
// внешним этапом connect. Повторов либо дополнительных логов не появляется.
func natsStartupFailure(stage natsStartupStage, cause error) error {
	if direct, ok := cause.(*natsStartupError); ok {
		return direct
	}
	var marked *natsStartupError
	if errors.As(cause, &marked) {
		stage = marked.stage
	}
	return &natsStartupError{stage: stage, cause: cause}
}

// diagnosticNATSOption wraps only an option's existing error, preserving its
// application count, configuration mutations and SDK callbacks unchanged.
// diagnosticNATSOption оборачивает лишь прежнюю ошибку опции, сохраняя число
// применений, изменения конфигурации и обратные вызовы SDK без изменений.
func diagnosticNATSOption(stage natsStartupStage, option nats.Option) nats.Option {
	return func(options *nats.Options) error {
		if err := option(options); err != nil {
			return natsStartupFailure(stage, err)
		}
		return nil
	}
}

// natsStageLabel maps the closed registry to fixed text, including unknown input.
// natsStageLabel переводит закрытый реестр в фиксированный текст, включая unknown.
func natsStageLabel(stage natsStartupStage) string {
	switch stage {
	case natsStageConnect:
		return "connect realtime NATS consumer"
	case natsStageVerify:
		return "verify realtime NATS consumer"
	case natsStageLoadCA:
		return "load realtime NATS CA"
	case natsStageLoadClientCertificate:
		return "load realtime NATS client certificate"
	case natsStageLoadCredentials:
		return "load realtime NATS credentials"
	default:
		return "realtime NATS startup"
	}
}

// natsFailureClass gives permanent authentication/TLS/configuration evidence
// precedence over network wrappers. SDK ErrNoServers is not proof of refusal:
// nats.go can discard the original dial cause. No Error() text is inspected.
// natsFailureClass отдаёт постоянным auth/TLS/configuration-признакам приоритет
// над сетевыми wrapper. ErrNoServers не доказывает отказ соединения: nats.go
// может потерять исходную dial-причину. Текст Error() не анализируется.
func natsFailureClass(stage natsStartupStage, cause error) string {
	for _, auth := range []error{nats.ErrAuthorization, nats.ErrAuthExpired, nats.ErrAuthRevoked, nats.ErrAccountAuthExpired, nats.ErrPermissionViolation} {
		if errors.Is(cause, auth) {
			return "authorization"
		}
	}
	if class := errorclass.TLS(cause); class != "unknown" {
		return class
	}
	if errors.Is(cause, nats.ErrTLS) {
		return "tls_handshake"
	}
	var path *os.PathError
	if errors.As(cause, &path) {
		return "filesystem"
	}
	for _, credential := range []error{nkeys.ErrNoSeedFound, nkeys.ErrInvalidNkeySeed, nkeys.ErrInvalidUserSeed} {
		if errors.Is(cause, credential) {
			return "credentials_configuration"
		}
	}
	switch stage {
	case natsStageLoadCA, natsStageLoadClientCertificate:
		return "tls_configuration"
	case natsStageLoadCredentials:
		return "credentials_configuration"
	}
	if errors.Is(cause, nats.ErrSecureConnRequired) || errors.Is(cause, nats.ErrSecureConnWanted) {
		return "tls_configuration"
	}
	if errors.Is(cause, nats.ErrBadTimeout) || errors.Is(cause, nats.ErrInvalidArg) {
		return "configuration"
	}
	if errors.Is(cause, context.Canceled) {
		return "canceled"
	}
	if errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, nats.ErrTimeout) {
		return "timeout"
	}
	if errors.Is(cause, nats.ErrNoServers) {
		return "no_servers"
	}
	return errorclass.Network(cause)
}
