# go-tgz 独立库抽取实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `aric-go/tgz` CLI 的核心备份功能抽取为独立库 `github.com/afeiship/go-tgz`，并最终让 CLI 重构为依赖此库。

**Architecture:** 根目录单包 `tgz`，公开入口 `Archive(sources, opts...)` 一次调用完成校验/文件名生成/建目录/写 tar.gz；tar 写入内部实现按行为分任务移植；流式抽象通过 `io.Reader`/`io.Writer` 注入，不绑定标准流。测试放在 `__tests__/`（外部测试包 `package tgz_test`，经 `go test __tests__/*.go` 显式列文件运行，已探针验证可行）。

**Tech Stack:** Go 1.21，仅标准库（`archive/tar` + `compress/gzip`）；CLI 侧保留 Cobra/Viper。

**两仓库路径：**
- lib 仓库（Task 1-7）：`/Users/afei/github/go-tgz`
- CLI 仓库（Task 8）：`/Users/afei/github/aric-go/tgz`

**执行状态（2026-11-10）：**
- Task 1-7 ✅ 全部完成并通过双审（实现审查 + 质量审查），提交区间 `579f44b..91d14e4`（10 commits），22/22 测试通过
- 已批准的偏差：`Filename` 参数 `ts`→`tsLayout`；stdin 混入其他 source 由静默忽略改为报错；写入器 Close 错误检查 + 失败清理半成品归档（复审加固）；补充 MIT LICENSE
- Task 8 ⏸ 等待用户推送 go-tgz 并打 tag `v1.0.0` 后执行

**注意：** 测试命令统一用 `go test __tests__/*.go -v`（全量 glob，helpers 在 tgz_test.go 中共享；单跑某个文件会因缺 helper 编译失败，属预期）。

---

### Task 1: 修正模块路径并清理模板占位

**Files:**
- Modify: `/Users/afei/github/go-tgz/go.mod`
- Delete: `/Users/afei/github/go-tgz/pkgjson.go`
- Delete: `/Users/afei/github/go-tgz/__tests__/pkgjson_test.go`

- [ ] **Step 1: 修正 go.mod 模块路径**

将 go.mod 第一行改为（模块路径必须与 GitHub 仓库 afeiship/go-tgz 一致）：

```
module github.com/afeiship/go-tgz

go 1.21.0
```

- [ ] **Step 2: 删除模板占位文件**

```bash
rm /Users/afei/github/go-tgz/pkgjson.go /Users/afei/github/go-tgz/__tests__/pkgjson_test.go
```

- [ ] **Step 3: 验证模块可解析**

Run: `cd /Users/afei/github/go-tgz && go build ./...`
Expected: 无输出（成功，此时无 Go 源文件也通过）

- [ ] **Step 4: Commit**

```bash
cd /Users/afei/github/go-tgz && git add -A && git commit -m "chore: fix module path and remove template placeholders"
```

---

### Task 2: Filename 纯函数

**Files:**
- Create: `/Users/afei/github/go-tgz/__tests__/filename_test.go`
- Create: `/Users/afei/github/go-tgz/filename.go`

- [ ] **Step 1: 写失败测试**

```go
package tgz_test

import (
	"testing"
	"time"

	"github.com/afeiship/go-tgz"
)

func TestFilename(t *testing.T) {
	now := time.Date(2026, 11, 8, 15, 30, 5, 0, time.UTC)
	tests := []struct {
		name string
		base string
		pre  string
		ts   string
		suf  string
		want string
	}{
		{"defaults", "demo", "", "20060102-150405", "", "demo_20261108-153005.tar.gz"},
		{"custom pre", "demo", "aric", "20060102-150405", "", "aric_demo_20261108-153005.tar.gz"},
		{"custom suf", "demo", "", "20060102-150405", "aric", "demo_20261108-153005_aric.tar.gz"},
		{"pre and suf combined", "demo", "aric", "20060102-150405", "v1", "aric_demo_20261108-153005_v1.tar.gz"},
		{"custom ts", "demo", "", "20060102", "", "demo_20261108.tar.gz"},
		{"invalid ts passthrough", "demo", "", "bogus", "", "demo_bogus.tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tgz.Filename(tt.base, tt.pre, tt.ts, tt.suf, now); got != tt.want {
				t.Errorf("Filename(%q, %q, %q, %q) = %q, want %q", tt.base, tt.pre, tt.ts, tt.suf, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /Users/afei/github/go-tgz && go test __tests__/*.go -v`
