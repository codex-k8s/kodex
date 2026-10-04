// Package imageinventory наблюдает закрытый toolchain в read-only final rootfs.
package imageinventory

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const Mode = "image-tool-inventory"
const outputPath = "/tmp/kodex-tool-inventory.json"

var invalid = errors.New("image tool inventory probe failed")
var versionPattern = regexp.MustCompile(`(?:^|[^0-9])v?([0-9]+\.[0-9]+(?:\.[0-9]+){0,2}(?:[-+][A-Za-z0-9.-]{1,48})?)`)

// Ввод назначает wrapper, а не recipe: credentials и runtime input не читаются.
func Run(ctx context.Context, args []string) error {
	if len(args) != 5 || os.Geteuid() != 0 || args[1] != Mode {
		return invalid
	}
	manifest := runtimecontract.ImageToolManifest{Schema: runtimecontract.ImageInventorySchema,
		SpecSHA256: args[2], ImmutableBuildSHA256: args[3], RuntimeContractSHA256: args[4],
		Platform: runtime.GOOS + "/" + runtime.GOARCH, Tools: []runtimecontract.ImageToolObservation{}}
	for _, probe := range runtimecontract.ImageToolProbes() {
		manifest.Tools = append(manifest.Tools, runtimecontract.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required})
	}
	if manifest.Validate() != nil {
		return invalid
	}
	manifest.Tools = nil
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	root, err := os.OpenRoot("/image")
	if err != nil {
		return invalid
	}
	defer root.Close()
	// Hash и наблюдение читают только финализированный rootfs; запись идёт снаружи.
	for _, probe := range runtimecontract.ImageToolProbes() {
		if ctx.Err() != nil {
			return invalid
		}
		item := observe(ctx, root, probe)
		manifest.Tools = append(manifest.Tools, item)
	}
	if manifest.Validate() != nil {
		return invalid
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return invalid
	}
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
	if err != nil {
		return invalid
	}
	_, writeErr := file.Write(raw)
	if closeErr := file.Close(); writeErr != nil || closeErr != nil {
		return invalid
	}
	return nil
}

func observe(ctx context.Context, root *os.Root, probe runtimecontract.ImageToolProbe) runtimecontract.ImageToolObservation {
	item := runtimecontract.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required}
	for _, path := range probe.Paths {
		file, err := root.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			continue
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Size() < 1 || info.Size() > 256<<20 {
			_ = file.Close()
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(file, (256<<20)+1))
		_ = file.Close()
		if err != nil || len(raw) > 256<<20 {
			continue
		}
		item.Path, item.SHA256, item.Status = path, runtimecontract.ImageInventorySHA256(raw), "PROBE_FAILED"
		clear(raw)
		if version, ok := runProbe(ctx, path, probe.Args); ok {
			item.Version, item.Status = version, "VERIFIED"
		} else if probe.Name == "goimports" {
			file, err := root.Open(strings.TrimPrefix(path, "/"))
			if err == nil {
				info, readErr := buildinfo.Read(file)
				_ = file.Close()
				readinessOutput, ready := runProbeReadiness(ctx, path, probe.Args)
				clear(readinessOutput)
				if readErr == nil && ready && info != nil {
					match := versionPattern.FindStringSubmatch(" " + info.Main.Version)
					if len(match) == 2 {
						item.Version, item.Status = match[1], "VERIFIED"
					}
				}
			}
		}
		return item
	}
	return item
}

type boundedOutput struct {
	bytes.Buffer
	overflow bool
}

func (output *boundedOutput) Write(raw []byte) (int, error) {
	count := len(raw)
	remaining := 4096 - output.Len()
	if len(raw) > remaining {
		output.overflow = true
		raw = raw[:remaining]
	}
	_, _ = output.Buffer.Write(raw)
	return count, nil
}

func runProbe(ctx context.Context, path string, args []string) (string, bool) {
	raw, ok := runProbeReadiness(ctx, path, args)
	defer clear(raw)
	if !ok {
		return "", false
	}
	match := versionPattern.FindSubmatch(raw)
	if len(match) != 2 {
		return "", false
	}
	return string(match[1]), true
}

func runProbeReadiness(ctx context.Context, path string, args []string) ([]byte, bool) {
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(call, path, args...)
	command.Dir = "/"
	command.Env = []string{"PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "HOME=/nonexistent", "LANG=C", "LC_ALL=C", "NO_COLOR=1", "COREPACK_ENABLE_NETWORK=0", "GOTOOLCHAIN=local"}
	command.SysProcAttr = &syscall.SysProcAttr{Chroot: "/image", Credential: &syscall.Credential{Uid: 10001, Gid: 10001, NoSetGroups: true}, Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
	command.WaitDelay = 100 * time.Millisecond
	var output boundedOutput
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	if err != nil || output.overflow {
		clear(output.Bytes())
		return nil, false
	}
	return output.Bytes(), true
}
