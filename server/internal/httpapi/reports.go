package httpapi

import (
	"encoding/csv"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/reporting"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const reportFormatParam = "format"

const (
	reportFormatJSON = "json"
	reportFormatCSV  = "csv"
)

func parseReportFormat(r *http.Request) (string, error) {
	switch v := r.URL.Query().Get(reportFormatParam); v {
	case "":
		return reportFormatJSON, nil
	case reportFormatJSON, reportFormatCSV:
		return v, nil
	default:
		return "", fmt.Errorf("format must be %q or %q (got %q)", reportFormatJSON, reportFormatCSV, v)
	}
}

func reportRepositoryFor(cfg Config, scope storage.Scope) (storage.ReportRepository, error) {
	if cfg.Store == nil {
		return nil, errors.New("httpapi: reports: no storage configured")
	}
	return cfg.Store.ForGroupReports(scope.GroupID())
}

func filenameForReport(name string, now time.Time) string {
	return "hho-report-" + name + "-" + now.UTC().Format("2006-01-02") + ".csv"
}

func writeReportCSV(w http.ResponseWriter, r *http.Request, cfg Config, reportName string, header []string, records [][]string) {
	h := w.Header()
	h.Set("Content-Type", exportCSVContentType)
	h.Set("Content-Disposition", attachmentContentDisposition("attachment", filenameForReport(reportName, time.Now())))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")

	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "reports: write CSV header failed",
			slog.String("request_id", requestid.FromContext(r.Context())),
			slog.String("report", reportName),
			slog.String("error", err.Error()),
		)
		return
	}
	for _, rec := range records {
		if err := cw.Write(rec); err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "reports: write CSV row failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("report", reportName),
				slog.String("error", err.Error()),
			)
			return
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "reports: flush CSV failed",
			slog.String("request_id", requestid.FromContext(r.Context())),
			slog.String("report", reportName),
			slog.String("error", err.Error()),
		)
	}
}

type reportValuationRow struct {
	GroupKey        string `json:"group_key"`
	GroupLabel      string `json:"group_label"`
	ItemCount       int64  `json:"item_count"`
	TotalValueMinor int64  `json:"total_value_minor"`
}

type reportValuationResponse struct {
	Rows []reportValuationRow `json:"rows"`
}

func reportsValuationHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		format, err := parseReportFormat(r)
		if err != nil {
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		}

		raw := r.URL.Query().Get("group_by")
		if raw == "" {
			problem.Write(w, r, problem.BadRequest(`group_by is required and must be "location" or "label"`))
			return
		}
		groupBy, err := reporting.ParseGroupBy(raw)
		if err != nil {
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		}

		repo, err := reportRepositoryFor(cfg, scope)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "reports: valuation: bind report repository failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		rows, err := reporting.Valuation(r.Context(), repo, groupBy)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "reports: valuation failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		if format == reportFormatCSV {
			records := make([][]string, 0, len(rows))
			for _, row := range rows {
				records = append(records, []string{
					row.GroupKey,
					row.GroupLabel,
					strconv.FormatInt(row.ItemCount, 10),
					strconv.FormatInt(row.TotalValueMinor, 10),
				})
			}
			writeReportCSV(w, r, cfg, "valuation", []string{"group_key", "group_label", "item_count", "total_value_minor"}, records)
			return
		}

		out := make([]reportValuationRow, 0, len(rows))
		for _, row := range rows {
			out = append(out, reportValuationRow{
				GroupKey:        row.GroupKey,
				GroupLabel:      row.GroupLabel,
				ItemCount:       row.ItemCount,
				TotalValueMinor: row.TotalValueMinor,
			})
		}
		writeJSON(w, r, http.StatusOK, reportValuationResponse{Rows: out})
	}
}

type reportWarrantyExpiringRow struct {
	ItemID        string `json:"item_id"`
	ItemName      string `json:"item_name"`
	ExpiresOn     string `json:"expires_on"`
	DaysRemaining int    `json:"days_remaining"`
}

type reportWarrantyExpiringResponse struct {
	Rows []reportWarrantyExpiringRow `json:"rows"`
}

func reportsWarrantyExpiringHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		format, err := parseReportFormat(r)
		if err != nil {
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		}

		withinDays := reporting.DefaultWithinDays
		if raw := r.URL.Query().Get("within_days"); raw != "" {
			n, convErr := strconv.Atoi(raw)
			if convErr != nil {
				problem.Write(w, r, problem.BadRequest("within_days must be an integer"))
				return
			}
			withinDays = n
		}

		repo, err := reportRepositoryFor(cfg, scope)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "reports: warranty expiring: bind report repository failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		rows, err := reporting.WarrantyExpiring(r.Context(), repo, time.Now(), withinDays)
		switch {
		case errors.Is(err, reporting.ErrWithinDaysInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "reports: warranty expiring failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		if format == reportFormatCSV {
			records := make([][]string, 0, len(rows))
			for _, row := range rows {
				records = append(records, []string{
					row.ItemID,
					row.ItemName,
					row.ExpiresOn,
					strconv.Itoa(row.DaysRemaining),
				})
			}
			writeReportCSV(w, r, cfg, "warranty-expiring", []string{"item_id", "item_name", "expires_on", "days_remaining"}, records)
			return
		}

		out := make([]reportWarrantyExpiringRow, 0, len(rows))
		for _, row := range rows {
			out = append(out, reportWarrantyExpiringRow{
				ItemID:        row.ItemID,
				ItemName:      row.ItemName,
				ExpiresOn:     row.ExpiresOn,
				DaysRemaining: row.DaysRemaining,
			})
		}
		writeJSON(w, r, http.StatusOK, reportWarrantyExpiringResponse{Rows: out})
	}
}

type reportPurchaseRow struct {
	ItemID             string `json:"item_id"`
	ItemName           string `json:"item_name"`
	PurchasedOn        string `json:"purchased_on"`
	Vendor             string `json:"vendor"`
	PurchasePriceMinor int64  `json:"purchase_price_minor"`
}

type reportPurchasesResponse struct {
	Rows []reportPurchaseRow `json:"rows"`
}

func reportsPurchasesHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		format, err := parseReportFormat(r)
		if err != nil {
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		}

		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		switch {
		case from == "" && to == "":
			problem.Write(w, r, problem.BadRequest("from and to are both required (ISO-8601 YYYY-MM-DD)"))
			return
		case from == "":
			problem.Write(w, r, problem.BadRequest("from is required (ISO-8601 YYYY-MM-DD)"))
			return
		case to == "":
			problem.Write(w, r, problem.BadRequest("to is required (ISO-8601 YYYY-MM-DD)"))
			return
		}

		repo, err := reportRepositoryFor(cfg, scope)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "reports: purchases: bind report repository failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		rows, err := reporting.PurchasesInRange(r.Context(), repo, from, to)
		switch {
		case errors.Is(err, reporting.ErrDateRangeInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "reports: purchases failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		if format == reportFormatCSV {
			records := make([][]string, 0, len(rows))
			for _, row := range rows {
				records = append(records, []string{
					row.ItemID,
					row.ItemName,
					row.PurchasedOn,
					row.Vendor,
					strconv.FormatInt(row.PurchasePriceMinor, 10),
				})
			}
			writeReportCSV(w, r, cfg, "purchases", []string{"item_id", "item_name", "purchased_on", "vendor", "purchase_price_minor"}, records)
			return
		}

		out := make([]reportPurchaseRow, 0, len(rows))
		for _, row := range rows {
			out = append(out, reportPurchaseRow{
				ItemID:             row.ItemID,
				ItemName:           row.ItemName,
				PurchasedOn:        row.PurchasedOn,
				Vendor:             row.Vendor,
				PurchasePriceMinor: row.PurchasePriceMinor,
			})
		}
		writeJSON(w, r, http.StatusOK, reportPurchasesResponse{Rows: out})
	}
}

type reportLocationItemCountRow struct {
	LocationID   string `json:"location_id"`
	LocationName string `json:"location_name"`
	ItemCount    int64  `json:"item_count"`
}

type reportItemCountByLocationResponse struct {
	Rows []reportLocationItemCountRow `json:"rows"`
}

func reportsItemCountByLocationHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		format, err := parseReportFormat(r)
		if err != nil {
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		}

		rows, err := reporting.ItemCountByLocation(r.Context(), scope.Locations())
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "reports: item count by location failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		if format == reportFormatCSV {
			records := make([][]string, 0, len(rows))
			for _, row := range rows {
				records = append(records, []string{
					row.LocationID,
					row.LocationName,
					strconv.FormatInt(row.ItemCount, 10),
				})
			}
			writeReportCSV(w, r, cfg, "item-count-by-location", []string{"location_id", "location_name", "item_count"}, records)
			return
		}

		out := make([]reportLocationItemCountRow, 0, len(rows))
		for _, row := range rows {
			out = append(out, reportLocationItemCountRow{
				LocationID:   row.LocationID,
				LocationName: row.LocationName,
				ItemCount:    row.ItemCount,
			})
		}
		writeJSON(w, r, http.StatusOK, reportItemCountByLocationResponse{Rows: out})
	}
}