Expected: FAIL，编译错误 `undefined: tgz.Filename`

- [ ] **Step 3: 最小实现**

`filename.go`：

```go
package tgz

import "time"

// Filename generates the archive filename:
// [{pre}_]{base}_{ts}[_{suf}].tar.gz
// The base is always kept; pre adds a leading tag, suf appends one.
// An invalid ts layout passes through time.Format as-is (caller's choice).
func Filename(base, pre, ts, suf string, t time.Time) string {
	if pre != "" {
		base = pre + "_" + base
	}
	name := base + "_" + t.Format(ts)
	if suf != "" {
		name += "_" + suf
	}
	return name + ".tar.gz"
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /Users/afei/github/go-tgz && go test __tests__/*.go -v`
Expected: PASS（6 个子用例全过）

- [ ] **Step 5: Commit**

```bash
cd /Users/afei/github/go-tgz && git add -A && git commit -m "feat: add Filename generator with table-driven tests"
```

---

### Task 3: Archive 基础 —— 文件源落盘

**Files:**
- Create: `/Users/afei/github/go-tgz/__tests__/tgz_test.go`
- Create: `/Users/afei/github/go-tgz/tgz.go`
- Create: `/Users/afei/github/go-tgz/archive.go`

- [ ] **Step 1: 写失败测试（含共享 helpers）**

`__tests__/tgz_test.go`：

```go
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
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /Users/afei/github/go-tgz && go test __tests__/*.go -v`
Expected: FAIL，编译错误 `undefined: tgz.Archive`（及 tgz.WithDir / tgz.WithTime）

- [ ] **Step 3: 实现 tgz.go（完整 API，含全部选项）**

`tgz.go`：

```go
// Package tgz packs files, directories and streams into timestamped
// .tar.gz archives.
package tgz

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultTimestamp = "20060102-150405"
	stdinBase        = "bk"
)

// Result describes the archive produced by Archive.
type Result struct {
	// Name is the archive filename, e.g. demo_20261108-153005.tar.gz.
	Name string
	// Path is the full path of the archive; empty in stream mode.
	Path string
}

// options carries the resolved configuration for Archive.
type options struct {
	prefix string
	suffix string
	dir    string
	ts     string
	writer io.Writer
	reader io.Reader
	now    time.Time
}

// Option configures Archive.
type Option func(*options)

// WithPrefix places a tag before the basename in the filename.
func WithPrefix(p string) Option { return func(o *options) { o.prefix = p } }

// WithSuffix appends a tag after the timestamp in the filename.
func WithSuffix(s string) Option { return func(o *options) { o.suffix = s } }

// WithDir sets the output directory (auto-created). Default ".".
func WithDir(dir string) Option { return func(o *options) { o.dir = dir } }

// WithTimestamp sets the timestamp layout (Go time format).
// Default "20060102-150405".
func WithTimestamp(layout string) Option { return func(o *options) { o.ts = layout } }

// WithWriter streams the gzip output to w instead of writing a file.
func WithWriter(w io.Writer) Option { return func(o *options) { o.writer = w } }

// WithReader provides the data for the "-" source (stdin mode).
func WithReader(r io.Reader) Option { return func(o *options) { o.reader = r } }

// WithTime fixes the timestamp used in the filename. Default time.Now().
func WithTime(t time.Time) Option { return func(o *options) { o.now = t } }

// Archive validates all sources, generates a timestamped filename and
// writes a tar.gz archive. With sources[0] == "-" it packs the reader
// given via WithReader as a single "stdin" entry. The library never
// prints; callers decide how to report the result.
func Archive(sources []string, opts ...Option) (*Result, error) {
	o := &options{dir: ".", ts: defaultTimestamp, now: time.Now()}
	for _, opt := range opts {
		opt(o)
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("no sources given")
	}

	stdinMode := sources[0] == "-"
	if stdinMode {
		if o.reader == nil {
			return nil, fmt.Errorf("reader is required for source \"-\"")
		}
		sources = sources[:1]
	}

	base := stdinBase
	if !stdinMode {
		base = strings.TrimSuffix(filepath.Base(sources[0]), filepath.Ext(sources[0]))
		for _, src := range sources {
			if _, err := os.Lstat(src); err != nil {
				return nil, fmt.Errorf("source not found: %s", src)
			}
		}
	}

	name := Filename(base, o.prefix, o.ts, o.suffix, o.now)

	if o.writer != nil {
		if err := writeTarGz(o.writer, sources, o.reader); err != nil {
			return nil, err
		}
		return &Result{Name: name}, nil
	}

	if err := os.MkdirAll(o.dir, 0o755); err != nil {
		return nil, err
	}
	out, err := os.Create(filepath.Join(o.dir, name))
	if err != nil {
		return nil, err
	}
	defer out.Close()

	if err := writeTarGz(out, sources, o.reader); err != nil {
		return nil, err
	}
	return &Result{Name: name, Path: filepath.Join(o.dir, name)}, nil
}
```

