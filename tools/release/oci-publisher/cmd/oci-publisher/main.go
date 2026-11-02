// Command oci-publisher exposes bounded archive publication with file-based authentication.
// RU: Command oci-publisher публикует bounded archive с file-based authentication.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	publisher "github.com/codex-k8s/kodex/tools/release/oci-publisher"
)

// main reports only stable error classes and verified digest metadata, never upstream diagnostics.
// RU: main выводит только стабильные классы ошибок и verified digest metadata, без upstream diagnostics.
func main() {
	var input publisher.Input
	flag.StringVar(&input.Archive, "archive", "", "owned OCI archive file")
	flag.StringVar(&input.DockerConfigFile, "docker-config", "", "private Docker auth file")
	flag.StringVar(&input.Destination, "destination", "", "exact registry/repository:tag")
	flag.StringVar(&input.ExpectedDigest, "expected-digest", "", "BuildKit manifest digest")
	flag.StringVar(&input.CAFile, "ca-file", "", "optional public CA PEM file")
	flag.Int64Var(&input.ChunkBytes, "chunk-bytes", 8<<20, "bounded registry upload chunk size")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Publisher arguments rejected")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := publisher.Publish(ctx, input)
	if err != nil {
		code := "unavailable; reconcile destination digest"
		switch {
		case errors.Is(err, publisher.ErrInvalid):
			code = "input rejected"
		case errors.Is(err, publisher.ErrUnauthorized):
			code = "credential rejected"
		case errors.Is(err, publisher.ErrConflict):
			code = "destination digest conflict"
		}
		fmt.Fprintln(os.Stderr, "Publisher "+code)
		os.Exit(1)
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		os.Exit(1)
	}
}
