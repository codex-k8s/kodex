package archive

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// errWorkspaceStreamInvalid is a fixed format sentinel without filesystem paths or object details.
// errWorkspaceStreamInvalid — фиксированный format sentinel без filesystem paths и object details.
var errWorkspaceStreamInvalid = errors.New("session workspace stream is invalid")

// workspaceStreamEncoder creates one deterministic stream from already fenced regular tree entries.
// It does not prove owner scope, stopped writers, inode identity or source access by itself.
// workspaceStreamEncoder создаёт deterministic stream из уже fenced regular tree entries.
// Сам codec не доказывает owner scope, stopped writer, inode identity или source access.
type workspaceStreamEncoder struct {
	ctx          context.Context
	tar          *tar.Writer
	output       *workspaceStreamOutput
	bytes        int64
	entries      int64
	maximumBytes int64
	maximumFiles int64
	last         string
	closed       bool
	failed       bool
}

// workspaceStreamOutput bounds total TAR bytes and hashes exactly the bytes accepted by the sink.
// It never allocates a complete work tree or treats a short write as successful publication.
// workspaceStreamOutput ограничивает TAR bytes и хеширует только принятые sink байты.
// Полное дерево не выделяется в памяти; short write не считается успешной публикацией.
type workspaceStreamOutput struct {
	ctx     context.Context
	sink    io.Writer
	digest  hash.Hash
	written int64
	maximum int64
}

// Write rejects cancellation, overflow and non-progress before acknowledging stream bytes.
// Write отклоняет cancellation, overflow и отсутствие прогресса до подтверждения stream bytes.
func (output *workspaceStreamOutput) Write(raw []byte) (int, error) {
	if output.ctx.Err() != nil || int64(len(raw)) > output.maximum-output.written {
		return 0, errWorkspaceStreamInvalid
	}
	written, err := output.sink.Write(raw)
	if written < 0 || written > len(raw) {
		return 0, errWorkspaceStreamInvalid
	}
	if written > 0 {
		_, _ = output.digest.Write(raw[:written])
		output.written += int64(written)
	}
	if err != nil || written != len(raw) {
		return written, errWorkspaceStreamInvalid
	}
	return written, nil
}

// newWorkspaceStreamEncoder applies the original aggregate policy without increasing any budget.
// Owner-selected scope, exact generation and quiescence are mandatory external preconditions.
// newWorkspaceStreamEncoder применяет прежнюю aggregate policy без увеличения budget.
// Owner-selected scope, exact generation и quiescence — обязательные внешние preconditions.
func newWorkspaceStreamEncoder(ctx context.Context, destination io.Writer, policy runtimecontract.RuntimeWorkspacePolicy) (*workspaceStreamEncoder, error) {
	if ctx == nil || ctx.Err() != nil || destination == nil || policy.Validate() != nil {
		return nil, errWorkspaceStreamInvalid
	}
	maximum, err := runtimecontract.MaximumWorkspaceStreamBytes(policy.MaximumWritableBytes, policy.MaximumFileCount)
	if err != nil {
		return nil, errWorkspaceStreamInvalid
	}
	output := &workspaceStreamOutput{ctx: ctx, sink: destination, digest: sha256.New(), maximum: maximum}
	return &workspaceStreamEncoder{ctx: ctx, tar: tar.NewWriter(output), output: output,
		maximumBytes: policy.MaximumWritableBytes, maximumFiles: policy.MaximumFileCount}, nil
}

// workspaceStreamPath accepts bounded relative UTF-8 paths with no ambiguous or special segments.
// Byte limits are applied before PAX encoding, including non-ASCII names and escaped metadata.
// workspaceStreamPath принимает bounded relative UTF-8 paths без неоднозначных сегментов.
// Byte limit применяется до PAX encoding, включая non-ASCII имена и escaped metadata.
func workspaceStreamPath(name string) bool {
	if len(name) < 1 || len(name) > runtimecontract.MaximumWorkspaceArchivePathBytes || !utf8.ValidString(name) ||
		path.IsAbs(name) || path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") {
		return false
	}
	for _, symbol := range name {
		if unicode.IsControl(symbol) {
			return false
		}
	}
	for _, component := range strings.Split(name, "/") {
		if len(component) > 255 {
			return false
		}
	}
	return true
}

