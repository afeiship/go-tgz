# go-tgz 支持 zip 输出与 zip 密码加密设计文档

日期：2026-11-11
需求来源：在现有 tar.gz 归档能力之外，增加 zip 格式输出，以及"双击压缩包弹密码框"的标准 zip 加密。

## 决策记录

- 依赖：`github.com/alexmullins/zip`（`archive/zip` 的 fork，实现 WinZip AES-256 / AE-2），用户已确认可直接使用，不再自行实现密码学。
- 该库同时作为**唯一 zip writer**：未加密 zip 也走它，避免"普通 zip 用标准库 / 加密 zip 用 fork"的双实现。已实测其未加密输出可被标准库 `archive/zip`、`bsdtar`、Python `zipfile` 正常读取。
- Go 版本允许升到最高。跟随 `golang.org/x/crypto v0.58.0` 的 `go 1.26.0` 要求，把 `go.mod` 的 go directive 从 `1.21.0` 改为 `1.26.0`；`x/crypto` 显式钉版本，**禁止再用 `go get -u` 造成版本漂移**（`-u` 会拉到要求 go 1.26 的新版，虽可接受但会不可控地抬高使用者门槛）。
- 包名 `tgz`、模块名 `github.com/afeiship/go-tgz` 保持不变，避免破坏性变更。
- 默认行为完全不变：不带新选项时仍是 `.tar.gz`。
- 加密粒度：**仅普通文件内容加密**，目录条目与 symlink 条目保持明文。这是实测得出的兼容性要求，见"实测结论"。
- tar.gz 与密码组合**直接报错**，不静默忽略，避免调用方误以为已加密。

## 实测结论（本机 go1.27.3 / macOS）

| 验证项 | 结果 |
| --- | --- |
| `alexmullins/zip` 的 `Encrypt(name, pwd)` 生成包 | `zipinfo` 显示 method 99 / encrypted ✅ |
| `bsdtar --passphrase pass` 解开（macOS 归档实用工具底层同为 libarchive） | 内容正确，**双击弹密码框成立** ✅ |
| 错误密码 | `bsdtar` 报 `Incorrect passphrase` 并失败 ✅ |
| 中文文件名 / 空文件 / 多源 / 空目录 | 全部正常 ✅ |
| 目录和 symlink **也**加密时 | `bsdtar` 报 `Unsupported ZIP compression method ... link entry (99: aes)`，symlink 退化为 0 字节普通文件 ❌ |
| 改为仅普通文件加密后 | 中文名、空文件、symlink（`ls -l` 显示为链接）、空目录全部正确解开 ✅ |
| 未加密 zip 用标准库 `archive/zip` 列条目 / Python `zipfile` 解包 | 正常 ✅ |
| Info-ZIP `unzip -P` / Python `zipfile` 解**加密**包 | ❌ 不支持 WinZip AES（只支持老式 ZipCrypto） |
| 锁 `x/crypto@v0.24.0`（go 1.18） | 可编译，但用户已允许升版本，故不采用降级锁 |

## 公开 API

```go
// 格式：默认 FormatTarGz，零破坏
type Format uint8
const (
    FormatTarGz Format = iota
    FormatZip
)

func WithFormat(f Format) Option        // 选择归档格式，同时决定文件扩展名
func WithPassword(pwd string) Option   // zip 的 AES-256 密码；空字符串 == 不加密
```

`Result`、`Filename`、`WithPrefix/WithSuffix/WithDir/WithTimestamp/WithWriter/WithReader/WithTime` 全部保持原签名与语义不变。

`Filename` 保持 5 参数纯函数且仍返回 `.tar.gz`；内部拆出 `filenameWithExt(base, pre, ts, suf, t, ext)`，`Archive` 按 `o.format` 传入 `.tar.gz` 或 `.zip`。理由：公开 helper 不破坏，同时避免"改了格式忘了改后缀"的状态不一致。

### 选项语义表

| format | password | 结果 |
| --- | --- | --- |
| `FormatTarGz` | 空 | 现有行为，`.tar.gz` |
| `FormatZip` | 空 | 未加密 `.zip` |
| `FormatZip` | 非空 | AES-256 加密 `.zip`，仅文件数据加密 |
| `FormatTarGz` | 非空 | 报错 `password requires zip format` |

密码不得进入 `Result`、`error` 文案或任何输出；error 中只出现固定的组合错误信息。

## 架构

改动集中在 `archive.go`，`tgz.go` 增加 2 个 Option 与 format 字段，`filename.go` 参数化后缀。

### 抽出遍历层（本次唯一有分量的重构）

现 `writeTarGz`（`archive.go:19`）把"遍历 sources / 拼 entry 路径 / stdin 模式"与"写 tar 格式"混在一起。抽出与格式无关的遍历层，让两种格式共用同一套路径与语义：

```go
// sink 只负责"把一个条目写进某种归档格式"，不关心来源。
type sink interface {
    AddFile(name string, info fs.FileInfo, size int64, r io.Reader) error
    AddDir(name string, info fs.FileInfo) error
    AddSymlink(name string, info fs.FileInfo, link string) error
    Close() error
}

func writeArchive(w io.Writer, sources []string, r io.Reader, s sink) error
```

