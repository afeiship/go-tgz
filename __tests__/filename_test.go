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