- [ ] **Step 4: 实现 archive.go（本任务仅文件源分支）**

`archive.go`：

```go
package tgz

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
)

// writeTarGz packs sources into gzipped tar, writing to w.
// A source "-" reads from r (only valid as the single source).
// Directories are walked recursively with entry paths relative to the
// directory itself. All sources must already be validated by the caller.
func writeTarGz(w io.Writer, sources []string, r io.Reader) error {
	gzw := gzip.NewWriter(w)
	defer gzw.Close()
	tw := tar.NewWriter(gzw)
	defer tw.Close()

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
```

注：本任务 `addDir`、`addFromReader` 尚未实现，文件源路径不会触达；下一任务补齐。（若希望本任务即可编译运行，先加入 Task 4 Step 2 的 `addDir` 与 Task 6 Step 3 的 `addFromReader` 两个函数亦可，测试仍按任务推进。）

- [ ] **Step 5: 运行确认通过**

Run: `cd /Users/afei/github/go-tgz && go test __tests__/*.go -v`
Expected: PASS（TestFilename、TestArchiveFile 全过）

- [ ] **Step 6: Commit**

```bash
cd /Users/afei/github/go-tgz && git add -A && git commit -m "feat: add Archive with functional options for file sources"
```

---

### Task 4: 目录源 —— 递归 / 嵌套 / 空目录 / symlink

**Files:**
- Modify: `/Users/afei/github/go-tgz/__tests__/tgz_test.go`（追加测试）
- Modify: `/Users/afei/github/go-tgz/archive.go`（追加 addDir/addSymlink）

- [ ] **Step 1: 写失败测试（追加到 tgz_test.go）**

