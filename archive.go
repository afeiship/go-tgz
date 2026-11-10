package tgz

// The internals below are ported verbatim from aric-go/tgz cmd/archive.go.
// Deliberate deviation: close errors are checked so a failed gzip flush
// cannot be reported as success.

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// writeTarGz packs sources into gzipped tar, writing to w.
// A source "-" reads from r (only valid as the single source; Archive
// has already truncated the source list to that single "-").
// Directories are walked recursively with entry paths relative to the
// directory itself. All sources must already be validated by the caller.
func writeTarGz(w io.Writer, sources []string, r io.Reader) (err error) {
	gzw := gzip.NewWriter(w)
	defer func() {
		if e := gzw.Close(); err == nil && e != nil {
			err = e
		}
	}()
	tw := tar.NewWriter(gzw)
	defer func() {
		if e := tw.Close(); err == nil && e != nil {
			err = e
		}
	}()

	if len(sources) == 1 && sources[0] == "-" {
		return addFromReader(tw, r)
	}
	for _, src := range sources {
		info, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := addDir(tw, src); err != nil {
				return err
			}
			continue
		}
		if err := addFile(tw, src, info, filepath.Base(src)); err != nil {
			return err
		}
	}
	return nil
}

func addFile(tw *tar.Writer, path string, info fs.FileInfo, name string) error {
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
}

func addDir(tw *tar.Writer, dir string) error {
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
			// record the symlink itself, do not follow
			return addSymlink(tw, path, info, name)
		}
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{
				Name:     name + "/",
				Typeflag: tar.TypeDir,
				Mode:     int64(info.Mode().Perm()),
				ModTime:  info.ModTime(),
			})
		}
		return addFile(tw, path, info, name)
	})
}

func addSymlink(tw *tar.Writer, path string, info fs.FileInfo, name string) error {
	link, err := os.Readlink(path)
	if err != nil {
		return err
	}
	return tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeSymlink,
		Linkname: link,
		Mode:     int64(info.Mode().Perm()),
		ModTime:  info.ModTime(),
	})
}

// addFromReader packs r as a single "stdin" entry. The tar header needs
// the size upfront, so the data is staged through a temp file.
func addFromReader(tw *tar.Writer, r io.Reader) error {
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

	hdr := &tar.Header{
		Name:    "stdin",
		Mode:    0o644,
		Size:    size,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, tmp)
	return err
}
