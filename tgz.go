// Package tgz packs files, directories and streams into timestamped
// .tar.gz archives, or .zip archives via WithFormat(FormatZip).
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

// Format selects the archive container.
type Format uint8

const (
	FormatTarGz Format = iota
	FormatZip
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

// WithWriter streams the archive output to w instead of writing a file.
// Takes precedence over WithDir.
func WithWriter(w io.Writer) Option { return func(o *options) { o.writer = w } }

// WithReader provides the data for the "-" source (stdin mode).
func WithReader(r io.Reader) Option { return func(o *options) { o.reader = r } }

// WithTime fixes the timestamp used in the filename. Default time.Now().
func WithTime(t time.Time) Option { return func(o *options) { o.now = t } }

// WithFormat selects the archive format. Default FormatTarGz.
func WithFormat(f Format) Option { return func(o *options) { o.format = f } }

// WithPassword sets the AES-256 password for zip archives. An empty
// string means no encryption. Only valid with FormatZip.
func WithPassword(pwd string) Option { return func(o *options) { o.password = pwd } }

func (o *options) newSink(w io.Writer) sink {
	if o.format == FormatZip {
		return newZipSink(w, o.password)
	}
	return newTarSink(w)
}

// Archive validates all sources, generates a timestamped filename and
// writes a tar.gz (or zip, via WithFormat) archive. With sources[0] ==
// "-" it packs the reader given via WithReader as a single "stdin"
// entry. The library never prints; callers decide how to report the
// result.
func Archive(sources []string, opts ...Option) (*Result, error) {
	o := &options{dir: ".", ts: defaultTimestamp, now: time.Now()}
	for _, opt := range opts {
		opt(o)
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("no sources given")
	}

	if o.password != "" && o.format != FormatZip {
		return nil, fmt.Errorf("password requires zip format")
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

	ext := ".tar.gz"
	if o.format == FormatZip {
		ext = ".zip"
	}
	name := filenameWithExt(base, o.prefix, o.ts, o.suffix, o.now, ext)

	if o.writer != nil {
		if err := writeArchive(sources, o.reader, o.newSink(o.writer)); err != nil {
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
	if err := writeArchive(sources, o.reader, o.newSink(out)); err != nil {
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
