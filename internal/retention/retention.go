// Package retention bounds a directory's file count and byte size, always
// keeping the newest MinKeep files. It is a pure function of filesystem state
// plus policy; the sweeper (sweeper.go) drives it on a ticker.
package retention

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Policy struct {
	MaxBytes int64 // 0 = unlimited
	MaxFiles int   // 0 = unlimited
	MinKeep  int   // always retain the newest N matching files
}

func (p Policy) enabled() bool { return p.MaxBytes > 0 || p.MaxFiles > 0 }

type fileInfo struct {
	name string
	size int64
}

// Prune deletes oldest matching files until both MaxFiles and MaxBytes are
// satisfied, never deleting the newest MinKeep. Files are ordered by name
// (ascending == oldest-first) because all retained dirs use timestamp-prefixed
// names. Per-file remove errors are joined and returned; the sweep continues.
func Prune(dir string, match func(name string) bool, p Policy) (int, error) {
	if !p.enabled() {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	files := make([]fileInfo, 0, len(entries))
	var totalBytes int64
	for _, e := range entries {
		if e.IsDir() || !match(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{name: e.Name(), size: info.Size()})
		totalBytes += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name }) // oldest first

	totalFiles := len(files)
	removed := 0
	var errs []error
	for i := 0; i < len(files) && totalFiles > p.MinKeep; i++ {
		overCount := p.MaxFiles > 0 && totalFiles > p.MaxFiles
		overBytes := p.MaxBytes > 0 && totalBytes > p.MaxBytes
		if !overCount && !overBytes {
			break
		}
		if err := os.Remove(dir + "/" + files[i].name); err != nil {
			errs = append(errs, err)
			continue
		}
		totalFiles--
		totalBytes -= files[i].size
		removed++
	}
	if len(errs) > 0 {
		return removed, fmt.Errorf("prune %s: %d errors, first: %w", dir, len(errs), errs[0])
	}
	return removed, nil
}

// ParseByteSize parses "1024", "64Mi", "2Ki", "1Gi". Empty string == 0.
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "Ki"):
		mult, s = 1024, strings.TrimSuffix(s, "Ki")
	case strings.HasSuffix(s, "Mi"):
		mult, s = 1024*1024, strings.TrimSuffix(s, "Mi")
	case strings.HasSuffix(s, "Gi"):
		mult, s = 1024*1024*1024, strings.TrimSuffix(s, "Gi")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse byte size %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("byte size must not be negative: %q", s)
	}
	return n * mult, nil
}
