// Package model aliases and decodes the canonical immutable runner contract.
// Пакет model предоставляет псевдонимы и декодирует канонический неизменяемый контракт runner.
package model

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// Input aliases the canonical RunnerInput contract without creating another input schema or decoder-owned state.
// Input является псевдонимом канонического контракта RunnerInput без создания второй входной схемы или состояния отдельного декодера.
type Input = runtimecontract.RunnerInput

// TLSBinding aliases the canonical runtime TLS binding so callers use the same wire fields and validation source.
// TLSBinding является псевдонимом канонической привязки TLS выполнения, сохраняя общий набор wire-полей и источник проверки.
type TLSBinding = runtimecontract.RuntimeTLSBinding

// DecodeInput opens a clean absolute path, bounds the read to MaximumRunnerInputBytes and delegates all payload validation to the canonical decoder.
// Opening follows ordinary os.Open semantics; this helper does not independently verify ownership, reject symlinks or authenticate the mounted source.
// DecodeInput открывает нормализованный абсолютный путь, ограничивает чтение MaximumRunnerInputBytes и передаёт проверку содержимого каноническому декодеру.
// Открытие использует обычную семантику os.Open; функция отдельно не проверяет владельца, не запрещает ссылки и не аутентифицирует смонтированный источник.
func DecodeInput(path string) (Input, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Input{}, errors.New("runtime input path is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return Input{}, errors.New("open runtime input")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, runtimecontract.MaximumRunnerInputBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > runtimecontract.MaximumRunnerInputBytes {
		return Input{}, errors.New("read runtime input")
	}
	return runtimecontract.DecodeRunnerInput(raw)
}
