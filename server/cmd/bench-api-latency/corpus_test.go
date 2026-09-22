package main

import (
	"math/rand"
	"strings"
	"testing"
)

func TestItemNameIsReproducibleFromSeed(t *testing.T) {
	r1 := rand.New(rand.NewSource(42))
	r2 := rand.New(rand.NewSource(42))
	for i := 0; i < 20; i++ {
		a, b := itemName(r1), itemName(r2)
		if a != b {
			t.Fatalf("iteration %d: itemName diverged under the same seed: %q != %q", i, a, b)
		}
	}
}

func TestItemNameProducesVariety(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	seen := make(map[string]struct{})
	const n = 2000
	for i := 0; i < n; i++ {
		seen[itemName(r)] = struct{}{}
	}
	if len(seen) < n/4 {
		t.Fatalf("itemName produced only %d distinct names over %d draws — looks degenerate", len(seen), n)
	}
}

func TestItemDescriptionProducesVariety(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	seen := make(map[string]struct{})
	const n = 500
	for i := 0; i < n; i++ {
		d := itemDescription(r)
		if d == "" {
			t.Fatalf("itemDescription returned an empty string")
		}
		seen[d] = struct{}{}
	}
	if len(seen) < n/4 {
		t.Fatalf("itemDescription produced only %d distinct descriptions over %d draws — looks degenerate", len(seen), n)
	}
}

func TestSearchTermsAreDedupedSingleWordsAndNonEmpty(t *testing.T) {
	if len(searchTerms) == 0 {
		t.Fatal("searchTerms is empty")
	}
	seen := make(map[string]struct{}, len(searchTerms))
	for _, term := range searchTerms {
		if term == "" {
			t.Fatal("searchTerms contains an empty term")
		}
		if strings.ContainsAny(term, " \t") {
			t.Fatalf("searchTerms contains a multi-word term %q — listItems' q parameter ANDs whitespace-separated words, which would make this term's own hit-rate depend on both words appearing together", term)
		}
		if _, dup := seen[term]; dup {
			t.Fatalf("searchTerms contains a duplicate: %q", term)
		}
		seen[term] = struct{}{}
	}
}

func TestPickIsWithinBounds(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	xs := []string{"a", "b", "c"}
	valid := map[string]bool{"a": true, "b": true, "c": true}
	for i := 0; i < 100; i++ {
		if got := pick(r, xs); !valid[got] {
			t.Fatalf("pick returned %q, not a member of %v", got, xs)
		}
	}
}
