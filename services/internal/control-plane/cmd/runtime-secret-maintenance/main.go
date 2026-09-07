package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	op "github.com/codex-k8s/kodex/services/internal/control-plane/internal/maintenance/runtimeorphan"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if run(ctx, os.Args[1:]) != nil {
		fmt.Fprintln(os.Stderr, "Runtime secret maintenance failed: guard or dependency rejected")
		os.Exit(1)
	}
	fmt.Println("Runtime secret maintenance completed")
}
func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("runtime-secret-maintenance", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mode := flags.String("mode", "plan", "plan or apply")
	contextName := flags.String("context", "", "exact disposable context")
	source := flags.String("expected-sha", "", "exact clean source SHA")
	marker := flags.String("cluster-marker", "/var/lib/kodex-dev/cluster-identity.json", "root-owned disposable cluster marker")
	file := flags.String("plan-file", "", "private plan and receipt")
	name := flags.String("secret-name", "", "single metadata target for plan")
	confirmation := flags.String("confirm", "", "explicit reset confirmation")
	if flags.Parse(args) != nil || flags.NArg() != 0 || (*mode != "plan" && *mode != "apply" && *mode != "reset") || *contextName == "" || strings.Contains(strings.ToLower(*contextName), "prod") || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(*source) {
		return op.ErrGuard
	}
	if *mode != "reset" && *confirmation != "" {
		return op.ErrGuard
	}
	root, err := command(ctx, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	actual, err := command(ctx, "git", "rev-parse", "HEAD")
	if err != nil || actual != *source {
		return op.ErrGuard
	}
	dirty, err := command(ctx, "git", "status", "--porcelain")
	if err != nil || dirty != "" {
		return op.ErrGuard
	}
	tree, err := command(ctx, "git", "rev-parse", "HEAD^{tree}")
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(root, "tools/dev/reset-local.sh")); err != nil {
		return op.ErrGuard
	}
	if *marker != "/var/lib/kodex-dev/cluster-identity.json" {
		return op.ErrGuard
	}
	markerStat, err := command(ctx, "sudo", "-n", "stat", "-c", "%u:%g:%a:%F", "--", *marker)
	if err != nil || markerStat != "0:0:600:regular file" {
		return op.ErrGuard
	}
	raw, err := command(ctx, "sudo", "-n", "cat", "--", *marker)
	if err != nil || len(raw) > 4096 {
		return op.ErrGuard
	}
	var identity op.Cluster
	if json.Unmarshal([]byte(raw), &identity) != nil {
		return op.ErrGuard
	}
	loading := clientcmd.NewDefaultClientConfigLoadingRules()
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, &clientcmd.ConfigOverrides{})
	rawConfig, err := config.RawConfig()
	if err != nil || rawConfig.CurrentContext != *contextName {
		return op.ErrGuard
	}
	restConfig, err := config.ClientConfig()
	if err != nil {
		return op.ErrGuard
	}
	ca := restConfig.CAData
	if len(ca) == 0 {
		ca, err = os.ReadFile(restConfig.CAFile)
		if err != nil {
			return op.ErrGuard
		}
	}
	digest := sha256.Sum256(ca)
	if identity.Endpoint != restConfig.Host || identity.CA != hex.EncodeToString(digest[:]) || identity.Version != 1 {
		return op.ErrGuard
	}
	k, err := op.NewKubernetes(restConfig, identity)
	if err != nil {
		return err
	}
	cluster, err := k.Client.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil || string(cluster.UID) != identity.UID {
		return op.ErrGuard
	}
	k.OwnerCheck = op.OwnerReferences
	if *mode == "reset" {
		if *confirmation != "DELETE-KODEX-LOCAL-DATA" || *file != "" || *name != "" {
			return op.ErrGuard
		}
		return k.Reset(ctx)
	}
	if !filepath.IsAbs(*file) || *file == root || strings.HasPrefix(filepath.Clean(*file), root+string(os.PathSeparator)) {
		return op.ErrGuard
	}
	store, err := op.OpenStore(*file)
	if err != nil {
		return err
	}
	defer store.Close()
	if *mode == "plan" {
		if *name == "" {
			return op.ErrGuard
		}
		p, err := op.Prepare(ctx, k, *source, tree, *name)
		if err != nil {
			return err
		}
		return store.Save(p, true)
	}
	if *name != "" {
		return op.ErrGuard
	}
	p, err := store.Read()
	if err != nil || p.Source != *source || p.Tree != tree || p.Snapshot.Cluster != identity {
		return op.ErrGuard
	}
	s, r, err := k.Namespaces(ctx)
	if err != nil || s != p.Snapshot.System || r != p.Snapshot.Runtime {
		return op.ErrGuard
	}
	k.System = s
	k.Runtime = r
	return op.Apply(ctx, k, &p, func() error { return store.Save(p, false) })
}
func command(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", errors.New("maintenance source read failed")
	}
	return strings.TrimSpace(string(out)), nil
}
