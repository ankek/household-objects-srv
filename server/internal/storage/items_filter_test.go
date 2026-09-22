package storage

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

type filterFixture struct {
	scopeA, scopeB Scope
	storage        *Storage
}

func newFilterFixture(t *testing.T) filterFixture {
	t.Helper()
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "anchorA", 1)
	seedItem(t, s, "groupB", "anchorB", 1)

	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err := s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}
	return filterFixture{scopeA: scopeA, scopeB: scopeB, storage: s}
}

func seedFilterItem(t *testing.T, s *Storage, groupID, itemID, name, description, locationID string, updatedAt int64) {
	t.Helper()
	var loc any
	if locationID != "" {
		loc = locationID
	}
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO items (id, group_id, name, description, location_id, short_code, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
			itemID, groupID, name, description, loc, itemID, updatedAt)
		return err
	}); err != nil {
		t.Fatalf("seed item %q: %v", itemID, err)
	}
}

func seedIdentification(t *testing.T, s *Storage, groupID, itemID, value string) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO item_identifications (id, group_id, item_id, kind, value, created_at, updated_at)
			 VALUES (?, ?, ?, 'serial', ?, 1, 1)`,
			"idn-"+itemID, groupID, itemID, value)
		return err
	}); err != nil {
		t.Fatalf("seed identification for %q: %v", itemID, err)
	}
}

func seedWarrantyNotes(t *testing.T, s *Storage, groupID, itemID, notes string) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO item_warranty (id, group_id, item_id, notes, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 1, 1)`,
			"wty-"+itemID, groupID, itemID, notes)
		return err
	}); err != nil {
		t.Fatalf("seed warranty for %q: %v", itemID, err)
	}
}

func seedLabelRow(t *testing.T, s *Storage, groupID, labelID, name string) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO labels (id, group_id, name, color, created_at, updated_at)
			 VALUES (?, ?, ?, '#000000', 1, 1)`,
			labelID, groupID, name)
		return err
	}); err != nil {
		t.Fatalf("seed label %q: %v", labelID, err)
	}
}

func seedItemLabel(t *testing.T, s *Storage, groupID, itemID, labelID string) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO item_labels (id, group_id, item_id, label_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 1, 1)`,
			itemID+"/"+labelID, groupID, itemID, labelID)
		return err
	}); err != nil {
		t.Fatalf("seed item_label %s/%s: %v", itemID, labelID, err)
	}
}

func seedLocationRow(t *testing.T, s *Storage, groupID, locationID, name, parentID string) {
	t.Helper()
	var parent any
	if parentID != "" {
		parent = parentID
	}
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO locations (id, group_id, name, parent_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 1, 1)`,
			locationID, groupID, name, parent)
		return err
	}); err != nil {
		t.Fatalf("seed location %q: %v", locationID, err)
	}
}

func seedFilterItemWithTimestamps(t *testing.T, s *Storage, groupID, itemID, name, locationID string, createdAt, updatedAt int64) {
	t.Helper()
	var loc any
	if locationID != "" {
		loc = locationID
	}
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO items (id, group_id, name, description, location_id, short_code, created_at, updated_at)
			 VALUES (?, ?, ?, '', ?, ?, ?, ?)`,
			itemID, groupID, name, loc, itemID, createdAt, updatedAt)
		return err
	}); err != nil {
		t.Fatalf("seed item with timestamps %q: %v", itemID, err)
	}
}

func seedSortItem(t *testing.T, s *Storage, groupID, itemID, name string, quantity, createdAt, updatedAt int64) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO items (id, group_id, name, description, quantity, short_code, created_at, updated_at)
			 VALUES (?, ?, ?, '', ?, ?, ?, ?)`,
			itemID, groupID, name, quantity, itemID, createdAt, updatedAt)
		return err
	}); err != nil {
		t.Fatalf("seed sort item %q: %v", itemID, err)
	}
}

type customFieldSeed struct {
	ID, ItemID, Name, FieldType string
	TextValue, DateValue        *string
	NumberValue                 *float64
	BoolValue                   *int64
}

func seedCustomField(t *testing.T, s *Storage, groupID string, cf customFieldSeed) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO item_custom_fields
			   (id, group_id, item_id, name, field_type, text_value, number_value, bool_value, date_value, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 1)`,
			cf.ID, groupID, cf.ItemID, cf.Name, cf.FieldType, cf.TextValue, cf.NumberValue, cf.BoolValue, cf.DateValue)
		return err
	}); err != nil {
		t.Fatalf("seed custom field %q: %v", cf.ID, err)
	}
}

type warrantySeed struct {
	ID, ItemID          string
	IsLifetime          bool
	StartsOn, ExpiresOn *string
}

