package workspace

import (
	"path"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// CheckWorkLink verifies a relative dependency link with a kernel-confined metadata
// lookup beneath the already opened work directory. It never reads target bytes,
// crosses mounts, follows magic links, or permits an absolute/escaping target.
// CheckWorkLink проверяет относительную ссылку зависимости через ограниченное ядром
// чтение метаданных внутри уже открытого work. Содержимое цели не читается; переход
// между mounts, magic links, абсолютная цель и выход из work запрещены.
func CheckWorkLink(workFD int, relative string) error {
	denied := &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	if relative == "" || relative == "." || path.IsAbs(relative) || path.Clean(relative) != relative || strings.HasPrefix(relative, "../") {
		return denied
	}
	fd, err := unix.Openat2(workFD, relative, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		return denied
	}
	defer unix.Close(fd)
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || (info.Mode&unix.S_IFMT != unix.S_IFREG && info.Mode&unix.S_IFMT != unix.S_IFDIR) {
		return denied
	}
	return nil
}
