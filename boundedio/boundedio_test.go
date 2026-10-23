package boundedio_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/nostosx/kitx/atomicfile"
	"github.com/nostosx/kitx/boundedio"
)

func TestReadAllAndCopyLimits(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		input string
		limit int64
		want  string
		err   error
	}{
		{"empty", "", 0, "", nil},
		{"zero limit", "abc", 0, "", boundedio.ErrLimitExceeded},
		{"below limit", "abc", 4, "abc", nil},
		{"exact limit", "abc", 3, "abc", nil},
		{"above limit", "abcde", 3, "abc", boundedio.ErrLimitExceeded},
		{"negative limit", "abc", -1, "", boundedio.ErrInvalidLimit},
		{"max int64", "abc", math.MaxInt64, "abc", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			data, err := boundedio.ReadAll(r, tt.limit)
			if !errors.Is(err, tt.err) || err != nil && data != nil || err == nil && string(data) != tt.want {
				t.Fatalf("ReadAll = %q, %v", data, err)
			}
			consumed := int64(len(tt.input) - r.Len())
			if tt.limit < 0 && consumed != 0 || tt.limit >= 0 && tt.limit < math.MaxInt64 && consumed > tt.limit+1 {
				t.Errorf("ReadAll consumed %d bytes", consumed)
			}
			r = strings.NewReader(tt.input)
			var dst bytes.Buffer
			n, err := boundedio.Copy(&dst, r, tt.limit)
			if !errors.Is(err, tt.err) || dst.String() != tt.want || n != int64(len(tt.want)) {
				t.Fatalf("Copy = %d, %v, data %q", n, err, dst.String())
			}
			consumed = int64(len(tt.input) - r.Len())
			if tt.limit < 0 && consumed != 0 || tt.limit >= 0 && tt.limit < math.MaxInt64 && consumed > tt.limit+1 {
				t.Errorf("Copy consumed %d bytes", consumed)
			}
		})
	}
}

func TestReadAllAndCopyPreserveReadFailures(t *testing.T) {
	t.Parallel()
	failure := errors.New("source failed")
	for _, tt := range []struct {
		name   string
		prefix string
		limit  int64
	}{
		{"before limit", "ab", 3},
		{"at limit", "abc", 3},
		{"zero limit", "", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := func() io.Reader { return io.MultiReader(strings.NewReader(tt.prefix), iotest.ErrReader(failure)) }
			if data, err := boundedio.ReadAll(reader(), tt.limit); data != nil || !errors.Is(err, failure) {
				t.Fatalf("ReadAll = %q, %v", data, err)
			}
			var dst bytes.Buffer
			n, err := boundedio.Copy(&dst, reader(), tt.limit)
			if !errors.Is(err, failure) || n != int64(len(tt.prefix)) || dst.String() != tt.prefix {
				t.Fatalf("Copy = %d, %v, data %q", n, err, dst.String())
			}
		})
	}
}

func TestReadAllAndCopyStopAtReportedEOF(t *testing.T) {
	t.Parallel()
	for _, limit := range []int64{3, 4} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			r := &terminalReader{data: []byte("abc")}
			data, err := boundedio.ReadAll(r, limit)
			if string(data) != "abc" || err != nil {
				t.Errorf("ReadAll = %q, %v", data, err)
			}
			if r.reads != 1 {
				t.Errorf("ReadAll read %d times", r.reads)
			}
			r = &terminalReader{data: []byte("abc")}
			var dst bytes.Buffer
			n, err := boundedio.Copy(&dst, r, limit)
			if n != 3 || dst.String() != "abc" || err != nil {
				t.Fatalf("Copy = %d, %v, data %q", n, err, dst.String())
			}
			if r.reads != 1 {
				t.Errorf("Copy read %d times", r.reads)
			}
		})
	}
}

type terminalReader struct {
	data  []byte
	reads int
}