func seedWarranty(t *testing.T, s *Storage, groupID string, w warrantySeed) {
	t.Helper()
	lifetime := int64(0)
	if w.IsLifetime {
		lifetime = 1
	}
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO item_warranty (id, group_id, item_id, starts_on, expires_on, is_lifetime, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, 1, 1)`,
			w.ID, groupID, w.ItemID, w.StartsOn, w.ExpiresOn, lifetime)
		return err
	}); err != nil {
		t.Fatalf("seed warranty %q: %v", w.ID, err)
	}
}

func i64Ptr(i int64) *int64 { return &i }

func idsOf(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	sort.Strings(out)
	return out
}

func idListOrdered(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func assertIDs(t *testing.T, got []Item, want ...string) {
	t.Helper()
	sort.Strings(want)
	gotIDs := idsOf(got)
	if strings.Join(gotIDs, ",") != strings.Join(want, ",") {
		t.Fatalf("got items %v, want %v", gotIDs, want)
	}
}

var wholePage = Page{Limit: 200, Offset: 0}

func TestZeroFilterReturnsExactlyWhatListReturns(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Cordless drill", "18V", "", 30)
	seedFilterItem(t, f.storage, "groupA", "itm-kettle", "Kettle", "stainless", "", 20)

	plain, err := f.scopeA.Items().List(t.Context(), wholePage)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	filtered, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if strings.Join(idsOf(plain), ",") != strings.Join(idsOf(filtered), ",") {
		t.Fatalf("a zero filter returned %v but List returned %v", idsOf(filtered), idsOf(plain))
	}
}

func TestSearchReachesEveryIndexedColumn(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Cordless drill", "eighteen volt hammer", "", 30)
	seedIdentification(t, f.storage, "groupA", "itm-drill", "SNXYZ")
	seedWarrantyNotes(t, f.storage, "groupA", "itm-drill", "purchased at Hardwareshop")
	seedFilterItem(t, f.storage, "groupA", "itm-kettle", "Kettle", "stainless", "", 20)

	for name, term := range map[string]string{
		"name":                 "Cordless",
		"description":          "hammer",
		"identification value": "SNXYZ",
		"warranty notes":       "Hardwareshop",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: term}, wholePage)
			if err != nil {
				t.Fatalf("search %q: %v", term, err)
			}
			assertIDs(t, got, "itm-drill")
		})
	}
}

func TestSearchTreatsInputAsLiteralTextNotAnExpression(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Cordless drill", "18V", "", 30)
	seedIdentification(t, f.storage, "groupA", "itm-drill", "SN-999")
	seedFilterItem(t, f.storage, "groupA", "itm-ladder", "Ladder", "20-inch step", "", 20)
	seedFilterItem(t, f.storage, "groupA", "itm-kettle", "Kettle", "stainless", "", 10)

	for name, tc := range map[string]struct {
		query string
		want  []string
	}{
		"a hyphenated serial number":       {"SN-999", []string{"itm-drill"}},
		"a hyphenated size":                {"20-inch", []string{"itm-ladder"}},
		"a bare asterisk":                  {"*", nil},
		"an unbalanced parenthesis":        {"(drill", []string{"itm-drill"}},
		"a stray double quote":             {`drill"`, []string{"itm-drill"}},
		"a caret":                          {"^", nil},
		"a colon (FTS5's column operator)": {"name:drill", nil},
		"two words are ANDed":              {"cordless drill", []string{"itm-drill"}},
		"a prefix, as a person types":      {"dri", []string{"itm-drill"}},
		"whitespace only is no search":     {"   ", []string{"anchorA", "itm-drill", "itm-kettle", "itm-ladder"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: tc.query}, wholePage)
			if err != nil {
				t.Fatalf("search %q returned an error: %v -- raw input must never reach FTS5 unsanitised", tc.query, err)
			}
			assertIDs(t, got, tc.want...)
		})
	}
}

func TestSearchExcludesAnotherGroupsMatchingItem(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-a-drill", "Cordless drill", "A's", "", 30)
	seedFilterItem(t, f.storage, "groupB", "itm-b-drill", "Cordless drill", "B's", "", 30)

	gotA, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: "drill"}, wholePage)
	if err != nil {
		t.Fatalf("search as A: %v", err)
	}
	assertIDs(t, gotA, "itm-a-drill")

	gotB, err := f.scopeB.Items().ListFiltered(t.Context(), ItemFilter{Query: "drill"}, wholePage)
	if err != nil {
		t.Fatalf("search as B: %v", err)
	}
	assertIDs(t, gotB, "itm-b-drill")
}

func TestEveryHandWrittenStatementIsGroupScoped(t *testing.T) {
	boundScope := regexp.MustCompile(`(?i)\bgroup_id\s*=\s*\?`)
	literalScope := regexp.MustCompile(`(?i)group_id\s*=\s*'`)

	check := func(t *testing.T, name, query string, args []any) {
		t.Helper()
		if !boundScope.MatchString(query) {
			t.Fatalf("%s has no parameter-bound group_id predicate -- the .sql lint cannot see this statement:\n%s", name, query)
		}
		if literalScope.MatchString(query) {
			t.Fatalf("%s compares group_id to a literal; the tenant must arrive as a bound parameter:\n%s", name, query)
		}
		if !strings.Contains(query, "deleted_at IS NULL") {
			t.Fatalf("%s does not filter tombstones:\n%s", name, query)
		}
		if args == nil {
			return
		}
		if got, want := strings.Count(query, "?"), len(args); got != want {
			t.Fatalf("%s has %d placeholders but binds %d arguments -- SQLite would bind the wrong values while compiling and running perfectly:\n%s", name, got, want, query)
		}
	}

	t.Run("searchItemIDsSQL", func(t *testing.T) {
		check(t, "searchItemIDsSQL", searchItemIDsSQL, nil)
	})

	for name, f := range map[string]ItemFilter{
		"search only":                              {Query: "drill"},
		"one location":                             {LocationIDs: []string{"loc-a"}},
		"three locations":                          {LocationIDs: []string{"loc-a", "loc-b", "loc-c"}},
		"one label":                                {LabelIDs: []string{"lbl-a"}},
		"three labels":                             {LabelIDs: []string{"lbl-a", "lbl-b", "lbl-c"}},
		"repeated label":                           {LabelIDs: []string{"lbl-a", "lbl-a"}},
		"locations and labels":                     {LocationIDs: []string{"loc-a", "loc-b"}, LabelIDs: []string{"lbl-a", "lbl-b"}},
		"search, locations, labels":                {Query: "drill", LocationIDs: []string{"loc-a", "loc-b"}, LabelIDs: []string{"lbl-a", "lbl-b"}},
		"one custom field, text value":             {CustomFields: []CustomFieldMatch{{Name: "Colour", Value: "Red"}}},
		"one custom field, numeric-looking value":  {CustomFields: []CustomFieldMatch{{Name: "Weight", Value: "12.5"}}},
		"one custom field, bool-looking value":     {CustomFields: []CustomFieldMatch{{Name: "Fragile", Value: "true"}}},
		"three custom fields":                      {CustomFields: []CustomFieldMatch{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}, {Name: "c", Value: "x"}}},
		"warranty status none":                     {WarrantyStatus: WarrantyStatusNone},
		"warranty status any":                      {WarrantyStatus: WarrantyStatusAny},
		"warranty status active":                   {WarrantyStatus: WarrantyStatusActive},
		"warranty status expired":                  {WarrantyStatus: WarrantyStatusExpired},
		"warranty status lifetime":                 {WarrantyStatus: WarrantyStatusLifetime},
		"warranty status unknown token is ignored": {WarrantyStatus: "not-a-real-status"},
		"created from only":                        {CreatedFrom: i64Ptr(100)},
		"created to only":                          {CreatedTo: i64Ptr(200)},
		"created range":                            {CreatedFrom: i64Ptr(100), CreatedTo: i64Ptr(200)},
		"updated range":                            {UpdatedFrom: i64Ptr(100), UpdatedTo: i64Ptr(200)},
		"sort name ascending":                      {Sort: ItemSort{Field: ItemSortName}},
		"sort name descending":                     {Sort: ItemSort{Field: ItemSortName, Descending: true}},
		"sort quantity ascending":                  {Sort: ItemSort{Field: ItemSortQuantity}},
		"sort quantity descending":                 {Sort: ItemSort{Field: ItemSortQuantity, Descending: true}},
		"sort created_at ascending":                {Sort: ItemSort{Field: ItemSortCreatedAt}},
		"sort updated_at descending":               {Sort: ItemSort{Field: ItemSortUpdatedAt, Descending: true}},
		"sort unknown field falls back":            {Sort: ItemSort{Field: ItemSortField("not-a-real-column")}},
		"sort combined with every filter": {
			Query: "drill", LocationIDs: []string{"loc-a", "loc-b"}, LabelIDs: []string{"lbl-a", "lbl-b"},
			CustomFields:   []CustomFieldMatch{{Name: "Colour", Value: "Red"}, {Name: "Weight", Value: "12.5"}},
			WarrantyStatus: WarrantyStatusActive,
			CreatedFrom:    i64Ptr(100), CreatedTo: i64Ptr(200),
			UpdatedFrom: i64Ptr(100), UpdatedTo: i64Ptr(200),
			Sort: ItemSort{Field: ItemSortQuantity, Descending: true},
		},
		"everything at once": {
			Query: "drill", LocationIDs: []string{"loc-a", "loc-b"}, LabelIDs: []string{"lbl-a", "lbl-b"},
			CustomFields:   []CustomFieldMatch{{Name: "Colour", Value: "Red"}, {Name: "Weight", Value: "12.5"}},
			WarrantyStatus: WarrantyStatusActive,
			CreatedFrom:    i64Ptr(100), CreatedTo: i64Ptr(200),
			UpdatedFrom: i64Ptr(100), UpdatedTo: i64Ptr(200),
		},
	} {
		t.Run("buildFilteredItemsQuery/"+name, func(t *testing.T) {
			var matched []string
			if f.Query != "" {
				matched = []string{"itm-1", "itm-2"}
			}
			query, args := buildFilteredItemsQuery("groupA", matched, f, Page{Limit: 50}, "2026-09-04")
			check(t, "buildFilteredItemsQuery("+name+")", query, args)
		})
	}
}

