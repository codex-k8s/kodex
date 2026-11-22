package kubernetes

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"k8s.io/klog/v2"
)

// TestSSHPrivateHTTPRejectsEchoWithoutBodyLogs exercises the actual TLS request at verbose SDK logging in an isolated child process; a provider echo must not reach stdout/stderr or an SDK diagnostic on non-2xx responses.
// TestSSHPrivateHTTPRejectsEchoWithoutBodyLogs выполняет actual TLS request при verbose SDK logging в isolated child process; provider echo не должен попасть в stdout/stderr либо SDK diagnostic при non-2xx response.
func TestSSHPrivateHTTPRejectsEchoWithoutBodyLogs(t *testing.T) {
	if os.Getenv("KODEX_TEST_SSH_PRIVATE_HTTP_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSSHPrivateHTTPRejectsEchoWithoutBodyLogs$")
		child.Env = append(os.Environ(), "KODEX_TEST_SSH_PRIVATE_HTTP_CHILD=1")
		output, err := child.CombinedOutput()
		defer clear(output)
		_, secret := sshMaterialFixture()
		marker := secret.Data[sshIdentityMaterialKey]
		if bytes.Contains(output, marker) || bytes.Contains(output, []byte(base64.StdEncoding.EncodeToString(marker))) {
			t.Fatal("verbose private HTTP error echoed material into process output")
		}
		if err != nil {
			t.Fatal("isolated verbose private HTTP fixture failed")
		}
		return
	}
	settings := flag.NewFlagSet("private-identity-verbose", flag.ContinueOnError)
	klog.InitFlags(settings)
	if err := settings.Set("v", "10"); err != nil {
		t.Fatal("configure isolated SDK verbose logging")
	}
	defer klog.Flush()
	store, fixture, material := newSSHMaterialWire(t)
	fixture.createStatus, fixture.echoBody = http.StatusServiceUnavailable, true
	locator, err := store.CreateSSHIdentity(t.Context(), fixture.provision, material)
	if !errors.Is(err, sshrepository.ErrIdentityUnknown) || locator.Valid() || fixture.posts.Load() != 1 || fixture.gets.Load() != 0 {
		t.Fatal("verbose private HTTP request changed single-attempt rejection")
	}
}
