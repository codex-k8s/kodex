package helper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// runtimeFixture opens real kernel-enforced namespaces with synthetic unprivileged ownership; installed root provenance remains a separate process qualification.
// runtimeFixture открывает настоящие kernel-enforced namespaces с synthetic unprivileged ownership; installed root provenance проверяется отдельной process qualification.
type runtimeFixture struct {
	runtime *Runtime
	profile managedssh.Profile
	root    string
}

// newRuntimeFixture builds isolated private directories without touching operator configuration, host accounts or network state.
// newRuntimeFixture создаёт изолированные private directories без изменения operator configuration, host accounts либо network state.
func newRuntimeFixture(t *testing.T) runtimeFixture {
	t.Helper()
	if os.Getuid() == 0 || os.Getgid() == 0 {
		t.Fatal("native helper tests require an unprivileged process")
	}
	base := t.TempDir()
	root, state := filepath.Join(base, "documents"), filepath.Join(base, "receipts")
	for _, name := range []string{root, state} {
		if err := os.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	profile := managedssh.Profile{Version: managedssh.Version, Revision: 1, ValidUntil: time.Now().Add(time.Hour).Unix(), UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), Groups: []uint32{}, ExecutableDigest: strings.Repeat("a", 64), ShellDigest: strings.Repeat("8", 64), StateDirectory: state, ReceiptLimit: 128, Roots: []managedssh.RootScope{{Alias: "documents", Path: root}}, Services: []managedssh.ServiceScope{}}
	runtime, err := openRuntime(t.Context(), profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	return runtimeFixture{runtime: runtime, profile: profile, root: root}
}

// request assigns a synthetic unique attempt and the actual opened profile pin; it never grants permission or chooses another helper namespace.
// request назначает synthetic unique attempt и фактический opened profile pin; permission и другой helper namespace она не назначает.
func (fixture runtimeFixture) request(operation managedssh.Operation, attempt uint64) managedssh.Request {
	value := strconv.FormatUint(attempt, 16)
	return managedssh.Request{Version: managedssh.Version, ProfileDigest: fixture.runtime.digest, AttemptID: "11111111-1111-4111-8111-" + strings.Repeat("0", 12-len(value)) + value, Operation: operation}
}

// execute requires both the real runtime operation and its production wire validator to agree about every observed outcome.
// execute требует согласия actual runtime operation и production wire validator для каждого observed outcome.
func (fixture runtimeFixture) execute(t *testing.T, request managedssh.Request) managedssh.Reply {
	t.Helper()
	reply, err := fixture.runtime.Execute(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := managedssh.EncodeReply(request, reply)
	if err != nil {
		t.Fatalf("invalid helper reply: %v", err)
	}
	decoded, err := managedssh.DecodeReply(request, raw)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

// contentDigest supplies the same full-content version used by the runtime without asserting inode identity or ownership.
// contentDigest задаёт ту же full-content version, что runtime, без утверждения inode identity либо ownership.
func contentDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// TestFileLifecycleAndVersionFence verifies actual create/read/replace/delete effects, stale-input rejection and durable recovery across reopening.
// TestFileLifecycleAndVersionFence проверяет actual create/read/replace/delete effects, отказ stale input и durable recovery после повторного открытия.
func TestFileLifecycleAndVersionFence(t *testing.T) {
	f := newRuntimeFixture(t)
	create := f.request(managedssh.FileCreate, 1)
	create.Resource, create.Path, create.Content = "documents", "report.txt", []byte("first")
	reply := f.execute(t, create)
	if reply.Status != managedssh.Applied || reply.Receipt.Version != contentDigest(create.Content) {
		t.Fatal("create was not durably applied")
	}
	read := f.request(managedssh.FileRead, 2)
	read.Resource, read.Path, read.Expected, read.Limit = create.Resource, create.Path, reply.Receipt.Version, 3
	fragment := f.execute(t, read)
	if fragment.Status != managedssh.Observed || !fragment.Fragment.Truncated || string(fragment.Fragment.Content) != "fir" {
		t.Fatal("version-bound fragment changed")
	}
	replace := f.request(managedssh.FileReplace, 3)
	replace.Resource, replace.Path, replace.Expected, replace.Content = create.Resource, create.Path, read.Expected, []byte("second")
	if f.execute(t, replace).Status != managedssh.Applied {
		t.Fatal("replace failed")
	}
	stale := f.execute(t, read)
	if stale.Status != managedssh.Rejected || stale.Code != "VERSION_MISMATCH" {
		t.Fatal("stale content version accepted")
	}
	delete := f.request(managedssh.FileDelete, 4)
	delete.Resource, delete.Path, delete.Expected = create.Resource, create.Path, contentDigest(replace.Content)
	if f.execute(t, delete).Status != managedssh.Applied {
		t.Fatal("delete failed")
	}
	if _, err := os.Stat(filepath.Join(f.root, create.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted entry remains")
	}
	reopened, err := openRuntime(t.Context(), f.profile)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	f.runtime = reopened
	recovery := f.request(managedssh.ReceiptRead, 4)
	if receipt := f.execute(t, recovery); receipt.Receipt.Status != managedssh.Applied || receipt.Receipt.Operation != managedssh.FileDelete {
		t.Fatal("durable completion changed after reopen")
	}
}

// TestUnknownIntentCannotAuthorizeReplay proves an interrupted pre-effect attempt remains UNKNOWN and cannot dispatch after reconnecting.
// TestUnknownIntentCannotAuthorizeReplay доказывает, что прерванная pre-effect attempt остаётся UNKNOWN и не отправляется после reconnect.
func TestUnknownIntentCannotAuthorizeReplay(t *testing.T) {
	f := newRuntimeFixture(t)
	request := f.request(managedssh.FileCreate, 10)
	request.Resource, request.Path, request.Content = "documents", "never-created", []byte("synthetic content")
	intent, ok := f.runtime.begin(request)
	if !ok || intent.Status != managedssh.Unknown {
		t.Fatal("intent not persisted")
	}
	f.runtime.Close()
	reopened, err := openRuntime(t.Context(), f.profile)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	f.runtime = reopened
	if reply := f.execute(t, request); reply.Status != managedssh.Rejected || reply.Code != "ATTEMPT_CONFLICT" {
		t.Fatal("unknown attempt dispatched again")
	}
	recovery := f.request(managedssh.ReceiptRead, 10)
	if reply := f.execute(t, recovery); reply.Receipt.Status != managedssh.Unknown || reply.Receipt.RequestDigest != intent.RequestDigest {
		t.Fatal("unknown receipt changed")
	}
	if _, err := os.Stat(filepath.Join(f.root, request.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("recovery performed an effect")
	}
}

// failingWriter models a lost output acknowledgement after the helper already synced its effect and receipt.
// failingWriter моделирует потерянное подтверждение вывода после sync effect и receipt helper.
type failingWriter struct{}

// Write always rejects delivery without exposing response bytes, leaving durable observation as the only recovery path.
// Write всегда отклоняет delivery без раскрытия response bytes; восстановление возможно только через durable observation.
func (failingWriter) Write([]byte) (int, error) { return 0, ErrRejected }

// TestLostOutputAcknowledgementRecoversWithoutAnotherEffect drives the production Serve boundary and observes its completed receipt from a fresh runtime.
// TestLostOutputAcknowledgementRecoversWithoutAnotherEffect проверяет production Serve boundary и completed receipt из нового runtime.
func TestLostOutputAcknowledgementRecoversWithoutAnotherEffect(t *testing.T) {
	f := newRuntimeFixture(t)
	request := f.request(managedssh.FileCreate, 11)
	request.Resource, request.Path, request.Content = "documents", "report.txt", []byte("applied once")
	raw, err := managedssh.EncodeRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.runtime.Serve(t.Context(), bytes.NewReader(raw), failingWriter{}); !errors.Is(err, ErrRejected) {
		t.Fatal("lost output was reported as delivery")
	}
	reopened, err := openRuntime(t.Context(), f.profile)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	f.runtime = reopened
	if reply := f.execute(t, f.request(managedssh.ReceiptRead, 11)); reply.Receipt.Status != managedssh.Applied {
		t.Fatal("completed effect lost its authoritative receipt")
	}
	if reply := f.execute(t, request); reply.Status != managedssh.Rejected {
		t.Fatal("effect replay was accepted")
	}
	raw, err = os.ReadFile(filepath.Join(f.root, request.Path))
	if err != nil || !bytes.Equal(raw, request.Content) {
		t.Fatal("recovery changed content")
	}
}

// TestKernelPathBoundaryRejectsLinksAndForeignScopes checks actual symlink/hardlink traversal and private inode constraints instead of validating path strings alone.
// TestKernelPathBoundaryRejectsLinksAndForeignScopes проверяет actual symlink/hardlink traversal и private inode constraints, а не только path strings.
func TestKernelPathBoundaryRejectsLinksAndForeignScopes(t *testing.T) {
	f := newRuntimeFixture(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("synthetic outside secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(f.root, "hardlink")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"link", "hardlink"} {
		request := f.request(managedssh.FileStat, 20)
		request.Resource, request.Path = "documents", name
		if reply := f.execute(t, request); reply.Status != managedssh.Rejected {
			t.Fatal("linked inode was projected")
		}
	}
	request := f.request(managedssh.FileCreate, 21)
	request.Resource, request.Path, request.Content = "unselected", "file", []byte("refused")
	if reply := f.execute(t, request); reply.Code != "SCOPE_UNAVAILABLE" {
		t.Fatal("foreign alias was admitted")
	}
}

// TestServeRejectsMalformedInputWithoutEcho verifies the process wire boundary emits no data for malformed, duplicate or oversized frames.
// TestServeRejectsMalformedInputWithoutEcho проверяет отсутствие вывода process wire boundary для malformed, duplicate либо oversized frames.
func TestServeRejectsMalformedInputWithoutEcho(t *testing.T) {
	f := newRuntimeFixture(t)
	for _, raw := range [][]byte{[]byte(`{"version":1,"version":1,"content":"synthetic sensitive input"}`), []byte(`null`), bytes.Repeat([]byte("x"), managedssh.MaximumFrame+1)} {
		var output bytes.Buffer
		if err := f.runtime.Serve(t.Context(), bytes.NewReader(raw), &output); !errors.Is(err, ErrRejected) || output.Len() != 0 {
			t.Fatal("invalid input leaked or executed")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.runtime.Execute(ctx, f.request(managedssh.SystemRead, 30)); !errors.Is(err, ErrRejected) {
		t.Fatal("cancelled request dispatched")
	}
}

// TestReceiptCapacityStopsNewEffectsWithoutDeletingEvidence verifies bounded durable storage remains closed after its operator-selected capacity is consumed.
// TestReceiptCapacityStopsNewEffectsWithoutDeletingEvidence проверяет закрытый отказ bounded durable storage после исчерпания выбранной оператором capacity без удаления evidence.
func TestReceiptCapacityStopsNewEffectsWithoutDeletingEvidence(t *testing.T) {
	f := newRuntimeFixture(t)
	f.profile.ReceiptLimit = 1
	f.runtime.Close()
	limited, err := openRuntime(t.Context(), f.profile)
	if err != nil {
		t.Fatal(err)
	}
	defer limited.Close()
	f.runtime = limited
	first := f.request(managedssh.FileCreate, 40)
	first.Resource, first.Path, first.Content = "documents", "first", []byte("retained")
	if f.execute(t, first).Status != managedssh.Applied {
		t.Fatal("first bounded effect failed")
	}
	second := f.request(managedssh.FileCreate, 41)
	second.Resource, second.Path, second.Content = "documents", "second", []byte("refused")
	if f.execute(t, second).Status != managedssh.Rejected {
		t.Fatal("receipt capacity did not stop a new effect")
	}
	if _, err := os.Stat(filepath.Join(f.root, "second")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("capacity refusal still created content")
	}
	if f.execute(t, f.request(managedssh.ReceiptRead, 40)).Receipt.Status != managedssh.Applied {
		t.Fatal("capacity pressure removed authoritative evidence")
	}
}