func TestFilteredQueryBindsTheGroupToEveryTenantTable(t *testing.T) {
	for name, tc := range map[string]struct {
		filter    ItemFilter
		wantTable string
	}{
		"labels":             {ItemFilter{LabelIDs: []string{"lbl-a"}}, "il"},
		"custom field":       {ItemFilter{CustomFields: []CustomFieldMatch{{Name: "Colour", Value: "Red"}}}, "cf"},
		"warranty":           {ItemFilter{WarrantyStatus: WarrantyStatusActive}, "w"},
		"labels with a sort": {ItemFilter{LabelIDs: []string{"lbl-a"}, Sort: ItemSort{Field: ItemSortName, Descending: true}}, "il"},
	} {
		t.Run(name, func(t *testing.T) {
			query, args := buildFilteredItemsQuery("groupA", nil, tc.filter, Page{Limit: 50}, "2026-09-04")
			needle := tc.wantTable + ".group_id = ?"
			if !strings.Contains(query, needle) {
				t.Fatalf("the %s subquery does not bind its own group_id:\n%s", tc.wantTable, query)
			}
			groups := 0
			for _, a := range args {
				if s, ok := a.(string); ok && s == "groupA" {
					groups++
				}
			}
			if groups != 2 {
				t.Fatalf("the group id is bound %d times, want 2 (items and the satellite table); a missing one means a tenant table is scoped only by inheritance", groups)
			}
		})
	}
}

