// Package boundedio reads or copies a stream while detecting a size limit violation.
package boundedio

import (
	"errors"
	"io"
)

// ErrInvalidLimit reports a negative byte limit. Zero is a valid limit.
var ErrInvalidLimit = errors.New("boundedio: limit must be non-negative")

// ErrLimitExceeded reports that the stream contains more bytes than allowed.
var ErrLimitExceeded = errors.New("boundedio: byte limit exceeded")

// ReadAll reads an entire stream of at most limit bytes. On any error it returns
// nil data. Unlike io.LimitReader, it reports ErrLimitExceeded instead of silently
// accepting a truncated stream. It consumes at most limit+1 bytes (without
// overflowing at MaxInt64) and never closes r. The caller must set an appropriate
// memory limit and bound blocking reads through the underlying reader. It probes
// beyond the limit only if the source has not already reported EOF.
func ReadAll(r io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, ErrInvalidLimit
	}
	source := eofTracker{reader: r}
	data, err := io.ReadAll(io.LimitReader(&source, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) == limit && !source.atEOF {
		if err := probe(r); err != nil {
			return nil, err
		}
	}
	return data, nil
}

// Copy writes at most limit bytes from src to dst and returns the bytes written.
// If the source exceeds limit it returns ErrLimitExceeded after consuming one
// extra byte, which is not written. Partial writes remain in dst on failure;
// callers needing transactional publication can compose Copy with atomicfile.Write.
// Neither endpoint is closed. Calls are synchronous and cannot interrupt blocking
// I/O; cancellation and deadlines belong to the endpoints. A negative limit returns
// ErrInvalidLimit without reading or writing. A source that has reported EOF is
// not read again, even when its last bytes exactly fill the limit.
func Copy(dst io.Writer, src io.Reader, limit int64) (int64, error) {
	if limit < 0 {
		return 0, ErrInvalidLimit
	}
	source := eofTracker{reader: src}
	n, err := io.Copy(dst, io.LimitReader(&source, limit))
	if err != nil || n < limit || source.atEOF {
		return n, err
	}
	return n, probe(src)
}

// eofTracker distinguishes the source's EOF from a limit's synthetic EOF.
type eofTracker struct {
	reader io.Reader
	atEOF  bool
}

func (r *eofTracker) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.atEOF = errors.Is(err, io.EOF)
	return n, err
}

func probe(r io.Reader) error {
	var extra [1]byte
	n, err := io.ReadFull(r, extra[:])
	if n != 0 {
		return ErrLimitExceeded
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
