# Zip Output & AES Password Encryption Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `FormatZip` output and WinZip AES-256 password encryption to go-tgz while keeping tar.gz as the unchanged default.

**Architecture:** Introduce a format-agnostic `sink` interface and shared `writeArchive` walker (path construction, symlink/dir/stdin handling), with `tarSink` (existing gzip→tar) and `zipSink` (single `github.com/alexmullins/zip` writer for both plain and encrypted zip). Only regular-file data is encrypted; dir and symlink entries stay plain for libarchive compatibility. `Filename` keeps its 5-arg public signature; an internal `filenameWithExt` supplies the `.tar.gz`/`.zip` extension.

**Tech Stack:** Go 1.26.0 module directive (toolchain go1.27.3), `archive/tar` + `compress/gzip`, `github.com/alexmullins/zip` (pseudo-version `v0.0.0-20180717182244-4affb64b04d0`), `golang.org/x/crypto v0.58.0` (indirect, pinned). Tests via `npm test` → `go test __tests__/*.go -v`.

**Spec:** `docs/superpowers/specs/2026-11-11-zip-format-and-encryption-design.md`

---

## File Map

| File | Responsibility |
| --- | --- |
| `go.mod` | go directive → 1.26.0; add `alexmullins/zip`, pin `x/crypto v0.58.0` |
| `go.sum` | generated checksums (committed) |
| `tgz.go` | `Format` type/consts, `WithFormat`, `WithPassword`, `format`/`password` options fields, format dispatch, combination error |
| `filename.go` | internal `filenameWithExt`; public `Filename` delegates with `.tar.gz` |
| `archive.go` | `sink` interface, `writeArchive` walker, `tarSink` (port of existing code) |
| `zip.go` (new) | `zipSink` implementation using `alexmullins/zip` |
| `__tests__/tgz_test.go` | extend `readEntries` to dispatch on zip; add format/password/combination tests |
| `__tests__/archive_test.go` | add zip-specific stdlib-compat and encryption round-trip tests |
| `README.md` | new options, examples, compatibility/risk notes |
| `llms.txt` | sync API/options/behavior notes |

All tasks are on `main`, no worktree. **Do not push to remote** (global rule).

---

## Task 1: Dependencies and go directive

**Files:**
- Modify: `go.mod`
- Create: `go.sum` (generated)

- [x] **Step 1: Add dependencies with pinned versions**

Run from `/Users/afei/github/go-tgz`:

```bash
http_proxy=http://127.0.0.1:9090 https_proxy=http://127.0.0.1:9090 go get github.com/alexmullins/zip@v0.0.0-20180717182244-4affb64b04d0
http_proxy=http://127.0.0.1:9090 https_proxy=http://127.0.0.1:9090 go get golang.org/x/crypto@v0.58.0
```

Then change the `go` directive in `go.mod` to `1.26.0`. Do not hand-edit the `require` block: `go get` writes it and `go mod tidy` normalizes it. Final `go.mod` must be:

```
module github.com/afeiship/go-tgz

go 1.26.0

require github.com/alexmullins/zip v0.0.0-20180717182244-4affb64b04d0

require golang.org/x/crypto v0.58.0 // indirect
```

- [x] **Step 2: Tidy and verify build**

```bash
go mod tidy
go build ./...
npm test
```

Expected: build succeeds; all 14 existing tests PASS (regression baseline). `go.sum` is created.

- [x] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add alexmullins/zip dependency and bump go directive"
```

---

## Task 2: Parameterize the filename extension (internal helper)

**Files:**
- Modify: `filename.go:1-17`
- Test: `__tests__/filename_test.go`

- [x] **Step 1: Write the failing test for zip extension**

Append to `__tests__/filename_test.go`:

```go
func TestFilenameWithExt(t *testing.T) {
	now := time.Date(2026, 11, 8, 15, 30, 5, 0, time.UTC)
	// filenameWithExt is unexported; verified indirectly through Archive in
	// Task 5. This test locks the public helper's unchanged tar.gz behavior.
	if got := tgz.Filename("demo", "", "20060102", "", now); got != "demo_20261108.tar.gz" {
		t.Errorf("Filename = %q, want demo_20261108.tar.gz", got)
	}
}
```

- [x] **Step 2: Run to confirm it passes (locks the baseline)**

```bash
go test __tests__/filename_test.go -v
```

Expected: PASS (all 7 tests including the new one).

- [x] **Step 3: Implement `filenameWithExt`**

Replace the entire contents of `filename.go` with:

```go
package tgz

