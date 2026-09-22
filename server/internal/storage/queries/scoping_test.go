package queries

import (
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/migrations"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

var scopeColumnOverrides = map[string]string{
	"groups": "id",
}

var unscopedWriteAllowlist = map[string]string{
	"CountGroups": "T036's cheap pre-hash existence check asks the exact same cross-tenant " +
		"question CreateGroupIfNone's own NOT EXISTS clause asks -- 'does any group exist at all' " +
		"-- which by definition has no tenant to scope to, the same pre-tenant class as " +
		"ux_users_username and CreateGroupIfNone itself",
	"ListGroupIDs": "T088's periodic reclamation pass (FR-045) enumerates every tenant on " +
		"purpose, the identical 'no tenant to scope to' class CountGroups already occupies, here " +
		"because the query deliberately touches every group rather than none -- a process-level " +
		"maintenance sweep is not a request scoped to any one tenant, and storage.Storage.GroupIDs " +
		"(its only caller) is a THIRD top-level administrative escape hatch beside Close and " +
		"SchemaVersion, never reachable through storage.Storage.ForGroup's own per-tenant Scope",
	"CreateGroupIfNone": "the existence check inside it is deliberately cross-tenant -- there is " +
		"no tenant yet to scope to, the same pre-tenant class as ux_users_username -- and the row " +
		"it writes carries its id as an explicit, required, positional column",
	"CreateGroup": "FR-005's re-opened-registration path (storage.RegisterNewGroup): like " +
		"CreateGroupIfNone it targets the tenant root itself, which has no group_id to scope to, " +
		"and the row it writes carries its id as an explicit, required, positional column",
	"CreateUser": "group_id is an explicit, required, positional VALUES column; users.group_id is " +
		"NOT NULL with no default, so sqlc refuses to compile this statement without it",
	"GetUserForLogin": "login resolves the group FROM the username (the same pre-tenant class as " +
		"ux_users_username, per migration 0001's comment on that index) -- there is no tenant to " +
		"scope this lookup to until after it returns",
	"CreateSession": "group_id is an explicit, required, positional VALUES column; sessions.group_id " +
		"is NOT NULL with no default, so sqlc refuses to compile this statement without it -- the " +
		"same shape as CreateUser",
	"GetSessionForAuth": "the bearer token IS what resolves the session's group (ux_sessions_token_hash's " +
		"schema comment), the same pre-tenant class as GetUserForLogin -- there is no tenant to scope " +
		"this lookup to until after it returns",
	"CreateDeviceToken": "group_id is an explicit, required, positional VALUES column; " +
		"device_tokens.group_id is NOT NULL with no default, so sqlc refuses to compile this " +
		"statement without it -- the same shape as CreateSession",
	"GetDeviceTokenForAuth": "the bearer token IS what resolves the device token's group " +
		"(ux_device_tokens_token_hash's schema comment), the same pre-tenant class as " +
		"GetSessionForAuth -- there is no tenant to scope this lookup to until after it returns",
	"CreateInvite": "group_id is an explicit, required, positional VALUES column; " +
		"invites.group_id is NOT NULL with no default, so sqlc refuses to compile this statement " +
		"without it -- the same shape as CreateSession and CreateDeviceToken",
	"GetInviteForRedeem": "T040's unauthenticated redemption lookup resolves the group FROM the " +
		"token (ux_invites_token_hash's A21 exception, the same pre-tenant class GetUserForLogin " +
		"and GetSessionForAuth already are) -- there is no tenant to scope this lookup to until " +
		"after it returns",
	"CreateItemLabel": "G3: group_id is an explicit, required, positional VALUES column; " +
		"item_labels.group_id is NOT NULL REFERENCES groups (id) with no default, so sqlc refuses " +
		"to compile this statement without it -- the same shape as CreateItem and " +
		"CreateItemIdentification. There is no existing row being read here to leak (a fresh " +
		"INSERT, not a filtered SELECT/UPDATE), and BOTH composite foreign keys -- " +
		"(group_id, item_id) REFERENCES items and (group_id, label_id) REFERENCES labels -- mean " +
		"an edge attached to another tenant's item or label is not representable at all",
	"CreateItem": "T051: group_id is an explicit, required, positional VALUES column; " +
		"items.group_id is NOT NULL with no default, so sqlc refuses to compile this statement " +
		"without it -- the same shape as CreateInvite, CreateSession, CreateDeviceToken and " +
		"CreateUser, all already allowlisted here for the identical reason. There is also no " +
		"existing row being read by this statement to leak in the first place (a fresh INSERT, not " +
		"a filtered SELECT/UPDATE); a WHERE clause added purely to satisfy this lint would either " +
		"be a no-op tautology or a second, weaker (silent zero-rows instead of a loud constraint " +
		"failure) copy of the FOREIGN KEY (group_id) REFERENCES groups (id) the schema already " +
		"enforces on this same column.",
	"CreateItemWarranty": "T052: group_id is an explicit, required, positional VALUES column; " +
		"item_warranty.group_id is NOT NULL with no default, so sqlc refuses to compile this " +
		"statement without it -- the identical shape as CreateItem, CreateInvite, CreateSession, " +
		"CreateDeviceToken and CreateUser, all already allowlisted here for the same reason. Like " +
		"CreateItem it is a fresh INSERT with no existing row being read to leak, and it carries " +
		"a SECOND schema-level guard the others do not: FOREIGN KEY (group_id, item_id) " +
		"REFERENCES items (group_id, id) means the parent item must be in the SAME group, so a " +
		"warranty attached across tenants is not a row this schema can hold at all.",
	"CreateItemSale": "T053: group_id is an explicit, required, positional VALUES column; " +
		"item_sale.group_id is NOT NULL with no default, so sqlc refuses to compile this " +
		"statement without it -- the identical shape as CreateItemWarranty, CreateItem, " +
		"CreateInvite, CreateSession, CreateDeviceToken and CreateUser, all already allowlisted " +
		"here for the same reason. Like CreateItemWarranty it is a fresh INSERT with no existing " +
		"row being read to leak, and it carries the identical SECOND schema-level guard: " +
		"FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id) means the parent item " +
		"must be in the SAME group, so a sale block attached across tenants is not a row this " +
		"schema can hold at all.",
	"CreateItemPurchase": "T054: group_id is an explicit, required, positional VALUES column; " +
		"item_purchase.group_id is NOT NULL with no default, so sqlc refuses to compile this " +
		"statement without it -- the identical shape as CreateItemSale, CreateItemWarranty, " +
		"CreateItem, CreateInvite, CreateSession, CreateDeviceToken and CreateUser, all already " +
		"allowlisted here for the same reason. Like CreateItemSale it is a fresh INSERT with no " +
		"existing row being read to leak, and it carries the identical SECOND schema-level guard: " +
		"FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id) means the parent item " +
		"must be in the SAME group, so a purchase block attached across tenants is not a row this " +
		"schema can hold at all.",
	"CreateItemIdentification": "T056: group_id is an explicit, required, positional VALUES " +
		"column; item_identifications.group_id is NOT NULL with no default, so sqlc refuses to " +
		"compile this statement without it -- the identical shape as CreateItemPurchase, " +
		"CreateItemSale, CreateItemWarranty, CreateItem, CreateInvite, CreateSession, " +
		"CreateDeviceToken and CreateUser, all already allowlisted here for the same reason. " +
		"Unlike those three detail blocks this table is 0..N (no partial UNIQUE index at all, so " +
		"there is no existing-row pre-read of its OWN table to leak either), but it carries the " +
		"identical SECOND schema-level guard: FOREIGN KEY (group_id, item_id) REFERENCES items " +
		"(group_id, id) means the parent item must be in the SAME group, so a row attached across " +
		"tenants is not representable at all.",
	"CreateCustomFieldDef": "T057: group_id is an explicit, required, positional VALUES column; " +
		"custom_field_defs.group_id is NOT NULL with no default, so sqlc refuses to compile this " +
		"statement without it -- the identical shape as CreateItemIdentification, CreateItemPurchase, " +
		"CreateItemSale, CreateItemWarranty, CreateItem, CreateInvite, CreateSession, " +
		"CreateDeviceToken and CreateUser, all already allowlisted here for the same reason. Like " +
		"CreateItemIdentification it is 0..N with no partial UNIQUE index to collide on (A101.2: " +
		"duplicate names are permitted), so there is no existing-row pre-read of its OWN table to " +
		"leak. Unlike every Create* above it also carries NO second schema-level guard: this row's " +
		"only parent is the group itself, already the whole of what this write is scoped to, so " +
		"there is no composite FOREIGN KEY to a parent item the way CreateItemWarranty/" +
		"CreateItemSale/CreateItemPurchase/CreateItemIdentification each have -- group_id being an " +
		"explicit, required column IS the entire tenant guarantee this statement carries, and it " +
		"is the one this lint would otherwise (wrongly) flag as unscoped.",
	"CreateItemCustomField": "T058: group_id is an explicit, required, positional VALUES column; " +
		"item_custom_fields.group_id is NOT NULL with no default, so sqlc refuses to compile this " +
		"statement without it -- the identical shape as CreateCustomFieldDef, CreateItemIdentification, " +
		"CreateItemPurchase, CreateItemSale, CreateItemWarranty, CreateItem, CreateInvite, " +
		"CreateSession, CreateDeviceToken and CreateUser, all already allowlisted here for the same " +
		"reason. Like CreateItemIdentification it is 0..N with no partial UNIQUE index to collide " +
		"on, so there is no existing-row pre-read of its OWN table to leak. Unlike CreateCustomFieldDef " +
		"-- which lacks any second schema-level guard at all -- this statement carries TWO: the " +
		"identical FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id) " +
		"CreateItemIdentification/CreateItemWarranty/CreateItemSale/CreateItemPurchase each have, " +
		"PLUS a second, independent one CreateCustomFieldDef has no occasion to need: FOREIGN KEY " +
		"(group_id, field_def_id) REFERENCES custom_field_defs (group_id, id), nullable so it " +
		"applies only when a row is standardized against a definition, but when it is, a " +
		"definition id from another tenant is not representable at all either -- group_id being an " +
		"explicit, required column is still the entire tenant guarantee this statement carries " +
		"(the second FK only catches a WRONG-TENANT definition, never a same-tenant one whose " +
		"field_type disagrees, which is package items' own A103.2 read-before-write job, not this " +
		"lint's), and it is the one this lint would otherwise (wrongly) flag as unscoped.",
	"CreateStockAdjustment": "T059: group_id is an explicit, required, positional VALUES column; " +
		"stock_adjustments.group_id is NOT NULL with no default, so sqlc refuses to compile this " +
		"statement without it -- the identical shape as CreateItemCustomField, CreateCustomFieldDef, " +
		"CreateItemIdentification, CreateItemPurchase, CreateItemSale, CreateItemWarranty, " +
		"CreateItem, CreateInvite, CreateSession, CreateDeviceToken and CreateUser, all already " +
		"allowlisted here for the same reason. Like CreateItemIdentification it is 0..N (append-only, " +
		"per data-model.md's own words on this table) with no partial UNIQUE index to collide on, so " +
		"there is no existing-row pre-read of its OWN table to leak, and it carries the identical " +
		"SECOND schema-level guard: FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id) " +
		"means the parent item must be in the SAME group, so a row attached across tenants is not " +
		"representable at all.",
	"CreateLabel": "T067: group_id is an explicit, required, positional VALUES column; " +
		"labels.group_id is NOT NULL with no default, so sqlc refuses to compile this statement " +
		"without it -- the identical shape as CreateStockAdjustment, CreateItemCustomField, " +
		"CreateCustomFieldDef, CreateItemIdentification, CreateItemPurchase, CreateItemSale, " +
		"CreateItemWarranty, CreateItem, CreateInvite, CreateSession, CreateDeviceToken and " +
		"CreateUser, all already allowlisted here for the same reason. Like CreateCustomFieldDef " +
		"this table's own parent is the group itself (no composite FOREIGN KEY to a parent item), " +
		"so group_id being an explicit, required column IS the entire tenant guarantee this " +
		"statement carries. Unlike CreateCustomFieldDef (A101.2 permits duplicate names) this " +
		"table's uniqueness rule (A109: no two LIVE labels in one group may share a name) IS " +
		"enforced -- but by a SEPARATE, scoped SELECT (GetLabelByName, storage/labels.go's own " +
		"Create) that runs ahead of this INSERT inside the same transaction, not by this " +
		"statement or by a schema-level UNIQUE index; that pre-read query is ordinarily scoped " +
		"and is not itself in this allowlist.",
	"CreateLocation": "T065: group_id is an explicit, required, positional VALUES column; " +
		"locations.group_id is NOT NULL with no default, so sqlc refuses to compile this " +
		"statement without it -- the identical shape as CreateStockAdjustment, " +
		"CreateItemCustomField, CreateCustomFieldDef, CreateItemIdentification, CreateItemPurchase, " +
		"CreateItemSale, CreateItemWarranty, CreateItem, CreateInvite, CreateSession, " +
		"CreateDeviceToken and CreateUser, all already allowlisted here for the same reason. Like " +
		"CreateCustomFieldDef this row's only NON-SELF parent is the group itself, and there is no " +
		"partial UNIQUE index on this table to collide on, so there is no existing-row pre-read of " +
		"its OWN table to leak here either. Unlike CreateCustomFieldDef it DOES carry a schema-level " +
		"guard on its optional parent_id -- the self-referential composite FOREIGN KEY " +
		"(group_id, parent_id) REFERENCES locations (group_id, id) -- but that guard sits BEHIND " +
		"storage.locationRepository.Create's own pre-read (a scoped GetLocation on a non-empty " +
		"parent_id, inside the same transaction, answering storage.ErrLocationParentNotFound before " +
		"this INSERT is ever reached), so a cross-group or dangling parent_id is already rejected " +
		"one layer up, and this INSERT's own group_id VALUES column is what makes the row that " +
		"DOES land unconditionally the caller's own tenant.",
	"CreateAttachment": "T083: group_id is an explicit, required, positional VALUES column; " +
		"attachments.group_id is NOT NULL REFERENCES groups (id) with no default, so sqlc refuses " +
		"to compile this statement without it -- the identical shape as CreateItemCustomField, " +
		"CreateStockAdjustment, CreateCustomFieldDef, CreateItemIdentification, CreateItemPurchase, " +
		"CreateItemSale, CreateItemWarranty, CreateItem, CreateInvite, CreateSession, " +
		"CreateDeviceToken and CreateUser, all already allowlisted here for the same reason. Like " +
		"CreateItemCustomField it is 0..N (an item may carry any number of attachments) with no " +
		"partial UNIQUE index on (group_id, item_id, ...) to collide on, so there is no " +
		"existing-row pre-read of its OWN table to leak, and it carries the identical SECOND " +
		"schema-level guard: FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id) means " +
		"the parent item must be in the SAME group, so an attachment recorded across tenants is " +
		"not representable at all.",
	"CreateImportSession": "T148c: group_id is an explicit, required, positional VALUES column; " +
		"import_sessions.group_id is NOT NULL REFERENCES groups (id) with no default, so sqlc " +
		"refuses to compile this statement without it -- the identical shape as CreateAttachment, " +
		"CreateLabel and every other Create* already allowlisted here for the same reason. Like " +
		"CreateLabel/CreateCustomFieldDef this row's only parent is the group itself (no composite " +
		"FOREIGN KEY to a parent item), so group_id being an explicit, required column IS the " +
		"entire tenant guarantee this statement carries, and it is the one this lint would " +
		"otherwise (wrongly) flag as unscoped. There is also no existing row being read here to " +
		"leak (a fresh INSERT, not a filtered SELECT/UPDATE, and this table carries no partial " +
		"UNIQUE index of its own to collide on).",
	"CreateMutation": "T164b: group_id is an explicit, required, positional VALUES column; " +
		"mutations.group_id is NOT NULL REFERENCES groups (id) with no default, so sqlc refuses " +
		"to compile this statement without it -- the identical shape as CreateImportSession, " +
		"CreateAttachment, CreateLabel and every other Create* already allowlisted here for the " +
		"same reason. Like CreateImportSession this row's only parent is the group itself (no " +
		"composite FOREIGN KEY to a parent item), so group_id being an explicit, required column " +
		"IS the entire tenant guarantee this statement carries, and it is the one this lint would " +
		"otherwise (wrongly) flag as unscoped. Unlike CreateImportSession this table DOES carry a " +
		"UNIQUE index of its own to collide on -- ux_mutations_group_mutation_id, FULL not partial " +
		"(FR-091: idempotency must never lapse) -- but, like CreateItemWarranty's own " +
		"ErrWarrantyExists branch, that collision is detected via isUniqueConstraintViolation on " +
		"this very statement's own error, not by a pre-check SELECT ahead of it, so there is " +
		"still no existing row being read here to leak (a fresh INSERT, not a filtered " +
		"SELECT/UPDATE).",
	"CreateConflict": "T165b: group_id is an explicit, required, positional VALUES column; " +
		"conflicts.group_id is NOT NULL REFERENCES groups (id) with no default, so sqlc refuses " +
		"to compile this statement without it -- the identical shape as CreateMutation, " +
		"CreateImportSession, CreateAttachment, CreateLabel and every other Create* already " +
		"allowlisted here for the same reason. Like CreateImportSession this row's only parent is " +
		"the group itself (no composite FOREIGN KEY to a parent item -- entity_type/entity_id are " +
		"polymorphic, unconstrained TEXT, the identical shape mutations.entity_type/entity_id " +
		"already have), so group_id being an explicit, required column IS the entire tenant " +
		"guarantee this statement carries, and it is the one this lint would otherwise (wrongly) " +
		"flag as unscoped. Unlike CreateMutation this table carries NO UNIQUE index of its own to " +
		"collide on beyond its own primary key, so there is not even a same-table collision to " +
		"detect here -- a fresh INSERT with no existing row being read to leak, full stop.",
	"UpsertFieldVersion": "T165b: group_id is an explicit, required, positional VALUES column; " +
		"field_versions.group_id is NOT NULL REFERENCES groups (id) with no default (migration " +
		"0005), so sqlc refuses to compile this statement without it -- the identical shape as " +
		"CreateConflict, CreateMutation and every other Create* already allowlisted here for the " +
		"same reason. Unlike an ordinary Create* this statement is an UPSERT (INSERT ... ON " +
		"CONFLICT (group_id, entity_type, entity_id, field_name) DO UPDATE), so it CAN touch an " +
		"existing row -- but the row it can touch is pinned to the exact composite primary key " +
		"tuple its own VALUES clause supplies, INCLUDING group_id, so the DO UPDATE branch can " +
		"only ever land on that one tuple, never a different tenant's row with the same " +
		"entity_type/entity_id/field_name. group_id being an explicit, required VALUES column is " +
		"still the entire tenant guarantee this statement carries, and it is the one this lint " +
		"would otherwise (wrongly) flag as unscoped, since neither the VALUES list nor the ON " +
		"CONFLICT column list nor the DO UPDATE SET clause contains the literal `group_id = ...` " +
		"predicate shape this lint's regex looks for.",
}

var queryNamePattern = regexp.MustCompile(`^--\s*name:\s*(\w+)\s+:(\w+)`)

var tableRefPattern = regexp.MustCompile(`(?is)\b(?:from|join|into|update)\s+"?([a-z_][a-z0-9_]*)"?(?:\s+(?:as\s+)?([a-z_][a-z0-9_]*))?`)

var aliasStopWords = map[string]bool{
	"where": true, "set": true, "on": true, "values": true, "group": true, "order": true,
	"and": true, "or": true, "limit": true, "offset": true, "select": true, "left": true,
	"right": true, "inner": true, "outer": true, "cross": true, "using": true, "returning": true,
	"natural": true, "join": true,
}

type tableRef struct {
	table string
	alias string
}

func findTableRefs(region string) []tableRef {
	var out []tableRef
	for _, m := range tableRefPattern.FindAllStringSubmatch(region, -1) {
		table := strings.ToLower(m[1])
		alias := strings.ToLower(m[2])
		if aliasStopWords[alias] {
			alias = ""
		}
		out = append(out, tableRef{table: table, alias: alias})
	}
	return out
}

func splitRegions(text string) []string {
	var regions []string
	var top strings.Builder
	i := 0
	for i < len(text) {
		if text[i] != '(' {
			top.WriteByte(text[i])
			i++
			continue
		}
		close := matchingParen(text, i)
		if close < 0 {
			top.WriteString(text[i:])
			break
		}
		inner := text[i+1 : close]
		if isSelectSubquery(inner) {
			regions = append(regions, splitRegions(inner)...)
		} else {
			top.WriteString(text[i : close+1])
			regions = append(regions, splitRegions(inner)...)
		}
		i = close + 1
	}
	regions = append(regions, top.String())
	return regions
}

func isSelectSubquery(inner string) bool {
	trimmed := strings.TrimSpace(inner)
	return len(trimmed) >= 6 && strings.EqualFold(trimmed[:6], "select")
}

func matchingParen(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

type tenantTableResult struct {
	table  string
	scoped bool
}

func checkTenantTables(query string, scopeColumns map[string]string) []tenantTableResult {
	var out []tenantTableResult
	for _, region := range splitRegions(query) {
		qualifiers := map[string][]string{}
		var order []string
		for _, ref := range findTableRefs(region) {
			if _, isTenant := scopeColumns[ref.table]; !isTenant {
				continue
			}
			if _, seen := qualifiers[ref.table]; !seen {
				order = append(order, ref.table)
			}
			qualifiers[ref.table] = append(qualifiers[ref.table], ref.table)
			if ref.alias != "" && ref.alias != ref.table {
				qualifiers[ref.table] = append(qualifiers[ref.table], ref.alias)
			}
		}
		multi := len(order) > 1
		for _, table := range order {
			scope := scopeColumns[table]
			out = append(out, tenantTableResult{
				table:  table,
				scoped: regionScopesTable(region, scope, qualifiers[table], multi),
			})
		}
	}
	return out
}

func regionScopesTable(region, scope string, qualifiers []string, requireQualified bool) bool {
	const boundForms = `\s*=\s*(\?|sqlc\.arg\(|sqlc\.narg\(|@|\$)`
	for _, q := range qualifiers {
		pat := regexp.MustCompile(`\b` + regexp.QuoteMeta(q) + `\.` + regexp.QuoteMeta(scope) + boundForms)
		if pat.MatchString(region) {
			return true
		}
	}
	if requireQualified {
		return false
	}
	bare := regexp.MustCompile(`\b` + regexp.QuoteMeta(scope) + boundForms)
	return bare.MatchString(region)
}

func unscopedTenantTables(query string, scopeColumns map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range checkTenantTables(query, scopeColumns) {
		if r.scoped || seen[r.table] {
			continue
		}
		seen[r.table] = true
		out = append(out, r.table)
	}
	return out
}

type namedQuery struct {
	file string
	line int
	name string
	kind string
	text string
}

func TestEveryQueryIsGroupScoped(t *testing.T) {
	scopeColumns := tenantScopeColumns(t)
	if len(scopeColumns) == 0 {
		t.Fatal("derived zero tenant tables from the schema; the introspection is broken and this test asserts nothing")
	}

	found := loadQueries(t)
	if len(found) == 0 {
		t.Fatal("found zero named queries; the parser is broken or the directory is empty, and either way this test asserts nothing")
	}

	checked := 0
	for _, q := range found {
		if reason, allowed := unscopedWriteAllowlist[q.name]; allowed {
			_ = reason
			continue
		}
		results := checkTenantTables(q.text, scopeColumns)
		checked += len(results)

		reported := map[string]bool{}
		for _, r := range results {
			if r.scoped || reported[r.table] {
				continue
			}
			reported[r.table] = true
			t.Errorf("%s:%d: query %s (:%s) reads %s without its OWN parameter-bound %s predicate "+
				"(qualified by %s's own alias or table name, since this statement touches more than "+
				"one tenant table and an unqualified predicate cannot tell them apart); P-3 makes "+
				"tenant scope structural, so an unscoped or borrowed-scope read against a tenant "+
				"table is a cross-tenant read waiting for a guessed id\n%s",
				q.file, q.line, q.name, q.kind, r.table, scopeColumns[r.table], r.table, indent(q.text))
		}
	}
	if checked == 0 {
		t.Fatal("no query referenced a tenant table; either the table-reference regex stopped matching or the sample queries were removed, and this test is now vacuous")
	}

	seen := make(map[string]bool, len(found))
	for _, q := range found {
		seen[q.name] = true
	}
	for name := range unscopedWriteAllowlist {
		if !seen[name] {
			t.Errorf("unscopedWriteAllowlist names %q, which no .sql file defines; the entry is stale and should be removed", name)
		}
	}
}

func TestPerTableScopeIsNotSharedAcrossJoins(t *testing.T) {
	scopeColumns := tenantScopeColumns(t)

	cases := []struct {
		name         string
		sql          string
		wantUnscoped []string
	}{
		{
			name: "join: only the driving table has its own predicate",
			sql: `SELECT i.id FROM items i
JOIN item_labels il ON il.item_id = i.id
WHERE i.group_id = ?1;`,
			wantUnscoped: []string{"item_labels"},
		},
		{
			name: "IN (subquery): the subquery's table has no predicate of its own",
			sql: `SELECT id FROM items WHERE group_id = ?1
	AND id IN (SELECT item_id FROM item_labels);`,
			wantUnscoped: []string{"item_labels"},
		},
		{
			name: "join: both tables scoped by their own qualified predicate passes",
			sql: `SELECT i.id FROM items i
JOIN item_labels il ON il.item_id = i.id
WHERE i.group_id = ?1 AND il.group_id = ?1;`,
			wantUnscoped: nil,
		},
		{
			name: "IN (subquery): the subquery scoping itself passes",
			sql: `SELECT id FROM items WHERE group_id = ?1
	AND id IN (SELECT item_id FROM item_labels WHERE group_id = ?1);`,
			wantUnscoped: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unscopedTenantTables(tc.sql, scopeColumns)
			sort.Strings(got)
			want := append([]string(nil), tc.wantUnscoped...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("unscopedTenantTables() = %v, want %v\nquery:\n%s", got, want, tc.sql)
			}
		})
	}
}

func TestQueryFilesAreASCII(t *testing.T) {
	files := queryFiles(t)
	if len(files) == 0 {
		t.Fatal("found zero .sql files; this test asserts nothing")
	}

	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for offset, r := range string(body) {
			if r >= utf8.RuneSelf {
				line := 1 + strings.Count(string(body[:offset]), "\n")
				t.Errorf("%s:%d: non-ASCII character %q; sqlc's statement rewriter miscounts past it and can emit corrupted SQL. Use ASCII punctuation -- '--' for an em dash, '\"' for typographic quotes",
					file, line, r)
			}
		}
	}
}

