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
	snap := o.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	fs := snap["git_status"]
	if fs.Calls != 2 {
		t.Fatalf("calls=%d want 2", fs.Calls)
	}
	if fs.BytesSaved != 170 {
		t.Fatalf("bytes_saved=%d want 170", fs.BytesSaved)
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
