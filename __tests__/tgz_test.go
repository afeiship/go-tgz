package tgz_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
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

// readEntriesFile is readEntries for a file path.
func readEntriesFile(t *testing.T, path string) map[string]entry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
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
