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