- `writeArchive`：`WalkDir` 递归、路径拼接（`base/rel`）、`rel == "."` 时写目录名本身、symlink 不跟随、stdin 单条 `stdin` 条目、empty dir 保留 —— 全部从现有 `addDir`/`addFromReader` 平移，**行为逐字保持一致**。
- `tarSink`：`gzip.Writer` + `tar.Writer`，`AddFile` 直接用传入 size 写 header（stdin 仍走临时文件取 size，保留现行为）；`Close` 按现顺序检查 tar.Close / gzip.Close 错误。
- `zipSink`：`azip.Writer` + `CreateHeader`。`size` 由 zip 忽略（data descriptor 流式写）。`FileInfoHeader` 得到元数据后覆盖 `Name`、`Method=Deflate`；仅当 `AddFile` 且密码非空时调 `SetPassword`。目录用 `AddDir`（Name 带尾 `/`），symlink 用 `AddSymlink`（`SetMode(os.ModeSymlink|...)` 并把 link 目标作为内容写入）且不加密。`Close` 必须返回 `azip.Writer.Close()` 的错误 —— AE-2 的 HMAC 认证码在 Close 阶段落盘，吞掉它会产出损坏的加密包。
- `WithWriter` 流模式对两种格式都成立（zip 内部用 data descriptor，不需要预知总大小）。

### 数据流

```
Archive → 校验 sources → filenameWithExt → 文件/流
        → writeArchive(w, sources, reader, sink)
           ├─ FormatTarGz → tarSink (gzip→tar)
           └─ FormatZip   → zipSink (alexmullins/zip, 文件条目可选 AES)
```

`Archive` 现有的"失败删半成品"、`MkdirAll`、流模式优先级逻辑不动。

## 安全与风险提示

1. **文件名明文**：WinZip AES 规范只加密文件内容，central directory 里的文件名、目录结构、symlink 目标均为明文，双击前 `zipinfo` 即可列出清单。这是"标准 zip 密码"的正常行为，用户已确认接受。
2. **PBKDF2 迭代次数固定 1000**（AE-2 规范写死，调高会导致其他工具解不开），抗暴力破解偏弱，需强密码。库内为 AES-CTR + HMAC-SHA1 认证，实现自包含。
3. **依赖维护状态**：`alexmullins/zip` 无正式 tag（pseudo-version `v0.0.0-20180717182244-4affb64b04d0`）、无 go.mod、2018 年后无更新，是老版 `archive/zip` 的 fork，上游安全修复不会自动跟进。
4. **兼容性边界**：加密包可被 macOS 归档实用工具 / 7-Zip / WinZip 解开；Info-ZIP `unzip` 与 Python `zipfile` 解不开。
5. **加密仅限普通文件**：目录/symlink 条目不加密，否则 libarchive 解包失败（实测）。目录名本身因此也是明文。
6. **Go 版本抬到 1.26.0**，下游使用者需同步升级工具链。
7. 密码为空视为不加密；切勿静默忽略 tar.gz + 密码的组合。

## 测试

现有 13 个测试保持全绿（回归基线）。新增：

- 未加密 zip：单文件 / 目录递归（嵌套、空目录）/ symlink 不跟随 / 中文名 / 多源 / stdin 模式 / 流模式 `WithWriter` / 失败时删除半成品 / 自动建目录 / 文件名后缀与选项矩阵。
- 加密 zip：`WithPassword` 后用 `alexmullins/zip` 的 `File.SetPassword` 读回内容一致；错误密码触发 `ErrDecryption`；目录与 symlink 条目 `IsEncrypted() == false`；断言密码字符串不出现在任何 error。
- 组合错误：`FormatTarGz` + `WithPassword` 返回 `password requires zip format`。
- 测试 helper `readEntries` 支持按扩展名分派：tar.gz 走 `gzip→tar`，未加密 zip 走标准库 `archive/zip`。
- 互操作性（本次已手工实测，写入本文档）：`zipinfo -v` 确认 method 99 / encrypted；`bsdtar --passphrase` 正确解开、错误密码失败。该项依赖系统外部工具，不纳入 `npm test` 必跑用例。

## 文档

- `README.md`：新增 `WithFormat` / `WithPassword` 选项、zip 与加密示例、兼容性说明与风险提示。
- `llms.txt`：同步 API、选项、行为备注。
- 本文档存于 `docs/superpowers/specs/`。

## 实施步骤（每步带验证）

1. `go.mod` 改为 go 1.26.0，加 `alexmullins/zip` + 钉住 `golang.org/x/crypto v0.58.0`；`go mod tidy` 后 `go build ./...` 通过。
2. 抽出 `sink` 接口与 `writeArchive`，把现有 tar.gz 逻辑平移到 `tarSink`；跑现有测试全绿（回归）。
3. 新增 `zipSink`、`Format`、`WithFormat`，参数化 `filenameWithExt`；补未加密 zip 测试，`npm test` 全绿。
4. 新增 `WithPassword` 与加密粒度控制；补加密/错误密码/组合错误测试，`npm test` 全绿；手工复核 `zipinfo` + `bsdtar` 互操作。
5. 同步 `README.md` 与 `llms.txt`；`npm test` 全绿后收尾。
