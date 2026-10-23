package atomicfile

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileInRoot replaces name relative to root with data and perm. It follows
// WriteInRoot's directory boundary, ownership and publication rules.
func WriteFileInRoot(root *os.Root, name string, data []byte, perm fs.FileMode) error {
	return WriteInRoot(root, name, perm, func(w io.Writer) error {
		_, err := io.Copy(w, bytes.NewReader(data))
		return err
	})
}

// WriteInRoot replaces a file within root. It opens the destination's parent as
// a sub-root before invoking write; temporary creation, cleanup and rename all
// use that pinned directory. Absolute and escaping paths are rejected by os.Root.
// Parent symlinks must resolve within root. A destination symlink is replaced.
// The caller owns root and must keep it open for this call; it is never closed.
// A nil root, invalid mode or nil callback returns ErrInvalidArgument.
//
// File permissions, callback ownership, panic cleanup, platform atomicity and
// lack of directory fsync match Write. All os.Root platform limitations apply:
// this is not protection against hard links, mount changes or an adversary who
// can move an already-open directory outside the root. Concurrent namespace
// changes cannot redirect publication to a newly substituted parent directory.
func WriteInRoot(root *os.Root, name string, perm fs.FileMode, write func(io.Writer) error) error {
	if root == nil || write == nil || perm&^fs.ModePerm != 0 {
		return ErrInvalidArgument
	}
	dir, base := filepath.Split(name)
	if base == "" || base == "." || base == ".." {
		return &os.PathError{Op: "write", Path: name, Err: fs.ErrInvalid}
	}
	if dir == "" {
		dir = "."
	}
	parent, err := root.OpenRoot(dir)
	if err != nil {
		return err
	}
	// This read-only directory handle has no buffered data to flush.
	defer func() { _ = parent.Close() }()
	for range 10 {
		temporary := ".kitx-" + rand.Text()
		file, err := parent.OpenFile(temporary, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		return replace(file, perm, write, func() error { return parent.Remove(temporary) }, func() error {
			return parent.Rename(temporary, base)
		})
	}
	return &os.PathError{Op: "createtemp", Path: name, Err: fs.ErrExist}
}
