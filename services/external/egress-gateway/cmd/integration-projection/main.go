// This command materializes a verified local initial projection for OpenAPI egress.
// Команда материализует проверенную локальную начальную проекцию OpenAPI egress.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/projectionfiles"
)

// main runs the bounded projection command and exits on a closed diagnostic.
// main запускает ограниченную команду проекции и завершает её с закрытой диагностикой.
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "integration projection failed:", err)
		os.Exit(1)
	}
}

// run validates source and policy, resolves approved hosts, and writes private artifacts.
// run проверяет источник и политику, разрешает допущенные узлы и записывает закрытые артефакты.
func run(ctx context.Context) error {
	baseFile := flag.String("policy", "", "gateway policy file")
	revision := flag.String("revision", "", "expected gateway policy revision")
	digest := flag.String("digest", "", "expected gateway policy digest")
	source := flag.String("origins-file", "", "bounded JSON array of exact HTTPS hostnames; empty means first run")
	generation := flag.Int64("generation", 1, "owner projection generation")
	resolv := flag.String("resolv-conf", "/etc/resolv.conf", "trusted DNS resolver configuration")
	output := flag.String("output", "", "new output directory")
	flag.Parse()
	if flag.NArg() != 0 || *baseFile == "" || *output == "" || *generation < 1 {
		return errors.New("projection arguments are invalid")
	}
	base, err := policy.LoadFile(*baseFile, *revision, *digest)
	if err != nil {
		return err
	}
	hosts := []string{}
	if *source != "" {
		file, err := os.Open(*source)
		if err != nil {
			return errors.New("open origin source")
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, 16<<10+1))
		if err != nil || len(raw) > 16<<10 {
			return errors.New("origin source exceeds bound")
		}
		if json.Unmarshal(raw, &hosts) != nil || hosts == nil {
			return errors.New("origin source is invalid")
		}
	}
	servers, err := dnsresolver.LoadSystemServers(*resolv)
	if err != nil {
		return err
	}
	resolver, err := dnsresolver.New(base.DNS(), servers, nil, nil)
	if err != nil {
		return err
	}
	// DNS snapshot адаптируется к shared producer без обхода resolver validation.
	document, err := shared.Produce(ctx, hosts, *generation, base.Digest(), resolverAdapter{resolver})
	if err != nil {
		return err
	}
	files, err := shared.RenderFiles(document)
	if err != nil {
		return err
	}
	return projectionfiles.Write(*output, files)
}

// resolverAdapter maps validated DNS snapshots into the shared producer contract.
// resolverAdapter преобразует проверенные снимки DNS в контракт общего производителя.
type resolverAdapter struct{ *dnsresolver.Resolver }

// Resolve refreshes the host through the approved resolver and preserves expiry.
// Resolve обновляет узел через утверждённый resolver и сохраняет срок действия.
func (r resolverAdapter) Resolve(ctx context.Context, host string) (shared.Snapshot, error) {
	snapshot, err := r.Resolver.Refresh(ctx, host)
	return shared.Snapshot{Addresses: snapshot.Addresses, ExpiresAt: snapshot.ExpiresAt}, err
}
