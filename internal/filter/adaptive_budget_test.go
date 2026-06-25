package filter

import "testing"

func TestAdaptiveCompactionShouldFullPass(t *testing.T) {
	tests := []struct {
		name           string
		applied        int
		refetch        int
		compactedRatio float64
		want           bool
	}{
		{"no data floors to fixed", 0, 0, 0.34, false},
		{"below min samples floors to fixed even if all refetched", 4, 4, 0.34, false},
		{"low refetch keeps compacting", 10, 1, 0.34, false},
		{"refetch below break-even keeps compacting", 10, 6, 0.34, false}, // 0.60 < 0.66
		{"refetch at break-even demotes", 100, 66, 0.34, true},            // 0.66 >= 0.66
		{"refetch above break-even demotes", 10, 8, 0.34, true},           // 0.80 >= 0.66
		{"hard compaction tolerates high refetch", 10, 8, 0.10, false},    // break-even 0.90 > 0.80
		{"hard compaction demotes only when refetch passes 0.90", 10, 9, 0.10, true},
		{"weak compaction demotes readily", 10, 3, 0.80, true},                 // break-even 0.20, rate 0.30
		{"weak compaction holds below its low break-even", 10, 1, 0.80, false}, // rate 0.10 < 0.20
		{"ratio clamped above 1 -> break-even 0 -> always demote with samples", 10, 1, 1.5, true},
		{"ratio clamped below 0 -> break-even 1 -> never demote", 10, 9, -0.5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AdaptiveCompactionShouldFullPass(tt.applied, tt.refetch, tt.compactedRatio)
			if got != tt.want {
				t.Fatalf("AdaptiveCompactionShouldFullPass(applied=%d refetch=%d ratio=%.2f)=%v want %v",
					tt.applied, tt.refetch, tt.compactedRatio, got, tt.want)
			}
		})
	}
}

// TestAdaptiveCompactionFloorNeverRegresses is the zero-drawdown guard: for any
// re-fetch rate at all, while applied is below the min-sample floor the control
// must return false (compact = today's fixed behavior). If this fails, the
// mechanism could change behavior before it has trustworthy data.
func TestAdaptiveCompactionFloorNeverRegresses(t *testing.T) {
	for applied := 0; applied < adaptiveCompactionMinSamples; applied++ {
		for refetch := 0; refetch <= applied; refetch++ {
			for _, ratio := range []float64{0.0, 0.1, 0.34, 0.5, 0.9, 1.0} {
				if AdaptiveCompactionShouldFullPass(applied, refetch, ratio) {
					t.Fatalf("below floor (applied=%d) must keep fixed behavior, demoted at refetch=%d ratio=%.2f", applied, refetch, ratio)
				}
			}
		}
	}
}
