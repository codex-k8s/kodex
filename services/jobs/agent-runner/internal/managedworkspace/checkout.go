package managedworkspace

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	workspacefs "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

// snapshotFile carries only verified Git object bytes and mode before atomic local publication.
// snapshotFile хранит лишь проверенные Git object bytes и mode до atomic local publication.
type snapshotFile struct {
	Path       string
	Data       []byte
	Executable bool
}

// checkoutRequest selects an admitted Connection and exact commit, never an endpoint or PAT.
// checkoutRequest выбирает допущенный Connection и exact commit, не endpoint либо PAT.
type checkoutRequest struct {
	Connection  string `json:"connection_ref"`
	ProjectPath string `json:"project_path"`
	ProjectID   int64  `json:"project_id"`
	SHA         string `json:"commit_sha"`
	Destination string `json:"destination"`
}

// checkout materializes only a complete bounded snapshot using current personal gateway grants.
// Partial remote reads never publish a destination; API_SNAPSHOT does not fabricate a git HEAD.
// checkout материализует лишь полный bounded snapshot через текущие личные gateway grants.
// Partial remote reads не публикуют destination; API_SNAPSHOT не подделывает git HEAD.
func (handler *Handler) checkout(parent context.Context, id string, raw json.RawMessage) (any, error) {
	var request checkoutRequest
	if internalrpcauth.DecodeStrictJSON(raw, &request) != nil || request.ProjectID < 1 || request.ProjectID > 9007199254740991 || request.ProjectPath == "" || len(request.ProjectPath) > 255 ||
		strings.Count(request.Destination, "/") != 1 || !strings.HasPrefix(request.Destination, "work/") || request.Destination == "work/.git" || !gitObjectID(request.SHA) {
		return nil, errCheckout
	}
	if _, ok := runtimecontract.ManagedWorkspacePath(handler.store.input, request.Destination); !ok {
		return nil, errCheckout
	}
	grants, err := checkoutGrants(handler.store.input, request.Connection)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(maximumCommandSeconds)*time.Second)
	defer cancel()
	common := map[string]any{"project_id": request.ProjectID, "project_path": request.ProjectPath, "ref": request.SHA}
	commit, err := handler.repositoryRead(ctx, id+"-commit", grants["gitlab.commit.read"], common)
	if err != nil || commit["id"] != request.SHA {
		return nil, errCheckout
	}
	files, err := handler.snapshotFiles(ctx, id, grants, common)
	if err != nil {
		return nil, err
	}
	manifest := sha256.New()
	var total int64
	for _, file := range files {
		sum := sha256.Sum256(file.Data)
		fmt.Fprintf(manifest, "%d:%s:%t:%x\n", len(file.Path), file.Path, file.Executable, sum)
		total += int64(len(file.Data))
	}
	if err := handler.store.publishSnapshot(ctx, request.Destination, files); err != nil {
		return nil, err
	}
	return map[string]any{"representation": "API_SNAPSHOT", "source_commit_sha": request.SHA, "destination": request.Destination, "files": len(files), "bytes": total, "manifest_sha256": hex.EncodeToString(manifest.Sum(nil))}, nil
}

// checkoutGrants requires one unambiguous complete read-only trio on the exact pinned Connection.
// Current credential, target and grant checks remain at every repositoryRead gateway invocation.
// checkoutGrants требует однозначную read-only тройку exact pinned Connection. Каждый вызов
// repositoryRead сохраняет свежие gateway проверки credential, target и grant.
func checkoutGrants(input runtimecontract.RunnerInput, connection string) (map[string]string, error) {
	if !runtimecontract.RuntimeWorkspaceCheckoutAvailable(input) {
		return nil, errCheckout
	}
	result := map[string]string{}
	version := int64(0)
	definition := ""
	for _, grant := range input.IntegrationGrants {
		if grant.ConnectionRef != connection || !slices.Contains([]string{"gitlab.repository.tree.list", "gitlab.repository.file.read", "gitlab.commit.read"}, grant.Operation) {
			continue
		}
		if grant.DefinitionKey != "gitlab" || grant.Operation != grant.CapabilityKey || grant.Risk != "READ" || grant.GrantVersion < 1 || grant.ConnectionVersion < 1 || len(grant.DefinitionDigest) != 64 || grant.Ref == "" {
			return nil, errCheckout
		}
		if version != 0 && (version != grant.ConnectionVersion || definition != grant.DefinitionDigest) {
			return nil, errCheckout
		}
		if _, exists := result[grant.Operation]; exists {
			return nil, errCheckout
		}
		version = grant.ConnectionVersion
		definition = grant.DefinitionDigest
		result[grant.Operation] = grant.Ref
	}
	if len(result) != 3 {
		return nil, errCheckout
	}
	return result, nil
}

