package atomicfile_test

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nostosx/kitx/atomicfile"
)

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}

func TestWriteInRootPublicationAndFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := openRoot(t, dir)
	if err := root.Mkdir("nested", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteFileInRoot(root, "nested/value", []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("incomplete")
	err := atomicfile.WriteInRoot(root, "nested/value", 0o600, func(w io.Writer) error {
		if b, err := root.ReadFile("nested/value"); err != nil || string(b) != "old" {
			t.Errorf("before publish=%q,%v", b, err)
		}
		if _, err := io.WriteString(w, "partial"); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if b, err := root.ReadFile("nested/value"); err != nil || string(b) != "old" {
		t.Fatalf("after failure=%q,%v", b, err)
	}
	func() {
		defer func() {
			if recover() != "stop" {
				t.Error("panic lost")
			}
		}()
		_ = atomicfile.WriteInRoot(root, "nested/value", 0o600, func(io.Writer) error { panic("stop") })
	}()
	entries, err := os.ReadDir(filepath.Join(dir, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "value" {
		t.Fatalf("temporary files leaked: %v", entries)
	}
	if err := atomicfile.WriteFileInRoot(root, "nested/value", []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	if b, err := root.ReadFile("nested/value"); err != nil || string(b) != "new" {
		t.Fatalf("published=%q,%v", b, err)
	}
	if runtime.GOOS != "windows" {
		info, err := root.Stat("nested/value")
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Fatal(info.Mode())
		}
	}
}

func TestWriteInRootRejectsEscape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inside := filepath.Join(dir, "inside")
	outside := filepath.Join(dir, "outside")
	for _, p := range []string{inside, outside} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	root := openRoot(t, inside)
	for _, name := range []string{"../outside/value", filepath.Join(outside, "value"), "", ".", "..", "sub/"} {
		t.Run(name, func(t *testing.T) {
			err := atomicfile.WriteInRoot(root, name, 0o600, func(io.Writer) error { t.Error("invalid path reached callback"); return nil })
			if err == nil {
				t.Fatal("escaping or invalid path accepted")
			}
		})
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("escaped root")
	}
}

func TestWriteInRootInvalidArguments(t *testing.T) {
	t.Parallel()
	root := openRoot(t, t.TempDir())
	write := func(io.Writer) error { t.Error("invalid arguments reached callback"); return nil }
	for _, tt := range []struct {
		name  string
		root  *os.Root
		perm  fs.FileMode
		write func(io.Writer) error
	}{
		{"nil root", nil, 0o600, write},
		{"nil callback", root, 0o600, nil},
		{"invalid mode", root, fs.ModeDir, write},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := atomicfile.WriteInRoot(tt.root, "value", tt.perm, tt.write); !errors.Is(err, atomicfile.ErrInvalidArgument) {
				t.Fatalf("WriteInRoot = %v", err)
			}
		})
	}
}

func TestWriteInRootSymlinkBoundaries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inside := filepath.Join(dir, "inside")
	outside := filepath.Join(dir, "outside")
	for _, p := range []string{inside, outside} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "value"), []byte("protected"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, inside)
	if err := root.Symlink(outside, "parent"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := atomicfile.WriteFileInRoot(root, "parent/value", []byte("bad"), 0o600); err == nil {
		t.Fatal("parent symlink escaped root")
	}
	if err := root.Symlink(filepath.Join(outside, "value"), "target"); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteFileInRoot(root, "target", []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(outside, "value")); err != nil || string(b) != "protected" {
		t.Fatalf("outside=%q,%v", b, err)
	}
	if b, err := root.ReadFile("target"); err != nil || string(b) != "replacement" {
		t.Fatalf("inside=%q,%v", b, err)
	}
}

func TestWriteInRootPinsParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("renaming open directories has platform-specific restrictions")
	}
	t.Parallel()
	root := openRoot(t, t.TempDir())
	if err := root.Mkdir("parent", 0o700); err != nil {
		t.Fatal(err)
	}
	err := atomicfile.WriteInRoot(root, "parent/value", 0o600, func(w io.Writer) error {
		if err := root.Rename("parent", "moved"); err != nil {
			return err
		}
		if err := root.Mkdir("parent", 0o700); err != nil {
			return err
		}
		_, err := io.WriteString(w, "pinned")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, err := root.ReadFile("moved/value"); err != nil || string(b) != "pinned" {
		t.Fatalf("pinned=%q,%v", b, err)
	}
	if _, err := root.Stat("parent/value"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("publication redirected: %v", err)
	}
}

func TestWriteInRootCleanupPinsParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("renaming open directories has platform-specific restrictions")
	}
	t.Parallel()
	for _, mode := range []string{"error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			root := openRoot(t, dir)
			if err := root.Mkdir("parent", 0o700); err != nil {
				t.Fatal(err)
			}
			if err := root.WriteFile("parent/value", []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("writer stopped")
			type outcome struct {
				err       error
				recovered any
				returned  bool
			}
			done := make(chan outcome, 1)
			go func() {
				var result outcome
				defer func() {
					result.recovered = recover()
					done <- result
				}()
				result.err = atomicfile.WriteInRoot(root, "parent/value", 0o600, func(w io.Writer) error {
					if err := root.Rename("parent", "moved"); err != nil {
						return err
					}
					if err := root.Mkdir("parent", 0o700); err != nil {
						return err
					}
					if _, err := io.WriteString(w, "partial"); err != nil {
						return err
					}
					switch mode {
					case "panic":
						panic(failure)
					case "goexit":
						runtime.Goexit()
					}
					return failure
				})
				result.returned = true
			}()
			result := <-done
			switch mode {
			case "error":
				if !result.returned || !errors.Is(result.err, failure) {
					t.Fatalf("writer outcome = %+v", result)
				}
			case "panic":
				panicErr, ok := result.recovered.(error)
				if result.returned || !ok || !errors.Is(panicErr, failure) {
					t.Fatalf("writer outcome = %+v", result)
				}
			case "goexit":
				if result.returned || result.recovered != nil {
					t.Fatalf("writer outcome = %+v", result)
				}
			}
			if b, err := root.ReadFile("moved/value"); err != nil || string(b) != "old" {
				t.Fatalf("old file = %q, %v", b, err)
			}
			for _, name := range []string{"parent", "moved"} {
				entries, err := os.ReadDir(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if name == "moved" {
					want = 1
				}
				if len(entries) != want {
					t.Errorf("%s contains unexpected files: %v", name, entries)
				}
			}
		})
	}
}

func ExampleWriteFileInRoot() {
	dir, err := os.MkdirTemp("", "kitx-root-example-")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	root, err := os.OpenRoot(dir)
	if err != nil {
		panic(err)
	}
	defer func() { _ = root.Close() }()
	if err := atomicfile.WriteFileInRoot(root, "config", []byte("ready"), 0o600); err != nil {
		panic(err)
	}
	value, err := root.ReadFile("config")
	fmt.Println(string(value), err)
	// Output: ready <nil>
}
