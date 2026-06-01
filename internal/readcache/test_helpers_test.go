package readcache

import (
	"testing"
	"time"
)

func tempReadCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = FlushDir(dir)
		time.Sleep(readCacheAsyncFlushDelay * 2)
		_ = FlushDir(dir)
		clearMemoryDir(dir)
	})
	return dir
}
