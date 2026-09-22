package storage

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ItemFilter struct {
	Query string

	LocationIDs []string

	LabelIDs []string

	CustomFields []CustomFieldMatch

	WarrantyStatus string

	CreatedFrom, CreatedTo *int64
	UpdatedFrom, UpdatedTo *int64

	Sort ItemSort
}

type ItemSortField string

const (
	ItemSortName      ItemSortField = "name"
	ItemSortQuantity  ItemSortField = "quantity"
	ItemSortCreatedAt ItemSortField = "created_at"
	ItemSortUpdatedAt ItemSortField = "updated_at"
)

var itemSortColumns = map[ItemSortField]string{
	ItemSortName:      "items.name",
	ItemSortQuantity:  "items.quantity",
	ItemSortCreatedAt: "items.created_at",
	ItemSortUpdatedAt: "items.updated_at",
}

type ItemSort struct {
	Field      ItemSortField
	Descending bool
}

type CustomFieldMatch struct {
	Name  string
	Value string
}

const (
	WarrantyStatusNone     = "none"
	WarrantyStatusAny      = "any"
	WarrantyStatusActive   = "active"
	WarrantyStatusExpired  = "expired"
	WarrantyStatusLifetime = "lifetime"
)

const isoDateLayout = "2006-01-02"

func (f ItemFilter) active() bool {
	return f.Query != "" || len(f.LocationIDs) > 0 || len(f.LabelIDs) > 0 ||
		len(f.CustomFields) > 0 || f.WarrantyStatus != "" ||
		f.CreatedFrom != nil || f.CreatedTo != nil || f.UpdatedFrom != nil || f.UpdatedTo != nil ||
		f.Sort.Field != ""
}

