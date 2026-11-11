package tgz_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/afeiship/go-tgz"
)

var fixedTime = time.Date(2026, 11, 10, 20, 30, 5, 0, time.UTC)

// entry is one unpacked tar record.
type entry struct {
	typeflag byte
	link     string
	content  string
}

// readEntries unpacks a tar.gz stream and returns entries keyed by name
// (trailing "/" of dir entries stripped).
func readEntries(t *testing.T, r io.Reader) map[string]entry {
	t.Helper()
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	m := map[string]entry{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return m
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		e := entry{typeflag: hdr.Typeflag, link: hdr.Linkname}
		if hdr.Typeflag == tar.TypeReg {
			buf, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read %s: %v", hdr.Name, err)
			}
			e.content = string(buf)
		}
		m[strings.TrimSuffix(hdr.Name, "/")] = e
	}
}

// readZipEntries unpacks an unencrypted zip stream via the standard
// library, proving the output is interoperable.
func readZipEntries(t *testing.T, r io.Reader, size int64) map[string]entry {
	t.Helper()
	zr, err := zip.NewReader(r.(io.ReaderAt), size)
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	m := map[string]entry{}
	for _, f := range zr.File {
		e := entry{}
		switch {
		case f.Mode().IsDir():
			e.typeflag = tar.TypeDir
		case f.Mode()&fs.ModeSymlink != 0:
			e.typeflag = tar.TypeSymlink
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open symlink %s: %v", f.Name, err)
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read symlink %s: %v", f.Name, err)
			}
			e.link = string(b)
		default:
			e.typeflag = tar.TypeReg
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", f.Name, err)
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read %s: %v", f.Name, err)
			}
			e.content = string(b)
		}
		m[strings.TrimSuffix(f.Name, "/")] = e
	}
	return m
}

// readEntriesFile is readEntries for a file path.
func readEntriesFile(t *testing.T, path string) map[string]entry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	if strings.HasSuffix(path, ".zip") {
		info, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		return readZipEntries(t, f, info.Size())
	}
	return readEntries(t, f)
}

func TestArchiveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "demo.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	res, err := tgz.Archive([]string{src}, tgz.WithDir(outDir), tgz.WithTime(fixedTime))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	wantName := "demo_20261110-203005.tar.gz"
	if res.Name != wantName {
		t.Errorf("Name = %q, want %q", res.Name, wantName)
	}
	wantPath := filepath.Join(outDir, wantName)
	if res.Path != wantPath {
		t.Errorf("Path = %q, want %q", res.Path, wantPath)
	}

	entries := readEntriesFile(t, wantPath)
	e, ok := entries["demo.txt"]
	if !ok {
		t.Fatalf("entry demo.txt missing, got %v", entries)
	}
	if e.typeflag != tar.TypeReg || e.content != "hello" {
		t.Errorf("entry demo.txt = %+v, want reg file with content %q", e, "hello")
	}
}

func TestArchiveStdinRejectsExtraSources(t *testing.T) {
	_, err := tgz.Archive([]string{"-", "extra.txt"},
		tgz.WithReader(strings.NewReader("x")), tgz.WithDir(t.TempDir()))
	if err == nil {
		t.Fatal("Archive: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "stdin mode takes a single source") {
		t.Errorf("error = %q, want substring %q", err.Error(), "stdin mode takes a single source")
	}
}

func TestArchiveRemovesPartialOnFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod-000 does not block root from reading the file")
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	bPath := filepath.Join(srcDir, "b.txt")
	if err := os.WriteFile(bPath, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(bPath, 0o644) })

	outDir := t.TempDir()
	if _, err := tgz.Archive([]string{srcDir}, tgz.WithDir(outDir), tgz.WithTime(fixedTime)); err == nil {
		t.Fatal("Archive: expected error, got nil")
	}
	leftovers, err := filepath.Glob(filepath.Join(outDir, "*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("partial archive left behind: %v", leftovers)
	}
}

// failWriter accepts the first write (the gzip header) and fails every
// subsequent one, so the failure only surfaces when the gzip writer
// flushes its buffered data on Close.
type failWriter struct {
	writes int
}

func (w *failWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		return len(p), nil
	}
	return 0, io.ErrShortWrite
}

func TestArchiveWriterFlushError(t *testing.T) {
	if _, err := tgz.Archive([]string{"-"},
		tgz.WithReader(strings.NewReader("data")), tgz.WithWriter(&failWriter{})); err == nil {
		t.Fatal("Archive: expected flush error, got nil")
	}
}

