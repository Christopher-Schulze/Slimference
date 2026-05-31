package readcache

import "testing"

func tempReadCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = FlushDir(dir)
		clearMemoryDir(dir)
	})
	return dir
}