func ftsMatchExpression(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		quoted = append(quoted, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	quoted[len(quoted)-1] += "*"
	return strings.Join(quoted, " ")
}

const searchItemIDsSQL = `
SELECT i.id
FROM items_fts
JOIN items i ON i.rowid = items_fts.rowid
WHERE items_fts MATCH ?
  AND i.group_id = ?
  AND i.deleted_at IS NULL`

func (r itemRepository) searchItemIDs(ctx context.Context, expression string) ([]string, error) {
	rows, err := r.readPool().QueryContext(ctx, searchItemIDsSQL, expression, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: search items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: scan search result: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: search items: %w", err)
	}
	return ids, nil
}

const itemColumns = `items.id, items.group_id, items.name, items.description, items.location_id, ` +
	`items.quantity, items.short_code, items.created_at, items.updated_at, items.version, ` +
	`items.deleted_at, items.change_seq`

func idPlaceholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func buildFilteredItemsQuery(groupID string, matchedIDs []string, f ItemFilter, page Page, today string) (string, []any) {
	var sb strings.Builder
	args := make([]any, 0, 10+len(matchedIDs)+len(f.LocationIDs)+len(f.LabelIDs)+3*len(f.CustomFields))

	sb.WriteString("SELECT " + itemColumns + " FROM items\nWHERE items.group_id = ?\n  AND items.deleted_at IS NULL")
	args = append(args, groupID)

	if matchedIDs != nil {
		sb.WriteString("\n  AND items.id IN (" + idPlaceholders(len(matchedIDs)) + ")")
		for _, id := range matchedIDs {
			args = append(args, id)
		}
	}

	if len(f.LocationIDs) > 0 {
		sb.WriteString("\n  AND items.location_id IN (" + idPlaceholders(len(f.LocationIDs)) + ")")
		for _, id := range f.LocationIDs {
			args = append(args, id)
		}
	}

	if labels := dedupe(f.LabelIDs); len(labels) > 0 {
		sb.WriteString("\n  AND (SELECT COUNT(DISTINCT il.label_id) FROM item_labels il" +
			"\n        WHERE il.group_id = ?" +
			"\n          AND il.item_id = items.id" +
			"\n          AND il.deleted_at IS NULL" +
			"\n          AND il.label_id IN (" + idPlaceholders(len(labels)) + ")) = ?")
		args = append(args, groupID)
		for _, id := range labels {
			args = append(args, id)
		}
		args = append(args, len(labels))
	}

	for _, cf := range f.CustomFields {
		predicate, cfArgs := buildCustomFieldPredicate(groupID, cf.Name, cf.Value)
		sb.WriteString("\n  AND " + predicate)
		args = append(args, cfArgs...)
	}

	if predicate, wArgs := buildWarrantyStatusPredicate(groupID, f.WarrantyStatus, today); predicate != "" {
		sb.WriteString("\n  AND " + predicate)
		args = append(args, wArgs...)
	}

	if f.CreatedFrom != nil {
		sb.WriteString("\n  AND items.created_at >= ?")
		args = append(args, *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		sb.WriteString("\n  AND items.created_at <= ?")
		args = append(args, *f.CreatedTo)
	}
	if f.UpdatedFrom != nil {
		sb.WriteString("\n  AND items.updated_at >= ?")
		args = append(args, *f.UpdatedFrom)
	}
	if f.UpdatedTo != nil {
		sb.WriteString("\n  AND items.updated_at <= ?")
		args = append(args, *f.UpdatedTo)
	}

	column, direction := resolveItemSort(f.Sort)
	sb.WriteString("\nORDER BY " + column + " " + direction + ", items.id ASC\nLIMIT ? OFFSET ?")
	args = append(args, page.Limit, page.Offset)

	return sb.String(), args
}

func resolveItemSort(s ItemSort) (column, direction string) {
	column, ok := itemSortColumns[s.Field]
	if !ok {
		return "items.updated_at", "DESC"
	}
	if s.Descending {
		return column, "DESC"
	}
	return column, "ASC"
}

func buildCustomFieldPredicate(groupID, name, value string) (string, []any) {
	args := []any{groupID, name, value, value}
	var sb strings.Builder
	sb.WriteString("EXISTS (SELECT 1 FROM item_custom_fields cf" +
		"\n                WHERE cf.group_id = ?" +
		"\n                  AND cf.item_id = items.id" +
		"\n                  AND cf.deleted_at IS NULL" +
		"\n                  AND cf.name = ?" +
		"\n                  AND (cf.text_value = ? OR cf.date_value = ?")
	if n, err := strconv.ParseFloat(value, 64); err == nil {
		sb.WriteString(" OR cf.number_value = ?")
		args = append(args, n)
	}
	if b, err := strconv.ParseBool(value); err == nil {
		sb.WriteString(" OR cf.bool_value = ?")
		args = append(args, boolToSQLiteInt(b))
	}
	sb.WriteString("))")
	return sb.String(), args
}

func boolToSQLiteInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func buildWarrantyStatusPredicate(groupID, status, today string) (string, []any) {
	const (
		exists    = "EXISTS (SELECT 1 FROM item_warranty w\n                WHERE w.group_id = ?\n                  AND w.item_id = items.id\n                  AND w.deleted_at IS NULL"
		notExists = "NOT EXISTS (SELECT 1 FROM item_warranty w\n                WHERE w.group_id = ?\n                  AND w.item_id = items.id\n                  AND w.deleted_at IS NULL"
	)
	switch status {
	case WarrantyStatusNone:
		return notExists + ")", []any{groupID}
	case WarrantyStatusAny:
		return exists + ")", []any{groupID}
	case WarrantyStatusLifetime:
		return exists + "\n                  AND w.is_lifetime = 1)", []any{groupID}
	case WarrantyStatusActive:
		return exists +
				"\n                  AND (w.is_lifetime = 1" +
				"\n                       OR (w.expires_on IS NOT NULL AND w.expires_on >= ?" +
				"\n                           AND (w.starts_on IS NULL OR w.starts_on <= ?))))",
			[]any{groupID, today, today}
	case WarrantyStatusExpired:
		return exists + "\n                  AND w.is_lifetime = 0" +
				"\n                  AND w.expires_on IS NOT NULL AND w.expires_on < ?)",
			[]any{groupID, today}
	default:
		return "", nil
	}
}

func dedupe(ids []string) []string {
	if len(ids) < 2 {
		return ids
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (r itemRepository) ListFiltered(ctx context.Context, f ItemFilter, page Page) ([]Item, error) {
	if !f.active() {
		return r.List(ctx, page)
	}

	var matchedIDs []string
	if expression := ftsMatchExpression(f.Query); expression != "" {
		matched, err := r.searchItemIDs(ctx, expression)
		if err != nil {
			return nil, err
		}
		if len(matched) == 0 {
			return []Item{}, nil
		}
		matchedIDs = matched
	}

	today := time.Now().UTC().Format(isoDateLayout)
	query, args := buildFilteredItemsQuery(r.group(), matchedIDs, f, page, today)
	rows, err := r.readPool().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list filtered items for group %q: %w", r.group(), err)
	}
	defer func() { _ = rows.Close() }()

	items := []Item{}
	for rows.Next() {
		var i Item
		if err := rows.Scan(
			&i.ID, &i.GroupID, &i.Name, &i.Description, &i.LocationID, &i.Quantity,
			&i.ShortCode, &i.CreatedAt, &i.UpdatedAt, &i.Version, &i.DeletedAt, &i.ChangeSeq,
		); err != nil {
			return nil, fmt.Errorf("storage: scan filtered item: %w", err)
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: list filtered items for group %q: %w", r.group(), err)
	}
	return items, nil
}
