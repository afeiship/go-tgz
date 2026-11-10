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
| `WithDir(dir)` | `"."` | output directory (auto-created); ignored when `WithWriter` is set |
| `WithTimestamp(layout)` | `"20060102-150405"` | Go time layout for the filename |
| `WithWriter(w)` | nil | stream the gzip output to `w` instead of a file (takes precedence over `WithDir`) |
| `WithReader(r)` | nil | data for the `"-"` source (stdin mode) |
| `WithTime(t)` | `time.Now()` | fixed timestamp for the filename |

## Behavior

- filename: `[{prefix}_]{base}_{timestamp}[_{suffix}].tar.gz`; `base` is the first source's basename (extension stripped), `bk` in stdin mode
- sources[0] `"-"` packs the `WithReader` stream as a single `stdin` entry; mixing `"-"` with other sources is an error
- all sources are validated before packing; a missing source aborts with `source not found: xxx` and no archive is written; a failed pack removes the partial archive
- directories are packed recursively with entry paths relative to the directory itself; empty dirs are kept; symlinks are stored as symlinks (not followed)
- the library never prints; logs are the caller's job

## Errors

- `no sources given` — Archive called with an empty slice
- `reader is required for source "-"` — stdin mode without `WithReader`
- `stdin mode takes a single source "-"` — `"-"` mixed with other sources
- `source not found: <path>` — a source does not exist; other IO failures wrap the underlying error

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
