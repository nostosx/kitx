// Package atomicfile replaces files using a temporary file in the same directory.
// Write and WriteFile require trusted parent paths. The InRoot variants use an
// explicit os.Root boundary. None of these operations supplies a lock.
// On Unix, successful replacement is atomic for readers. Other operating systems
// follow os.Rename's guarantees, which may be weaker. The file is synced before
// replacement, but the directory is not: success does not promise crash durability.
package atomicfile

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrInvalidArgument indicates a nil root or callback, or non-permission mode bits.
var ErrInvalidArgument = errors.New("atomicfile: invalid root, write callback or file mode")

// WriteFile replaces name with data and perm. See Write for replacement rules.
func WriteFile(name string, data []byte, perm fs.FileMode) error {
	return Write(name, perm, func(w io.Writer) error {
		_, err := io.Copy(w, bytes.NewReader(data))
		return err
	})
}

// Write calls write to populate a temporary file, applies perm, syncs and closes
// it, then renames it over name. The parent directory must exist. Only permission
// bits (0000 through 0777) are accepted. The temporary file starts with mode 0600;
// perm is applied after the callback returns. Replacement does not preserve the
// existing file's permissions, ownership, ACLs or extended attributes. The
// callback runs synchronously and must not retain or close the writer.
// The parent path is resolved before calling write, so later working-directory
// changes do not redirect publication or cleanup. Parent symlinks are resolved
// before any following .. components; the destination itself is not resolved.
//
// Before rename, any error or callback panic leaves the old destination intact
// and attempts to remove the temporary file. Cleanup errors are joined with a
// returned error. Panics propagate after cleanup. A destination symlink is
// replaced, not followed. Concurrent writers use separate temporary files,
// and the last successful rename wins. Protect the containing directory against
// untrusted changes and use a separate lock for read-modify-write operations.
func Write(name string, perm fs.FileMode, write func(io.Writer) error) (err error) {
	if write == nil || perm&^fs.ModePerm != 0 {
		return ErrInvalidArgument
	}
	dir, base := filepath.Split(name)
	if dir == "" {
		dir = "."
	}
	// Dir and Abs clean paths lexically, which would change link/.. semantics.
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	name = filepath.Join(dir, base)
	file, err := os.CreateTemp(dir, ".kitx-*")
	if err != nil {
		return err
	}
	return replace(file, perm, write, func() error { return os.Remove(file.Name()) }, func() error {
		return os.Rename(file.Name(), name)
	})
}

// replace owns file and its publication/cleanup callbacks until return.
func replace(file *os.File, perm fs.FileMode, write func(io.Writer) error, remove, rename func() error) (err error) {
	isClosed := false
	isReplaced := false
	defer func() {
		if !isClosed {
			err = errors.Join(err, file.Close())
		}
		if !isReplaced {
			if cleanupErr := remove(); cleanupErr != nil && !errors.Is(cleanupErr, fs.ErrNotExist) {
				err = errors.Join(err, cleanupErr)
			}
		}
	}()
	if err := write(file); err != nil {
		return err
	}
	if err := file.Chmod(perm); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	err = file.Close()
	isClosed = true
	if err != nil {
		return err
	}
	err = rename()
	isReplaced = err == nil
	return err
}