// repositoryRead invokes an existing managed read tool and accepts only an observed successful
// gateway JSON result. Human Gate, missing scopes, 401, unknown outcomes and partial text fail shut.
// repositoryRead вызывает существующий managed read tool и принимает лишь observed successful
// gateway JSON result. Human Gate, missing scopes, 401, unknown outcome и partial text закрыты.
func (handler *Handler) repositoryRead(ctx context.Context, id, grant string, fields map[string]any) (map[string]any, error) {
	result, err := handler.owner(ctx, id, "invoke_integration", map[string]any{"grant_ref": grant, "input": fields})
	if err != nil || result["ok"] != true || result["admissionBlocked"] == true || result["owner_decision_required"] == true {
		return nil, errCheckout
	}
	text, ok := result["result"].(string)
	if !ok || len(text) > 256<<10 {
		return nil, errCheckout
	}
	var data map[string]any
	if internalrpcauth.DecodeStrictJSON([]byte(text), &data) != nil {
		return nil, errCheckout
	}
	return data, nil
}

// snapshotFiles traverses every admitted tree page with a shared operation/file/byte budget,
// checks exact Git blob hashes and rejects links/submodules/path aliases instead of dropping them.
// snapshotFiles обходит все допущенные tree pages с общей operation/file/byte квотой, проверяет
// exact Git blob hashes и отклоняет links/submodules/path aliases вместо молчаливого пропуска.
func (handler *Handler) snapshotFiles(ctx context.Context, id string, grants map[string]string, common map[string]any) ([]snapshotFile, error) {
	directories := []string{""}
	seen := map[string]bool{}
	files := []snapshotFile{}
	calls := 0
	total := int64(0)
	for directory := 0; directory < len(directories); directory++ {
		if len(directories) > 128 {
			return nil, errCheckout
		}
		cursor := 1
		for {
			calls++
			if calls > 384 || ctx.Err() != nil {
				return nil, errCheckout
			}
			fields := cloneFields(common)
			fields["path"] = directories[directory]
			fields["cursor"] = cursor
			fields["limit"] = 100
			page, err := handler.repositoryRead(ctx, fmt.Sprintf("%s-tree-%d", id, calls), grants["gitlab.repository.tree.list"], fields)
			if err != nil {
				return nil, err
			}
			encoded, ok := page["items"].(string)
			if !ok || len(encoded) > 65536 {
				return nil, errCheckout
			}
			var entries []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
				Path string `json:"path"`
				Mode string `json:"mode"`
			}
			if internalrpcauth.DecodeStrictJSON([]byte(encoded), &entries) != nil || len(entries) > 100 || numericInt(page["count"]) != int64(len(entries)) {
				return nil, errCheckout
			}
			for _, entry := range entries {
				if !safeRepositoryPath(entry.Path) || path.Dir(entry.Path) != directoryParent(directories[directory]) || path.Base(entry.Path) != entry.Name || seen[entry.Path] || !gitObjectID(entry.ID) {
					return nil, errCheckout
				}
				seen[entry.Path] = true
				if entry.Type == "tree" && entry.Mode == "040000" {
					directories = append(directories, entry.Path)
					continue
				}
				if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") || len(files) >= 128 {
					return nil, errCheckout
				}
				calls++
				if calls > 384 {
					return nil, errCheckout
				}
				fileInput := cloneFields(common)
				fileInput["file_path"] = entry.Path
				file, err := handler.repositoryRead(ctx, fmt.Sprintf("%s-file-%d", id, calls), grants["gitlab.repository.file.read"], fileInput)
				if err != nil || file["encoding"] != "base64" || file["file_path"] != entry.Path || file["ref"] != common["ref"] || file["blob_id"] != entry.ID {
					return nil, errCheckout
				}
				content, ok := file["content"].(string)
				if !ok || len(content) > 65536 {
					return nil, errCheckout
				}
				data, err := base64.StdEncoding.Strict().DecodeString(content)
				if err != nil || !matchesBlob(entry.ID, data) {
					return nil, errCheckout
				}
				total += int64(len(data))
				if total > 8<<20 {
					return nil, errCheckout
				}
				files = append(files, snapshotFile{Path: entry.Path, Data: data, Executable: entry.Mode == "100755"})
			}
			next := numericInt(page["next_cursor"])
			if page["next_cursor"] == nil || next == 0 {
				break
			}
			if next != int64(cursor+1) || next > 10000 {
				return nil, errCheckout
			}
			cursor = int(next)
		}
	}
	slices.SortFunc(files, func(a, b snapshotFile) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// cloneFields copies the small trusted input map so pagination cannot mutate the pinned ref.
// cloneFields копирует малый trusted input map, чтобы pagination не меняла pinned ref.
func cloneFields(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+3)
	for key, value := range source {
		result[key] = value
	}
	return result
}

