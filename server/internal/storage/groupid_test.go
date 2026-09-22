package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestNewGroupIDRejectsValuesThatCannotBeATenant(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"space", " "},
		{"tab", "\t"},
		{"newline", "\n"},
		{"path traversal", "../other"},
		{"sql fragment", "g1' OR '1'='1"},
		{"embedded null", "g1\x00"},
		{"too long", strings.Repeat("a", maxGroupIDLen+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := NewGroupID(tc.raw)
			if !errors.Is(err, ErrInvalidGroupID) {
				t.Fatalf("NewGroupID(%q) error = %v, want ErrInvalidGroupID", tc.raw, err)
			}
			if !g.IsZero() {
				t.Errorf("NewGroupID(%q) returned a usable scope %q alongside its error; a caller that ignores the error must not end up with a working GroupID", tc.raw, g)
			}
		})
	}
}

func TestNewGroupIDAcceptsWhatTheSchemaStores(t *testing.T) {
	for _, raw := range []string{
		"0199d6d0-8f8a-7000-8000-0123456789ab",
		"g1",
		"group_A-2",
		strings.Repeat("a", maxGroupIDLen),
	} {
		g, err := NewGroupID(raw)
		if err != nil {
			t.Fatalf("NewGroupID(%q): %v", raw, err)
		}
		if g.String() != raw {
			t.Errorf("NewGroupID(%q).String() = %q; the scope must be the identifier the schema stores, unaltered", raw, g.String())
		}
		if g.IsZero() {
			t.Errorf("NewGroupID(%q).IsZero() = true", raw)
		}
	}
}

func TestZeroGroupIDIsInertAndDetectable(t *testing.T) {
	var g GroupID
	if !g.IsZero() {
		t.Error("the zero GroupID does not report IsZero; every unscoped-construction guard in this package depends on it")
	}
	if g.String() != "" {
		t.Errorf("the zero GroupID stringifies to %q, want the empty string", g.String())
	}
	if g != (GroupID{}) {
		t.Error("GroupID is not comparable to its own zero value")
	}
}

func TestMustGroupIDPanicsRatherThanReturningAnUnvalidatedScope(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustGroupID(\"\") returned instead of panicking; a fixture with a bad literal must fail at the literal, not at the first empty result set")
		}
	}()
	_ = MustGroupID("")
}
