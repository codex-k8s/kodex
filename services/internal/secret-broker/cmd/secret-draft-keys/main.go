package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/stagingcrypto"
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