func TestSearchExcludesTombstonedItems(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Cordless drill", "18V", "", 30)
	if err := f.storage.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE items SET deleted_at = 99 WHERE id = 'itm-drill'`)
		return err
	}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: "drill"}, wholePage)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertIDs(t, got)
}

func TestLocationFilterIsDirectByDefault(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-garage", "Garage", "")
	seedLocationRow(t, f.storage, "groupA", "loc-shelf", "Shelf", "loc-garage")
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Drill", "", "loc-garage", 30)
	seedFilterItem(t, f.storage, "groupA", "itm-screws", "Screws", "", "loc-shelf", 20)
	seedFilterItem(t, f.storage, "groupA", "itm-kettle", "Kettle", "", "", 10)

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{LocationIDs: []string{"loc-garage"}}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	assertIDs(t, got, "itm-drill")
}

func TestLocationFilterAcceptsASetOfIDs(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-garage", "Garage", "")
	seedLocationRow(t, f.storage, "groupA", "loc-shelf", "Shelf", "loc-garage")
	seedLocationRow(t, f.storage, "groupA", "loc-attic", "Attic", "")
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Drill", "", "loc-garage", 30)
	seedFilterItem(t, f.storage, "groupA", "itm-screws", "Screws", "", "loc-shelf", 20)
	seedFilterItem(t, f.storage, "groupA", "itm-skis", "Skis", "", "loc-attic", 10)

	got, err := f.scopeA.Items().ListFiltered(t.Context(),
		ItemFilter{LocationIDs: []string{"loc-garage", "loc-shelf"}}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	assertIDs(t, got, "itm-drill", "itm-screws")
}

func TestLabelFilterANDsMultipleLabels(t *testing.T) {
	f := newFilterFixture(t)
	seedLabelRow(t, f.storage, "groupA", "lbl-fragile", "fragile")
	seedLabelRow(t, f.storage, "groupA", "lbl-lent", "lent out")
	seedFilterItem(t, f.storage, "groupA", "itm-both", "Vase on loan", "", "", 30)
	seedFilterItem(t, f.storage, "groupA", "itm-fragile", "Mirror", "", "", 20)
	seedFilterItem(t, f.storage, "groupA", "itm-lent", "Ladder", "", "", 10)
	seedItemLabel(t, f.storage, "groupA", "itm-both", "lbl-fragile")
	seedItemLabel(t, f.storage, "groupA", "itm-both", "lbl-lent")
	seedItemLabel(t, f.storage, "groupA", "itm-fragile", "lbl-fragile")
	seedItemLabel(t, f.storage, "groupA", "itm-lent", "lbl-lent")

	t.Run("one label", func(t *testing.T) {
		got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{LabelIDs: []string{"lbl-fragile"}}, wholePage)
		if err != nil {
			t.Fatalf("ListFiltered: %v", err)
		}
		assertIDs(t, got, "itm-both", "itm-fragile")
	})

	t.Run("two labels means AND", func(t *testing.T) {
		got, err := f.scopeA.Items().ListFiltered(t.Context(),
			ItemFilter{LabelIDs: []string{"lbl-fragile", "lbl-lent"}}, wholePage)
		if err != nil {
			t.Fatalf("ListFiltered: %v", err)
		}
		assertIDs(t, got, "itm-both")
	})

	t.Run("a repeated label counts once", func(t *testing.T) {
		got, err := f.scopeA.Items().ListFiltered(t.Context(),
			ItemFilter{LabelIDs: []string{"lbl-fragile", "lbl-fragile"}}, wholePage)
		if err != nil {
			t.Fatalf("ListFiltered: %v", err)
		}
		assertIDs(t, got, "itm-both", "itm-fragile")
	})
}

func TestLabelFilterIgnoresDetachedAssignments(t *testing.T) {
	f := newFilterFixture(t)
	seedLabelRow(t, f.storage, "groupA", "lbl-fragile", "fragile")
	seedFilterItem(t, f.storage, "groupA", "itm-mirror", "Mirror", "", "", 20)
	seedItemLabel(t, f.storage, "groupA", "itm-mirror", "lbl-fragile")
	if err := f.storage.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE item_labels SET deleted_at = 99 WHERE item_id = 'itm-mirror'`)
		return err
	}); err != nil {
		t.Fatalf("detach: %v", err)
	}

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{LabelIDs: []string{"lbl-fragile"}}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	assertIDs(t, got)
}

func TestFiltersCombineWithAND(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-garage", "Garage", "")
	seedLabelRow(t, f.storage, "groupA", "lbl-fragile", "fragile")

	seedFilterItem(t, f.storage, "groupA", "itm-target", "Cordless drill", "", "loc-garage", 40)
	seedItemLabel(t, f.storage, "groupA", "itm-target", "lbl-fragile")
	seedFilterItem(t, f.storage, "groupA", "itm-nolocation", "Cordless drill", "", "", 30)
	seedItemLabel(t, f.storage, "groupA", "itm-nolocation", "lbl-fragile")
	seedFilterItem(t, f.storage, "groupA", "itm-nolabel", "Cordless drill", "", "loc-garage", 20)
	seedFilterItem(t, f.storage, "groupA", "itm-nomatch", "Kettle", "", "loc-garage", 10)
	seedItemLabel(t, f.storage, "groupA", "itm-nomatch", "lbl-fragile")

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{
		Query:       "drill",
		LocationIDs: []string{"loc-garage"},
		LabelIDs:    []string{"lbl-fragile"},
	}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	assertIDs(t, got, "itm-target")
}