// appendEntry writes one strictly ordered regular file or directory with preserved execute bits.
// Links, special files, caller PAX metadata, owners and timestamps are never part of this format.
// appendEntry пишет одну строго упорядоченную regular file/directory, сохраняя execute bits.
// Links, special files, caller PAX metadata, owners и timestamps не входят в этот формат.
func (encoder *workspaceStreamEncoder) appendEntry(name string, directory bool, mode uint32, size int64, source io.Reader) (resultErr error) {
	if encoder != nil {
		defer func() {
			if resultErr != nil {
				encoder.failed = true
			}
		}()
	}
	if encoder == nil || encoder.closed || encoder.failed || encoder.ctx.Err() != nil || !workspaceStreamPath(name) ||
		encoder.entries >= encoder.maximumFiles || mode&^uint32(0o777) != 0 || size < 0 || size > encoder.maximumBytes-encoder.bytes {
		return errWorkspaceStreamInvalid
	}
	entryName := name
	typeFlag := byte(tar.TypeReg)
	if directory {
		if size != 0 || source != nil {
			return errWorkspaceStreamInvalid
		}
		entryName += "/"
		typeFlag = tar.TypeDir
	} else if source == nil {
		return errWorkspaceStreamInvalid
	}
	if encoder.last != "" && entryName <= encoder.last {
		return errWorkspaceStreamInvalid
	}
	before := encoder.output.written
	if encoder.tar.WriteHeader(&tar.Header{Name: entryName, Typeflag: typeFlag, Mode: int64(mode), Size: size,
		Uid: 0, Gid: 0, ModTime: time.Unix(0, 0), Format: tar.FormatPAX}) != nil ||
		encoder.output.written-before > runtimecontract.WorkspaceArchiveEntryOverheadBytes {
		return errWorkspaceStreamInvalid
	}
	if !directory {
		written, err := io.CopyN(encoder.tar, &workspaceStreamReader{ctx: encoder.ctx, source: source}, size)
		if err != nil || written != size {
			return errWorkspaceStreamInvalid
		}
		var extra [1]byte
		read, err := source.Read(extra[:])
		if read != 0 || !errors.Is(err, io.EOF) {
			return errWorkspaceStreamInvalid
		}
	}
	encoder.bytes += size
	encoder.entries++
	encoder.last = entryName
	return nil
}

// workspaceStreamReader observes cancellation between bounded TAR copy reads, without new goroutines.
// It owns neither the input file nor the caller's context and never upgrades partial reads to success.
// workspaceStreamReader проверяет отмену между TAR copy reads без новых goroutines.
// Он не владеет input file/context и не превращает partial read в успех.
type workspaceStreamReader struct {
	ctx    context.Context
	source io.Reader
}

// Read preserves normal reader progress while denying a cancelled checkpoint.
// Read сохраняет обычный reader progress, отклоняя cancelled checkpoint.
func (reader *workspaceStreamReader) Read(raw []byte) (int, error) {
	if reader.ctx.Err() != nil {
		return 0, errWorkspaceStreamInvalid
	}
	return reader.source.Read(raw)
}

// finish closes the canonical TAR and returns its exact content commitment, never an object receipt.
// The manifest/root object can be acknowledged only after independently verified immutable readback.
// finish закрывает canonical TAR и возвращает content commitment, не object receipt.
// Manifest/root object подтверждается лишь после independent immutable readback.
func (encoder *workspaceStreamEncoder) finish() (string, int64, int64, int64, error) {
	if encoder == nil || encoder.closed || encoder.failed || encoder.ctx.Err() != nil {
		return "", 0, 0, 0, errWorkspaceStreamInvalid
	}
	encoder.closed = true
	if encoder.tar.Close() != nil {
		return "", 0, 0, 0, errWorkspaceStreamInvalid
	}
	bound, err := runtimecontract.MaximumWorkspaceStreamBytes(encoder.bytes, encoder.entries)
	if err != nil || encoder.output.written > bound {
		return "", 0, 0, 0, errWorkspaceStreamInvalid
	}
	return hex.EncodeToString(encoder.output.digest.Sum(nil)), encoder.bytes, encoder.entries, encoder.output.written, nil
}