```go
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
		"demo":        {typeflag: tar.TypeDir},
		"demo/a.txt":  {typeflag: tar.TypeReg, content: "alpha"},
		"demo/sub":    {typeflag: tar.TypeDir},
		"demo/sub/b.txt": {typeflag: tar.TypeReg, content: "beta"},
		"demo/empty":  {typeflag: tar.TypeDir},
		"demo/link.txt": {typeflag: tar.TypeSymlink, link: "a.txt"},
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

- [ ] **Step 2: 运行确认失败**

Run: `cd /Users/afei/github/go-tgz && go test __tests__/*.go -v`
Expected: FAIL，编译错误 `undefined: addDir`（若 Task 3 未提前补函数）；或 TestArchiveDirectory FAIL（目录走 addFile 分支）

- [ ] **Step 3: 实现 addDir 与 addSymlink（追加到 archive.go）**

在 archive.go 的 import 中加入 `"io/fs"`，并追加（从 CLI `cmd/archive.go` 逐字移植）：

```go
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
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /Users/afei/github/go-tgz && go test __tests__/*.go -v`
Expected: PASS（TestArchiveDirectory 含 6 条目断言全过）

- [ ] **Step 5: Commit**

```bash
cd /Users/afei/github/go-tgz && git add -A && git commit -m "feat: pack directories recursively, keep symlinks and empty dirs"
```

---

### Task 5: 选项与错误矩阵

**Files:**
- Modify: `/Users/afei/github/go-tgz/__tests__/tgz_test.go`（追加测试）

- [ ] **Step 1: 追加矩阵测试（实现已在 Task 3 完成，本任务为行为验证）**

```go
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
```

- [ ] **Step 2: 运行验证（期望全 PASS；若 FAIL 说明 Task 3 实现有 bug，修复后重跑）**

Run: `cd /Users/afei/github/go-tgz && go test __tests__/*.go -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
cd /Users/afei/github/go-tgz && git add -A && git commit -m "test: cover options matrix, multi-source and error cases"
```

---

### Task 6: stdin 模式与流模式

**Files:**
- Create: `/Users/afei/github/go-tgz/__tests__/archive_test.go`
- Modify: `/Users/afei/github/go-tgz/archive.go`（追加 addFromReader）

- [ ] **Step 1: 写失败测试**

`__tests__/archive_test.go`：

```go
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
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /Users/afei/github/go-tgz && go test __tests__/*.go -v`
Expected: TestArchiveStdinMode / TestArchiveStreamMode FAIL（`undefined: addFromReader`，若 Task 3 未提前补）；TestArchiveStdinWithoutReader 可能已 PASS（校验在 Archive 中）

- [ ] **Step 3: 实现 addFromReader（追加到 archive.go）**

archive.go 的 import 中加入 `"time"`，并追加（从 CLI 移植，数据源改为注入的 reader；tar 头需要 Size，故先落临时文件测量）：

```go
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
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /Users/afei/github/go-tgz && go test __tests__/*.go -v`
Expected: PASS（全部测试）

- [ ] **Step 5: Commit**

```bash
cd /Users/afei/github/go-tgz && git add -A && git commit -m "feat: support stdin source via WithReader and stream output via WithWriter"
```

---

### Task 7: 文档（README.md + llms.txt）与全量验证

**Files:**
- Modify: `/Users/afei/github/go-tgz/README.md`
- Create: `/Users/afei/github/go-tgz/llms.txt`

- [ ] **Step 1: 重写 README.md**

```markdown
# go-tgz
> Pack files, directories and streams into timestamped .tar.gz archives. Go library extracted from the [tgz](https://github.com/aric-go/tgz) CLI.

## Installation

```sh
go get -u github.com/afeiship/go-tgz
```

## Usage

```go
package main

import (
	"fmt"

	"github.com/afeiship/go-tgz"
)

func main() {
	// defaults: ./demo_20261110-203005.tar.gz
	res, err := tgz.Archive([]string{"demo.txt"})
	fmt.Println(res.Path, err)

	// customized
	res, _ = tgz.Archive(
		[]string{"demo.txt", "assets/"},
		tgz.WithPrefix("snap"),
		tgz.WithSuffix("v1"),
		tgz.WithDir("./backup"),
		tgz.WithTimestamp("20060102"),
	)
	// → ./backup/snap_demo_20261110_v1.tar.gz
}
```

## Options

| Option | Default | Description |
| --- | --- | --- |
| `WithPrefix(p)` | `""` | tag placed before the basename |
| `WithSuffix(s)` | `""` | tag appended after the timestamp |
| `WithDir(dir)` | `"."` | output directory (auto-created) |
| `WithTimestamp(layout)` | `"20060102-150405"` | Go time layout for the filename |
| `WithWriter(w)` | nil | stream the gzip output to `w` instead of a file |
| `WithReader(r)` | nil | data for the `"-"` source (stdin mode) |
| `WithTime(t)` | `time.Now()` | fixed timestamp for the filename |

## Behavior

- filename: `[{prefix}_]{base}_{timestamp}[_{suffix}].tar.gz`; `base` is the first source's basename (extension stripped), `bk` in stdin mode
- sources[0] `"-"` packs the `WithReader` stream as a single `stdin` entry
- all sources are validated before packing; a missing source aborts with `source not found: xxx`
- directories are packed recursively with entry paths relative to the directory itself; empty dirs are kept; symlinks are stored as symlinks (not followed)
- the library never prints; logs are the caller's job

## Filename helper

```go
tgz.Filename("demo", "snap", "20060102", "v1", time.Now()) // snap_demo_20261110_v1.tar.gz
```

## Test

```sh
npm test
# or: go test __tests__/*.go -v
```

## License

MIT
```

- [ ] **Step 2: 创建 llms.txt**

```
# go-tgz

> Pack files, directories and streams into timestamped .tar.gz archives.
> Go library, standard library only (archive/tar + compress/gzip).
> Import: github.com/afeiship/go-tgz (package tgz)

## API

- tgz.Archive(sources []string, opts ...Option) (*Result, error) — validate sources, build timestamped filename, create output dir, write tar.gz. sources[0] == "-" packs the WithReader stream as a single "stdin" entry (base name falls back to "bk"). Errors: "no sources given", "reader is required for source \"-\"", "source not found: <path>".
- tgz.Filename(base, pre, tsLayout, suf string, t time.Time) string — "[{pre}_]{base}_{ts}[_{suf}].tar.gz".

## Options

- WithPrefix(string) — tag before basename
- WithSuffix(string) — tag after timestamp
- WithDir(string) — output dir, default ".", auto-created
- WithTimestamp(string) — Go time layout, default "20060102-150405"
- WithWriter(io.Writer) — stream mode, no file, Result.Path == ""
- WithReader(io.Reader) — data source for "-"
- WithTime(time.Time) — fixed timestamp, default time.Now()

## Result

- Name string — archive filename
- Path string — full path, empty in stream mode

## Behavior notes

- Validate-all-then-pack: no half-written archives.
- Dirs: recursive, entries relative to the dir itself, empty dirs kept, symlinks stored not followed.
- Library never prints.

## Tests

go test __tests__/*.go -v
```

- [ ] **Step 3: 全量验证**

Run: `cd /Users/afei/github/go-tgz && go vet ./... && go test __tests__/*.go -v`
Expected: vet 无输出；测试全 PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/afei/github/go-tgz && git add -A && git commit -m "docs: rewrite README and add llms.txt for the library API"
```

---

## ⏸ Checkpoint：用户发布 lib

**Task 8 开始前，用户手动执行（AI 不代做推送/发布）：**

```bash
cd /Users/afei/github/go-tgz
git push origin main
git tag v1.0.0 && git push origin v1.0.0
```

（若 go get 拉取超时，可用 `http_proxy=http://127.0.0.1:9090 https_proxy=http://127.0.0.1:9090 go get ...`）

---

### Task 8: CLI 重构为依赖 lib（aric-go/tgz）

**Files:**
- Delete: `/Users/afei/github/aric-go/tgz/cmd/archive.go`
- Delete: `/Users/afei/github/aric-go/tgz/cmd/filename.go`
- Delete: `/Users/afei/github/aric-go/tgz/cmd/backup.go`
- Delete: `/Users/afei/github/aric-go/tgz/cmd/filename_test.go`
- Modify: `/Users/afei/github/aric-go/tgz/cmd/root.go`
- Modify: `/Users/afei/github/aric-go/tgz/go.mod`（go get 自动）
- Modify: `/Users/afei/github/aric-go/tgz/README.md`（追加 Library 一节）

- [ ] **Step 1: 记录基线（重构前测试状态）**

Run: `cd /Users/afei/github/aric-go/tgz && go test ./cmd/... -v 2>&1 | tail -10`
Expected: 记录当前 PASS/FAIL 状态（已知 `TestRootVersionOutput` 断言 `1.0.0` 而默认 version 为 `1.0.1`，若基线即 FAIL，非本次重构引入，不处理）

- [ ] **Step 2: 添加依赖**

Run: `cd /Users/afei/github/aric-go/tgz && go get github.com/afeiship/go-tgz@v1.0.0 && go mod tidy`
Expected: go.mod 出现 `github.com/afeiship/go-tgz v1.0.0`

- [ ] **Step 3: 删除被替代的实现**

```bash
cd /Users/afei/github/aric-go/tgz && rm cmd/archive.go cmd/filename.go cmd/backup.go cmd/filename_test.go
```

- [ ] **Step 4: 改写 root.go**

`cmd/root.go` 完整新内容（import 增加 `"os"`、`"time"` 不需要；增加 lib import）：

```go
package cmd

import (
	"fmt"
	"os"

	"tgz/internal/config"

	"github.com/afeiship/go-tgz"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// version is injected at build time via -ldflags "-X cmd.version=<ver>".
var version = "1.0.1"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "tgz [source...]",
		Short:        viper.GetString("app.description"),
		Version:      version,
		SilenceUsage: true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "🌈 %s!\n\n", viper.GetString("app.welcome"))
				return cmd.Help()
			}
			pre, _ := cmd.Flags().GetString("pre")
			suf, _ := cmd.Flags().GetString("suf")
			dir, _ := cmd.Flags().GetString("dir")
			ts, _ := cmd.Flags().GetString("ts")

			opts := []tgz.Option{
				tgz.WithPrefix(pre),
				tgz.WithSuffix(suf),
				tgz.WithTimestamp(ts),
			}
			if dir == "-" {
				opts = append(opts, tgz.WithWriter(os.Stdout))
			} else {
				opts = append(opts, tgz.WithDir(dir))
			}
			if args[0] == "-" {
				opts = append(opts, tgz.WithReader(os.Stdin))
			}

			res, err := tgz.Archive(args, opts...)
			if err != nil {
				return err
			}
			if dir != "-" {
				fmt.Fprintf(os.Stderr, "✓ backup created: %s\n", res.Path)
			}
			return nil
		},
	}
	root.Flags().StringP("pre", "p", "", "tag placed before the basename (basename always kept)")
	root.Flags().StringP("suf", "s", "", "tag appended after the timestamp")
	root.Flags().StringP("dir", "d", ".", "output directory, or - for stdout")
	root.Flags().StringP("ts", "t", "20060102-150405", "timestamp layout for filename")
	root.SetVersionTemplate("{{with .Name}}{{printf \"%s \" .}}{{end}}{{printf \"version %s\" .Version}}\n")
	return root
}

var rootCmd = newRootCmd()

// Execute runs the root command. It is called by main.main().
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	if err := config.InitConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "Error reading config:", err)
		os.Exit(1)
	}
	rootCmd.Short = viper.GetString("app.description")
}
```

- [ ] **Step 5: 编译与测试**

Run: `cd /Users/afei/github/aric-go/tgz && go build ./... && go test ./cmd/... -v 2>&1 | tail -10`
Expected: 编译通过；测试结果与 Step 1 基线一致（无新增 FAIL）

- [ ] **Step 6: 手动验收（对应原 spec 示例）**

```bash
cd /Users/afei/github/aric-go/tgz && go run main.go demo.txt                       # 默认文件名落盘
cd /Users/afei/github/aric-go/tgz && go run main.go demo.txt --pre=snap --suf=v1   # 标签
cd /Users/afei/github/aric-go/tgz && go run main.go demo.txt --dir=./backup        # 目录自动创建
cd /Users/afei/github/aric-go/tgz && go run main.go demo.txt --dir=- | tar -tzf -  # 流模式
cd /Users/afei/github/aric-go/tgz && cat demo.txt | go run main.go -               # stdin 模式
cd /Users/afei/github/aric-go/tgz && go run main.go missing.txt                    # 报错 source not found
```

Expected: 各行为与重构前一致；`--dir=-` 无日志输出、stdout 为纯 gzip 流；清理产生的 `*.tar.gz` / `backup/` 测试产物

- [ ] **Step 7: README 追加 Library 说明**

在 README.md 末尾追加：

```markdown
## Library

The packing core lives in [go-tgz](https://github.com/afeiship/go-tgz) — use it directly in other Go projects:

```go
res, err := tgz.Archive([]string{"demo.txt"}, tgz.WithDir("./backup"))
```
```

- [ ] **Step 8: Commit**

```bash
cd /Users/afei/github/aric-go/tgz && git add -A && git commit -m "refactor: replace builtin packing with go-tgz library"
```

---

## Self-Review 记录

- Spec 覆盖：模块路径修正(T1)、Filename(T2)、Archive+选项(T3/T5)、目录/symlink/空目录(T4)、stdin+流(T6)、lib 静默(T3 注释)、测试三类文件(T2/T3/T5/T6)、README+llms.txt(T7)、CLI 重构+发布顺序 checkpoint(T8) —— 全部有对应任务
- 占位符扫描：无 TBD/TODO；所有代码步骤含完整代码
- 类型一致性：`Archive`/`Result`/`Option`/`Filename` 签名在各任务与测试中一致；`writeTarGz(w, sources, r)` 三处引用一致
- 已知风险标注：CLI `TestRootVersionOutput` 基线可能本就 FAIL（version 默认值 1.0.1 vs 断言 1.0.0），非本次引入，仅记录不修
