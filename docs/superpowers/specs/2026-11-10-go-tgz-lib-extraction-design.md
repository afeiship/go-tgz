# go-tgz 独立库抽取设计文档

日期：2026-11-10
需求来源：将 `aric-go/tgz` CLI 的核心功能抽取为独立库 `go-tgz`，未来其他项目复用；最终 CLI 本身也重构为依赖此库。

## 决策记录

- 模块路径：`github.com/afeiship/go-tgz`（当前 go.mod 误写为 `github.com/afeiship/tgz`，需修正）；Go 包名不能含连字符，包名为 `tgz`
- API 范围：完整备份工作流（校验 → 文件名生成 → 建目录 → 写 tar.gz），非仅打包核心
- 流式抽象：lib 不绑定 `os.Stdin`/`os.Stdout`，通过 `io.Reader`/`io.Writer` 注入；CLI 层传入标准流
- 参数风格：函数式选项（functional options）
- 依赖方式：**先发布后依赖** —— lib 推送打 tag 后，CLI 再 `go get`（推送/发布由用户手动执行）
- 包结构：根目录单包（无 pkg/ 子目录）
- lib 保持纯静默：不打印任何日志（`✓ backup created` 由 CLI 层输出）
- 压缩实现仅用标准库 `archive/tar` + `compress/gzip`
- 模板遗留的 `pkgjson` 命名（pkgjson.go、__tests__/pkgjson_test.go、README 中 go get pkgjson）统一替换为 tgz 命名

## 包结构（go-tgz）

```
go-tgz/
├── tgz.go          # Archive() 入口 + Option 定义
├── archive.go      # tar.gz 写入内部实现（从 CLI cmd/archive.go 移植）
├── filename.go     # 文件名生成纯函数（从 CLI cmd/filename.go 移植）
├── __tests__/
│   ├── tgz_test.go        # Archive 端到端
│   ├── filename_test.go   # 表驱动
│   └── archive_test.go    # 打包内容读回校验
├── go.mod          # module github.com/afeiship/go-tgz（修正）
├── package.json    # @jswork/go-tgz（发布用，保持）
├── README.md
└── llms.txt
```

`pkgjson.go` 删除。

## 公开 API

```go
// 一次调用完成：校验 sources → 生成时间戳文件名 → 建目录 → 写 tar.gz
func Archive(sources []string, opts ...Option) (*Result, error)

type Result struct {
    Name string // 归档文件名，如 demo_20261110-153000.tar.gz
    Path string // 完整路径；WithWriter 流模式下为 ""
}

// 选项（零值即默认）
WithPrefix(p string)         // 文件名前置标签
WithSuffix(s string)         // 文件名尾部标签
WithDir(dir string)          // 输出目录，默认 "."，自动创建（0755）
WithTimestamp(layout string) // 时间戳布局，默认 "20060102-150405"
WithWriter(w io.Writer)      // 流模式：写入 w 不落盘
WithReader(r io.Reader)      // 提供源 "-" 的数据（stdin 模式）
WithTime(t time.Time)        // 固定时间戳（调用方可控、可测试）
```

### 语义（与 CLI 完全一致）

- 文件名：`[{pre}_]{base}_{ts}[_{suf}].tar.gz`；`base` 取第 1 个 source 的 basename（去扩展名，目录 source 取目录名），stdin 模式回退 `bk`
- `sources[0] == "-"` → stdin 模式：仅打包该源，条目名 `stdin`（假头：Mode 0644、ModTime 当前时间）；未提供 `WithReader` 时报错
- 全部 source 先校验（`os.Lstat`）再打包，任一不存在整体报错（`source not found: xxx`），不产半成品
- 目录：`filepath.WalkDir` 递归打包，条目名相对该目录自身（保留目录名，如 `demo/`、`demo/sub/a.txt`）；空目录也写条目
- symlink：记录链接本身，不跟随
- 普通文件：真实 FileInfo（ModTime/Mode/Size），流式写入
- `ts` 布局非法时 `time.Format` 原样输出字符串，属可接受行为，不校验

## CLI 重构（aric-go/tgz）

- 删除：`cmd/archive.go`、`cmd/filename.go`、`cmd/backup.go`、`cmd/filename_test.go`（实现与测试进 lib）
- `cmd/root.go` RunE 变薄：解析 flags → `tgz.Archive(args, ...)` → stderr 打 `✓ backup created: <path>`
- 映射：`--dir=-` → `tgz.WithWriter(os.Stdout)`；源 `-` → `tgz.WithReader(os.Stdin)`
- go.mod 新增依赖 `github.com/afeiship/go-tgz`

### 执行顺序（含用户 checkpoint）

1. 完成 lib（本仓库）
2. **用户**：push go-tgz + 打 tag `v1.0.0`
3. 重构 CLI：`go get github.com/afeiship/go-tgz@v1.0.0`，替换实现
4. **用户**：review + 推送 CLI

## 测试

- `filename_test.go`：表驱动 —— 默认值、自定义 pre/suf/ts、stdin 回退 base
- `archive_test.go`：打包后读回 tar.gz，断言条目名与类型（文件/目录/嵌套/空目录/symlink 不跟随/stdin 条目）
- `tgz_test.go`：端到端 —— 临时目录 + `WithTime` 固定时间戳，断言生成路径、目录自动创建、选项组合、`source not found` 报错、流模式（写入 bytes.Buffer）

## 文档

- README.md：完整 API 说明 + 示例（修正模板遗留的 pkgjson 内容）
- llms.txt：同步生成
