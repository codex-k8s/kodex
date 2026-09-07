package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/stagingcrypto"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/stagingguard"
	corev1 "k8s.io/api/core/v1"
)

var errCommand = errors.New("secret draft key command failed")

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, errCommand.Error())
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errCommand
	}
	flags := flag.NewFlagSet("secret-draft-keys", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var inputFile, outputFile string
	var expected int64
	switch args[0] {
	case "generate":
		flags.StringVar(&outputFile, "output-file", "", "")
	case "rotate":
		flags.StringVar(&inputFile, "input-file", "", "")
		flags.StringVar(&outputFile, "output-file", "", "")
		flags.Int64Var(&expected, "expected-revision", 0, "")
	case "check":
		flags.StringVar(&inputFile, "input-file", "", "")
	case "copy":
		flags.StringVar(&inputFile, "input-file", "", "")
		flags.StringVar(&outputFile, "output-file", "", "")
	case "guard-check":
		// Snapshot приходит через private stdin, не через argv или диагностику.
	default:
		return errCommand
	}
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return errCommand
	}
	switch args[0] {
	case "generate":
		if err := stagingcrypto.GenerateFile(outputFile); err != nil {
			return errCommand
		}
	case "copy":
		if stagingcrypto.CopyFile(inputFile, outputFile) != nil {
			return errCommand
		}
	case "guard-check":
		return checkGuard(os.Stdin, output)
	case "rotate":
		if err := stagingcrypto.RotateFile(inputFile, outputFile, expected); err != nil {
			return errCommand
		}
	case "check":
		summary, err := stagingcrypto.CheckFile(inputFile)
		if err != nil {
			return errCommand
		}
		if json.NewEncoder(output).Encode(summary) != nil {
			return errCommand
		}
	}
	return nil
}

func checkGuard(input io.Reader, output io.Writer) error {
	var object corev1.ConfigMap
	raw, err := io.ReadAll(io.LimitReader(input, 1<<20+1))
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &object) != nil {
		return errCommand
	}
	manifest, err := stagingguard.InspectRecovery(&object)
	if err != nil {
		return errCommand
	}
	var summary *stagingcrypto.MaterialSummary
	if manifest != nil {
		summary = &stagingcrypto.MaterialSummary{Revision: manifest.Revision, Digest: manifest.Digest}
	}
	if json.NewEncoder(output).Encode(summary) != nil {
		return errCommand
	}
	return nil
}
