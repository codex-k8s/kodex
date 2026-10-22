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

// errCommand is the fixed administrative-CLI failure used instead of echoing private paths, arguments or
// parser diagnostics.
// errCommand — фиксированная ошибка административного CLI вместо вывода private paths, arguments и parser
// diagnostics.
var errCommand = errors.New("secret draft key command failed")

// main runs the administrative command and prints only the fixed command error before exiting 1; detailed
// internal errors never become CLI output here.
// main выполняет административную команду и перед выходом с кодом 1 выводит лишь фиксированную command
// error; подробные внутренние ошибки здесь не выводятся.
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, errCommand.Error())
		os.Exit(1)
	}
}

// run accepts only named generate/rotate/check/copy/guard-check commands with exact flag sets and no
// positional remainder, discards parser output and maps failures to errCommand; only safe check summaries
// are emitted.
// run принимает лишь generate/rotate/check/copy/guard-check с явными наборами flags и без оставшихся
// positional arguments, скрывает parser output и отображает failures в errCommand; выводит лишь safe check
// summaries.
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

// checkGuard reads at most 1MiB plus an overflow byte from the supplied stream, decodes a ConfigMap and
// applies the runtime guard parser; it emits null for genesis or revision/digest only, never modifies
// guard state.
// checkGuard читает из переданного потока до 1МиБ с дополнительным overflow byte, декодирует ConfigMap и
// применяет runtime guard parser; выводит null для genesis либо лишь revision/digest, guard state не
// меняет.
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
