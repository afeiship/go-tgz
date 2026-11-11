package tgz_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/afeiship/go-tgz"
)

func TestArchiveStdinMode(t *testing.T) {
	outDir := t.TempDir()
	res, err := tgz.Archive([]string{"-"},
		tgz.WithReader(bytes.NewReader([]byte("piped data"))),
		tgz.WithDir(outDir),
		tgz.WithTime(fixedTime),
	)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if res.Name != "bk_20261110-203005.tar.gz" {
		t.Errorf("Name = %q, want bk_20261110-203005.tar.gz", res.Name)
	}
	entries := readEntriesFile(t, res.Path)
	e, ok := entries["stdin"]
	if !ok {
		t.Fatalf("entry stdin missing, got %v", entries)
	}
	if e.typeflag != tar.TypeReg || e.content != "piped data" {
		t.Errorf("entry stdin = %+v, want reg file with content %q", e, "piped data")
	}
}

func TestArchiveStdinWithoutReader(t *testing.T) {
	_, err := tgz.Archive([]string{"-"}, tgz.WithDir(t.TempDir()))
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("reader is required")) {
		t.Errorf("err = %v, want reader is required", err)
	}
}

func TestArchiveStreamMode(t *testing.T) {
	var buf bytes.Buffer
	res, err := tgz.Archive([]string{"-"},
		tgz.WithReader(bytes.NewReader([]byte("streamed"))),
		tgz.WithWriter(&buf),
		tgz.WithTime(fixedTime),
	)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if res.Path != "" {
		t.Errorf("Path = %q, want empty in stream mode", res.Path)
	}
	if res.Name != "bk_20261110-203005.tar.gz" {
		t.Errorf("Name = %q, want bk_20261110-203005.tar.gz", res.Name)
	}
	entries := readEntries(t, &buf)
	if entries["stdin"].content != "streamed" {
		t.Errorf("entries = %v, want stdin entry with content %q", entries, "streamed")
	}
}

func TestTarGzBaselineAfterRefactor(t *testing.T) {
	fixture := writeFixture(t)
	outDir := t.TempDir()
	res, err := tgz.Archive([]string{filepath.Join(fixture, "demo")},
		tgz.WithDir(outDir), tgz.WithTime(fixedTime))
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

func TestArchiveZipStreamMode(t *testing.T) {
	var buf bytes.Buffer
	res, err := tgz.Archive([]string{"-"},
		tgz.WithReader(bytes.NewReader([]byte("streamed"))),
		tgz.WithWriter(&buf),
		tgz.WithTime(fixedTime),
		tgz.WithFormat(tgz.FormatZip),
	)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if res.Path != "" {
		t.Errorf("Path = %q, want empty in stream mode", res.Path)
	}
	if res.Name != "bk_20261110-203005.zip" {
		t.Errorf("Name = %q, want bk_20261110-203005.zip", res.Name)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "stdin" {
		t.Fatalf("zip entries = %v, want single stdin entry", zr.File)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "streamed" {
		t.Errorf("stdin content = %q, want streamed", b)
	}
}

func TestArchiveZipRemovesPartialOnFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod-000 does not block root from reading the file")
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
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
	if _, err := tgz.Archive([]string{srcDir},
		tgz.WithDir(outDir), tgz.WithTime(fixedTime), tgz.WithFormat(tgz.FormatZip)); err == nil {
		t.Fatal("Archive: expected error, got nil")
	}
	leftovers, err := filepath.Glob(filepath.Join(outDir, "*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("partial zip left behind: %v", leftovers)
	}
}