func TestFilteredListPaginatesTheFilteredSet(t *testing.T) {
	f := newFilterFixture(t)
	seedLabelRow(t, f.storage, "groupA", "lbl-x", "x")
	for i, id := range []string{"itm-m1", "itm-m2", "itm-m3"} {
		seedFilterItem(t, f.storage, "groupA", id, "Match", "", "", int64(100-i*10))
		seedItemLabel(t, f.storage, "groupA", id, "lbl-x")
	}
	for i, id := range []string{"itm-n1", "itm-n2", "itm-n3"} {
		seedFilterItem(t, f.storage, "groupA", id, "Other", "", "", int64(95-i*10))
	}

	filter := ItemFilter{LabelIDs: []string{"lbl-x"}}
	first, err := f.scopeA.Items().ListFiltered(t.Context(), filter, Page{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	assertIDs(t, first, "itm-m1", "itm-m2")

	second, err := f.scopeA.Items().ListFiltered(t.Context(), filter, Page{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	assertIDs(t, second, "itm-m3")
}

func TestFilterMatchingNothingIsAnEmptyListNotAnError(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Drill", "", "", 30)

	for name, filter := range map[string]ItemFilter{
		"a search matching nothing":      {Query: "zzzznothing"},
		"an unknown location":            {LocationIDs: []string{"loc-nowhere"}},
		"an unknown label":               {LabelIDs: []string{"lbl-nowhere"}},
		"a search plus unknown location": {Query: "drill", LocationIDs: []string{"loc-nowhere"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.scopeA.Items().ListFiltered(t.Context(), filter, wholePage)
			if err != nil {
				t.Fatalf("ListFiltered: %v", err)
			}
			assertIDs(t, got)
		})
	}
}

func TestDescendantsWalksThroughTombstonedNodes(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-house", "House", "")
	seedLocationRow(t, f.storage, "groupA", "loc-garage", "Garage", "loc-house")
	seedLocationRow(t, f.storage, "groupA", "loc-shelf", "Shelf", "loc-garage")
	if err := f.storage.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE locations SET deleted_at = 99 WHERE id = 'loc-garage'`)
		return err
	}); err != nil {
		t.Fatalf("tombstone the middle node: %v", err)
	}

	got, err := f.scopeA.Locations().Descendants(t.Context(), "loc-house")
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "loc-house,loc-shelf" {
		t.Fatalf("Descendants = %v, want [loc-house loc-shelf] -- the walk must descend THROUGH the tombstoned Garage to reach the live Shelf, and must not return the tombstone itself", got)
	}
}

func TestDescendantsExcludesAnotherGroupsTree(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupB", "loc-b-house", "B's house", "")
	seedLocationRow(t, f.storage, "groupB", "loc-b-garage", "B's garage", "loc-b-house")

	got, err := f.scopeA.Locations().Descendants(t.Context(), "loc-b-house")
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("household A expanded household B's subtree to %v, want nothing", got)
	}
}

func TestDescendantsTerminatesOnACyclicEdgeSet(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-a", "A", "")
	seedLocationRow(t, f.storage, "groupA", "loc-b", "B", "loc-a")
	if err := f.storage.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE locations SET parent_id = 'loc-b' WHERE id = 'loc-a'`)
		return err
	}); err != nil {
		t.Fatalf("close the cycle: %v", err)
	}

	got, err := f.scopeA.Locations().Descendants(t.Context(), "loc-a")
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "loc-a,loc-b" {
		t.Fatalf("Descendants on a cyclic edge set = %v, want each node once", got)
	}
}

func TestCustomFieldFilterMatchesTypedValue(t *testing.T) {
	f := newFilterFixture(t)

	seedFilterItem(t, f.storage, "groupA", "itm-red", "Item Red", "", "", 40)
	seedFilterItem(t, f.storage, "groupA", "itm-blue", "Item Blue", "", "", 30)
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-red", ItemID: "itm-red", Name: "Colour", FieldType: "text", TextValue: strPtr("Red")})
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-blue", ItemID: "itm-blue", Name: "Colour", FieldType: "text", TextValue: strPtr("Blue")})

	seedFilterItem(t, f.storage, "groupA", "itm-heavy", "Item Heavy", "", "", 40)
	seedFilterItem(t, f.storage, "groupA", "itm-light", "Item Light", "", "", 30)
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-heavy", ItemID: "itm-heavy", Name: "Weight", FieldType: "number", NumberValue: floatPtr(12.5)})
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-light", ItemID: "itm-light", Name: "Weight", FieldType: "number", NumberValue: floatPtr(1)})

	seedFilterItem(t, f.storage, "groupA", "itm-fragile", "Item Fragile", "", "", 40)
	seedFilterItem(t, f.storage, "groupA", "itm-sturdy", "Item Sturdy", "", "", 30)
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-fragile", ItemID: "itm-fragile", Name: "Fragile", FieldType: "boolean", BoolValue: i64Ptr(1)})
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-sturdy", ItemID: "itm-sturdy", Name: "Fragile", FieldType: "boolean", BoolValue: i64Ptr(0)})

	seedFilterItem(t, f.storage, "groupA", "itm-old", "Item Old", "", "", 40)
	seedFilterItem(t, f.storage, "groupA", "itm-new", "Item New", "", "", 30)
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-old", ItemID: "itm-old", Name: "Installed", FieldType: "date", DateValue: strPtr("2020-01-01")})
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-new", ItemID: "itm-new", Name: "Installed", FieldType: "date", DateValue: strPtr("2024-01-01")})

	for name, tc := range map[string]struct {
		match CustomFieldMatch
		want  []string
	}{
		"text value":   {CustomFieldMatch{Name: "Colour", Value: "Red"}, []string{"itm-red"}},
		"number value": {CustomFieldMatch{Name: "Weight", Value: "12.5"}, []string{"itm-heavy"}},
		"bool value":   {CustomFieldMatch{Name: "Fragile", Value: "true"}, []string{"itm-fragile"}},
		"date value":   {CustomFieldMatch{Name: "Installed", Value: "2020-01-01"}, []string{"itm-old"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{CustomFields: []CustomFieldMatch{tc.match}}, wholePage)
			if err != nil {
				t.Fatalf("ListFiltered: %v", err)
			}
			assertIDs(t, got, tc.want...)
		})
	}
}

func TestCustomFieldFilterValueOfWrongTypeMatchesNothingNotAnError(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-heavy", "Item Heavy", "", "", 40)
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-heavy", ItemID: "itm-heavy", Name: "Weight", FieldType: "number", NumberValue: floatPtr(12.5)})

	got, err := f.scopeA.Items().ListFiltered(t.Context(),
		ItemFilter{CustomFields: []CustomFieldMatch{{Name: "Weight", Value: "not-a-number"}}}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered returned an error for a type-mismatched custom field value: %v -- want an empty list, not a 500", err)
	}
	assertIDs(t, got)
}

func TestCustomFieldFilterCombinesMultipleFieldsWithAND(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-both", "Both", "", "", 40)
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-both-colour", ItemID: "itm-both", Name: "Colour", FieldType: "text", TextValue: strPtr("Red")})
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-both-weight", ItemID: "itm-both", Name: "Weight", FieldType: "number", NumberValue: floatPtr(5)})

	seedFilterItem(t, f.storage, "groupA", "itm-colour-only", "ColourOnly", "", "", 30)
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-co-colour", ItemID: "itm-colour-only", Name: "Colour", FieldType: "text", TextValue: strPtr("Red")})

	seedFilterItem(t, f.storage, "groupA", "itm-weight-only", "WeightOnly", "", "", 20)
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-wo-weight", ItemID: "itm-weight-only", Name: "Weight", FieldType: "number", NumberValue: floatPtr(5)})

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{CustomFields: []CustomFieldMatch{
		{Name: "Colour", Value: "Red"},
		{Name: "Weight", Value: "5"},
	}}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	assertIDs(t, got, "itm-both")
}

func TestCustomFieldFilterExcludesAnotherGroupsMatchingField(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-a", "A", "", "", 30)
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-a", ItemID: "itm-a", Name: "Colour", FieldType: "text", TextValue: strPtr("Red")})
	seedFilterItem(t, f.storage, "groupB", "itm-b", "B", "", "", 30)
	seedCustomField(t, f.storage, "groupB", customFieldSeed{ID: "cf-b", ItemID: "itm-b", Name: "Colour", FieldType: "text", TextValue: strPtr("Red")})

	gotA, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{CustomFields: []CustomFieldMatch{{Name: "Colour", Value: "Red"}}}, wholePage)
	if err != nil {
		t.Fatalf("search as A: %v", err)
	}
	assertIDs(t, gotA, "itm-a")

	gotB, err := f.scopeB.Items().ListFiltered(t.Context(), ItemFilter{CustomFields: []CustomFieldMatch{{Name: "Colour", Value: "Red"}}}, wholePage)
	if err != nil {
		t.Fatalf("search as B: %v", err)
	}
	assertIDs(t, gotB, "itm-b")
}

