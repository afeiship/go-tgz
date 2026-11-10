// Package tgz packs files, directories and streams into timestamped
// .tar.gz archives.
package tgz

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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
// Ignored when WithWriter is set.
func WithDir(dir string) Option { return func(o *options) { o.dir = dir } }

// WithTimestamp sets the timestamp layout (Go time format).
// Default "20060102-150405".
func WithTimestamp(layout string) Option { return func(o *options) { o.ts = layout } }

// WithWriter streams the gzip output to w instead of writing a file.
// Takes precedence over WithDir.
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
		if len(sources) > 1 {
			return nil, fmt.Errorf("stdin mode takes a single source \"-\"")
		}
		sources = sources[:1]
	}

	base := stdinBase
	if !stdinMode {
		base = strings.TrimSuffix(filepath.Base(sources[0]), filepath.Ext(sources[0]))
		for _, src := range sources {
			if _, err := os.Lstat(src); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil, fmt.Errorf("source not found: %s", src)
				}
				return nil, fmt.Errorf("source %s: %w", src, err)
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

	path := filepath.Join(o.dir, name)
	if err := writeTarGz(out, sources, o.reader); err != nil {
		out.Close()
		os.Remove(path)
		return nil, err
	}
	if err := out.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return &Result{Name: name, Path: path}, nil
}
