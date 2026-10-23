package atomicfile_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/nostosx/kitx/atomicfile"
)

func TestWriteFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	name := filepath.Join(dir, "state.json")
	for _, value := range []string{"first", "second", ""} {
		if err := atomicfile.WriteFile(name, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		assertContent(t, name, value)
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("mode=%v, want 0600", info.Mode())
		}
		assertEntries(t, dir, "state.json")
	}
}

func TestWriteFailurePreservesDestination(t *testing.T) {
	t.Parallel()
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprint(exists), func(t *testing.T) {
			dir := t.TempDir()
			name := filepath.Join(dir, "state")
			if exists {
				if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("encoder failed")
			err := atomicfile.Write(name, 0o600, func(w io.Writer) error {
				if _, err := io.WriteString(w, "partial"); err != nil {
					return err
				}
				return failure
			})
			if !errors.Is(err, failure) {
				t.Fatalf("error=%v", err)
			}
			if exists {
				assertContent(t, name, "old")
				assertEntries(t, dir, "state")
			} else {
				assertEntries(t, dir)
			}
		})
	}
}

func TestWriteRelativePathSurvivesCallbackChdir(t *testing.T) {
	for _, outcome := range []string{"success", "error", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			dir, other := t.TempDir(), t.TempDir()
			t.Chdir(dir)
			name := filepath.Join(dir, "state")
			otherName := filepath.Join(other, "state")
			if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(otherName, []byte("unrelated"), 0o600); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("encoder failed")
			var err error
			var panicValue any
			func() {
				defer func() { panicValue = recover() }()
				err = atomicfile.Write("state", 0o600, func(w io.Writer) error {
					if _, err := io.WriteString(w, "new"); err != nil {
						return err
					}
					if err := os.Chdir(other); err != nil {
						return err
					}
					switch outcome {
					case "error":
						return failure
					case "panic":
						panic("encoder panic")
					}
					return nil
				})
			}()
			want := "old"
			switch outcome {
			case "success":
				if err != nil || panicValue != nil {
					t.Fatalf("error=%v panic=%v", err, panicValue)
				}
				want = "new"
			case "error":
				if !errors.Is(err, failure) || panicValue != nil {
					t.Fatalf("error=%v panic=%v", err, panicValue)
				}
			case "panic":
				if panicValue != "encoder panic" {
					t.Fatalf("panic=%v, want encoder panic", panicValue)
				}
			}
			assertContent(t, name, want)
			assertContent(t, otherName, "unrelated")
			assertEntries(t, dir, "state")
			assertEntries(t, other, "state")
		})
	}
}

func TestWritePermissionsBeforeAndAfterPublication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows does not implement Unix permission bits")
	}
	t.Parallel()
	dir := t.TempDir()
	name := filepath.Join(dir, "state")
	if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := atomicfile.Write(name, 0o640, func(w io.Writer) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(entries) != 2 {
			t.Fatalf("directory has %d entries, want destination and temporary file", len(entries))
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("%s mode=%v before publication, want 0600", entry.Name(), info.Mode())
			}
		}
		_, err = io.WriteString(w, "new")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContent(t, name, "new")
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("published mode=%v, want 0640", info.Mode())
	}
	assertEntries(t, dir, "state")
}