import "time"

// Filename generates the tar.gz archive filename:
// [{pre}_]{base}_{tsLayout}[_{suf}].tar.gz
// The base is always kept; pre adds a leading tag, suf appends one.
// An invalid tsLayout is emitted verbatim, as Go's time.Format does.
func Filename(base, pre, tsLayout, suf string, t time.Time) string {
	return filenameWithExt(base, pre, tsLayout, suf, t, ".tar.gz")
}

// filenameWithExt is the shared implementation; ext is chosen by Archive
// from the resolved format so name and content never disagree.
func filenameWithExt(base, pre, tsLayout, suf string, t time.Time, ext string) string {
	if pre != "" {
		base = pre + "_" + base
	}
	name := base + "_" + t.Format(tsLayout)
	if suf != "" {
		name += "_" + suf
	}
	return name + ext
}
```

- [x] **Step 4: Run tests**

```bash
npm test
```

Expected: all 15 tests PASS.

- [x] **Step 5: Commit**

```bash
git add filename.go __tests__/filename_test.go
git commit -m "refactor: parameterize archive filename extension"
```

---

## Task 3: Extract `sink` and `writeArchive` walker, port tar.gz to `tarSink`

**Files:**
- Modify: `archive.go:1-155` (full rewrite)
- Test: `__tests__/tgz_test.go`, `__tests__/archive_test.go` (unchanged; regression)

- [x] **Step 1: Write a regression marker test (no new behavior)**

Append to `__tests__/archive_test.go`:

```go
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
```

- [x] **Step 2: Run baseline**

```bash
npm test
```

Expected: all 16 tests PASS (this test already passes against current code; it is the guard for the refactor).

- [x] **Step 3: Rewrite `archive.go`**

Replace the entire contents of `archive.go` with:

```go
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
func writeArchive(w io.Writer, sources []string, r io.Reader, s sink) (err error) {
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
```

- [x] **Step 4: Update `tgz.go` call sites (still tar.gz only)**

In `tgz.go`, replace both occurrences of:

```go
writeTarGz(o.writer, sources, o.reader)
```
and
```go
writeTarGz(out, sources, o.reader)
```

with:

```go
writeArchive(o.writer, sources, o.reader, newTarSink(o.writer))
```
and
```go
writeArchive(out, sources, o.reader, newTarSink(out))
```

No other changes to `tgz.go` in this task.

- [x] **Step 5: Run full regression**

```bash
go build ./...
npm test
```

Expected: build succeeds; all 16 tests PASS. Specifically `TestArchiveWriterFlushError` must still pass (gzip flush error propagated through `writeArchive`'s deferred Close), and `TestArchiveRemovesPartialOnFailure` must still pass (error from `os.Open` on chmod-000 file propagates unchanged).

- [x] **Step 6: Commit**

```bash
git add archive.go tgz.go __tests__/archive_test.go
git commit -m "refactor: extract format-agnostic archive walker and tar sink"
```

---

## Task 4: Add `Format`, `WithFormat`, and `zipSink` (unencrypted zip)

**Files:**
- Modify: `tgz.go:1-120`
- Create: `zip.go`
- Test: `__tests__/tgz_test.go`, `__tests__/archive_test.go`

- [x] **Step 1: Write failing tests for zip format**

Append to `__tests__/tgz_test.go`:

```go
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
```

- [x] **Step 2: Run to verify failure**

```bash
go test __tests__/tgz_test.go -run 'TestArchiveZip' -v
```

Expected: compile FAIL — `tgz.WithFormat` and `tgz.FormatZip` undefined.

- [x] **Step 3: Add `Format` and `WithFormat` to `tgz.go`**

Add after the `stdinBase` constant:

```go
// Format selects the archive container.
type Format uint8

const (
	FormatTarGz Format = iota
	FormatZip
)
```

Add `format Format` and `password string` to the `options` struct (password is consumed in Task 6, declared now so the struct is stable):

```go
type options struct {
	prefix   string
	suffix   string
	dir      string
	ts       string
	writer   io.Writer
	reader   io.Reader
	now      time.Time
	format   Format
	password string
}
```

Add the option after `WithTime`:

```go
// WithFormat selects the archive format. Default FormatTarGz.
func WithFormat(f Format) Option { return func(o *options) { o.format = f } }

// WithPassword sets the AES-256 password for zip archives. An empty
// string means no encryption. Only valid with FormatZip.
func WithPassword(pwd string) Option { return func(o *options) { o.password = pwd } }
```

- [x] **Step 4: Add `zip.go`**

Create `zip.go`:

```go
package tgz

import (
	"io"
	"io/fs"
	"path/filepath"

	azip "github.com/alexmullins/zip"
)

// zipSink writes zip archives. The same alexmullins/zip writer is used for
// both plain and encrypted output so there is a single zip code path.
// Only regular-file data is encrypted: encrypting dir/symlink entries
// makes libarchive (macOS Archive Utility, bsdtar) fail with
// "Unsupported ZIP compression method (99: aes)".
type zipSink struct {
	zw  *azip.Writer
	pwd string
}

func newZipSink(w io.Writer, pwd string) *zipSink {
	return &zipSink{zw: azip.NewWriter(w), pwd: pwd}
}

func (s *zipSink) newHeader(name string, info fs.FileInfo) (*azip.FileHeader, error) {
	hdr, err := azip.FileInfoHeader(info)
	if err != nil {
		return nil, err
	}
	hdr.Name = filepath.ToSlash(name)
	hdr.Method = azip.Deflate
	return hdr, nil
}

func (s *zipSink) AddFile(name string, info fs.FileInfo, size int64, r io.Reader) error {
	hdr, err := s.newHeader(name, info)
	if err != nil {
		return err
	}
	if s.pwd != "" {
		hdr.SetPassword(s.pwd)
	}
	w, err := s.zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

func (s *zipSink) AddDir(name string, info fs.FileInfo) error {
	hdr, err := s.newHeader(name+"/", info)
	if err != nil {
		return err
	}
	_, err = s.zw.CreateHeader(hdr)
	return err
}

func (s *zipSink) AddSymlink(name string, info fs.FileInfo, link string) error {
	hdr, err := s.newHeader(name, info)
	if err != nil {
		return err
	}
	w, err := s.zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, link)
	return err
}

func (s *zipSink) Close() error {
	return s.zw.Close()
}
```

- [x] **Step 5: Wire format dispatch into `Archive` in `tgz.go`**

Replace the name computation line:

```go
	name := Filename(base, o.prefix, o.ts, o.suffix, o.now)
```

with:

```go
	ext := ".tar.gz"
	if o.format == FormatZip {
		ext = ".zip"
	}
	name := filenameWithExt(base, o.prefix, o.ts, o.suffix, o.now, ext)
```

Replace the two `writeArchive(...newTarSink(...))` call sites with a helper. Add above `Archive`:

```go
func (o *options) newSink(w io.Writer) sink {
	if o.format == FormatZip {
		return newZipSink(w, o.password)
	}
	return newTarSink(w)
}
```

and change the call sites to:

```go
	if err := writeArchive(o.writer, sources, o.reader, o.newSink(o.writer)); err != nil {
```
and
```go
	if err := writeArchive(out, sources, o.reader, o.newSink(out)); err != nil {
```

- [x] **Step 6: Extend `readEntries` in `__tests__/tgz_test.go` to dispatch on zip**

Replace the body of `readEntries` so it accepts any archive reader and branches on the source filename. Because `readEntries` is shared by `read(t, r)` helpers that don't know the filename, change the helper signatures as follows:

```go
// readEntries unpacks a tar.gz stream.
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
```

Add `"archive/zip"`, `"io/fs"` to `__tests__/tgz_test.go` imports, and `"bytes"` (needed by `TestArchiveZipPasswordStreamMode` in Task 5). Change `readEntriesFile` to:

```go
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
```

For the stream-mode test (`TestArchiveStreamMode` in `__tests__/archive_test.go`), tar.gz remains the default so `readEntries(t, &buf)` is unchanged. Add a separate zip stream test in Task 4 Step 7.

- [x] **Step 7: Add failing zip-specific tests**

Append to `__tests__/archive_test.go` (add `"archive/zip"` and `"io/fs"` imports if not already present; `bytes` is already imported):

```go
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
```

- [x] **Step 8: Run all tests**

```bash
go build ./...
npm test
```

Expected: all 20 tests PASS. `TestArchiveZipRoundTrip` proves dir/nested/empty-dir/symlink/中文-compatible naming through the standard library reader.

- [x] **Step 9: Manual interoperability check (unencrypted zip)**

```bash
go test __tests__/*.go -run TestArchiveZipRoundTrip -v >/dev/null
# create a real zip via a tiny throwaway program is unnecessary; instead:
# the stdlib archive/zip reader in readZipEntries is the compatibility check.
```

Additionally confirm with an external tool by generating one zip through the test helper is not needed — `readZipEntries` uses the standard library, and the earlier design-phase probe already confirmed `bsdtar` reads plain `alexmullins/zip` output including symlink and Chinese names.

- [x] **Step 10: Commit**

```bash
git add tgz.go zip.go __tests__/tgz_test.go __tests__/archive_test.go
git commit -m "feat: add zip format output via alexmullins/zip"
```

---

## Task 5: Add `WithPassword` enforcement and encrypted zip tests

**Files:**
- Modify: `tgz.go:70-120`
- Test: `__tests__/tgz_test.go`, `__tests__/archive_test.go`

- [x] **Step 1: Write failing tests for password behavior**

Append to `__tests__/archive_test.go`:

```go
func TestArchiveZipPasswordRequiresZipFormat(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "demo.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := tgz.Archive([]string{src},
		tgz.WithDir(t.TempDir()), tgz.WithTime(fixedTime), tgz.WithPassword("pass"))
	if err == nil || !strings.Contains(err.Error(), "password requires zip format") {
		t.Errorf("err = %v, want password requires zip format", err)
	}
	if strings.Contains(err.Error(), "pass") {
		t.Errorf("error leaked the password: %v", err)
	}
}

func TestArchiveZipEncryptedRoundTrip(t *testing.T) {
	fixture := writeFixture(t)
	outDir := t.TempDir()
	res, err := tgz.Archive([]string{filepath.Join(fixture, "demo")},
		tgz.WithDir(outDir), tgz.WithTime(fixedTime),
		tgz.WithFormat(tgz.FormatZip), tgz.WithPassword("s3cret-pw"))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	rc, err := azip.OpenReader(res.Path)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer rc.Close()

	for _, f := range rc.File {
		if f.Mode().IsDir() || f.Mode()&fs.ModeSymlink != 0 {
			if f.IsEncrypted() {
				t.Errorf("metadata entry %q must stay unencrypted", f.Name)
			}
		} else if !f.IsEncrypted() {
			t.Errorf("file entry %q must be encrypted", f.Name)
		}
	}

	// correct password
	var hello *azip.File
	for _, f := range rc.File {
		if f.Name == "demo/a.txt" {
			hello = f
			break
		}
	}
	if hello == nil {
		t.Fatal("demo/a.txt missing")
	}
	hello.SetPassword("s3cret-pw")
	r, err := hello.Open()
	if err != nil {
		t.Fatalf("Open with correct password: %v", err)
	}
	b, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(b) != "alpha" {
		t.Errorf("content = %q, err = %v, want alpha", b, err)
	}

	// wrong password on a different file entry
	var beta *azip.File
	for _, f := range rc.File {
		if f.Name == "demo/sub/b.txt" {
			beta = f
			break
		}
	}
	if beta == nil {
		t.Fatal("demo/sub/b.txt missing")
	}
	beta.SetPassword("wrong")
	r2, err := beta.Open()
	if err == nil {
		b2, e2 := io.ReadAll(r2)
		r2.Close()
		if e2 == nil {
			t.Errorf("wrong password returned content %q with no error", b2)
		}
	} else if strings.Contains(err.Error(), "s3cret-pw") {
		t.Errorf("error leaked the password: %v", err)
	}
}

func TestArchiveZipEncryptedStdin(t *testing.T) {
	outDir := t.TempDir()
	res, err := tgz.Archive([]string{"-"},
		tgz.WithReader(strings.NewReader("piped")),
		tgz.WithDir(outDir), tgz.WithTime(fixedTime),
		tgz.WithFormat(tgz.FormatZip), tgz.WithPassword("pw"))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	rc, err := azip.OpenReader(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if len(rc.File) != 1 || !rc.File[0].IsEncrypted() {
		t.Fatalf("want single encrypted stdin entry, got %v", rc.File)
	}
	rc.File[0].SetPassword("pw")
	r, err := rc.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil || string(b) != "piped" {
		t.Errorf("content = %q, err = %v, want piped", b, err)
	}
}
```

Append to `__tests__/tgz_test.go`:

```go
func TestArchiveZipPasswordStreamMode(t *testing.T) {
	var buf bytes.Buffer
	_, err := tgz.Archive([]string{"-"},
		tgz.WithReader(strings.NewReader("data")),
		tgz.WithWriter(&buf), tgz.WithTime(fixedTime),
		tgz.WithFormat(tgz.FormatZip), tgz.WithPassword("pw"))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	rc, err := azip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if len(rc.File) != 1 || !rc.File[0].IsEncrypted() {
		t.Fatalf("want single encrypted stdin entry, got %v", rc.File)
	}
}
```

Add imports to `__tests__/archive_test.go`: `"strings"`, `"io/fs"`, `"path/filepath"`, `"io"`, `azip "github.com/alexmullins/zip"`. Add imports to `__tests__/tgz_test.go`: `azip "github.com/alexmullins/zip"` (`"bytes"` was already added in Task 4 Step 6).

- [x] **Step 2: Run to verify failure**

```bash
go test __tests__/*.go -run 'TestArchiveZipPassword|TestArchiveZipEncrypted' -v
```

Expected: `TestArchiveZipPasswordRequiresZipFormat` FAILS (no enforcement yet); encrypted round-trip tests PASS because `newZipSink` already consumes `o.password` (wired in Task 4 Step 4). That is acceptable — the failing test drives the remaining change.

- [x] **Step 3: Add combination-error enforcement in `Archive`**

In `tgz.go`, immediately after the `if len(sources) == 0 { ... }` block, add:

```go
	if o.password != "" && o.format != FormatZip {
		return nil, fmt.Errorf("password requires zip format")
	}
```

- [x] **Step 4: Run all tests**

```bash
go build ./...
npm test
```

Expected: all 24 tests PASS.

- [x] **Step 5: Manual interoperability check (encrypted zip)**

Generate a fixture zip with a throwaway program in a fresh temp dir (never inside the repo):

```bash
T=$(mktemp -d)
cd "$T"
go mod init probe >/dev/null 2>&1
# use the local module via a replace pointing at the repo
go mod edit -replace github.com/afeiship/go-tgz=/Users/afei/github/go-tgz
go get github.com/afeiship/go-tgz
printf 'hello' > a.txt
cat > main.go <<'EOF'
package main

import (
	"log"
	"github.com/afeiship/go-tgz"
)

func main() {
	_, err := tgz.Archive([]string{"a.txt"},
		tgz.WithDir("."), tgz.WithFormat(tgz.FormatZip), tgz.WithPassword("pass"))
	if err != nil { log.Fatal(err) }
}
EOF
go run .
zipinfo -v a_*.zip | grep -E 'compression method|file security' | head -4
mkdir out && bsdtar -xf a_*.zip -C out --passphrase pass && cat out/a.txt
# expected output: method 99 / encrypted, then "hello"
cd /
rm -rf "$T"
```

Expected: `zipinfo` reports `unknown (99)` and `encrypted`; `bsdtar` prints `hello`. Wrong-password behavior was already confirmed during the design phase.

- [x] **Step 6: Commit**

```bash
git add tgz.go __tests__/tgz_test.go __tests__/archive_test.go
git commit -m "feat: add AES-256 password encryption for zip archives"
```

---

## Task 6: Documentation sync (README.md and llms.txt)

**Files:**
- Modify: `README.md`
- Modify: `llms.txt`

- [x] **Step 1: Update `README.md`**

In the Options table, add two rows after `WithTime`:

```
| `WithFormat(f)` | `FormatTarGz` | `FormatZip` writes a `.zip` archive |
| `WithPassword(pwd)` | `""` | AES-256 password for zip; empty means no encryption; requires `FormatZip` |
```

Add after the existing Usage example:

```go
	// zip output
	res, _ = tgz.Archive([]string{"demo.txt"}, tgz.WithFormat(tgz.FormatZip))
	// → ./demo_20261110-203005.zip

	// encrypted zip (opens with a password prompt in macOS Archive Utility / 7-Zip)
	res, _ = tgz.Archive([]string{"demo.txt"},
		tgz.WithFormat(tgz.FormatZip), tgz.WithPassword("strong-passphrase"))
```

Add to the Behavior section:

```
- `FormatZip` uses `github.com/alexmullins/zip` (WinZip AES-256 / AE-2) for both plain and encrypted output; plain zip is readable by `archive/zip`, `bsdtar` and Python `zipfile`
- with `WithPassword`, only regular-file data is encrypted; filenames, directory structure and symlink targets remain visible in the central directory (standard WinZip AES behavior)
- encrypted zip opens in macOS Archive Utility, 7-Zip and WinZip; Info-ZIP `unzip` and Python `zipfile` cannot decrypt AES entries
- `WithPassword` with the default tar.gz format returns `password requires zip format`
- WinZip AE-2 fixes PBKDF2 at 1000 iterations (cannot be raised without breaking other tools): use a strong password
```

Add to the Errors section:

```
- `password requires zip format` — `WithPassword` used without `WithFormat(FormatZip)`
```

Add to Installation a note:

```
Requires Go 1.26+ (raised for `golang.org/x/crypto v0.58.0`).
```

- [x] **Step 2: Update `llms.txt`**

Replace the API bullet for `Archive` description to mention format/password, and add:

```
- tgz.Format / FormatTarGz / FormatZip — archive container selector
- tgz.WithFormat(Format) — default FormatTarGz; FormatZip writes .zip
- tgz.WithPassword(string) — WinZip AES-256 password; empty = no encryption; errors with "password requires zip format" when format is tar.gz; only regular-file data encrypted, filenames stay visible
```

Add to Behavior notes:

```
- Zip output uses github.com/alexmullins/zip (fork of archive/zip with WinZip AE-2). Plain zip interoperates with stdlib archive/zip, bsdtar, Python zipfile; encrypted zip needs macOS Archive Utility / 7-Zip / WinZip (Info-ZIP unzip cannot decrypt AES). Dir and symlink entries are NOT encrypted — encrypting them breaks libarchive extraction.
- Go directive is 1.26.0; golang.org/x/crypto v0.58.0 pinned indirect.
```

Update the Errors list with the new error string. Keep the "standard library only" line removed — it is no longer true.

- [x] **Step 3: Verify docs match code**

```bash
grep -n "WithFormat\|WithPassword\|FormatZip\|password requires zip" README.md llms.txt
go build ./...
npm test
```

Expected: grep shows all entries; build and all 24 tests PASS.

- [x] **Step 4: Commit**

```bash
git add README.md llms.txt
git commit -m "docs: document zip format and AES password options"
```

---

## Task 7: Final verification

- [x] **Step 1: Full test suite + vet**

```bash
go vet ./...
npm test
```

Expected: vet clean; all 24 tests PASS.

- [x] **Step 2: Dependency audit**

```bash
go list -m all
git status --short
```

Expected: module list contains `github.com/alexmullins/zip v0.0.0-20180717182244-4affb64b04d0` and `golang.org/x/crypto v0.58.0`; working tree clean; nothing pushed.

- [x] **Step 3: Report**

Summarize: 2 new options, 1 new file (`zip.go`), refactored `archive.go`/`tgz.go`/`filename.go`, 24 tests passing, docs synced, no remote push performed.

---

## Spec Coverage Map

| Spec section | Task |
| --- | --- |
| `alexmullins/zip` as sole zip writer | 1, 4 |
| go 1.26.0 + pinned x/crypto | 1 |
| default unchanged / no breaking API | 2, 3, 4 |
| `FilenameWithExt` internal helper | 2 |
| `sink` + `writeArchive` extraction | 3 |
| `Format` / `WithFormat` / `WithPassword` | 4, 5 |
| dir & symlink unencrypted; file data encrypted | 4, 5 |
| Close error propagation (HMAC) | 3, 4 |
| combination error | 5 |
| password never in errors/Result | 5 |
| tar.gz regressions (flush error, partial removal, stdin, stream, dirs, symlinks, Chinese, options matrix) | 3, 4, 5, 7 |
| zip tests (plain, stream, partial removal, stdlib compat) | 4 |
| encrypted tests (round trip, wrong password, stdin, stream, metadata unencrypted) | 5 |
| manual interoperability (`zipinfo`, `bsdtar`) | 5 |
| README + llms.txt sync | 6 |
| security/risk notes in docs | 6 |