func TestGeneratedCodeCoversEveryQuery(t *testing.T) {
	found := loadQueries(t)
	if len(found) == 0 {
		t.Fatal("found zero named queries; this test asserts nothing")
	}

	for _, q := range found {
		out := filepath.Join("..", "internal", "gen", strings.TrimSuffix(filepath.Base(q.file), ".sql")+".sql.go")
		body, err := os.ReadFile(out)
		if err != nil {
			t.Errorf("%s: query %s has no generated counterpart at %s: %v; run `go tool sqlc generate` from server/", q.file, q.name, out, err)
			continue
		}
		if !strings.Contains(string(body), "func (q *Queries) "+q.name+"(") {
			t.Errorf("%s:%d: query %s is not in %s; the committed generated code is behind its source -- run `go tool sqlc generate` from server/", q.file, q.line, q.name, out)
		}
	}
}

func tenantScopeColumns(t *testing.T) map[string]string {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "schema.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
	if _, err := migrations.Apply(t.Context(), db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	rows, err := db.Query(`
		SELECT t.name, c.name
		FROM pragma_table_list t
		JOIN pragma_table_info(t.name) c
		WHERE t.schema = 'main'
		  AND t.type IN ('table', 'virtual')
		  AND t.name NOT LIKE 'sqlite_%'
		  AND t.name <> 'goose_db_version'
		  AND c.name = 'group_id'`)
	if err != nil {
		t.Fatalf("list tenant tables: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close tenant-table rows: %v", err)
		}
	}()

	scopes := map[string]string{}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatalf("scan tenant table: %v", err)
		}
		scopes[table] = column
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tenant tables: %v", err)
	}

	for table, column := range scopeColumnOverrides {
		var exists int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_list WHERE schema = 'main' AND name = ?`, table).Scan(&exists); err != nil {
			t.Fatalf("check override table %s: %v", table, err)
		}
		if exists == 0 {
			t.Fatalf("scopeColumnOverrides names %q, which the schema does not contain; the override is stale", table)
		}
		scopes[table] = column
	}
	return scopes
}

func queryFiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob("*.sql")
	if err != nil {
		t.Fatalf("glob query files: %v", err)
	}
	return matches
}

func loadQueries(t *testing.T) []namedQuery {
	t.Helper()

	var out []namedQuery
	for _, file := range queryFiles(t) {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		var current *namedQuery
		for i, line := range strings.Split(string(body), "\n") {
			if m := queryNamePattern.FindStringSubmatch(line); m != nil {
				if current != nil {
					out = append(out, *current)
				}
				current = &namedQuery{file: file, line: i + 1, name: m[1], kind: m[2]}
				continue
			}
			if current == nil || strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			current.text += line + "\n"
		}
		if current != nil {
			out = append(out, *current)
		}
	}
	return out
}

func indent(s string) string {
	return "\t" + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n\t")
}
