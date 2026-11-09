package layout

import (
	"regexp"
	"testing"
)

func TestRandomNameWordsAreLowercaseLetters(t *testing.T) {
	t.Parallel()
	letters := regexp.MustCompile(`^[a-z]+$`)
	for _, words := range [][]string{adjectives, nouns} {
		if len(words) < 40 || len(words) > 60 {
			t.Errorf("word count = %d, want 40..60", len(words))
		}
		seen := map[string]bool{}
		for _, word := range words {
			if !letters.MatchString(word) {
				t.Errorf("word %q is not lowercase letters", word)
			}
			if seen[word] {
				t.Errorf("word %q is duplicated", word)
			}
			seen[word] = true
		}
	}
}

func TestRandomNameIsValidForEveryCombination(t *testing.T) {
	t.Parallel()
	for i := range adjectives {
		for j := range nouns {
			picks := []int{i, j}
			next := 0
			name := RandomName(func(int) int { next++; return picks[next-1] })
			if !IsValidTaskName(name) {
				t.Fatalf("%q is not a valid task name", name)
			}
		}
	}
}

func TestRandomNameUsesInjectedSource(t *testing.T) {
	t.Parallel()
	got := RandomName(func(int) int { return 0 })
	if want := adjectives[0] + "-" + nouns[0]; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
