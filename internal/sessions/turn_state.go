package sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
)

func FingerprintPaths(paths []string) string {
	normalised := sortedUniqueStrings(paths)
	if len(normalised) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(normalised, "\x00")))
	return hex.EncodeToString(sum[:])
}

func sortedUniqueStrings(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(path)), "./")
		if path != "" {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	uniq := out[:0]
	for _, path := range out {
		if len(uniq) > 0 && uniq[len(uniq)-1] == path {
			continue
		}
		uniq = append(uniq, path)
	}
	return uniq
}
