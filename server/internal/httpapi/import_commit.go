package httpapi

import (
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"os"
	"time"
)

const importCommitPathParam = "importID"

type importCommitSummaryResponse struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
}

type importCommitCreatedItemResponse struct {
	Line   int    `json:"line"`
	ItemID string `json:"item_id"`
}

type importCommitResponse struct {
	ImportID     string                            `json:"import_id"`
	Summary      importCommitSummaryResponse       `json:"summary"`
	CreatedItems []importCommitCreatedItemResponse `json:"created_items"`
}

type importCommitRejectedResponse struct {
	ImportID string                       `json:"import_id"`
	Rows     []importPreviewRowResponse   `json:"rows"`
	Summary  importPreviewSummaryResponse `json:"summary"`
}

func importCommitRepositoryFor(cfg Config, scope storage.Scope) (storage.ImportCommitRepository, error) {
	if cfg.Store == nil {
		return nil, errors.New("httpapi: import commit: no storage configured")
	}
	return cfg.Store.ForGroupImportCommit(scope.GroupID())
}

func toImportCommitRows(rows []importClassifiedRow) []storage.ImportCommitRow {
	out := make([]storage.ImportCommitRow, 0, len(rows))
	for _, cr := range rows {
		switch cr.Action {
		case "create":
			out = append(out, storage.ImportCommitRow{
				Line:     cr.Line,
				Action:   storage.ImportCommitRowCreate,
				Proposed: toImportCommitProposedRow(cr.Proposed),
			})
		case "update":
			out = append(out, storage.ImportCommitRow{
				Line:     cr.Line,
				Action:   storage.ImportCommitRowUpdate,
				ItemID:   cr.ItemID,
				Changes:  cr.Changes,
				Proposed: toImportCommitProposedRow(cr.Proposed),
			})
		case "unchanged":
			out = append(out, storage.ImportCommitRow{
				Line:   cr.Line,
				Action: storage.ImportCommitRowUnchanged,
				ItemID: cr.ItemID,
			})
		}
	}
	return out
}

func toImportCommitProposedRow(row importexport.StagedRow) storage.ImportCommitProposedRow {
	p := storage.ImportCommitProposedRow{
		Name:         row.Name,
		Description:  row.Description,
		Quantity:     row.Quantity,
		Labels:       row.Labels,
		CustomFields: row.CustomFields,
	}
	if row.LocationID.Valid {
		p.LocationID = row.LocationID.String
	}
	if len(row.Identifications) > 0 {
		p.Identifications = make([]storage.ImportCommitIdentification, len(row.Identifications))
		for i, idn := range row.Identifications {
			p.Identifications[i] = storage.ImportCommitIdentification{Kind: idn.Kind, Value: idn.Value}
		}
	}
	if row.Warranty != nil {
		p.Warranty = &storage.ImportCommitWarranty{
			Holder: row.Warranty.Holder, Provider: row.Warranty.Provider,
			StartsOn: row.Warranty.StartsOn, ExpiresOn: row.Warranty.ExpiresOn,
			IsLifetime: row.Warranty.IsLifetime, Notes: row.Warranty.Notes,
		}
	}
	if row.Purchase != nil {
		p.Purchase = &storage.ImportCommitPurchase{
			Vendor: row.Purchase.Vendor, PurchasedOn: row.Purchase.PurchasedOn,
			PurchasePriceMinor: row.Purchase.PurchasePriceMinor,
			OrderReference:     row.Purchase.OrderReference, Notes: row.Purchase.Notes,
		}
	}
	if row.Sale != nil {
		p.Sale = &storage.ImportCommitSale{
			BuyerName: row.Sale.BuyerName, SoldOn: row.Sale.SoldOn,
			SalePriceMinor: row.Sale.SalePriceMinor, Notes: row.Sale.Notes,
		}
	}
	return p
}

func importCommitHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		importID := chi.URLParam(r, importCommitPathParam)
		groupID := scope.GroupID().String()

		sessions, err := importSessionRepositoryFor(cfg, scope)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import commit: resolve session repository failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		session, ok, conflict, err := resolveStagedImportSession(r.Context(), sessions, importID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import commit: read session failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		case conflict:
			problem.Write(w, r, problem.Conflict())
			return
		case !ok:
			_ = importexport.RemoveStaged(cfg.DataDir, groupID, importID)
			problem.Write(w, r, problem.NotFound())
			return
		}
		_ = session

		raw, err := os.ReadFile(importexport.StagingPath(cfg.DataDir, groupID, importID))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				_ = sessions.Delete(r.Context(), importID)
				problem.Write(w, r, problem.NotFound())
				return
			}
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import commit: read staged file failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		classified, summary, err := classifyImportRows(r.Context(), scope, raw)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import commit: classify staged file failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		if summary.Error > 0 {
			resp := importCommitRejectedResponse{ImportID: importID, Rows: make([]importPreviewRowResponse, 0, len(classified)), Summary: summary}
			for _, row := range classified {
				resp.Rows = append(resp.Rows, importPreviewRowResponse{
					Line: row.Line, Action: row.Action, ItemID: row.ItemID, Name: row.Name, Changes: row.Changes, Errors: row.Errors,
				})
			}
			writeJSON(w, r, http.StatusUnprocessableEntity, resp)
			return
		}

		commit, err := importCommitRepositoryFor(cfg, scope)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import commit: resolve commit repository failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		result, err := commit.Commit(r.Context(), storage.ImportCommitParams{
			ImportSessionID: importID,
			Rows:            toImportCommitRows(classified),
			Now:             time.Now().UnixMilli(),
		})
		switch {
		case errors.Is(err, storage.ErrImportSessionNotStaged):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import commit: commit transaction failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		if err := importexport.RemoveStaged(cfg.DataDir, groupID, importID); err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import commit: remove staged file after successful commit failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
		}

		resp := importCommitResponse{
			ImportID:     importID,
			Summary:      importCommitSummaryResponse{Created: result.Created, Updated: result.Updated, Unchanged: result.Unchanged},
			CreatedItems: make([]importCommitCreatedItemResponse, 0, len(result.CreatedItems)),
		}
		for _, ci := range result.CreatedItems {
			resp.CreatedItems = append(resp.CreatedItems, importCommitCreatedItemResponse{Line: ci.Line, ItemID: ci.ItemID})
		}
		writeJSON(w, r, http.StatusOK, resp)
	}
}
