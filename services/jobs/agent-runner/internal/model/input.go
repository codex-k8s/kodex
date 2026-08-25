// Package model читает канонический immutable runner contract.
package model

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

type Input = runtimecontract.RunnerInput
type TLSBinding = runtimecontract.RuntimeTLSBinding

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
