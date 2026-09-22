package importexport

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

func parseISODate(s string) error {
	if s == "" {
		return nil
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return fmt.Errorf("must be an ISO-8601 calendar day (YYYY-MM-DD), got %q", s)
	}
	if got := t.Format(dateLayout); got != s {
		return fmt.Errorf("date %q is not zero-padded (want %q)", s, got)
	}
	return nil
}

var identificationKinds = map[string]bool{
	"serial":    true,
	"model":     true,
	"asset_tag": true,
	"barcode":   true,
	"other":     true,
}

var customFieldTypes = map[string]bool{
	"text":    true,
	"number":  true,
	"boolean": true,
	"date":    true,
}

var attachmentCategories = map[string]bool{
	"image":    true,
	"manual":   true,
	"warranty": true,
	"receipt":  true,
	"general":  true,
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("must be %q or %q (case-insensitive), got %q", "true", "false", s)
	}
}

func parseQuantity(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("must be an integer, got %q", s)
	}
	return n, nil
}

func parsePriceMinor(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("must be an integer, got %q", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("must not be negative, got %d", n)
	}
	return n, nil
}
