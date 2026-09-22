package integration

import (
	"net/http"
	"net/url"
	"testing"
)

func TestTenantIsolationItemListFilters(t *testing.T) {
	f := newCrossTenantFixture(t)

	const neverExistedID = neverExistedItemID

	for _, cred := range []credential{cookieCredential, bearerCredential} {
		t.Run(cred.kind, func(t *testing.T) {
			t.Run("search_q", func(t *testing.T) {
				query := url.Values{"q": {f.a.name}}.Encode()
				f.requireItemListExactly(t, cred, query)

				f.requireIdenticalItemListResponses(t, cred, query,
					url.Values{"q": {"zzz-search-term-nobody-created-zzz"}}.Encode())
			})

			t.Run("location_id", func(t *testing.T) {
				query := url.Values{"location_id": {f.a.locationID}}.Encode()
				f.requireItemListExactly(t, cred, query)
				f.requireIdenticalItemListResponses(t, cred, query,
					url.Values{"location_id": {neverExistedID}}.Encode())
			})

			t.Run("label_id", func(t *testing.T) {
				query := url.Values{"label_id": {f.a.labelID}}.Encode()
				f.requireItemListExactly(t, cred, query)
				f.requireIdenticalItemListResponses(t, cred, query,
					url.Values{"label_id": {neverExistedID}}.Encode())
			})

			t.Run("custom_field", func(t *testing.T) {
				query := url.Values{"custom_field": {"Colour:" + f.a.itemCustomFieldValue}}.Encode()
				f.requireItemListExactly(t, cred, query)
				f.requireIdenticalItemListResponses(t, cred, query,
					url.Values{"custom_field": {"Colour:zzz-value-nobody-set-zzz"}}.Encode())
			})

			t.Run("warranty_status_active_excludes_another_groups_item", func(t *testing.T) {
				f.requireItemListExactly(t, cred, url.Values{"warranty_status": {"active"}}.Encode())
			})

			t.Run("warranty_status_any_is_scoped_to_b_only", func(t *testing.T) {
				f.requireItemListExactly(t, cred, url.Values{"warranty_status": {"any"}}.Encode(), f.b.itemID)
			})

			t.Run("created_range", func(t *testing.T) {
				query := url.Values{"created_from": {"0"}, "created_to": {"9999999999999"}}.Encode()
				f.requireItemListExactly(t, cred, query, f.b.itemID)
			})

			t.Run("updated_range", func(t *testing.T) {
				query := url.Values{"updated_from": {"0"}, "updated_to": {"9999999999999"}}.Encode()
				f.requireItemListExactly(t, cred, query, f.b.itemID)
			})

			t.Run("sort", func(t *testing.T) {
				f.requireItemListExactly(t, cred, url.Values{"sort": {"-created_at"}}.Encode(), f.b.itemID)
			})

			t.Run("malformed_custom_field_is_silently_dropped", func(t *testing.T) {
				f.requireItemListExactly(t, cred, url.Values{"custom_field": {"no-colon-here"}}.Encode(), f.b.itemID)
			})

			t.Run("malformed_warranty_status_is_silently_ignored", func(t *testing.T) {
				f.requireItemListExactly(t, cred, url.Values{"warranty_status": {"bogus"}}.Encode(), f.b.itemID)
			})
		})
	}
}

func (f *crossTenantFixture) requireItemListExactly(t *testing.T, cred credential, rawQuery string, wantItemIDs ...string) itemListResponse {
	t.Helper()
	rec := f.request(t, cred, http.MethodGet, "/api/v1/items?"+rawQuery, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s (%s) GET /api/v1/items?%s: status = %d, want 200: %s",
			f.b.name, cred.kind, rawQuery, rec.Code, rec.Body.String())
	}
	var got itemListResponse
	mustDecode(t, rec, &got)

	for _, it := range got.Items {
		if it.ID == f.a.itemID {
			t.Fatalf("%s (%s) GET /api/v1/items?%s: response contains %s's item id %q -- cross-tenant leak in GET /items' filters",
				f.b.name, cred.kind, rawQuery, f.a.name, f.a.itemID)
		}
	}

	want := make(map[string]bool, len(wantItemIDs))
	for _, id := range wantItemIDs {
		want[id] = true
	}
	if len(got.Items) != len(want) {
		t.Fatalf("%s (%s) GET /api/v1/items?%s: got %d item(s) %+v, want exactly %v",
			f.b.name, cred.kind, rawQuery, len(got.Items), got.Items, wantItemIDs)
	}
	for _, it := range got.Items {
		if !want[it.ID] {
			t.Fatalf("%s (%s) GET /api/v1/items?%s: response contains unexpected item id %q, want exactly %v",
				f.b.name, cred.kind, rawQuery, it.ID, wantItemIDs)
		}
	}
	return got
}

func (f *crossTenantFixture) requireIdenticalItemListResponses(t *testing.T, cred credential, queryNamingForeignResource, queryNamingNothing string) {
	t.Helper()
	foreignRec := f.request(t, cred, http.MethodGet, "/api/v1/items?"+queryNamingForeignResource, nil)
	unknownRec := f.request(t, cred, http.MethodGet, "/api/v1/items?"+queryNamingNothing, nil)

	if foreignRec.Code != unknownRec.Code {
		t.Fatalf("existence oracle: %s (%s) GET /api/v1/items?%s -> %d, GET /api/v1/items?%s -> %d; a filter value naming another tenant's real resource must answer identically to one naming nothing at all",
			f.b.name, cred.kind, queryNamingForeignResource, foreignRec.Code, queryNamingNothing, unknownRec.Code)
	}
	if foreignRec.Body.String() != unknownRec.Body.String() {
		t.Fatalf("existence oracle: %s (%s) GET /api/v1/items?%s = %s, GET /api/v1/items?%s = %s; bodies must be byte-identical",
			f.b.name, cred.kind, queryNamingForeignResource, foreignRec.Body.String(), queryNamingNothing, unknownRec.Body.String())
	}
}