// numericInt accepts only exact JSON integers; missing/invalid values retain a rejected sentinel.
// numericInt принимает лишь exact JSON integers; missing/invalid value сохраняет отказной sentinel.
func numericInt(value any) int64 {
	number, ok := value.(json.Number)
	if !ok {
		return -1
	}
	n, err := number.Int64()
	if err != nil {
		return -1
	}
	return n
}

// directoryParent maps the repository root selector to GitLab's relative dirname representation.
// directoryParent преобразует root selector репозитория в относительный dirname GitLab.
func directoryParent(value string) string {
	if value == "" {
		return "."
	}
	return value
}

// safeRepositoryPath rejects traversal, controls, aliases and git metadata from snapshot paths.
// safeRepositoryPath отклоняет traversal, controls, aliases и git metadata в snapshot paths.
func safeRepositoryPath(value string) bool {
	if value == "" || len(value) > 1024 || path.IsAbs(value) || path.Clean(value) != value || value == "." || strings.HasPrefix(value, "../") || strings.ContainsAny(value, "\\\x00\n\r") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".git" {
			return false
		}
	}
	return true
}

// gitObjectID accepts only full lowercase SHA-1/SHA-256 Git object IDs; branch names are excluded.
// gitObjectID допускает только полный lowercase SHA-1/SHA-256 Git object ID, исключая branch names.
func gitObjectID(value string) bool {
	if (len(value) != 40 && len(value) != 64) || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// matchesBlob checks Git's typed length-delimited object hash, not the hash of unframed bytes.
// matchesBlob проверяет typed length-delimited Git object hash, не хеш неоформленных bytes.
func matchesBlob(id string, data []byte) bool {
	framed := append([]byte(fmt.Sprintf("blob %d\x00", len(data))), data...)
	if len(id) == 40 {
		sum := sha1.Sum(framed)
		return hex.EncodeToString(sum[:]) == id
	}
	sum := sha256.Sum256(framed)
	return hex.EncodeToString(sum[:]) == id
}

// publishSnapshot writes an own random staging tree and atomically installs only an absent
// destination. Failed staging is removed through an os.Root handle, never an arbitrary user path.
// publishSnapshot пишет собственный random staging tree и атомарно публикует лишь absent
// destination. Failed staging удаляется через os.Root handle, не произвольный user path.
func (store *fileStore) publishSnapshot(ctx context.Context, destination string, files []snapshotFile) (resultErr error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return errCheckout
	}
	stageName := ".kodex-checkout-" + hex.EncodeToString(random)
	stage := "work/" + stageName
	lock, work, _, err := store.lock(ctx, "work")
	if err != nil {
		return err
	}
	usageFiles, _, err := store.totalUsage(ctx, int(lock.Fd()))
	if err == nil && usageFiles+1 <= store.input.WorkspacePolicy.MaximumFileCount {
		err = unix.Mkdirat(int(work.Fd()), stageName, 0o2770)
	} else {
		err = errCheckout
	}
	if err == nil {
		fd, e := store.openDirectory(int(work.Fd()), stageName)
		if e != nil {
			err = e
		} else {
			err = unix.Fchmod(fd, 0o2770)
			unix.Close(fd)
		}
	}
	work.Close()
	lock.Close()
	if err != nil {
		return errCheckout
	}
	installed := false
	defer func() {
		if !installed {
			root, e := os.OpenRoot(store.root)
			if e == nil {
				_ = root.RemoveAll(stage)
				_ = root.Close()
			}
		}
	}()
	for _, file := range files {
		if _, err := store.writeBytes(ctx, stage+"/"+file.Path, file.Data, "ABSENT"); err != nil {
			return err
		}
		if file.Executable {
			lock, base, relative, e := store.lock(ctx, stage+"/"+file.Path)
			if e != nil {
				return e
			}
			fd, e := unix.Openat2(int(base.Fd()), relative, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
			if e == nil {
				e = unix.Fchmod(fd, 0o770)
				if e == nil {
					e = unix.Fsync(fd)
				}
				unix.Close(fd)
			}
			base.Close()
			lock.Close()
			if e != nil {
				return errCheckout
			}
		}
	}
	lock, err = workspacefs.Lock(ctx, store.root)
	if err != nil {
		return errCheckout
	}
	defer lock.Close()
	work, err = store.openBaseUnderLock(int(lock.Fd()), "work")
	if err != nil {
		return err
	}
	defer work.Close()
	if ctx.Err() != nil || unix.Renameat2(int(work.Fd()), stageName, int(work.Fd()), strings.TrimPrefix(destination, "work/"), unix.RENAME_NOREPLACE) != nil {
		return errCheckout
	}
	installed = true
	if unix.Fsync(int(work.Fd())) != nil {
		return errCheckout
	}
	return nil
}