func TestWarrantyStatusFiltersToClosedSet(t *testing.T) {
	f := newFilterFixture(t)
	today := time.Now().UTC()
	yesterday := today.AddDate(0, 0, -1).Format(isoDateLayout)
	tomorrow := today.AddDate(0, 0, 1).Format(isoDateLayout)

	seedFilterItem(t, f.storage, "groupA", "itm-none", "None", "", "", 50)

	seedFilterItem(t, f.storage, "groupA", "itm-active", "Active", "", "", 40)
	seedWarranty(t, f.storage, "groupA", warrantySeed{ID: "wty-active", ItemID: "itm-active", ExpiresOn: strPtr(tomorrow)})

	seedFilterItem(t, f.storage, "groupA", "itm-expired", "Expired", "", "", 30)
	seedWarranty(t, f.storage, "groupA", warrantySeed{ID: "wty-expired", ItemID: "itm-expired", ExpiresOn: strPtr(yesterday)})

	seedFilterItem(t, f.storage, "groupA", "itm-lifetime", "Lifetime", "", "", 20)
	seedWarranty(t, f.storage, "groupA", warrantySeed{ID: "wty-lifetime", ItemID: "itm-lifetime", IsLifetime: true})

	seedFilterItem(t, f.storage, "groupA", "itm-undated", "Undated", "", "", 10)
	seedWarranty(t, f.storage, "groupA", warrantySeed{ID: "wty-undated", ItemID: "itm-undated"})

	for name, tc := range map[string]struct {
		status string
		want   []string
	}{
		"none":     {WarrantyStatusNone, []string{"anchorA", "itm-none"}},
		"any":      {WarrantyStatusAny, []string{"itm-active", "itm-expired", "itm-lifetime", "itm-undated"}},
		"active":   {WarrantyStatusActive, []string{"itm-active", "itm-lifetime"}},
		"expired":  {WarrantyStatusExpired, []string{"itm-expired"}},
		"lifetime": {WarrantyStatusLifetime, []string{"itm-lifetime"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{WarrantyStatus: tc.status}, wholePage)
			if err != nil {
				t.Fatalf("ListFiltered: %v", err)
			}
			assertIDs(t, got, tc.want...)
		})
	}
}

func TestWarrantyStatusActiveRequiresTheStartDateToHaveArrived(t *testing.T) {
	f := newFilterFixture(t)
	today := time.Now().UTC()
	future := today.AddDate(0, 0, 30).Format(isoDateLayout)
	farFuture := today.AddDate(0, 0, 400).Format(isoDateLayout)

	seedFilterItem(t, f.storage, "groupA", "itm-not-yet", "NotYet", "", "", 30)
	seedWarranty(t, f.storage, "groupA", warrantySeed{ID: "wty-not-yet", ItemID: "itm-not-yet", StartsOn: strPtr(future), ExpiresOn: strPtr(farFuture)})

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{WarrantyStatus: WarrantyStatusActive}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	assertIDs(t, got)
}

func TestWarrantyStatusUnknownTokenIsIgnored(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-1", "One", "", "", 30)
	seedFilterItem(t, f.storage, "groupA", "itm-2", "Two", "", "", 20)

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{WarrantyStatus: "not-a-real-status"}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	assertIDs(t, got, "anchorA", "itm-1", "itm-2")
}

func TestWarrantyStatusExcludesAnotherGroupsWarranty(t *testing.T) {
	f := newFilterFixture(t)
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format(isoDateLayout)

	seedFilterItem(t, f.storage, "groupA", "itm-a", "A", "", "", 30)
	seedWarranty(t, f.storage, "groupA", warrantySeed{ID: "wty-a", ItemID: "itm-a", ExpiresOn: strPtr(tomorrow)})
	seedFilterItem(t, f.storage, "groupB", "itm-b", "B", "", "", 30)
	seedWarranty(t, f.storage, "groupB", warrantySeed{ID: "wty-b", ItemID: "itm-b", ExpiresOn: strPtr(tomorrow)})

	gotA, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{WarrantyStatus: WarrantyStatusActive}, wholePage)
	if err != nil {
		t.Fatalf("as A: %v", err)
	}
	assertIDs(t, gotA, "itm-a")

	gotB, err := f.scopeB.Items().ListFiltered(t.Context(), ItemFilter{WarrantyStatus: WarrantyStatusActive}, wholePage)
	if err != nil {
		t.Fatalf("as B: %v", err)
	}
	assertIDs(t, gotB, "itm-b")
}

func TestCreatedAtRangeFilter(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItemWithTimestamps(t, f.storage, "groupA", "itm-early", "Early", "", 1000, 1000)
	seedFilterItemWithTimestamps(t, f.storage, "groupA", "itm-mid", "Mid", "", 2000, 2000)
	seedFilterItemWithTimestamps(t, f.storage, "groupA", "itm-late", "Late", "", 3000, 3000)

	for name, tc := range map[string]struct {
		filter ItemFilter
		want   []string
	}{
		"from only":                  {ItemFilter{CreatedFrom: i64Ptr(2000)}, []string{"itm-mid", "itm-late"}},
		"to only":                    {ItemFilter{CreatedTo: i64Ptr(2000)}, []string{"anchorA", "itm-early", "itm-mid"}},
		"both, inclusive boundaries": {ItemFilter{CreatedFrom: i64Ptr(1000), CreatedTo: i64Ptr(2000)}, []string{"itm-early", "itm-mid"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.scopeA.Items().ListFiltered(t.Context(), tc.filter, wholePage)
			if err != nil {
				t.Fatalf("ListFiltered: %v", err)
			}
			assertIDs(t, got, tc.want...)
		})
	}
}