// writeFixture creates a temp source tree:
// demo/a.txt, demo/sub/b.txt, demo/empty/ (empty dir), demo/link.txt -> a.txt
func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"demo/sub", "demo/empty"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "demo", "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo", "sub", "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(dir, "demo", "link.txt")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestArchiveDirectory(t *testing.T) {
	fixture := writeFixture(t)
	outDir := t.TempDir()

	res, err := tgz.Archive([]string{filepath.Join(fixture, "demo")}, tgz.WithDir(outDir), tgz.WithTime(fixedTime))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if res.Name != "demo_20261110-203005.tar.gz" {
		t.Errorf("Name = %q, want demo_20261110-203005.tar.gz", res.Name)
	}

	entries := readEntriesFile(t, res.Path)
	want := map[string]entry{
		"demo":           {typeflag: tar.TypeDir},
		"demo/a.txt":     {typeflag: tar.TypeReg, content: "alpha"},
		"demo/sub":       {typeflag: tar.TypeDir},
		"demo/sub/b.txt": {typeflag: tar.TypeReg, content: "beta"},
		"demo/empty":     {typeflag: tar.TypeDir},
		"demo/link.txt":  {typeflag: tar.TypeSymlink, link: "a.txt"},
	}
	for name, e := range want {
		got, ok := entries[name]
		if !ok {
			t.Errorf("entry %s missing, got %v", name, entries)
			continue
		}
		if got.typeflag != e.typeflag || got.content != e.content || got.link != e.link {
			t.Errorf("entry %s = %+v, want %+v", name, got, e)
		}
	}
}

func TestArchiveOptionsMatrix(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "demo.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()

	tests := []struct {
		name string
		opts []tgz.Option
		want string
	}{
		{"pre and suf", []tgz.Option{tgz.WithPrefix("snap"), tgz.WithSuffix("v1"), tgz.WithTime(fixedTime)}, "snap_demo_20261110-203005_v1.tar.gz"},
		{"custom ts", []tgz.Option{tgz.WithTimestamp("20060102"), tgz.WithTime(fixedTime)}, "demo_20261110.tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := append(tt.opts, tgz.WithDir(outDir))
			res, err := tgz.Archive([]string{src}, opts...)
			if err != nil {
				t.Fatalf("Archive: %v", err)
			}
			if res.Name != tt.want {
				t.Errorf("Name = %q, want %q", res.Name, tt.want)
			}
			if _, err := os.Stat(res.Path); err != nil {
				t.Errorf("archive not created: %v", err)
			}
		})
	}
}

func TestArchiveCreatesOutputDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "demo.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "nested", "backup")

	res, err := tgz.Archive([]string{src}, tgz.WithDir(outDir), tgz.WithTime(fixedTime))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Errorf("archive not created in auto-made dir: %v", err)
	}
}

func TestArchiveMultipleSources(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "first.txt")
	b := filepath.Join(dir, "second.txt")
	if err := os.WriteFile(a, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()

	res, err := tgz.Archive([]string{a, b}, tgz.WithDir(outDir), tgz.WithTime(fixedTime))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if res.Name != "first_20261110-203005.tar.gz" {
		t.Errorf("Name = %q, want first_20261110-203005.tar.gz", res.Name)
	}
	entries := readEntriesFile(t, res.Path)
	if entries["first.txt"].content != "1" || entries["second.txt"].content != "2" {
		t.Errorf("entries = %v, want both files packed", entries)
	}
}

func TestArchiveSourceNotFound(t *testing.T) {
	_, err := tgz.Archive([]string{"definitely-missing.txt"}, tgz.WithDir(t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "source not found: definitely-missing.txt") {
		t.Errorf("err = %v, want source not found", err)
	}
}

func TestArchiveNoSources(t *testing.T) {
	if _, err := tgz.Archive(nil); err == nil || !strings.Contains(err.Error(), "no sources") {
		t.Errorf("err = %v, want no sources error", err)
	}
}

func TestArchiveZipFilename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "demo.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	res, err := tgz.Archive([]string{src},
		tgz.WithDir(outDir), tgz.WithTime(fixedTime), tgz.WithFormat(tgz.FormatZip))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if res.Name != "demo_20261110-203005.zip" {
		t.Errorf("Name = %q, want demo_20261110-203005.zip", res.Name)
	}
	if res.Path != filepath.Join(outDir, "demo_20261110-203005.zip") {
		t.Errorf("Path = %q, want zip path", res.Path)
	}
}

func TestArchiveZipRoundTrip(t *testing.T) {
	fixture := writeFixture(t)
	outDir := t.TempDir()
	res, err := tgz.Archive([]string{filepath.Join(fixture, "demo")},
		tgz.WithDir(outDir), tgz.WithTime(fixedTime), tgz.WithFormat(tgz.FormatZip))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	entries := readEntriesFile(t, res.Path)
	want := map[string]entry{
		"demo":           {typeflag: tar.TypeDir},
		"demo/a.txt":     {typeflag: tar.TypeReg, content: "alpha"},
		"demo/sub":       {typeflag: tar.TypeDir},
		"demo/sub/b.txt": {typeflag: tar.TypeReg, content: "beta"},
		"demo/empty":     {typeflag: tar.TypeDir},
		"demo/link.txt":  {typeflag: tar.TypeSymlink, link: "a.txt"},
	}
	for name, e := range want {
		got, ok := entries[name]
		if !ok {
			t.Errorf("entry %s missing, got %v", name, entries)
			continue
		}
		if got.typeflag != e.typeflag || got.content != e.content || got.link != e.link {
			t.Errorf("entry %s = %+v, want %+v", name, got, e)
		}
	}
}
