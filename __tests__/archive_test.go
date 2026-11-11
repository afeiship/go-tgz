package tgz_test

import (
	"archive/tar"
	"bytes"
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