func TestUpdatedAtRangeFilter(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-early", "Early", "", "", 1000)
	seedFilterItem(t, f.storage, "groupA", "itm-mid", "Mid", "", "", 2000)
	seedFilterItem(t, f.storage, "groupA", "itm-late", "Late", "", "", 3000)

	for name, tc := range map[string]struct {
		filter ItemFilter
		want   []string
	}{
		"from only":                  {ItemFilter{UpdatedFrom: i64Ptr(2000)}, []string{"itm-mid", "itm-late"}},
		"to only":                    {ItemFilter{UpdatedTo: i64Ptr(2000)}, []string{"anchorA", "itm-early", "itm-mid"}},
		"both, inclusive boundaries": {ItemFilter{UpdatedFrom: i64Ptr(1000), UpdatedTo: i64Ptr(2000)}, []string{"itm-early", "itm-mid"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.scopeA.Items().ListFiltered(t.Context(), tc.filter, wholePage)
			if err != nil {
				t.Fatalf("ListFiltered: %v", err)
			}
			assertIDs(t, got, tc.want...)
		})
	}
}

func TestDateRangeFiltersExcludeAnotherGroupsItems(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItemWithTimestamps(t, f.storage, "groupA", "itm-a", "A", "", 5000, 5000)
	seedFilterItemWithTimestamps(t, f.storage, "groupB", "itm-b", "B", "", 5000, 5000)

	gotA, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{CreatedFrom: i64Ptr(4000)}, wholePage)
	if err != nil {
		t.Fatalf("as A: %v", err)
	}
	assertIDs(t, gotA, "itm-a")

	gotB, err := f.scopeB.Items().ListFiltered(t.Context(), ItemFilter{CreatedFrom: i64Ptr(4000)}, wholePage)
	if err != nil {
		t.Fatalf("as B: %v", err)
	}
	assertIDs(t, gotB, "itm-b")
}

func TestNewFiltersCombineWithEverythingElseViaAND(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-garage", "Garage", "")
	seedLabelRow(t, f.storage, "groupA", "lbl-fragile", "fragile")
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format(isoDateLayout)

	seedFilterItemWithTimestamps(t, f.storage, "groupA", "itm-target", "Cordless drill", "loc-garage", 5000, 5000)
	seedItemLabel(t, f.storage, "groupA", "itm-target", "lbl-fragile")
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-target", ItemID: "itm-target", Name: "Colour", FieldType: "text", TextValue: strPtr("Red")})
	seedWarranty(t, f.storage, "groupA", warrantySeed{ID: "wty-target", ItemID: "itm-target", ExpiresOn: strPtr(tomorrow)})

	seedFilterItemWithTimestamps(t, f.storage, "groupA", "itm-almost", "Cordless drill", "loc-garage", 5000, 5000)
	seedItemLabel(t, f.storage, "groupA", "itm-almost", "lbl-fragile")
	seedWarranty(t, f.storage, "groupA", warrantySeed{ID: "wty-almost", ItemID: "itm-almost", ExpiresOn: strPtr(tomorrow)})

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{
		Query:          "drill",
		LocationIDs:    []string{"loc-garage"},
		LabelIDs:       []string{"lbl-fragile"},
		CustomFields:   []CustomFieldMatch{{Name: "Colour", Value: "Red"}},
		WarrantyStatus: WarrantyStatusActive,
		CreatedFrom:    i64Ptr(4000),
		UpdatedTo:      i64Ptr(6000),
	}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	assertIDs(t, got, "itm-target")
}

func TestSortOrdersByEachAllowedColumn(t *testing.T) {
	f := newFilterFixture(t)
	seedSortItem(t, f.storage, "groupA", "itm-x", "Bravo", 30, 200, 1000)
	seedSortItem(t, f.storage, "groupA", "itm-y", "Alpha", 10, 300, 3000)
	seedSortItem(t, f.storage, "groupA", "itm-z", "Charlie", 20, 100, 2000)

	for name, tc := range map[string]struct {
		sort ItemSort
		want []string
	}{
		"name ascending":        {ItemSort{Field: ItemSortName}, []string{"itm-y", "itm-x", "itm-z"}},
		"name descending":       {ItemSort{Field: ItemSortName, Descending: true}, []string{"itm-z", "itm-x", "itm-y"}},
		"quantity ascending":    {ItemSort{Field: ItemSortQuantity}, []string{"itm-y", "itm-z", "itm-x"}},
		"quantity descending":   {ItemSort{Field: ItemSortQuantity, Descending: true}, []string{"itm-x", "itm-z", "itm-y"}},
		"created_at ascending":  {ItemSort{Field: ItemSortCreatedAt}, []string{"itm-z", "itm-x", "itm-y"}},
		"created_at descending": {ItemSort{Field: ItemSortCreatedAt, Descending: true}, []string{"itm-y", "itm-x", "itm-z"}},
		"updated_at ascending":  {ItemSort{Field: ItemSortUpdatedAt}, []string{"itm-x", "itm-z", "itm-y"}},
		"updated_at descending": {ItemSort{Field: ItemSortUpdatedAt, Descending: true}, []string{"itm-y", "itm-z", "itm-x"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{Sort: tc.sort}, wholePage)
			if err != nil {
				t.Fatalf("ListFiltered: %v", err)
			}
			var gotIDs []string
			for _, it := range got {
				if it.ID == "anchorA" {
					continue
				}
				gotIDs = append(gotIDs, it.ID)
			}
			if strings.Join(gotIDs, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("order = %v, want %v", gotIDs, tc.want)
			}
		})
	}
}

func TestSortTieBreaksOnIDAscendingRegardlessOfInsertionOrder(t *testing.T) {
	f := newFilterFixture(t)
	seedSortItem(t, f.storage, "groupA", "itm-z", "Z", 5, 100, 100)
	seedSortItem(t, f.storage, "groupA", "itm-a", "A", 5, 100, 100)
	seedSortItem(t, f.storage, "groupA", "itm-m", "M", 5, 100, 100)

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{Sort: ItemSort{Field: ItemSortQuantity}}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	var gotIDs []string
	for _, it := range got {
		if it.ID == "anchorA" {
			continue
		}
		gotIDs = append(gotIDs, it.ID)
	}
	if want := []string{"itm-a", "itm-m", "itm-z"}; strings.Join(gotIDs, ",") != strings.Join(want, ",") {
		t.Fatalf("tie-break order = %v, want %v (id ascending)", gotIDs, want)
	}
}

