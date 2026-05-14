package filter

import (
	"sync"
	"testing"
	"time"
)

func TestRunFilter_Normal(t *testing.T) {
	out, ok, stats := runFilter("test", func() ([]byte, bool) {
		return []byte("result"), true
	})
	if !ok || string(out) != "result" {
		t.Fatalf("ok=%v out=%q", ok, out)
	}
	if stats.Name != "test" || !stats.Matched {
		t.Fatalf("stats=%+v", stats)
	}
	if stats.Elapsed < 0 {
		t.Fatal("negative elapsed")
	}
}

func TestRunFilter_Panic(t *testing.T) {
	out, ok, stats := runFilter("panicker", func() ([]byte, bool) {
		panic("boom")
	})
	if ok {
		t.Fatal("should not match on panic")
	}
	if !stats.Panicked {
		t.Fatal("should record panic")
	}
	if stats.Matched {
		t.Fatal("should not be marked matched")
	}
	if out != nil {
		t.Fatalf("expected nil result, got %q", out)
	}
}

func TestRunFilter_NoMatch(t *testing.T) {
	out, ok, stats := runFilter("noop", func() ([]byte, bool) {
		return nil, false
	})
	if ok {
		t.Fatal("should not match")
	}
	if stats.Matched {
		t.Fatal("should not be marked matched")
	}
	if out != nil {
		t.Fatalf("expected nil, got %q", out)
	}
}

func TestFilterObservability_Record(t *testing.T) {
	o := NewFilterObservability(10)
	o.Record(FilterStats{
		Name:     "git_status",
		Elapsed:  5 * time.Millisecond,
		Matched:  true,
		InBytes:  100,
		OutBytes: 50,
	})
	o.Record(FilterStats{
		Name:     "git_status",
		Elapsed:  3 * time.Millisecond,
		Matched:  true,
		InBytes:  200,
		OutBytes: 80,
	})
	o.Record(FilterStats{
		Name:     "git_status",
		Elapsed:  1 * time.Millisecond,
		Matched:  false,
		InBytes:  30,
		OutBytes: 30,
	})
	snap := o.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	fs := snap["git_status"]
	if fs.Attempts != 3 {
		t.Fatalf("attempts=%d want 3", fs.Attempts)
	}
	if fs.Calls != 2 {
		t.Fatalf("calls=%d want 2", fs.Calls)
	}
	if fs.Matches != 2 || fs.Misses != 1 {
		t.Fatalf("matches=%d misses=%d want 2/1", fs.Matches, fs.Misses)
	}
	if fs.BytesSaved != 170 {
		t.Fatalf("bytes_saved=%d want 170", fs.BytesSaved)
	}
	if fs.BytesIn != 330 || fs.BytesOut != 160 {
		t.Fatalf("bytes in/out=%d/%d want 330/160", fs.BytesIn, fs.BytesOut)
	}
	if fs.HitRate < 0.66 || fs.HitRate > 0.67 {
		t.Fatalf("hit_rate=%f want about 0.666", fs.HitRate)
	}
}

func TestFilterObservability_PanicCounter(t *testing.T) {
	o := NewFilterObservability(50)
	o.Record(FilterStats{
		Name:     "broken",
		Elapsed:  time.Millisecond,
		Panicked: true,
	})
	snap := o.Snapshot()
	if snap["broken"].Panics != 1 {
		t.Fatalf("panics=%d", snap["broken"].Panics)
	}
	if snap["broken"].Attempts != 1 || snap["broken"].Calls != 0 {
		t.Fatalf("panic attempts/calls=%d/%d", snap["broken"].Attempts, snap["broken"].Calls)
	}
}

func TestFilterObservability_SlowFilter(t *testing.T) {
	o := NewFilterObservability(1)
	o.Record(FilterStats{
		Name:    "slow_filter",
		Elapsed: 100 * time.Millisecond,
	})
}

func TestFilterObservability_DefaultThreshold(t *testing.T) {
	o := NewFilterObservability(0)
	if o.slowMs != 50 {
		t.Fatalf("default slowMs=%d want 50", o.slowMs)
	}
}

func TestFilterObservability_SnapshotEmpty(t *testing.T) {
	o := NewFilterObservability(50)
	snap := o.Snapshot()
	if len(snap) != 0 {
		t.Fatalf("expected empty, got %d", len(snap))
	}
}

func TestFilterObservability_Concurrent(t *testing.T) {
	o := NewFilterObservability(50)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.Record(FilterStats{
				Name:     "concurrent",
				Elapsed:  time.Microsecond,
				Matched:  true,
				InBytes:  100,
				OutBytes: 50,
			})
		}()
	}
	wg.Wait()
	snap := o.Snapshot()
	if snap["concurrent"].Calls != 100 {
		t.Fatalf("calls=%d want 100", snap["concurrent"].Calls)
	}
	if snap["concurrent"].Attempts != 100 || snap["concurrent"].Matches != 100 {
		t.Fatalf("attempts/matches=%d/%d want 100/100", snap["concurrent"].Attempts, snap["concurrent"].Matches)
	}
}

func TestGlobalFilterObservability(t *testing.T) {
	g := GlobalFilterObservability()
	if g == nil {
		t.Fatal("global should not be nil")
	}
}

func TestSetGlobalObservability(t *testing.T) {
	orig := globalObservability
	defer func() { globalObservability = orig }()
	custom := NewFilterObservability(99)
	setGlobalObservability(custom)
	if globalObservability != custom {
		t.Fatal("should have replaced global")
	}
}

func TestApplyLayer0Filters_PanicSurvival(t *testing.T) {
	orig := globalObservability
	defer func() { globalObservability = orig }()
	globalObservability = NewFilterObservability(50)

	argv := []string{"git", "status", "--short"}
	stdout := []byte(" M file.go\n")
	out, matched := applyLayer0Filters(".", argv, stdout)
	if matched == "" {
		t.Fatal("should match a filter for git status input")
	}
	_ = out
}
