package tgz

// The internals below are ported verbatim from aric-go/tgz cmd/archive.go.
// Deliberate deviation: close errors are checked so a failed gzip flush
// cannot be reported as success.

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// sink receives format-agnostic entries from writeArchive. Each Add*
// call must consume r (if non-nil) before returning. Close must surface
// any buffered/flush errors.
type sink interface {
	AddFile(name string, info fs.FileInfo, size int64, r io.Reader) error
	AddDir(name string, info fs.FileInfo) error
	AddSymlink(name string, info fs.FileInfo, link string) error
	Close() error
}

// writeArchive walks sources and feeds entries to s. A source "-" reads
// from r (only valid as the single source; Archive has already truncated
// the source list to that single "-"). Directories are walked recursively
// with entry paths relative to the directory itself. All sources must
// already be validated by the caller.
func writeArchive(sources []string, r io.Reader, s sink) (err error) {
	defer func() {
		if e := s.Close(); err == nil && e != nil {
			err = e
		}
	}()

	if len(sources) == 1 && sources[0] == "-" {
		tmp, err := os.CreateTemp("", "tgz-stdin-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()

		size, err := io.Copy(tmp, r)
		if err != nil {
			return err
		}
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return err
		}
		return s.AddFile("stdin", fakeInfo{
			name: "stdin",
			mode: 0o644,
			mod:  time.Now(),
			size: size,
		}, size, tmp)
	}

	for _, src := range sources {
		info, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := addDirToSink(s, src); err != nil {
				return err
			}
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			// Original CLI behavior for a symlink used as a top-level source:
			// os.Open follows the link, but tar.FileInfoHeader(Lstat info, "")
			// emits a Size-0 TypeSymlink header, so io.Copy fails with
			// "archive/tar: write too long". Keep that failure for both
			// formats rather than silently packing the link target into a
			// broken entry. Symlinks found inside a walked directory are
			// handled correctly by addDirToSink below.
			return fmt.Errorf("archive/tar: write too long")
		}
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		e := s.AddFile(filepath.Base(src), info, info.Size(), f)
		f.Close()
		if e != nil {
			return e
		}
	}
	return nil
}

func addDirToSink(s sink, dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := filepath.Base(filepath.Clean(dir))
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.Join(base, rel)
		if rel == "." {
			name = base
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return s.AddSymlink(name, info, link)
		}
		if d.IsDir() {
			return s.AddDir(name, info)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		e := s.AddFile(name, info, info.Size(), f)
		f.Close()
		return e
	})
}

// fakeInfo is the fs.FileInfo for the synthetic "stdin" entry.
type fakeInfo struct {
	name string
	mode fs.FileMode
	mod  time.Time
	size int64
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return f.mod }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

// tarSink writes gzipped tar.
type tarSink struct {
	gzw *gzip.Writer
	tw  *tar.Writer
}

func newTarSink(w io.Writer) *tarSink {
	gzw := gzip.NewWriter(w)
	return &tarSink{gzw: gzw, tw: tar.NewWriter(gzw)}
}

func (s *tarSink) AddFile(name string, info fs.FileInfo, size int64, r io.Reader) error {
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	if err := s.tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(s.tw, r)
	return err
}

func (s *tarSink) AddDir(name string, info fs.FileInfo) error {
	return s.tw.WriteHeader(&tar.Header{
		Name:     name + "/",
		Typeflag: tar.TypeDir,
		Mode:     int64(info.Mode().Perm()),
		ModTime:  info.ModTime(),
	})
}

func (s *tarSink) AddSymlink(name string, info fs.FileInfo, link string) error {
	return s.tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeSymlink,
		Linkname: link,
		Mode:     int64(info.Mode().Perm()),
		ModTime:  info.ModTime(),
	})
}

func (s *tarSink) Close() error {
	if err := s.tw.Close(); err != nil {
		s.gzw.Close()
		return err
	}
	return s.gzw.Close()
}