func TestExplicitDefaultSortMatchesUnfilteredListOrder(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-2", "Second", "", "", 500)
	seedFilterItem(t, f.storage, "groupA", "itm-1", "First", "", "", 500)
	seedFilterItem(t, f.storage, "groupA", "itm-3", "Third", "", "", 500)

	unfiltered, err := f.scopeA.Items().List(t.Context(), wholePage)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sorted, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{
		Sort: ItemSort{Field: ItemSortUpdatedAt, Descending: true},
	}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if got, want := idListOrdered(sorted), idListOrdered(unfiltered); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the filtered path's default-equivalent sort disagreed with the unfiltered List order:\nListFiltered: %v\nList:         %v", got, want)
	}
}

func TestSortUnknownFieldFallsBackToTheDefaultOrder(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-1", "One", "", "", 100)
	seedFilterItem(t, f.storage, "groupA", "itm-2", "Two", "", "", 200)

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{
		Sort: ItemSort{Field: ItemSortField("not-a-real-column")},
	}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	want, err := f.scopeA.Items().List(t.Context(), wholePage)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotIDs, wantIDs := idListOrdered(got), idListOrdered(want); strings.Join(gotIDs, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("unknown sort field order = %v, want the default order %v", gotIDs, wantIDs)
	}
}

func TestSortComposesWithFilters(t *testing.T) {
	f := newFilterFixture(t)
	seedLabelRow(t, f.storage, "groupA", "lbl-x", "x")
	seedSortItem(t, f.storage, "groupA", "itm-in-b", "Bravo", 5, 100, 100)
	seedItemLabel(t, f.storage, "groupA", "itm-in-b", "lbl-x")
	seedSortItem(t, f.storage, "groupA", "itm-in-a", "Alpha", 5, 100, 100)
	seedItemLabel(t, f.storage, "groupA", "itm-in-a", "lbl-x")
	seedSortItem(t, f.storage, "groupA", "itm-out", "AAAA", 5, 100, 100)

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{
		LabelIDs: []string{"lbl-x"},
		Sort:     ItemSort{Field: ItemSortName},
	}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if want, gotIDs := []string{"itm-in-a", "itm-in-b"}, idListOrdered(got); strings.Join(gotIDs, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", gotIDs, want)
	}
}

func pageThroughAll(t *testing.T, list func(Page) ([]Item, error), pageSize int64) []Item {
	t.Helper()
	var all []Item
	offset := int64(0)
	for {
		page, err := list(Page{Limit: pageSize, Offset: offset})
		if err != nil {
			t.Fatalf("page at offset %d: %v", offset, err)
		}
		all = append(all, page...)
		if int64(len(page)) < pageSize {
			return all
		}
		offset += pageSize
	}
}

func assertNoDropsOrRepeats(t *testing.T, got []Item, want []string) {
	t.Helper()
	seen := make(map[string]int, len(got))
	for _, it := range got {
		seen[it.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("id %q was returned %d times across the page walk -- a page boundary repeated it", id, count)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("page walk returned %d rows, want %d (got ids: %v, want ids: %v)", len(got), len(want), idListOrdered(got), want)
	}
	for _, id := range want {
		if seen[id] == 0 {
			t.Errorf("id %q was never returned across the page walk -- a page boundary dropped it", id)
		}
	}
}

func TestPagingThroughDuplicateSortValuesReturnsEveryRowExactlyOnce(t *testing.T) {
	t.Run("default sort, unfiltered path", func(t *testing.T) {
		f := newFilterFixture(t)
		for i := 0; i < 7; i++ {
			id := fmt.Sprintf("itm-dup-%02d", i)
			seedFilterItem(t, f.storage, "groupA", id, id, "", "", 500)
		}

		list := func(p Page) ([]Item, error) {
			return f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{}, p)
		}
		oneShot, err := list(wholePage)
		if err != nil {
			t.Fatalf("one-shot list: %v", err)
		}
		assembled := pageThroughAll(t, list, 2)
		assertNoDropsOrRepeats(t, assembled, idsOf(oneShot))
	})

	t.Run("named sort plus a filter, filtered path", func(t *testing.T) {
		f := newFilterFixture(t)
		seedLabelRow(t, f.storage, "groupA", "lbl-x", "x")
		for i := 0; i < 7; i++ {
			id := fmt.Sprintf("itm-dup-%02d", i)
			seedSortItem(t, f.storage, "groupA", id, "SameName", 5, 100, int64(100+i))
			seedItemLabel(t, f.storage, "groupA", id, "lbl-x")
		}

		filter := ItemFilter{LabelIDs: []string{"lbl-x"}, Sort: ItemSort{Field: ItemSortName}}
		list := func(p Page) ([]Item, error) {
			return f.scopeA.Items().ListFiltered(t.Context(), filter, p)
		}
		oneShot, err := list(wholePage)
		if err != nil {
			t.Fatalf("one-shot list: %v", err)
		}
		assembled := pageThroughAll(t, list, 2)
		assertNoDropsOrRepeats(t, assembled, idsOf(oneShot))
	})
}

func TestSortExcludesAnotherGroupsItemsWhilePaging(t *testing.T) {
	f := newFilterFixture(t)
	for i := 0; i < 4; i++ {
		seedSortItem(t, f.storage, "groupA", fmt.Sprintf("itm-a-%02d", i), "Same", 5, 100, 100)
	}
	for i := 0; i < 4; i++ {
		seedSortItem(t, f.storage, "groupB", fmt.Sprintf("itm-b-%02d", i), "Same", 5, 100, 100)
	}

	list := func(p Page) ([]Item, error) {
		return f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{Sort: ItemSort{Field: ItemSortQuantity}}, p)
	}
	for _, it := range pageThroughAll(t, list, 2) {
		if it.GroupID != "groupA" {
			t.Fatalf("paging as group A surfaced group %q's item %q", it.GroupID, it.ID)
		}
	}
}