func TestWritePreservesParentSymlinkResolution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges are environment-dependent on windows")
	}
	t.Parallel()
	dir := t.TempDir()
	actual := filepath.Join(dir, "actual")
	if err := os.MkdirAll(filepath.Join(actual, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(actual, "child"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(actual, "state")
	if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Keep link/.. intact: lexical cleaning would select the wrong directory.
	err := atomicfile.Write(dir+"/link/../state", 0o600, func(w io.Writer) error {
		entries, err := os.ReadDir(actual)
		if err != nil {
			return err
		}
		if len(entries) != 3 {
			t.Errorf("destination directory has %d entries, want child, state and temporary file", len(entries))
		}
		assertContent(t, name, "old")
		_, err = io.WriteString(w, "new")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContent(t, name, "new")
	assertEntries(t, dir, "actual", "link")
	assertEntries(t, actual, "child", "state")
}

func TestWritePublishesOnlyAfterCallbackReturns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	name := filepath.Join(dir, "state")
	if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	var writeErr error
	go func() {
		defer close(done)
		writeErr = atomicfile.Write(name, 0o600, func(w io.Writer) error {
			_, err := io.WriteString(w, "new")
			close(started)
			<-release
			return err
		})
	}()
	// Always release and join the writer, including after a fatal assertion.
	t.Cleanup(func() {
		close(release)
		<-done
		if writeErr != nil {
			t.Error(writeErr)
		}
		assertContent(t, name, "new")
	})
	select {
	case <-started:
		assertContent(t, name, "old")
	case <-done:
		t.Fatalf("write ended before callback: %v", writeErr)
	case <-t.Context().Done():
		t.Fatal("write did not reach callback")
	}
}

func TestWritePanicCleansTemporaryFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	name := filepath.Join(dir, "state")
	if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if got := recover(); got != "encoder panic" {
				t.Errorf("panic=%v", got)
			}
		}()
		_ = atomicfile.Write(name, 0o600, func(w io.Writer) error {
			if _, err := io.WriteString(w, "partial"); err != nil {
				return err
			}
			panic("encoder panic")
		})
	}()
	assertContent(t, name, "old")
	assertEntries(t, dir, "state")
}

func TestWriteInvalidAndFilesystemErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	name := filepath.Join(dir, "state")
	if err := atomicfile.Write(name, 0o600, nil); !errors.Is(err, atomicfile.ErrInvalidArgument) {
		t.Fatal(err)
	}
	for _, mode := range []fs.FileMode{fs.ModeDir, fs.ModeSymlink, fs.ModeSetuid, fs.ModeSticky} {
		t.Run(mode.String(), func(t *testing.T) {
			if err := atomicfile.WriteFile(name, nil, mode|0o600); !errors.Is(err, atomicfile.ErrInvalidArgument) {
				t.Fatal(err)
			}
		})
	}
	assertEntries(t, dir)
	if err := atomicfile.WriteFile(filepath.Join(dir, "missing", "state"), nil, 0o600); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Mkdir(name, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteFile(name, []byte("new"), 0o600); err == nil {
		t.Fatal("replaced directory")
	}
	assertEntries(t, dir, "state")
	info, err := os.Stat(name)
	if err != nil || !info.IsDir() {
		t.Fatalf("directory not preserved: %v", err)
	}
}

func TestWriteReplacesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges are environment-dependent on windows")
	}
	t.Parallel()
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target"), filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteFile(link, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertContent(t, target, "target")
	assertContent(t, link, "replacement")
	info, err := os.Lstat(link)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("not a regular file: %v", err)
	}
}

func TestWriteConcurrentReadersAndWriters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Rename does not guarantee atomic replacement on windows")
	}
	t.Parallel()
	dir := t.TempDir()
	name := filepath.Join(dir, "state")
	a, b := bytes.Repeat([]byte("a"), 8192), bytes.Repeat([]byte("b"), 8192)
	if err := atomicfile.WriteFile(name, a, 0o600); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for _, content := range [][]byte{a, b} {
		workers.Go(func() {
			for range 20 {
				if err := atomicfile.WriteFile(name, content, 0o600); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	for range 2 {
		workers.Go(func() {
			for range 100 {
				data, err := os.ReadFile(name)
				if err != nil {
					t.Error(err)
					return
				}
				if !bytes.Equal(data, a) && !bytes.Equal(data, b) {
					t.Error("reader observed partial data")
					return
				}
			}
		})
	}
	workers.Wait()
	assertEntries(t, dir, "state")
}

func ExampleWrite() {
	dir, err := os.MkdirTemp("", "kitx-example-*")
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			panic(err)
		}
	}()
	name := filepath.Join(dir, "state.json")
	err = atomicfile.Write(name, 0o600, func(w io.Writer) error {
		return json.NewEncoder(w).Encode(struct {
			Status string `json:"status"`
		}{Status: "ready"})
	})
	if err != nil {
		panic(err)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(data))
	// Output: {"status":"ready"}
}

func assertContent(t *testing.T, name, want string) {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("content=%q, want %q", data, want)
	}
}

func assertEntries(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(names) {
		t.Fatalf("directory has %d entries, want %d: %v", len(entries), len(names), entries)
	}
	for i, name := range names {
		if entries[i].Name() != name {
			t.Fatalf("entry=%q, want %q", entries[i].Name(), name)
		}
	}
}
