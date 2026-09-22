package importexport

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"sort"
	"strings"
)

type CustomFieldColumnKind int

const (
	CustomFieldColumnUnknown CustomFieldColumnKind = iota
	CustomFieldColumnDef
	CustomFieldColumnAdHoc
)

type CustomFieldColumn struct {
	Kind      CustomFieldColumnKind
	Name      string
	FieldType string
	DefID     string
}

func ParseCustomFieldColumn(header string) (CustomFieldColumn, bool) {
	switch {
	case strings.HasPrefix(header, CustomFieldAdHocColumnPrefix):
		rest := header[len(CustomFieldAdHocColumnPrefix):]
		parts := SplitEscaped(rest, ':')
		if len(parts) != 2 {
			return CustomFieldColumn{}, false
		}
		return CustomFieldColumn{
			Kind:      CustomFieldColumnAdHoc,
			FieldType: UnescapeSubValue(parts[0]),
			Name:      UnescapeSubValue(parts[1]),
		}, true
	case strings.HasPrefix(header, CustomFieldColumnPrefix):
		rest := header[len(CustomFieldColumnPrefix):]
		parts := SplitEscaped(rest, '#')
		switch len(parts) {
		case 1:
			return CustomFieldColumn{Kind: CustomFieldColumnDef, Name: UnescapeSubValue(parts[0])}, true
		case 2:
			return CustomFieldColumn{Kind: CustomFieldColumnDef, Name: UnescapeSubValue(parts[0]), DefID: parts[1]}, true
		default:
			return CustomFieldColumn{}, false
		}
	default:
		return CustomFieldColumn{}, false
	}
}

func customFieldDefNameCounts(defs []storage.CustomFieldDef) map[string]int {
	counts := make(map[string]int, len(defs))
	for _, d := range defs {
		counts[d.Name]++
	}
	return counts
}

func customFieldDefHeader(def storage.CustomFieldDef, nameCounts map[string]int) string {
	name := escapeColumnName(def.Name)
	if nameCounts[def.Name] > 1 {
		return CustomFieldColumnPrefix + name + "#" + def.ID
	}
	return CustomFieldColumnPrefix + name
}

type adHocColumnKey struct {
	fieldType string
	name      string
}

func (k adHocColumnKey) header() string {
	return CustomFieldAdHocColumnPrefix + k.fieldType + ":" + escapeColumnName(k.name)
}

func liveCustomFieldDefIDs(defs []storage.CustomFieldDef) map[string]struct{} {
	ids := make(map[string]struct{}, len(defs))
	for _, d := range defs {
		ids[d.ID] = struct{}{}
	}
	return ids
}

func isAdHocCustomFieldValue(v storage.ItemCustomField, liveDefIDs map[string]struct{}) bool {
	if !v.FieldDefID.Valid {
		return true
	}
	_, live := liveDefIDs[v.FieldDefID.String]
	return !live
}

func collectAdHocColumns(liveDefIDs map[string]struct{}, rows []Row) []adHocColumnKey {
	seen := make(map[adHocColumnKey]struct{})
	var keys []adHocColumnKey
	for _, row := range rows {
		for _, v := range row.CustomFields {
			if !isAdHocCustomFieldValue(v, liveDefIDs) {
				continue
			}
			k := adHocColumnKey{fieldType: v.FieldType, name: v.Name}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].fieldType != keys[j].fieldType {
			return keys[i].fieldType < keys[j].fieldType
		}
		return keys[i].name < keys[j].name
	})
	return keys
}
