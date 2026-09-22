package importexport

import (
	"encoding/csv"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
	"sort"
	"strconv"
	"strings"
)

type Row struct {
	Item            storage.Item
	LocationPath    string
	Labels          []storage.Label
	Identifications []storage.Identification
	Attachments     []storage.Attachment
	Warranty        *storage.Warranty
	Purchase        *storage.Purchase
	Sale            *storage.Sale
	CustomFields    []storage.ItemCustomField
}

func nullString(valid bool, value string) string {
	if !valid {
		return ""
	}
	return value
}

func EncodeCSV(w io.Writer, defs []storage.CustomFieldDef, rows []Row) error {
	sortedDefs := sortCustomFieldDefs(defs)
	nameCounts := customFieldDefNameCounts(sortedDefs)
	liveDefIDs := liveCustomFieldDefIDs(sortedDefs)
	adHocCols := collectAdHocColumns(liveDefIDs, rows)

	cw := csv.NewWriter(w)

	cfStart := len(FixedColumns)
	cfxStart := cfStart + len(sortedDefs)
	header := make([]string, 0, cfxStart+len(adHocCols))
	header = append(header, FixedColumns...)
	for _, def := range sortedDefs {
		header = append(header, customFieldDefHeader(def, nameCounts))
	}
	for _, k := range adHocCols {
		header = append(header, k.header())
	}
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("importexport: write CSV header: %w", err)
	}

	record := make([]string, len(header))
	for i, row := range rows {
		fillFixedColumns(record[:cfStart], row)
		fillCustomFieldColumns(record[cfStart:cfxStart], sortedDefs, row.CustomFields)
		fillAdHocCustomFieldColumns(record[cfxStart:], adHocCols, row.CustomFields, liveDefIDs)
		if err := cw.Write(record); err != nil {
			return fmt.Errorf("importexport: write CSV row %d (item %q): %w", i, row.Item.ID, err)
		}
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("importexport: flush CSV: %w", err)
	}
	return nil
}

func sortCustomFieldDefs(defs []storage.CustomFieldDef) []storage.CustomFieldDef {
	sorted := make([]storage.CustomFieldDef, len(defs))
	copy(sorted, defs)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
}

func fillFixedColumns(dst []string, row Row) {
	item := row.Item
	dst[0] = item.ID
	dst[1] = item.Name
	dst[2] = item.Description
	dst[3] = strconv.FormatInt(item.Quantity, 10)
	dst[4] = row.LocationPath
	dst[5] = nullString(item.LocationID.Valid, item.LocationID.String)
	dst[6] = item.ShortCode
	dst[7] = encodeLabels(row.Labels)
	dst[8] = encodeIdentifications(row.Identifications)
	dst[9] = encodeAttachments(row.Attachments)

	if w := row.Warranty; w != nil {
		dst[10] = w.Holder
		dst[11] = w.Provider
		dst[12] = nullString(w.StartsOn.Valid, w.StartsOn.String)
		dst[13] = nullString(w.ExpiresOn.Valid, w.ExpiresOn.String)
		dst[14] = formatBool(w.IsLifetime != 0)
		dst[15] = w.Notes
	} else {
		dst[10], dst[11], dst[12], dst[13], dst[14], dst[15] = "", "", "", "", "", ""
	}

	if p := row.Purchase; p != nil {
		dst[16] = p.Vendor
		dst[17] = nullString(p.PurchasedOn.Valid, p.PurchasedOn.String)
		dst[18] = strconv.FormatInt(p.PurchasePriceMinor, 10)
		dst[19] = p.OrderReference
		dst[20] = p.Notes
	} else {
		dst[16], dst[17], dst[18], dst[19], dst[20] = "", "", "", "", ""
	}

	if s := row.Sale; s != nil {
		dst[21] = s.BuyerName
		dst[22] = nullString(s.SoldOn.Valid, s.SoldOn.String)
		dst[23] = strconv.FormatInt(s.SalePriceMinor, 10)
		dst[24] = s.Notes
	} else {
		dst[21], dst[22], dst[23], dst[24] = "", "", "", ""
	}
}

func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func fillCustomFieldColumns(dst []string, sortedDefs []storage.CustomFieldDef, values []storage.ItemCustomField) {
	byDefID := make(map[string]storage.ItemCustomField, len(values))
	for _, v := range values {
		if v.FieldDefID.Valid {
			byDefID[v.FieldDefID.String] = v
		}
	}
	for i, def := range sortedDefs {
		v, ok := byDefID[def.ID]
		if !ok {
			dst[i] = ""
			continue
		}
		dst[i] = customFieldCellValue(v)
	}
}

func fillAdHocCustomFieldColumns(dst []string, adHocCols []adHocColumnKey, values []storage.ItemCustomField, liveDefIDs map[string]struct{}) {
	byKey := make(map[adHocColumnKey]storage.ItemCustomField, len(values))
	for _, v := range values {
		if !isAdHocCustomFieldValue(v, liveDefIDs) {
			continue
		}
		k := adHocColumnKey{fieldType: v.FieldType, name: v.Name}
		if _, exists := byKey[k]; exists {
			continue
		}
		byKey[k] = v
	}
	for i, k := range adHocCols {
		v, ok := byKey[k]
		if !ok {
			dst[i] = ""
			continue
		}
		dst[i] = customFieldCellValue(v)
	}
}

func customFieldCellValue(v storage.ItemCustomField) string {
	switch v.FieldType {
	case "text":
		return nullString(v.TextValue.Valid, v.TextValue.String)
	case "number":
		if !v.NumberValue.Valid {
			return ""
		}
		return strconv.FormatFloat(v.NumberValue.Float64, 'f', -1, 64)
	case "boolean":
		if !v.BoolValue.Valid {
			return ""
		}
		return formatBool(v.BoolValue.Int64 != 0)
	case "date":
		return nullString(v.DateValue.Valid, v.DateValue.String)
	default:
		return ""
	}
}

func encodeLabels(labels []storage.Label) string {
	if len(labels) == 0 {
		return ""
	}
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return joinEscaped(names, '|')
}

func encodeIdentifications(ids []storage.Identification) string {
	if len(ids) == 0 {
		return ""
	}
	pairs := make([]string, len(ids))
	for i, id := range ids {
		pairs[i] = EscapeSubValue(id.Kind) + ":" + EscapeSubValue(id.Value)
	}
	return joinNoFurtherEscape(pairs)
}

func encodeAttachments(atts []storage.Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	triples := make([]string, len(atts))
	for i, a := range atts {
		triples[i] = EscapeSubValue(a.Category) + ":" + EscapeSubValue(a.OriginalFilename) + ":" + EscapeSubValue(a.Sha256)
	}
	return joinNoFurtherEscape(triples)
}

func joinNoFurtherEscape(values []string) string {
	return strings.Join(values, "|")
}
