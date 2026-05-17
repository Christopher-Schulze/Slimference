package extract

import (
	"math"
	"testing"
)

func TestNewTFIDF_EmptyCorpus(t *testing.T) {
	tf := NewTFIDF(nil)
	if tf.Vocab() != 0 {
		t.Fatalf("empty corpus should yield empty vocab")
	}
	if tf.Score("anything") != 0 {
		t.Fatalf("empty TFIDF should score zero on anything")
	}
}

func TestNewTFIDF_ScoresHigherForRareTerm(t *testing.T) {
	corpus := []string{
		"the cat sat on the mat",
		"the dog jumped over the log",
		"the bird flew above the cloud",
		"the unique zebra appeared once",
	}
	tf := NewTFIDF(corpus)
	rare := tf.Score("zebra is unique")
	common := tf.Score("the the the")
	if rare <= common {
		t.Fatalf("rare-term sentence (%.3f) should outscore common-term sentence (%.3f)", rare, common)
	}
}

func TestNewTFIDF_HandlesSingleDocument(t *testing.T) {
	tf := NewTFIDF([]string{"a single document with several words"})
	if tf.Vocab() == 0 {
		t.Fatalf("single doc should still build vocab")
	}
	// IDF for any term in this single-doc corpus = log(1 + 1/1) = log(2) ≈ 0.693
	for _, term := range []string{"a", "single", "document"} {
		got := tf.idf[term]
		want := math.Log(2)
		if math.Abs(got-want) > 1e-6 {
			t.Errorf("idf[%q] = %v, want %v", term, got, want)
		}
	}
}

func TestTFIDF_VocabCount(t *testing.T) {
	tf := NewTFIDF([]string{"alpha beta", "beta gamma"})
	if tf.Vocab() != 3 {
		t.Fatalf("expected 3 distinct terms, got %d", tf.Vocab())
	}
}

func TestTFIDF_NilReceiverScoresZero(t *testing.T) {
	var tf *TFIDF
	if tf.Score("anything") != 0 {
		t.Fatalf("nil TFIDF should score zero")
	}
	if tf.Vocab() != 0 {
		t.Fatalf("nil TFIDF Vocab() should be 0")
	}
}

func TestNewTFIDF_ClampDFToOne(t *testing.T) {
	// We can't easily exercise the df<1 clamp through the public
	// API since tokenizeWords only adds existing tokens. The clamp
	// is defensive against future refactors. We assert by directly
	// invoking the path via a 1-doc corpus where no term appears
	// twice; df is exactly 1, so the clamp is a no-op but must not
	// crash.
	tf := NewTFIDF([]string{"x"})
	if tf.idf["x"] == 0 {
		t.Fatalf("single-token corpus should still produce non-zero idf")
	}
}

func TestTFIDF_DeterministicAcrossRuns(t *testing.T) {
	corpus := []string{"foo bar baz", "bar qux"}
	a := NewTFIDF(corpus).Score("foo bar")
	b := NewTFIDF(corpus).Score("foo bar")
	if a != b {
		t.Fatalf("non-deterministic: %v != %v", a, b)
	}
}
