package tgz_test

import (
	"archive/tar"
	"bytes"
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
