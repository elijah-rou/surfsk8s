package components

import "testing"

func TestFuzzyScoreMatchesSubsequence(t *testing.T) {
	score, ok := FuzzyScore("prod-us-east-1", "pue1")
	if !ok {
		t.Fatalf("expected match")
	}
	if score <= 0 {
		t.Fatalf("score = %d, want > 0", score)
	}
}

func TestFuzzyScoreRejectsMissingCharacters(t *testing.T) {
	if _, ok := FuzzyScore("staging", "xyz"); ok {
		t.Fatalf("expected no match")
	}
}
