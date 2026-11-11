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
