package sshcompose

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ContainerMount is the bounded Docker evidence needed to validate one release-owned mount.
// ContainerMount — bounded Docker evidence для проверки release-owned mount.
type ContainerMount struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	RW          bool   `json:"rw"`
	Type        string `json:"type"`
}

// ContainerEvidence carries only exact container identity and profile/resource pins, never environment values.
// ContainerEvidence содержит exact container identity/profile/resource pins без environment values.
type ContainerEvidence struct {
	ID       string           `json:"id"`
	Image    string           `json:"image"`
	Project  string           `json:"project"`
	Readonly bool             `json:"readonly"`
	Memory   int64            `json:"memory"`
	NanoCPUs int64            `json:"nanoCpus"`
	PIDs     int64            `json:"pids"`
	Restart  string           `json:"restart"`
	Mounts   []ContainerMount `json:"mounts"`
}

// BootEvidenceReader observes only the container belonging to the fixed target project.
// BootEvidenceReader читает только container фиксированного target project.
type BootEvidenceReader interface {
	// Evidence returns one exact existing container or rejects incomplete/ambiguous Docker readback.
	// Evidence возвращает один exact existing container либо отклоняет incomplete/ambiguous readback.
	Evidence(context.Context, string) (ContainerEvidence, error)
}

// Evidence projects fixed Docker fields and refuses zero or multiple target containers.
// Evidence проецирует fixed Docker fields и отклоняет ноль либо несколько target containers.
func (DockerRunner) Evidence(ctx context.Context, project string) (ContainerEvidence, error) {
	var evidence ContainerEvidence
	if !strings.HasPrefix(project, "kodex_") || len(project) != 22 {
		return evidence, ErrorInvalid
	}
	raw, err := readDocker(ctx, 1024, "container", "ls", "--all", "--no-trunc", "--filter", "label=com.docker.compose.project="+project, "--format", "{{.ID}}")
	if err != nil {
		return evidence, err
	}
	ids := strings.Fields(string(raw))
	if len(ids) != 1 || !digestPattern.MatchString(ids[0]) {
		return evidence, ErrorInvalid
	}
	format := `{"id":{{json .Id}},"image":{{json .Config.Image}},"project":{{json (index .Config.Labels "com.docker.compose.project")}},"readonly":{{json .HostConfig.ReadonlyRootfs}},"memory":{{json .HostConfig.Memory}},"nanoCpus":{{json .HostConfig.NanoCpus}},"pids":{{json .HostConfig.PidsLimit}},"restart":{{json .HostConfig.RestartPolicy.Name}},"mounts":[{{range $index,$mount := .Mounts}}{{if $index}},{{end}}{"name":{{json $mount.Name}},"source":{{json $mount.Source}},"destination":{{json $mount.Destination}},"rw":{{json $mount.RW}},"type":{{json $mount.Type}}}{{end}}]}`
	raw, err = readDocker(ctx, 8192, "container", "inspect", "--format", format, ids[0])
	if err != nil || Decode(raw, &evidence, 8192) != nil || evidence.ID != ids[0] || evidence.Project != project {
		return ContainerEvidence{}, ErrorInvalid
	}
	return evidence, nil
}

// readDocker reads a fixed helper-owned command projection under a bounded context and byte limit.
// readDocker читает fixed helper-owned command projection с bounded context и byte limit.
func readDocker(ctx context.Context, maximum int, args ...string) ([]byte, error) {
	command, err := dockerCommand(ctx, args...)
	if err != nil {
		return nil, err
	}
	pipe, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = io.Discard
	if command.Start() != nil {
		return nil, ErrorInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(pipe, int64(maximum+1)))
	if len(raw) > maximum {
		_ = command.Process.Kill()
	}
	if command.Wait() != nil || readErr != nil || len(raw) > maximum {
		return nil, ErrorInvalid
	}
	return raw, nil
}

// Boot starts only an existing exact container backed by a durable successful health receipt.
// Boot запускает лишь existing exact container с durable successful health receipt.
func (helper Helper) Boot(ctx context.Context) error {
	if helper.Target.Validate() != nil || helper.Runner == nil || !filepath.IsAbs(helper.RootDirectory) || filepath.Clean(helper.RootDirectory) != helper.RootDirectory {
		return ErrorInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !helper.Runner.Rootless(bounded) {
		return ErrorInvalid
	}
	root := filepath.Join(helper.RootDirectory, "journal")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || len(entries) > 1025 {
		return ErrorInvalid
	}
	var committed Receipt
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		receipt, err := ReadReceipt(root, strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return ErrorInvalid
		}
		if receipt.TargetRef == helper.Target.Ref && receipt.ProjectRef == helper.Target.ProjectRef && receipt.State == "SUCCEEDED" && receipt.HealthOK && receipt.Image == helper.Target.RuntimeImage &&
			digestPattern.MatchString(receipt.BundleSHA256) && receipt.FinishedAt > committed.FinishedAt {
			committed = receipt
		}
	}
	if committed.DeploymentRef == "" {
		return nil
	}
	reader, ok := helper.Runner.(BootEvidenceReader)
	if !ok {
		return ErrorInvalid
	}
	project := "kodex_" + Digest([]byte(helper.Target.ProjectRef + "\x00" + helper.Target.Ref))[:16]
	evidence, err := reader.Evidence(bounded, project)
	if err != nil || !validCommittedContainer(helper.Target, helper.RootDirectory, committed, evidence) {
		return ErrorInvalid
	}
	return helper.Runner.Run(bounded, []string{"docker", "container", "start", evidence.ID})
}

// validCommittedContainer rejects image, resource or mount substitution before boot recovery.
// validCommittedContainer отклоняет image/resource/mount substitution до boot recovery.
func validCommittedContainer(target Target, root string, receipt Receipt, evidence ContainerEvidence) bool {
	project := "kodex_" + Digest([]byte(target.ProjectRef + "\x00" + target.Ref))[:16]
	if !digestPattern.MatchString(evidence.ID) || evidence.Project != project || evidence.Image != receipt.Image || !evidence.Readonly || evidence.Memory != 384<<20 || evidence.NanoCPUs != 750000000 || evidence.PIDs != 64 || evidence.Restart != "no" {
		return false
	}
	source, state := false, false
	for _, mount := range evidence.Mounts {
		switch mount.Destination {
		case "/app":
			if source || mount.Type != "bind" || mount.RW || mount.Source != filepath.Join(root, "releases", receipt.BundleSHA256, "source") {
				return false
			}
			source = true
		case "/state":
			if state || mount.Type != "volume" || mount.Name != project+"_state" || !mount.RW {
				return false
			}
			state = true
		case "/tmp":
			if mount.Type != "tmpfs" {
				return false
			}
		default:
			return false
		}
	}
	return source && state && receipt.TargetRef == target.Ref && receipt.ProjectRef == target.ProjectRef
}