func (r *terminalReader) Read(p []byte) (int, error) {
	r.reads++
	if len(r.data) == 0 {
		return 0, errors.New("read after reported EOF")
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, io.EOF
	}
	return n, nil
}

func TestCopyPreservesPartialWritesAndWriterErrors(t *testing.T) {
	t.Parallel()
	failure := errors.New("destination failed")
	n, err := boundedio.Copy(partialWriter{err: failure}, strings.NewReader("abc"), 3)
	if n != 1 || !errors.Is(err, failure) {
		t.Fatalf("partial Copy = %d, %v", n, err)
	}
	n, err = boundedio.Copy(partialWriter{}, strings.NewReader("abc"), 3)
	if n != 1 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short Copy = %d, %v", n, err)
	}
}

type partialWriter struct{ err error }

func (w partialWriter) Write([]byte) (int, error) { return 1, w.err }

func FuzzReadAllAndCopy(f *testing.F) {
	f.Add([]byte("abc"), uint16(3))
	f.Add([]byte("abc"), uint16(2))
	f.Add([]byte{}, uint16(0))
	f.Fuzz(func(t *testing.T, input []byte, limit uint16) {
		r := bytes.NewReader(input)
		data, err := boundedio.ReadAll(r, int64(limit))
		if len(input) > int(limit) {
			if data != nil || !errors.Is(err, boundedio.ErrLimitExceeded) {
				t.Fatalf("oversized input: len=%d, limit=%d, error=%v", len(input), limit, err)
			}
		} else if err != nil || !bytes.Equal(data, input) {
			t.Fatalf("valid input: len=%d, limit=%d, error=%v", len(input), limit, err)
		}
		if consumed := len(input) - r.Len(); consumed > int(limit)+1 {
			t.Fatalf("consumed %d bytes with limit %d", consumed, limit)
		}
		r = bytes.NewReader(input)
		var dst bytes.Buffer
		n, err := boundedio.Copy(&dst, r, int64(limit))
		want := min(len(input), int(limit))
		if n != int64(want) || !bytes.Equal(dst.Bytes(), input[:want]) {
			t.Fatalf("Copy wrote %d bytes with input %d and limit %d", n, len(input), limit)
		}
		if len(input) > int(limit) {
			if !errors.Is(err, boundedio.ErrLimitExceeded) {
				t.Fatalf("Copy accepted oversized input: len=%d, limit=%d, error=%v", len(input), limit, err)
			}
		} else if err != nil {
			t.Fatalf("Copy rejected valid input: len=%d, limit=%d, error=%v", len(input), limit, err)
		}
		if consumed := len(input) - r.Len(); consumed > int(limit)+1 {
			t.Fatalf("Copy consumed %d bytes with limit %d", consumed, limit)
		}
	})
}

func ExampleReadAll() {
	data, err := boundedio.ReadAll(strings.NewReader("hello"), 5)
	fmt.Println(string(data), err)
	_, err = boundedio.ReadAll(strings.NewReader("too large"), 5)
	fmt.Println(errors.Is(err, boundedio.ErrLimitExceeded))
	// Output:
	// hello <nil>
	// true
}

func ExampleCopy() {
	var dst bytes.Buffer
	n, err := boundedio.Copy(&dst, strings.NewReader("hello"), 5)
	fmt.Println(n, dst.String(), err)
	// Output: 5 hello <nil>
}

func TestCopyFailurePreservesAtomicDestination(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "destination")
	if err := os.WriteFile(name, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := atomicfile.Write(name, 0o600, func(dst io.Writer) error {
		_, err := boundedio.Copy(dst, strings.NewReader("oversized"), 3)
		return err
	})
	if !errors.Is(err, boundedio.ErrLimitExceeded) {
		t.Fatal(err)
	}
	data, err := os.ReadFile(name)
	if err != nil || string(data) != "original" {
		t.Fatalf("destination = %q, %v", data, err)
	}
}
