package operator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"golang.org/x/sys/unix"
)

// BundleSchema identifies one local render record; it is not a Kubernetes publication receipt or proof that an operator performed remote qualification.
// BundleSchema обозначает один local render record; это не Kubernetes publication receipt и не доказательство remote qualification оператором.
const BundleSchema = "kodex.ssh-qualification-overlay/v1"

// maximumBundleBytes bounds the escaped qualification and its three fixed deployment objects within the shared owned-record limit.
// maximumBundleBytes ограничивает escaped qualification и три fixed deployment objects внутри общего owned-record limit.
const maximumBundleBytes = 262144

// Bundle stores only the complete deterministic operator projection; no credentials, arbitrary patch paths or additional runtime objects are accepted.
// Bundle хранит только полную deterministic operator projection; credentials, arbitrary patch paths либо дополнительные runtime objects не принимаются.
type Bundle struct {
	Schema          string          `json:"schema"`
	ArtifactSHA256  string          `json:"artifactSHA256"`
	ConfigMap       json.RawMessage `json:"configMap"`
	DeploymentPatch json.RawMessage `json:"deploymentPatch"`
	NetworkPolicy   json.RawMessage `json:"networkPolicy"`
}

// Result reports local record validation only; neither state permits network access, host installation or Kubernetes apply.
// Result сообщает только local record validation; ни одно состояние не разрешает network access, host installation либо Kubernetes apply.
type Result struct {
	Status         string `json:"status"`
	ArtifactSHA256 string `json:"artifactSHA256"`
	BundleSHA256   string `json:"bundleSHA256"`
}

// Run reads a canonical qualification from one process-owned private directory and exclusively writes or validates one immutable projection record; retries never overwrite existing or uncertain output.
// Run читает canonical qualification из одного process-owned private directory и exclusively пишет либо проверяет immutable projection record; retry не перезаписывает existing либо uncertain output.
func Run(parent context.Context, args []string) (Result, error) {
	if parent == nil || len(args) != 7 || (args[0] != "render" && args[0] != "check") || os.Getuid() != os.Geteuid() || os.Getgid() != os.Getegid() {
		return Result{}, ErrRejected
	}
	values := map[string]string{}
	for index := 1; index < len(args); index += 2 {
		key := args[index]
		if (key != "--directory" && key != "--input" && key != "--output") || values[key] != "" || args[index+1] == "" {
			return Result{}, ErrRejected
		}
		values[key] = args[index+1]
	}
	directory := values["--directory"]
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || values["--input"] == values["--output"] {
		return Result{}, ErrRejected
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return Result{}, ErrRejected
	}
	defer unix.Close(fd)
	owner := securefile.OwnedRecordIdentity{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	raw, err := securefile.ReadOwnedRecordAt(ctx, fd, values["--input"], managedssh.MaximumFrame, owner)
	if err != nil {
		return Result{}, ErrRejected
	}
	projection, err := Render(raw, time.Now().Unix())
	if err != nil {
		return Result{}, ErrRejected
	}
	bundle := Bundle{Schema: BundleSchema, ArtifactSHA256: projection.ArtifactSHA256, ConfigMap: projection.ConfigMap, DeploymentPatch: projection.DeploymentPatch, NetworkPolicy: projection.NetworkPolicy}
	encoded, err := json.Marshal(bundle)
	if err != nil || len(encoded) > maximumBundleBytes {
		return Result{}, ErrRejected
	}
	if args[0] == "render" {
		if securefile.WriteOwnedRecordAt(ctx, fd, values["--output"], encoded, maximumBundleBytes, owner) != nil {
			return Result{}, ErrRejected
		}
	} else {
		existing, err := securefile.ReadOwnedRecordAt(ctx, fd, values["--output"], maximumBundleBytes, owner)
		if err != nil || !bytes.Equal(existing, encoded) {
			return Result{}, ErrRejected
		}
	}
	hash := sha256.Sum256(encoded)
	status := "RENDERED_LOCAL_RECORD"
	if args[0] == "check" {
		status = "CHECKED_LOCAL_RECORD"
	}
	return Result{Status: status, ArtifactSHA256: projection.ArtifactSHA256, BundleSHA256: hex.EncodeToString(hash[:])}, nil
}
