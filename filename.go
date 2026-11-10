package tgz

import "time"

// Filename generates the archive filename:
// [{pre}_]{base}_{tsLayout}[_{suf}].tar.gz
// The base is always kept; pre adds a leading tag, suf appends one.
// An invalid tsLayout is emitted verbatim, as Go's time.Format does.
func Filename(base, pre, tsLayout, suf string, t time.Time) string {
	if pre != "" {
		base = pre + "_" + base
	}
	name := base + "_" + t.Format(tsLayout)
	if suf != "" {
		name += "_" + suf
	}
	return name + ".tar.gz"
}
