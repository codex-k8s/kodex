package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/mailpolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "mail projection failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	source := flag.String("configuration", "", "exact typed mailbox configuration file")
	baseFile := flag.String("policy", "", "gateway policy file")
	revision := flag.String("revision", "", "expected gateway policy revision")
	digest := flag.String("digest", "", "expected gateway policy digest")
	resolv := flag.String("resolv-conf", "/etc/resolv.conf", "trusted DNS resolver configuration")
	output := flag.String("output", "", "new output directory")
	flag.Parse()
	if flag.NArg() != 0 || *source == "" || *output == "" {
		return errors.New("projection arguments are invalid")
	}
	base, err := policy.LoadFile(*baseFile, *revision, *digest)
	if err != nil {
		return err
	}
	servers, err := dnsresolver.LoadSystemServers(*resolv)
	if err != nil {
		return err
	}
	resolver, err := dnsresolver.New(base.DNS(), servers, nil, nil)
	if err != nil {
		return err
	}
	file, err := os.Open(*source)
	if err != nil {
		return errors.New("open mailbox configuration")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 4<<20+1))
	if err != nil || len(raw) > 4<<20 {
		return errors.New("mailbox configuration exceeds bound")
	}
	document, err := mailpolicy.Produce(ctx, raw, base, resolver)
	if err != nil {
		return err
	}
	files, err := mailpolicy.RenderFiles(document)
	if err != nil {
		return err
	}
	if err := os.Mkdir(*output, 0700); err != nil {
		return errors.New("create projection output directory")
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(*output, name), value, 0600); err != nil {
			return errors.New("write projection artifact")
		}
	}
	return nil
}
