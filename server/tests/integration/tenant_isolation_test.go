package integration

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/attachments"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTenantIsolation(t *testing.T) {
	f := newCrossTenantFixture(t)

	doomed := provisionAndDeleteDoomedItem(t, f)

	for _, cred := range []credential{cookieCredential, bearerCredential} {
		t.Run(cred.kind, func(t *testing.T) {
			t.Run("sessions_revoke", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/auth/sessions/"+f.a.sessionID, f.a.sessionID)
			})
			t.Run("sessions_list", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/auth/sessions", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s list own sessions: status = %d, want 200: %s", f.b.name, rec.Code, rec.Body.String())
				}
				var got sessionListResponse
				mustDecode(t, rec, &got)
				for _, s := range got.Sessions {
					if s.ID == f.a.sessionID {
						t.Fatalf("%s's own session list contains %s's session id %q -- cross-tenant leak in GET /auth/sessions", f.b.name, f.a.name, f.a.sessionID)
					}
				}
				if len(got.Sessions) == 0 {
					t.Fatalf("%s's session list is empty; a listing that returns nothing for everyone would satisfy the exclusion check above vacuously", f.b.name)
				}
			})

			t.Run("device_tokens_revoke", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/auth/device-tokens/"+f.a.deviceTokenID, f.a.deviceTokenID)
			})
			t.Run("device_tokens_list", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/auth/device-tokens", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s list own device tokens: status = %d, want 200: %s", f.b.name, rec.Code, rec.Body.String())
				}
				var got deviceTokenListResponse
				mustDecode(t, rec, &got)
				for _, d := range got.DeviceTokens {
					if d.ID == f.a.deviceTokenID {
						t.Fatalf("%s's own device-token list contains %s's device token id %q -- cross-tenant leak in GET /auth/device-tokens", f.b.name, f.a.name, f.a.deviceTokenID)
					}
				}
				if len(got.DeviceTokens) == 0 {
					t.Fatalf("%s's device-token list is empty; the exclusion check above would then hold vacuously", f.b.name)
				}
			})

			t.Run("invites_revoke", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/invites/"+f.a.inviteID, f.a.inviteID)
			})
			t.Run("invites_list", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/invites", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s (an owner) list own invites: status = %d, want 200 (a 403 here means the owner gate, not the tenant boundary, answered): %s", f.b.name, rec.Code, rec.Body.String())
				}
				var got inviteListResponse
				mustDecode(t, rec, &got)
				for _, inv := range got.Invites {
					if inv.ID == f.a.inviteID {
						t.Fatalf("%s's own invite list contains %s's invite id %q -- cross-tenant leak in GET /invites", f.b.name, f.a.name, f.a.inviteID)
					}
				}
				if len(got.Invites) != 1 || got.Invites[0].ID != f.b.inviteID {
					t.Fatalf("%s's invite list = %+v, want exactly %s's own invite %q", f.b.name, got.Invites, f.b.name, f.b.inviteID)
				}
			})

			t.Run("items_get", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID, f.a.itemID)
			})
			t.Run("items_update", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+f.a.itemID, f.a.itemID,
					itemUpdateBody("pwned by "+f.b.name, 1))
			})
			t.Run("items_delete", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+f.a.itemID, f.a.itemID)
			})
			t.Run("items_list", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/items", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s list own items: status = %d, want 200: %s", f.b.name, rec.Code, rec.Body.String())
				}
				var got itemListResponse
				mustDecode(t, rec, &got)
				for _, it := range got.Items {
					if it.ID == f.a.itemID {
						t.Fatalf("%s's own item list contains %s's item id %q -- cross-tenant leak in GET /items", f.b.name, f.a.name, f.a.itemID)
					}
				}
				if len(got.Items) != 1 || got.Items[0].ID != f.b.itemID {
					t.Fatalf("%s's item list = %+v, want exactly %s's own item %q", f.b.name, got.Items, f.b.name, f.b.itemID)
				}
			})

			t.Run("export_items_csv", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/export/items.csv", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s export items.csv: status = %d, want 200: %s", f.b.name, rec.Code, rec.Body.String())
				}
				records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
				if err != nil {
					t.Fatalf("%s export items.csv: response body is not valid CSV: %v; body = %q", f.b.name, err, rec.Body.String())
				}
				if len(records) == 0 {
					t.Fatalf("%s export items.csv: response body has no header row at all", f.b.name)
				}
				header := records[0]
				idCol := slices.Index(header, importexport.ColumnID)
				nameCol := slices.Index(header, importexport.ColumnName)
				if idCol < 0 || nameCol < 0 {
					t.Fatalf("%s export items.csv: header %v is missing the %q/%q column", f.b.name, header, importexport.ColumnID, importexport.ColumnName)
				}

				var sawB bool
				for _, row := range records[1:] {
					if row[idCol] == f.a.itemID {
						t.Fatalf("%s's export contains %s's item id %q -- cross-tenant leak in GET /export/items.csv", f.b.name, f.a.name, f.a.itemID)
					}
					if row[idCol] == f.b.itemID {
						sawB = true
						if row[nameCol] != f.b.itemName {
							t.Fatalf("%s's own exported row for item %q has name %q, want %q", f.b.name, f.b.itemID, row[nameCol], f.b.itemName)
						}
					}
				}
				if !sawB {
					t.Fatalf("%s's export does not contain %s's own item %q; the exclusion check above would then hold vacuously", f.b.name, f.b.name, f.b.itemID)
				}
				if got := len(records) - 1; got != 1 {
					t.Fatalf("%s's export has %d data row(s), want exactly 1 (%s's own item)", f.b.name, got, f.b.name)
				}
			})

			t.Run("labels_qr_batch", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodPost, "/api/v1/labels/qr/batch",
					labelsQRBatchBody(f.a.itemID, f.b.itemID))
				if rec.Code != http.StatusOK {
					t.Fatalf("%s labels/qr/batch naming both %s's and %s's own item id: status = %d, want 200: %s", f.b.name, f.a.name, f.b.name, rec.Code, rec.Body.String())
				}
				var got labelsQRBatchResponse
				mustDecode(t, rec, &got)

				var sawB bool
				for _, item := range got.Items {
					if item.ID == f.a.itemID {
						t.Fatalf("%s's labels/qr/batch response contains %s's item id %q -- cross-tenant leak in POST /labels/qr/batch", f.b.name, f.a.name, f.a.itemID)
					}
					if item.ID == f.b.itemID {
						sawB = true
						if item.Name != f.b.itemName {
							t.Fatalf("%s's own labels/qr/batch entry for item %q has name %q, want %q", f.b.name, f.b.itemID, item.Name, f.b.itemName)
						}
					}
				}
				if !sawB {
					t.Fatalf("%s's labels/qr/batch response does not contain %s's own item %q even though it was named in the same request as %s's; the exclusion check above would then hold vacuously", f.b.name, f.b.name, f.b.itemID, f.a.name)
				}
				if got := len(got.Items); got != 1 {
					t.Fatalf("%s's labels/qr/batch response has %d item(s), want exactly 1 (%s's own item)", f.b.name, got, f.b.name)
				}
			})

			t.Run("items_delete_tombstone_cascade_cross_group", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+doomed.itemID, doomed.itemID)
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+doomed.itemID, doomed.itemID,
					itemUpdateBody("pwned by "+f.b.name, 1))
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+doomed.itemID, doomed.itemID)

				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+doomed.itemID+"/warranty", doomed.itemID)
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+doomed.itemID+"/warranty", doomed.itemID,
					warrantyUpdateBody("pwned by "+f.b.name, 1))
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+doomed.itemID+"/warranty", doomed.itemID)

				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+doomed.itemID+"/sale", doomed.itemID)
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+doomed.itemID+"/sale", doomed.itemID,
					saleUpdateBody("pwned by "+f.b.name, 1))
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+doomed.itemID+"/sale", doomed.itemID)

				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+doomed.itemID+"/purchase", doomed.itemID)
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+doomed.itemID+"/purchase", doomed.itemID,
					purchaseUpdateBody("pwned by "+f.b.name, 1))
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+doomed.itemID+"/purchase", doomed.itemID)

				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+doomed.itemID+"/identifications", doomed.itemID)
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+doomed.itemID+"/identifications/"+doomed.identificationID, doomed.itemID,
					identificationUpdateBody("serial", "pwned by "+f.b.name, 1))
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+doomed.itemID+"/identifications/"+doomed.identificationID, doomed.itemID)

				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+doomed.itemID+"/custom-fields", doomed.itemID)
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+doomed.itemID+"/custom-fields/"+doomed.itemCustomFieldID, doomed.itemID,
					itemCustomFieldUpdateBody("Colour", "pwned by "+f.b.name, 1))
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+doomed.itemID+"/custom-fields/"+doomed.itemCustomFieldID, doomed.itemID)

				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+doomed.itemID+"/stock-adjustments", doomed.itemID)

				t.Run("no_existence_oracle", func(t *testing.T) {
					recTombstoned := f.request(t, cred, http.MethodGet, "/api/v1/items/"+doomed.itemID, nil)
					if recTombstoned.Code != wantCrossGroupStatus {
						t.Fatalf("%s (%s) GET a tombstoned item: status = %d, want %d: %s",
							f.b.name, cred.kind, recTombstoned.Code, wantCrossGroupStatus, recTombstoned.Body.String())
					}
					recNeverExisted := f.request(t, cred, http.MethodGet, "/api/v1/items/"+neverExistedItemID, nil)
					if recNeverExisted.Code != wantCrossGroupStatus {
						t.Fatalf("%s (%s) GET an id that was never minted: status = %d, want %d: %s",
							f.b.name, cred.kind, recNeverExisted.Code, wantCrossGroupStatus, recNeverExisted.Body.String())
					}
					tombstoned := decodeProblem(t, recTombstoned)
					neverExisted := decodeProblem(t, recNeverExisted)
					tombstoned.RequestID, neverExisted.RequestID = "", ""
					if tombstoned != neverExisted {
						t.Fatalf("%s (%s) GET a TOMBSTONED item = %+v, GET an id that was NEVER minted = %+v "+
							"(request_id cleared from both) -- these must be byte-for-byte identical, or "+
							"the tombstone event is itself an existence oracle", f.b.name, cred.kind, tombstoned, neverExisted)
					}

					recTombstonedWarranty := f.request(t, cred, http.MethodGet, "/api/v1/items/"+doomed.itemID+"/warranty", nil)
					if recTombstonedWarranty.Code != wantCrossGroupStatus {
						t.Fatalf("%s (%s) GET a tombstoned item's warranty: status = %d, want %d: %s",
							f.b.name, cred.kind, recTombstonedWarranty.Code, wantCrossGroupStatus, recTombstonedWarranty.Body.String())
					}
					recNeverExistedWarranty := f.request(t, cred, http.MethodGet, "/api/v1/items/"+neverExistedItemID+"/warranty", nil)
					if recNeverExistedWarranty.Code != wantCrossGroupStatus {
						t.Fatalf("%s (%s) GET a never-minted item's warranty: status = %d, want %d: %s",
							f.b.name, cred.kind, recNeverExistedWarranty.Code, wantCrossGroupStatus, recNeverExistedWarranty.Body.String())
					}
					tombstonedWarranty := decodeProblem(t, recTombstonedWarranty)
					neverExistedWarranty := decodeProblem(t, recNeverExistedWarranty)
					tombstonedWarranty.RequestID, neverExistedWarranty.RequestID = "", ""
					if tombstonedWarranty != neverExistedWarranty {
						t.Fatalf("%s (%s) GET a TOMBSTONED item's warranty = %+v, GET a never-minted item's "+
							"warranty = %+v (request_id cleared from both) -- must be byte-for-byte identical",
							f.b.name, cred.kind, tombstonedWarranty, neverExistedWarranty)
					}
				})
			})

			t.Run("warranty_get", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID+"/warranty", f.a.itemID)
			})
			t.Run("warranty_create", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPost,
					"/api/v1/items/"+f.a.itemID+"/warranty", f.a.itemID,
					warrantyCreateBody("pwned by "+f.b.name))
			})
			t.Run("warranty_update", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+f.a.itemID+"/warranty", f.a.itemID,
					warrantyUpdateBody("pwned by "+f.b.name, 1))
			})
			t.Run("warranty_delete", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+f.a.itemID+"/warranty", f.a.itemID)
			})

			t.Run("sale_get", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID+"/sale", f.a.itemID)
			})
			t.Run("sale_create", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPost,
					"/api/v1/items/"+f.a.itemID+"/sale", f.a.itemID,
					saleCreateBody("pwned by "+f.b.name))
			})
			t.Run("sale_update", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+f.a.itemID+"/sale", f.a.itemID,
					saleUpdateBody("pwned by "+f.b.name, 1))
			})
			t.Run("sale_delete", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+f.a.itemID+"/sale", f.a.itemID)
			})

			t.Run("purchase_get", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID+"/purchase", f.a.itemID)
			})
			t.Run("purchase_create", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPost,
					"/api/v1/items/"+f.a.itemID+"/purchase", f.a.itemID,
					purchaseCreateBody("pwned by "+f.b.name))
			})
			t.Run("purchase_update", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+f.a.itemID+"/purchase", f.a.itemID,
					purchaseUpdateBody("pwned by "+f.b.name, 1))
			})
			t.Run("purchase_delete", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+f.a.itemID+"/purchase", f.a.itemID)
			})

			t.Run("identifications_list", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID+"/identifications", f.a.itemID)
			})
			t.Run("identifications_create", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPost,
					"/api/v1/items/"+f.a.itemID+"/identifications", f.a.itemID,
					identificationCreateBody("serial", "pwned by "+f.b.name))
			})
			t.Run("identifications_update", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+f.a.itemID+"/identifications/"+f.a.identificationID, f.a.itemID,
					identificationUpdateBody("serial", "pwned by "+f.b.name, 1))
			})
			t.Run("identifications_delete", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+f.a.itemID+"/identifications/"+f.a.identificationID, f.a.itemID)
			})

			t.Run("custom_field_defs_list", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/custom-field-defs", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s list own custom field defs: status = %d, want 200: %s", f.b.name, rec.Code, rec.Body.String())
				}
				var got customFieldDefListResponse
				mustDecode(t, rec, &got)
				for _, d := range got.CustomFieldDefs {
					if d.ID == f.a.customFieldDefID {
						t.Fatalf("%s's own custom-field-def list contains %s's def id %q -- cross-tenant leak in GET /custom-field-defs", f.b.name, f.a.name, f.a.customFieldDefID)
					}
				}
				if len(got.CustomFieldDefs) != 1 || got.CustomFieldDefs[0].ID != f.b.customFieldDefID {
					t.Fatalf("%s's custom-field-def list = %+v, want exactly %s's own definition %q", f.b.name, got.CustomFieldDefs, f.b.name, f.b.customFieldDefID)
				}
			})
			t.Run("custom_field_defs_update", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/custom-field-defs/"+f.a.customFieldDefID, f.a.customFieldDefID,
					customFieldDefUpdateBody("pwned by "+f.b.name, "text", 0, 1))
			})
			t.Run("custom_field_defs_delete", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/custom-field-defs/"+f.a.customFieldDefID, f.a.customFieldDefID)
			})

			t.Run("labels_list", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/labels", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s list own labels: status = %d, want 200: %s", f.b.name, rec.Code, rec.Body.String())
				}
				var got labelListResponse
				mustDecode(t, rec, &got)
				for _, l := range got.Labels {
					if l.ID == f.a.labelID {
						t.Fatalf("%s's own label list contains %s's label id %q -- cross-tenant leak in GET /labels", f.b.name, f.a.name, f.a.labelID)
					}
				}
				if len(got.Labels) != 1 || got.Labels[0].ID != f.b.labelID {
					t.Fatalf("%s's label list = %+v, want exactly %s's own label %q", f.b.name, got.Labels, f.b.name, f.b.labelID)
				}
			})
			t.Run("labels_update", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/labels/"+f.a.labelID, f.a.labelID,
					labelUpdateBody("pwned by "+f.b.name, "#654321", 1))
			})
			t.Run("labels_delete", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/labels/"+f.a.labelID, f.a.labelID)
			})

			t.Run("item_labels_list", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID+"/labels", f.a.itemID)
			})
			t.Run("item_labels_attach_foreign_item", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodPut,
					"/api/v1/items/"+f.a.itemID+"/labels/"+f.b.labelID, f.a.itemID)
			})
			t.Run("item_labels_attach_foreign_label", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodPut,
					"/api/v1/items/"+f.b.itemID+"/labels/"+f.a.labelID, f.a.labelID)
			})
			t.Run("item_labels_detach", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+f.a.itemID+"/labels/"+f.a.labelID, f.a.labelID)
			})

			t.Run("item_custom_fields_list", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID+"/custom-fields", f.a.itemID)
			})
			t.Run("item_custom_fields_create", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPost,
					"/api/v1/items/"+f.a.itemID+"/custom-fields", f.a.itemID,
					itemCustomFieldCreateBody("Colour", "pwned by "+f.b.name))
			})
			t.Run("item_custom_fields_update", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/items/"+f.a.itemID+"/custom-fields/"+f.a.itemCustomFieldID, f.a.itemID,
					itemCustomFieldUpdateBody("Colour", "pwned by "+f.b.name, 1))
			})
			t.Run("item_custom_fields_delete", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+f.a.itemID+"/custom-fields/"+f.a.itemCustomFieldID, f.a.itemID)
			})

			t.Run("stock_adjustments_list", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID+"/stock-adjustments", f.a.itemID)
			})
			t.Run("stock_adjustments_create", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPost,
					"/api/v1/items/"+f.a.itemID+"/stock-adjustments", f.a.itemID,
					stockAdjustmentCreateBody(1, "pwned by "+f.b.name, "pwned"))
			})

			t.Run("locations_list", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/locations", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s (%s) list own locations: status = %d, want 200: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
				}
				var got locationListResponse
				mustDecode(t, rec, &got)
				for _, loc := range got.Locations {
					if loc.ID == f.a.locationID {
						t.Fatalf("%s (%s)'s own location list contains %s's location id %q -- cross-tenant leak in GET /locations", f.b.name, cred.kind, f.a.name, f.a.locationID)
					}
				}
				if len(got.Locations) != 1 || got.Locations[0].ID != f.b.locationID {
					t.Fatalf("%s (%s)'s location list = %+v, want exactly %s's own location %q", f.b.name, cred.kind, got.Locations, f.b.name, f.b.locationID)
				}
			})
			t.Run("locations_get", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/locations/"+f.a.locationID, f.a.locationID)
			})
			t.Run("locations_update", func(t *testing.T) {
				f.assertCrossGroupNotFoundWithBody(t, cred, http.MethodPut,
					"/api/v1/locations/"+f.a.locationID, f.a.locationID,
					locationUpdateBody("pwned by "+f.b.name, "", 1))
			})
			t.Run("locations_delete", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/locations/"+f.a.locationID, f.a.locationID)
			})

			t.Run("groups_visibility_get", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/groups/detail-visibility", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s get own group visibility: status = %d, want 200: %s", f.b.name, rec.Code, rec.Body.String())
				}
				var got groupVisibilityResponse
				mustDecode(t, rec, &got)
				if got.Version < 1 {
					t.Fatalf("%s's group visibility version = %d, want >= 1 -- a version of 0 would mean this read is not seeing a real row", f.b.name, got.Version)
				}
			})
			t.Run("groups_visibility_update", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/groups/detail-visibility", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s get own group visibility before update: status = %d, want 200: %s", f.b.name, rec.Code, rec.Body.String())
				}
				var before groupVisibilityResponse
				mustDecode(t, rec, &before)

				rec = f.request(t, cred, http.MethodPut, "/api/v1/groups/detail-visibility",
					groupVisibilityUpdateBody(true, false, false, before.Version))
				if rec.Code != http.StatusOK {
					t.Fatalf("%s (an owner) update own group visibility: status = %d, want 200 (a 403 would mean the owner gate, not this test, is answering; a 409 would mean the fresh version read above did not match): %s", f.b.name, rec.Code, rec.Body.String())
				}
				var after groupVisibilityResponse
				mustDecode(t, rec, &after)
				if !after.WarrantyVisible || after.Version != before.Version+1 {
					t.Fatalf("%s's group visibility after its own update = %+v, want {warranty_visible:true version:%d}", f.b.name, after, before.Version+1)
				}
			})

			t.Run("members_list", func(t *testing.T) {
				rec := f.request(t, cred, http.MethodGet, "/api/v1/groups/members", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s (an owner) list own group's members: status = %d, want 200: %s", f.b.name, rec.Code, rec.Body.String())
				}
				var got groupMembersResponse
				mustDecode(t, rec, &got)
				for _, m := range got.Members {
					if m.Username == f.a.name {
						t.Fatalf("%s's own member list contains %s's username %q -- cross-tenant leak in GET /groups/members", f.b.name, f.a.name, f.a.name)
					}
				}
				if len(got.Members) != 1 || got.Members[0].Username != f.b.name {
					t.Fatalf("%s's member list = %+v, want exactly %s's own membership row", f.b.name, got.Members, f.b.name)
				}
				if got.Members[0].Role != "owner" {
					t.Errorf("%s's own membership row role = %q, want %q (the registering user is the group's owner, FR-007)", f.b.name, got.Members[0].Role, "owner")
				}
			})

			t.Run("attachments_list", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID+"/attachments", f.a.itemID)
			})
			t.Run("attachments_download", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID+"/attachments/"+f.a.attachmentID, f.a.attachmentID)
			})
			t.Run("attachments_thumbnail_download", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodGet,
					"/api/v1/items/"+f.a.itemID+"/attachments/"+f.a.attachmentID+"/thumbnail", f.a.attachmentID)
			})
			t.Run("attachments_delete", func(t *testing.T) {
				f.assertCrossGroupNotFound(t, cred, http.MethodDelete,
					"/api/v1/items/"+f.a.itemID+"/attachments/"+f.a.attachmentID, f.a.attachmentID)
			})
		})
	}

	for _, cred := range []credential{cookieCredential, bearerCredential} {
		t.Run(cred.kind+"/invites_create_readback", func(t *testing.T) {
			rec := f.request(t, cred, http.MethodPost, "/api/v1/invites", nil)
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create invite: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var created inviteCreateResponse
			mustDecode(t, rec, &created)

			listRec := f.request(t, cred, http.MethodGet, "/api/v1/invites", nil)
			if listRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) list own invites after create: status = %d, want 200: %s", f.b.name, cred.kind, listRec.Code, listRec.Body.String())
			}
			var bList inviteListResponse
			mustDecode(t, listRec, &bList)
			found := false
			for _, inv := range bList.Invites {
				if inv.ID == created.ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s (%s)'s newly created invite %q is absent from %s's own listing -- POST /invites did not land the row in the caller's group", f.b.name, cred.kind, created.ID, f.b.name)
			}

			aRec := f.srv.do(t, http.MethodGet, "/api/v1/invites", f.a.cookie, nil)
			if aRec.Code != http.StatusOK {
				t.Fatalf("%s list own invites: status = %d, want 200: %s", f.a.name, aRec.Code, aRec.Body.String())
			}
			var aList inviteListResponse
			mustDecode(t, aRec, &aList)
			for _, inv := range aList.Invites {
				if inv.ID == created.ID {
					t.Fatalf("%s (%s)'s newly created invite %q leaked into %s's listing -- cross-tenant create leak in POST /invites", f.b.name, cred.kind, created.ID, f.a.name)
				}
			}
		})

		t.Run(cred.kind+"/items_create_readback", func(t *testing.T) {
			name := fmt.Sprintf("%s's %s crowbar", f.b.name, cred.kind)
			rec := f.request(t, cred, http.MethodPost, "/api/v1/items", itemCreateBody(name))
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create item: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var created itemResponse
			mustDecode(t, rec, &created)
			if created.ID == "" {
				t.Fatalf("%s (%s) create item returned no id; both readback checks below would look for the empty string", f.b.name, cred.kind)
			}

			listRec := f.request(t, cred, http.MethodGet, "/api/v1/items", nil)
			if listRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) list own items after create: status = %d, want 200: %s", f.b.name, cred.kind, listRec.Code, listRec.Body.String())
			}
			var bList itemListResponse
			mustDecode(t, listRec, &bList)
			found := false
			for _, it := range bList.Items {
				if it.ID == created.ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s (%s)'s newly created item %q is absent from %s's own listing -- POST /items did not land the row in the caller's group", f.b.name, cred.kind, created.ID, f.b.name)
			}

			aRec := f.srv.do(t, http.MethodGet, "/api/v1/items", f.a.cookie, nil)
			if aRec.Code != http.StatusOK {
				t.Fatalf("%s list own items: status = %d, want 200: %s", f.a.name, aRec.Code, aRec.Body.String())
			}
			var aList itemListResponse
			mustDecode(t, aRec, &aList)
			for _, it := range aList.Items {
				if it.ID == created.ID {
					t.Fatalf("%s (%s)'s newly created item %q leaked into %s's listing -- cross-tenant create leak in POST /items", f.b.name, cred.kind, created.ID, f.a.name)
				}
			}
			getRec := f.srv.do(t, http.MethodGet, "/api/v1/items/"+created.ID, f.a.cookie, nil)
			if getRec.Code != http.StatusNotFound {
				t.Fatalf("%s GET /items/%s (an item %s just created) -> %d, want 404 -- POST /items wrote into the wrong group, or reading across groups by id is possible: %s", f.a.name, created.ID, f.b.name, getRec.Code, getRec.Body.String())
			}
		})

		t.Run(cred.kind+"/warranty_create_readback", func(t *testing.T) {
			itemName := fmt.Sprintf("%s's %s toolbox", f.b.name, cred.kind)
			itemRec := f.request(t, cred, http.MethodPost, "/api/v1/items", itemCreateBody(itemName))
			if itemRec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create item to hang a warranty on: status = %d, want 201: %s", f.b.name, cred.kind, itemRec.Code, itemRec.Body.String())
			}
			var host itemResponse
			mustDecode(t, itemRec, &host)
			if host.ID == "" {
				t.Fatalf("%s (%s) create item returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			holder := fmt.Sprintf("%s's %s holder", f.b.name, cred.kind)
			rec := f.request(t, cred, http.MethodPost, "/api/v1/items/"+host.ID+"/warranty", warrantyCreateBody(holder))
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create warranty on own item: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var createdWarranty warrantyResponse
			mustDecode(t, rec, &createdWarranty)
			if createdWarranty.Holder != holder || createdWarranty.ItemID != host.ID {
				t.Fatalf("%s (%s) created warranty = %+v, want holder %q on item %q", f.b.name, cred.kind, createdWarranty, holder, host.ID)
			}

			ownRec := f.request(t, cred, http.MethodGet, "/api/v1/items/"+host.ID+"/warranty", nil)
			if ownRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) read own warranty back: status = %d, want 200 -- POST did not land the row in the caller's group: %s", f.b.name, cred.kind, ownRec.Code, ownRec.Body.String())
			}
			var ownWarranty warrantyResponse
			mustDecode(t, ownRec, &ownWarranty)
			if ownWarranty.Holder != holder {
				t.Fatalf("%s (%s) read back holder %q, want %q", f.b.name, cred.kind, ownWarranty.Holder, holder)
			}

			aRec := f.srv.do(t, http.MethodGet, "/api/v1/items/"+host.ID+"/warranty", f.a.cookie, nil)
			if aRec.Code != http.StatusNotFound {
				t.Fatalf("%s GET /items/%s/warranty (a block %s just created) -> %d, want 404 -- POST wrote into the wrong group, or reading across groups is possible: %s", f.a.name, host.ID, f.b.name, aRec.Code, aRec.Body.String())
			}
			if strings.Contains(aRec.Body.String(), holder) {
				t.Fatalf("%s's 404 body echoed %s's warranty holder %q: %s", f.a.name, f.b.name, holder, aRec.Body.String())
			}
		})

		t.Run(cred.kind+"/sale_create_readback", func(t *testing.T) {
			itemName := fmt.Sprintf("%s's %s bicycle", f.b.name, cred.kind)
			itemRec := f.request(t, cred, http.MethodPost, "/api/v1/items", itemCreateBody(itemName))
			if itemRec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create item to hang a sale on: status = %d, want 201: %s", f.b.name, cred.kind, itemRec.Code, itemRec.Body.String())
			}
			var host itemResponse
			mustDecode(t, itemRec, &host)
			if host.ID == "" {
				t.Fatalf("%s (%s) create item returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			buyer := fmt.Sprintf("%s's %s buyer", f.b.name, cred.kind)
			rec := f.request(t, cred, http.MethodPost, "/api/v1/items/"+host.ID+"/sale", saleCreateBody(buyer))
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create sale on own item: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var createdSale saleResponse
			mustDecode(t, rec, &createdSale)
			if createdSale.BuyerName != buyer || createdSale.ItemID != host.ID {
				t.Fatalf("%s (%s) created sale = %+v, want buyer_name %q on item %q", f.b.name, cred.kind, createdSale, buyer, host.ID)
			}

			ownRec := f.request(t, cred, http.MethodGet, "/api/v1/items/"+host.ID+"/sale", nil)
			if ownRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) read own sale back: status = %d, want 200 -- POST did not land the row in the caller's group: %s", f.b.name, cred.kind, ownRec.Code, ownRec.Body.String())
			}
			var ownSale saleResponse
			mustDecode(t, ownRec, &ownSale)
			if ownSale.BuyerName != buyer {
				t.Fatalf("%s (%s) read back buyer_name %q, want %q", f.b.name, cred.kind, ownSale.BuyerName, buyer)
			}

			aRec := f.srv.do(t, http.MethodGet, "/api/v1/items/"+host.ID+"/sale", f.a.cookie, nil)
			if aRec.Code != http.StatusNotFound {
				t.Fatalf("%s GET /items/%s/sale (a block %s just created) -> %d, want 404 -- POST wrote into the wrong group, or reading across groups is possible: %s", f.a.name, host.ID, f.b.name, aRec.Code, aRec.Body.String())
			}
			if strings.Contains(aRec.Body.String(), buyer) {
				t.Fatalf("%s's 404 body echoed %s's sale buyer_name %q: %s", f.a.name, f.b.name, buyer, aRec.Body.String())
			}
		})

		t.Run(cred.kind+"/purchase_create_readback", func(t *testing.T) {
			itemName := fmt.Sprintf("%s's %s lawnmower", f.b.name, cred.kind)
			itemRec := f.request(t, cred, http.MethodPost, "/api/v1/items", itemCreateBody(itemName))
			if itemRec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create item to hang a purchase on: status = %d, want 201: %s", f.b.name, cred.kind, itemRec.Code, itemRec.Body.String())
			}
			var host itemResponse
			mustDecode(t, itemRec, &host)
			if host.ID == "" {
				t.Fatalf("%s (%s) create item returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			vendor := fmt.Sprintf("%s's %s vendor", f.b.name, cred.kind)
			rec := f.request(t, cred, http.MethodPost, "/api/v1/items/"+host.ID+"/purchase", purchaseCreateBody(vendor))
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create purchase on own item: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var createdPurchase purchaseResponse
			mustDecode(t, rec, &createdPurchase)
			if createdPurchase.Vendor != vendor || createdPurchase.ItemID != host.ID {
				t.Fatalf("%s (%s) created purchase = %+v, want vendor %q on item %q", f.b.name, cred.kind, createdPurchase, vendor, host.ID)
			}

			ownRec := f.request(t, cred, http.MethodGet, "/api/v1/items/"+host.ID+"/purchase", nil)
			if ownRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) read own purchase back: status = %d, want 200 -- POST did not land the row in the caller's group: %s", f.b.name, cred.kind, ownRec.Code, ownRec.Body.String())
			}
			var ownPurchase purchaseResponse
			mustDecode(t, ownRec, &ownPurchase)
			if ownPurchase.Vendor != vendor {
				t.Fatalf("%s (%s) read back vendor %q, want %q", f.b.name, cred.kind, ownPurchase.Vendor, vendor)
			}

			aRec := f.srv.do(t, http.MethodGet, "/api/v1/items/"+host.ID+"/purchase", f.a.cookie, nil)
			if aRec.Code != http.StatusNotFound {
				t.Fatalf("%s GET /items/%s/purchase (a block %s just created) -> %d, want 404 -- POST wrote into the wrong group, or reading across groups is possible: %s", f.a.name, host.ID, f.b.name, aRec.Code, aRec.Body.String())
			}
			if strings.Contains(aRec.Body.String(), vendor) {
				t.Fatalf("%s's 404 body echoed %s's purchase vendor %q: %s", f.a.name, f.b.name, vendor, aRec.Body.String())
			}
		})

		t.Run(cred.kind+"/identifications_create_readback", func(t *testing.T) {
			itemName := fmt.Sprintf("%s's %s toaster", f.b.name, cred.kind)
			itemRec := f.request(t, cred, http.MethodPost, "/api/v1/items", itemCreateBody(itemName))
			if itemRec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create item to hang an identification on: status = %d, want 201: %s", f.b.name, cred.kind, itemRec.Code, itemRec.Body.String())
			}
			var host itemResponse
			mustDecode(t, itemRec, &host)
			if host.ID == "" {
				t.Fatalf("%s (%s) create item returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			value := fmt.Sprintf("%s's %s barcode", f.b.name, cred.kind)
			rec := f.request(t, cred, http.MethodPost, "/api/v1/items/"+host.ID+"/identifications", identificationCreateBody("barcode", value))
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create identification on own item: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var created identificationResponse
			mustDecode(t, rec, &created)
			if created.Value != value || created.ItemID != host.ID {
				t.Fatalf("%s (%s) created identification = %+v, want value %q on item %q", f.b.name, cred.kind, created, value, host.ID)
			}
			if created.ID == "" {
				t.Fatalf("%s (%s) identification create returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			ownRec := f.request(t, cred, http.MethodGet, "/api/v1/items/"+host.ID+"/identifications", nil)
			if ownRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) read own identification list back: status = %d, want 200 -- POST did not land the row in the caller's group: %s", f.b.name, cred.kind, ownRec.Code, ownRec.Body.String())
			}
			var ownList identificationListResponse
			mustDecode(t, ownRec, &ownList)
			found := false
			for _, row := range ownList.Identifications {
				if row.ID == created.ID {
					found = true
					if row.Value != value {
						t.Fatalf("%s (%s) read back identification %+v, want value %q", f.b.name, cred.kind, row, value)
					}
				}
			}
			if !found {
				t.Fatalf("%s (%s)'s newly created identification %q is absent from its own item's list -- POST did not land the row in the caller's group", f.b.name, cred.kind, created.ID)
			}

			aListRec := f.srv.do(t, http.MethodGet, "/api/v1/items/"+host.ID+"/identifications", f.a.cookie, nil)
			if aListRec.Code != http.StatusNotFound {
				t.Fatalf("%s GET /items/%s/identifications (an item %s just created) -> %d, want 404 -- POST wrote into the wrong group, or reading across groups is possible: %s", f.a.name, host.ID, f.b.name, aListRec.Code, aListRec.Body.String())
			}
			if strings.Contains(aListRec.Body.String(), value) {
				t.Fatalf("%s's 404 body echoed %s's identification value %q: %s", f.a.name, f.b.name, value, aListRec.Body.String())
			}
			aPutRec := f.srv.do(t, http.MethodPut, "/api/v1/items/"+host.ID+"/identifications/"+created.ID, f.a.cookie,
				identificationUpdateBody("serial", "pwned", 1))
			if aPutRec.Code != http.StatusNotFound {
				t.Fatalf("%s PUT /items/%s/identifications/%s (a row %s just created) -> %d, want 404: %s", f.a.name, host.ID, created.ID, f.b.name, aPutRec.Code, aPutRec.Body.String())
			}
		})

		t.Run(cred.kind+"/item_custom_fields_create_readback", func(t *testing.T) {
			itemName := fmt.Sprintf("%s's %s lamp", f.b.name, cred.kind)
			itemRec := f.request(t, cred, http.MethodPost, "/api/v1/items", itemCreateBody(itemName))
			if itemRec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create item to hang a custom field on: status = %d, want 201: %s", f.b.name, cred.kind, itemRec.Code, itemRec.Body.String())
			}
			var host itemResponse
			mustDecode(t, itemRec, &host)
			if host.ID == "" {
				t.Fatalf("%s (%s) create item returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			value := fmt.Sprintf("%s's %s colour", f.b.name, cred.kind)
			rec := f.request(t, cred, http.MethodPost, "/api/v1/items/"+host.ID+"/custom-fields", itemCustomFieldCreateBody("Colour", value))
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create custom field on own item: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var created itemCustomFieldResponse
			mustDecode(t, rec, &created)
			if created.TextValue == nil || *created.TextValue != value || created.ItemID != host.ID {
				t.Fatalf("%s (%s) created custom field = %+v, want text_value %q on item %q", f.b.name, cred.kind, created, value, host.ID)
			}
			if created.ID == "" {
				t.Fatalf("%s (%s) custom field create returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			ownRec := f.request(t, cred, http.MethodGet, "/api/v1/items/"+host.ID+"/custom-fields", nil)
			if ownRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) read own custom field list back: status = %d, want 200 -- POST did not land the row in the caller's group: %s", f.b.name, cred.kind, ownRec.Code, ownRec.Body.String())
			}
			var ownList itemCustomFieldListResponse
			mustDecode(t, ownRec, &ownList)
			found := false
			for _, row := range ownList.CustomFields {
				if row.ID == created.ID {
					found = true
					if row.TextValue == nil || *row.TextValue != value {
						t.Fatalf("%s (%s) read back custom field %+v, want text_value %q", f.b.name, cred.kind, row, value)
					}
				}
			}
			if !found {
				t.Fatalf("%s (%s)'s newly created custom field %q is absent from its own item's list -- POST did not land the row in the caller's group", f.b.name, cred.kind, created.ID)
			}

			aListRec := f.srv.do(t, http.MethodGet, "/api/v1/items/"+host.ID+"/custom-fields", f.a.cookie, nil)
			if aListRec.Code != http.StatusNotFound {
				t.Fatalf("%s GET /items/%s/custom-fields (an item %s just created) -> %d, want 404 -- POST wrote into the wrong group, or reading across groups is possible: %s", f.a.name, host.ID, f.b.name, aListRec.Code, aListRec.Body.String())
			}
			if strings.Contains(aListRec.Body.String(), value) {
				t.Fatalf("%s's 404 body echoed %s's custom field value %q: %s", f.a.name, f.b.name, value, aListRec.Body.String())
			}
			aPutRec := f.srv.do(t, http.MethodPut, "/api/v1/items/"+host.ID+"/custom-fields/"+created.ID, f.a.cookie,
				itemCustomFieldUpdateBody("Colour", "pwned", 1))
			if aPutRec.Code != http.StatusNotFound {
				t.Fatalf("%s PUT /items/%s/custom-fields/%s (a row %s just created) -> %d, want 404: %s", f.a.name, host.ID, created.ID, f.b.name, aPutRec.Code, aPutRec.Body.String())
			}
		})

		t.Run(cred.kind+"/stock_adjustments_create_readback", func(t *testing.T) {
			itemName := fmt.Sprintf("%s's %s kettle", f.b.name, cred.kind)
			itemRec := f.request(t, cred, http.MethodPost, "/api/v1/items", itemCreateBody(itemName))
			if itemRec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create item to hang a stock adjustment on: status = %d, want 201: %s", f.b.name, cred.kind, itemRec.Code, itemRec.Body.String())
			}
			var host itemResponse
			mustDecode(t, itemRec, &host)
			if host.ID == "" {
				t.Fatalf("%s (%s) create item returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			reason := fmt.Sprintf("%s's %s restock", f.b.name, cred.kind)
			rec := f.request(t, cred, http.MethodPost, "/api/v1/items/"+host.ID+"/stock-adjustments", stockAdjustmentCreateBody(4, reason, "note"))
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create stock adjustment on own item: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var created stockAdjustmentResponse
			mustDecode(t, rec, &created)
			if created.Reason != reason || created.ItemID != host.ID {
				t.Fatalf("%s (%s) created stock adjustment = %+v, want reason %q on item %q", f.b.name, cred.kind, created, reason, host.ID)
			}
			if created.ID == "" {
				t.Fatalf("%s (%s) stock adjustment create returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			ownRec := f.request(t, cred, http.MethodGet, "/api/v1/items/"+host.ID+"/stock-adjustments", nil)
			if ownRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) read own stock adjustment list back: status = %d, want 200 -- POST did not land the row in the caller's group: %s", f.b.name, cred.kind, ownRec.Code, ownRec.Body.String())
			}
			var ownList stockAdjustmentListResponse
			mustDecode(t, ownRec, &ownList)
			found := false
			for _, row := range ownList.StockAdjustments {
				if row.ID == created.ID {
					found = true
					if row.Reason != reason {
						t.Fatalf("%s (%s) read back stock adjustment %+v, want reason %q", f.b.name, cred.kind, row, reason)
					}
				}
			}
			if !found {
				t.Fatalf("%s (%s)'s newly created stock adjustment %q is absent from its own item's list -- POST did not land the row in the caller's group", f.b.name, cred.kind, created.ID)
			}

			aListRec := f.srv.do(t, http.MethodGet, "/api/v1/items/"+host.ID+"/stock-adjustments", f.a.cookie, nil)
			if aListRec.Code != http.StatusNotFound {
				t.Fatalf("%s GET /items/%s/stock-adjustments (an item %s just created) -> %d, want 404 -- POST wrote into the wrong group, or reading across groups is possible: %s", f.a.name, host.ID, f.b.name, aListRec.Code, aListRec.Body.String())
			}
			if strings.Contains(aListRec.Body.String(), reason) {
				t.Fatalf("%s's 404 body echoed %s's stock adjustment reason %q: %s", f.a.name, f.b.name, reason, aListRec.Body.String())
			}
		})

		t.Run(cred.kind+"/custom_field_defs_create_readback", func(t *testing.T) {
			name := fmt.Sprintf("%s's %s custom field", f.b.name, cred.kind)
			rec := f.request(t, cred, http.MethodPost, "/api/v1/custom-field-defs", customFieldDefCreateBody(name, "text", 0))
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create custom field def: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var created customFieldDefResponse
			mustDecode(t, rec, &created)
			if created.Name != name {
				t.Fatalf("%s (%s) created custom field def = %+v, want name %q", f.b.name, cred.kind, created, name)
			}
			if created.ID == "" {
				t.Fatalf("%s (%s) custom field def create returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			listRec := f.request(t, cred, http.MethodGet, "/api/v1/custom-field-defs", nil)
			if listRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) list own custom field defs after create: status = %d, want 200: %s", f.b.name, cred.kind, listRec.Code, listRec.Body.String())
			}
			var bList customFieldDefListResponse
			mustDecode(t, listRec, &bList)
			found := false
			for _, d := range bList.CustomFieldDefs {
				if d.ID == created.ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s (%s)'s newly created custom field def %q is absent from %s's own listing -- POST /custom-field-defs did not land the row in the caller's group", f.b.name, cred.kind, created.ID, f.b.name)
			}

			aRec := f.srv.do(t, http.MethodGet, "/api/v1/custom-field-defs", f.a.cookie, nil)
			if aRec.Code != http.StatusOK {
				t.Fatalf("%s list own custom field defs: status = %d, want 200: %s", f.a.name, aRec.Code, aRec.Body.String())
			}
			var aList customFieldDefListResponse
			mustDecode(t, aRec, &aList)
			for _, d := range aList.CustomFieldDefs {
				if d.ID == created.ID {
					t.Fatalf("%s (%s)'s newly created custom field def %q leaked into %s's listing -- cross-tenant create leak in POST /custom-field-defs", f.b.name, cred.kind, created.ID, f.a.name)
				}
			}
		})

		t.Run(cred.kind+"/labels_create_readback", func(t *testing.T) {
			name := fmt.Sprintf("%s's %s label", f.b.name, cred.kind)
			rec := f.request(t, cred, http.MethodPost, "/api/v1/labels", labelCreateBody(name, "#abcdef"))
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create label: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var created labelResponse
			mustDecode(t, rec, &created)
			if created.Name != name {
				t.Fatalf("%s (%s) created label = %+v, want name %q", f.b.name, cred.kind, created, name)
			}
			if created.ID == "" {
				t.Fatalf("%s (%s) label create returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			listRec := f.request(t, cred, http.MethodGet, "/api/v1/labels", nil)
			if listRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) list own labels after create: status = %d, want 200: %s", f.b.name, cred.kind, listRec.Code, listRec.Body.String())
			}
			var bList labelListResponse
			mustDecode(t, listRec, &bList)
			found := false
			for _, l := range bList.Labels {
				if l.ID == created.ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s (%s)'s newly created label %q is absent from %s's own listing -- POST /labels did not land the row in the caller's group", f.b.name, cred.kind, created.ID, f.b.name)
			}

			aRec := f.srv.do(t, http.MethodGet, "/api/v1/labels", f.a.cookie, nil)
			if aRec.Code != http.StatusOK {
				t.Fatalf("%s list own labels: status = %d, want 200: %s", f.a.name, aRec.Code, aRec.Body.String())
			}
			var aList labelListResponse
			mustDecode(t, aRec, &aList)
			for _, l := range aList.Labels {
				if l.ID == created.ID {
					t.Fatalf("%s (%s)'s newly created label %q leaked into %s's listing -- cross-tenant create leak in POST /labels", f.b.name, cred.kind, created.ID, f.a.name)
				}
			}
		})

		t.Run(cred.kind+"/locations_create_readback", func(t *testing.T) {
			name := fmt.Sprintf("%s's %s attic", f.b.name, cred.kind)
			rec := f.request(t, cred, http.MethodPost, "/api/v1/locations", locationCreateBody(name, ""))
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s (%s) create location: status = %d, want 201: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var created locationResponse
			mustDecode(t, rec, &created)
			if created.Name != name {
				t.Fatalf("%s (%s) created location = %+v, want name %q", f.b.name, cred.kind, created, name)
			}
			if created.ID == "" {
				t.Fatalf("%s (%s) location create returned no id; every check below would name the empty string", f.b.name, cred.kind)
			}

			listRec := f.request(t, cred, http.MethodGet, "/api/v1/locations", nil)
			if listRec.Code != http.StatusOK {
				t.Fatalf("%s (%s) list own locations after create: status = %d, want 200: %s", f.b.name, cred.kind, listRec.Code, listRec.Body.String())
			}
			var bList locationListResponse
			mustDecode(t, listRec, &bList)
			found := false
			for _, loc := range bList.Locations {
				if loc.ID == created.ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s (%s)'s newly created location %q is absent from %s's own listing -- POST /locations did not land the row in the caller's group", f.b.name, cred.kind, created.ID, f.b.name)
			}

			aRec := f.srv.do(t, http.MethodGet, "/api/v1/locations", f.a.cookie, nil)
			if aRec.Code != http.StatusOK {
				t.Fatalf("%s list own locations: status = %d, want 200: %s", f.a.name, aRec.Code, aRec.Body.String())
			}
			var aList locationListResponse
			mustDecode(t, aRec, &aList)
			for _, loc := range aList.Locations {
				if loc.ID == created.ID {
					t.Fatalf("%s (%s)'s newly created location %q leaked into %s's listing -- cross-tenant create leak in POST /locations", f.b.name, cred.kind, created.ID, f.a.name)
				}
			}
		})
	}

	reportsA := provisionReportsFixture(t, f.srv, f.a)
	reportsB := provisionReportsFixture(t, f.srv, f.b)

	for _, cred := range []credential{cookieCredential, bearerCredential} {
		t.Run(cred.kind+"/reports_valuation", func(t *testing.T) {
			rec := f.request(t, cred, http.MethodGet, "/api/v1/reports/valuation?group_by=label", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s (%s) reports/valuation: status = %d, want 200: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var got reportValuationResponse
			mustDecode(t, rec, &got)

			var sawB bool
			for _, row := range got.Rows {
				if row.GroupKey == f.a.labelID {
					t.Fatalf("%s (%s)'s valuation-by-label report contains %s's label id %q -- cross-tenant leak in GET /reports/valuation", f.b.name, cred.kind, f.a.name, f.a.labelID)
				}
				if row.GroupKey == f.b.labelID {
					sawB = true
					if row.GroupLabel != f.b.labelName {
						t.Fatalf("%s (%s)'s own valuation row for label %q has group_label %q, want %q", f.b.name, cred.kind, f.b.labelID, row.GroupLabel, f.b.labelName)
					}
					if row.ItemCount != 2 {
						t.Fatalf("%s (%s)'s own valuation row for label %q has item_count %d, want 2 (%s's original item plus the report-fixture item both carry this label)", f.b.name, cred.kind, f.b.labelID, row.ItemCount, f.b.name)
					}
					if row.TotalValueMinor != reportsB.purchasePriceMinor {
						t.Fatalf("%s (%s)'s own valuation row for label %q has total_value_minor %d, want %d (only the report-fixture item carries a price; %s's original item carries none)", f.b.name, cred.kind, f.b.labelID, row.TotalValueMinor, reportsB.purchasePriceMinor, f.b.name)
					}
				}
			}
			if !sawB {
				t.Fatalf("%s (%s)'s valuation-by-label report does not contain %s's own label %q; the exclusion check above would then hold vacuously", f.b.name, cred.kind, f.b.name, f.b.labelID)
			}
		})

		t.Run(cred.kind+"/reports_warranty_expiring", func(t *testing.T) {
			rec := f.request(t, cred, http.MethodGet, "/api/v1/reports/warranty-expiring", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s (%s) reports/warranty-expiring: status = %d, want 200: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var got reportWarrantyExpiringResponse
			mustDecode(t, rec, &got)

			var sawB bool
			for _, row := range got.Rows {
				if row.ItemID == reportsA.itemID {
					t.Fatalf("%s (%s)'s warranty-expiring report contains %s's item id %q -- cross-tenant leak in GET /reports/warranty-expiring", f.b.name, cred.kind, f.a.name, reportsA.itemID)
				}
				if row.ItemID == reportsB.itemID {
					sawB = true
					if row.ItemName != reportsB.itemName || row.ExpiresOn != reportsB.warrantyExpiresOn {
						t.Fatalf("%s (%s)'s own warranty-expiring row = %+v, want item_name %q expires_on %q", f.b.name, cred.kind, row, reportsB.itemName, reportsB.warrantyExpiresOn)
					}
				}
			}
			if !sawB {
				t.Fatalf("%s (%s)'s warranty-expiring report does not contain %s's own item %q; the exclusion check above would then hold vacuously", f.b.name, cred.kind, f.b.name, reportsB.itemID)
			}
		})

		t.Run(cred.kind+"/reports_purchases", func(t *testing.T) {
			rec := f.request(t, cred, http.MethodGet,
				"/api/v1/reports/purchases?from=2026-01-01&to=2026-12-31", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s (%s) reports/purchases: status = %d, want 200: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var got reportPurchasesResponse
			mustDecode(t, rec, &got)

			var sawB bool
			for _, row := range got.Rows {
				if row.ItemID == reportsA.itemID {
					t.Fatalf("%s (%s)'s purchases report contains %s's item id %q -- cross-tenant leak in GET /reports/purchases", f.b.name, cred.kind, f.a.name, reportsA.itemID)
				}
				if row.Vendor == reportsA.purchaseVendor {
					t.Fatalf("%s (%s)'s purchases report contains %s's vendor %q -- cross-tenant leak in GET /reports/purchases", f.b.name, cred.kind, f.a.name, reportsA.purchaseVendor)
				}
				if row.ItemID == reportsB.itemID {
					sawB = true
					if row.ItemName != reportsB.itemName || row.Vendor != reportsB.purchaseVendor ||
						row.PurchasedOn != reportsB.purchasedOn || row.PurchasePriceMinor != reportsB.purchasePriceMinor {
						t.Fatalf("%s (%s)'s own purchases row = %+v, want item_name %q vendor %q purchased_on %q purchase_price_minor %d", f.b.name, cred.kind, row, reportsB.itemName, reportsB.purchaseVendor, reportsB.purchasedOn, reportsB.purchasePriceMinor)
					}
				}
			}
			if !sawB {
				t.Fatalf("%s (%s)'s purchases report does not contain %s's own item %q; the exclusion check above would then hold vacuously", f.b.name, cred.kind, f.b.name, reportsB.itemID)
			}
		})

		t.Run(cred.kind+"/reports_item_count_by_location", func(t *testing.T) {
			rec := f.request(t, cred, http.MethodGet, "/api/v1/reports/item-count-by-location", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s (%s) reports/item-count-by-location: status = %d, want 200: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			var got reportItemCountByLocationResponse
			mustDecode(t, rec, &got)

			var sawB bool
			for _, row := range got.Rows {
				if row.LocationID == f.a.locationID {
					t.Fatalf("%s (%s)'s item-count-by-location report contains %s's location id %q -- cross-tenant leak in GET /reports/item-count-by-location", f.b.name, cred.kind, f.a.name, f.a.locationID)
				}
				if row.LocationID == f.b.locationID {
					sawB = true
					if row.LocationName != f.b.locationName {
						t.Fatalf("%s (%s)'s own item-count-by-location row for location %q has location_name %q, want %q", f.b.name, cred.kind, f.b.locationID, row.LocationName, f.b.locationName)
					}
					if row.ItemCount != 1 {
						t.Fatalf("%s (%s)'s own item-count-by-location row for location %q has item_count %d, want 1 (only the report-fixture item is placed there; %s's original item is unplaced)", f.b.name, cred.kind, f.b.locationID, row.ItemCount, f.b.name)
					}
				}
			}
			if !sawB {
				t.Fatalf("%s (%s)'s item-count-by-location report does not contain %s's own location %q; the exclusion check above would then hold vacuously", f.b.name, cred.kind, f.b.name, f.b.locationID)
			}
		})

		t.Run(cred.kind+"/export_bom", func(t *testing.T) {
			parseBoMCSV := func(t *testing.T, rec *httptest.ResponseRecorder) [][]string {
				t.Helper()
				records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
				if err != nil {
					t.Fatalf("%s (%s) export bom: response body is not valid CSV: %v; body = %q", f.b.name, cred.kind, err, rec.Body.String())
				}
				if len(records) == 0 {
					t.Fatalf("%s (%s) export bom: response body has no header row at all", f.b.name, cred.kind)
				}
				return records
			}

			rec := f.request(t, cred, http.MethodGet,
				"/api/v1/export/bom?item_id="+reportsA.itemID+"&item_id="+reportsB.itemID, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s (%s) export bom (item_id mode): status = %d, want 200: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			records := parseBoMCSV(t, rec)
			header := records[0]
			idCol := slices.Index(header, "item_id")
			nameCol := slices.Index(header, "item_name")
			valueCol := slices.Index(header, "value_minor")
			if idCol < 0 || nameCol < 0 || valueCol < 0 {
				t.Fatalf("%s (%s) export bom (item_id mode): header %v is missing item_id/item_name/value_minor", f.b.name, cred.kind, header)
			}
			var sawB bool
			for _, row := range records[1:] {
				if row[idCol] == reportsA.itemID {
					t.Fatalf("%s (%s) export bom (item_id mode), naming %s's real item id %q explicitly alongside its own, contains it anyway -- cross-tenant leak in GET /export/bom", f.b.name, cred.kind, f.a.name, reportsA.itemID)
				}
				if row[idCol] == reportsB.itemID {
					sawB = true
					if row[nameCol] != reportsB.itemName {
						t.Fatalf("%s (%s) export bom (item_id mode): own row for item %q has item_name %q, want %q", f.b.name, cred.kind, reportsB.itemID, row[nameCol], reportsB.itemName)
					}
					if row[valueCol] != strconv.FormatInt(reportsB.purchasePriceMinor, 10) {
						t.Fatalf("%s (%s) export bom (item_id mode): own row for item %q has value_minor %q, want %d", f.b.name, cred.kind, reportsB.itemID, row[valueCol], reportsB.purchasePriceMinor)
					}
				}
			}
			if !sawB {
				t.Fatalf("%s (%s) export bom (item_id mode): response does not contain %s's own item %q even though it was named in the same request as %s's; the exclusion check above would then hold vacuously", f.b.name, cred.kind, f.b.name, reportsB.itemID, f.a.name)
			}
			if got := len(records) - 1; got != 1 {
				t.Fatalf("%s (%s) export bom (item_id mode): %d data row(s), want exactly 1 (%s's own item; %s's was named but must be silently omitted)", f.b.name, cred.kind, got, f.b.name, f.a.name)
			}

			rec = f.request(t, cred, http.MethodGet,
				"/api/v1/export/bom?location_id="+f.a.locationID+"&location_id="+f.b.locationID, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s (%s) export bom (location_id mode): status = %d, want 200: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			records = parseBoMCSV(t, rec)
			header = records[0]
			idCol = slices.Index(header, "item_id")
			if idCol < 0 {
				t.Fatalf("%s (%s) export bom (location_id mode): header %v is missing item_id", f.b.name, cred.kind, header)
			}
			sawB = false
			for _, row := range records[1:] {
				if row[idCol] == reportsA.itemID {
					t.Fatalf("%s (%s) export bom (location_id mode), naming %s's real location id %q explicitly alongside its own, contains %s's item anyway -- cross-tenant leak in GET /export/bom", f.b.name, cred.kind, f.a.name, f.a.locationID, f.a.name)
				}
				if row[idCol] == reportsB.itemID {
					sawB = true
				}
			}
			if !sawB {
				t.Fatalf("%s (%s) export bom (location_id mode): response does not contain %s's own item %q even though its own location %q was named in the same request as %s's; the exclusion check above would then hold vacuously", f.b.name, cred.kind, f.b.name, reportsB.itemID, f.b.locationID, f.a.name)
			}

			rec = f.request(t, cred, http.MethodGet,
				"/api/v1/export/bom?label_id="+f.a.labelID+"&label_id="+f.b.labelID, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s (%s) export bom (label_id mode): status = %d, want 200: %s", f.b.name, cred.kind, rec.Code, rec.Body.String())
			}
			records = parseBoMCSV(t, rec)
			header = records[0]
			idCol = slices.Index(header, "item_id")
			if idCol < 0 {
				t.Fatalf("%s (%s) export bom (label_id mode): header %v is missing item_id", f.b.name, cred.kind, header)
			}
			sawB = false
			for _, row := range records[1:] {
				if row[idCol] == reportsA.itemID {
					t.Fatalf("%s (%s) export bom (label_id mode), naming %s's real label id %q explicitly alongside its own, contains %s's item anyway -- cross-tenant leak in GET /export/bom", f.b.name, cred.kind, f.a.name, f.a.labelID, f.a.name)
				}
				if row[idCol] == reportsB.itemID {
					sawB = true
				}
			}
			if !sawB {
				t.Fatalf("%s (%s) export bom (label_id mode): response does not contain %s's own item %q even though its own label %q was named in the same request as %s's; the exclusion check above would then hold vacuously", f.b.name, cred.kind, f.b.name, reportsB.itemID, f.b.labelID, f.a.name)
			}
		})
	}

	importAID := stageOwnExportAsImport(t, f.srv, f.a)
	importBID := stageOwnExportAsImport(t, f.srv, f.b)

	for _, cred := range []credential{cookieCredential, bearerCredential} {
		t.Run(cred.kind+"/import_preview", func(t *testing.T) {
			f.assertCrossGroupNotFound(t, cred, http.MethodGet,
				"/api/v1/import/"+importAID+"/preview", importAID)
		})
		t.Run(cred.kind+"/import_commit", func(t *testing.T) {
			f.assertCrossGroupNotFound(t, cred, http.MethodPost,
				"/api/v1/import/"+importAID+"/commit", importAID)
		})

		t.Run(cred.kind+"/import_native_upload_symmetry", func(t *testing.T) {
			assertImportUnreachable(t, f.srv, cred, f.a, f.b, http.MethodGet,
				"/api/v1/import/"+importBID+"/preview", importBID)
			assertImportUnreachable(t, f.srv, cred, f.a, f.b, http.MethodPost,
				"/api/v1/import/"+importBID+"/commit", importBID)
		})
	}

	t.Run("import_group_a_untouched", func(t *testing.T) {
		previewRec := f.srv.do(t, http.MethodGet, "/api/v1/import/"+importAID+"/preview", f.a.cookie, nil)
		if previewRec.Code != http.StatusOK {
			t.Fatalf("%s's own import preview after %s's cross-tenant attempts: status = %d, want 200 -- the session was deleted or otherwise mutated by an attempt that should have been a pure no-op: %s", f.a.name, f.b.name, previewRec.Code, previewRec.Body.String())
		}
		var preview importPreviewResponseMirror
		mustDecode(t, previewRec, &preview)
		for _, row := range preview.Rows {
			if row.Action != "unchanged" {
				t.Fatalf("%s's own import preview row %+v after %s's cross-tenant attempts: Action = %q, want %q -- this file round-trips %s's own current export, so anything other than \"unchanged\" means the staged file or the group's own item data moved", f.a.name, row, f.b.name, row.Action, "unchanged", f.a.name)
			}
		}
		if preview.Summary.Create != 0 || preview.Summary.Update != 0 || preview.Summary.Error != 0 || preview.Summary.Unchanged != len(preview.Rows) {
			t.Fatalf("%s's own import preview summary after %s's cross-tenant attempts = %+v, want {Create:0 Update:0 Unchanged:%d Error:0}", f.a.name, f.b.name, preview.Summary, len(preview.Rows))
		}

		commitRec := f.srv.do(t, http.MethodPost, "/api/v1/import/"+importAID+"/commit", f.a.cookie, nil)
		if commitRec.Code != http.StatusOK {
			t.Fatalf("%s's own import commit after %s's cross-tenant attempts: status = %d, want 200: %s", f.a.name, f.b.name, commitRec.Code, commitRec.Body.String())
		}
		var commit importCommitResponseMirror
		mustDecode(t, commitRec, &commit)
		if commit.Summary.Created != 0 || commit.Summary.Updated != 0 || commit.Summary.Unchanged != len(preview.Rows) || len(commit.CreatedItems) != 0 {
			t.Fatalf("%s's own import commit summary after %s's cross-tenant attempts = %+v, want {Created:0 Updated:0 Unchanged:%d} and no created_items -- a cross-tenant attempt that should have been rejected outright left a partial write behind", f.a.name, f.b.name, commit.Summary, len(preview.Rows))
		}
	})

	t.Run("cookie/device_tokens_create_readback", func(t *testing.T) {
		rec := f.srv.do(t, http.MethodPost, "/api/v1/auth/device-tokens", f.b.cookie,
			[]byte(`{"device_label":"carol's tablet"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s create device token: status = %d, want 201: %s", f.b.name, rec.Code, rec.Body.String())
		}
		var created deviceTokenIssueResponse
		mustDecode(t, rec, &created)

		listRec := f.srv.do(t, http.MethodGet, "/api/v1/auth/device-tokens", f.b.cookie, nil)
		if listRec.Code != http.StatusOK {
			t.Fatalf("%s list own device tokens after create: status = %d, want 200: %s", f.b.name, listRec.Code, listRec.Body.String())
		}
		var bList deviceTokenListResponse
		mustDecode(t, listRec, &bList)
		found := false
		for _, d := range bList.DeviceTokens {
			if d.ID == created.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s's newly created device token %q is absent from %s's own listing -- POST /auth/device-tokens did not land the row in the caller's group", f.b.name, created.ID, f.b.name)
		}

		aRec := f.srv.do(t, http.MethodGet, "/api/v1/auth/device-tokens", f.a.cookie, nil)
		if aRec.Code != http.StatusOK {
			t.Fatalf("%s list own device tokens: status = %d, want 200: %s", f.a.name, aRec.Code, aRec.Body.String())
		}
		var aList deviceTokenListResponse
		mustDecode(t, aRec, &aList)
		for _, d := range aList.DeviceTokens {
			if d.ID == created.ID {
				t.Fatalf("%s's newly created device token %q leaked into %s's listing -- cross-tenant create leak in POST /auth/device-tokens", f.b.name, created.ID, f.a.name)
			}
		}
	})

	t.Run("bearer/device_tokens_issue_is_cookie_only", func(t *testing.T) {
		rec := f.request(t, bearerCredential, http.MethodPost, "/api/v1/auth/device-tokens",
			[]byte(`{"device_label":"a token minting a token"}`))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("bearer POST /auth/device-tokens: status = %d, want 401 -- issuance is cookie-only by design, and this is NOT a tenant-isolation outcome", rec.Code)
		}
	})

	t.Run("group_a_resources_untouched", func(t *testing.T) {
		rec := f.srv.do(t, http.MethodGet, "/api/v1/auth/sessions", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's session after %s's cross-tenant revoke attempts: status = %d, want 200 -- an attempt succeeded silently", f.a.name, f.b.name, rec.Code)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/auth/device-tokens", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's device-token list: status = %d, want 200", f.a.name, rec.Code)
		}
		var devices deviceTokenListResponse
		mustDecode(t, rec, &devices)
		if len(devices.DeviceTokens) != 1 || devices.DeviceTokens[0].Revoked {
			t.Fatalf("%s's device token was revoked by %s's cross-tenant attempts: %+v", f.a.name, f.b.name, devices.DeviceTokens)
		}

		rec = f.srv.do(t, http.MethodPost, "/api/v1/invites/redeem", "",
			redeemBody(f.a.inviteToken, "dave", "dave-password-1"))
		if rec.Code != http.StatusCreated {
			t.Fatalf("redeeming %s's invite after %s's cross-tenant revoke attempts: status = %d, want 201 -- an attempt silently revoked it: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var redeemed inviteRedeemResponse
		mustDecode(t, rec, &redeemed)
		if redeemed.GroupID != f.a.groupID {
			t.Fatalf("redeeming %s's invite landed the new user in group %q, want %s's group %q", f.a.name, redeemed.GroupID, f.a.name, f.a.groupID)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID, f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own item after %s's cross-tenant PUT and DELETE attempts: status = %d, want 200: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aItem itemResponse
		mustDecode(t, rec, &aItem)
		if aItem.Name != f.a.itemName || aItem.Version != 2 || aItem.Quantity != f.a.itemQuantityAfterAdjustment {
			t.Fatalf("%s's item is now {name:%q version:%d quantity:%d}, want {name:%q version:2 quantity:%d} -- %s's cross-tenant PUT answered 404 but the write landed anyway", f.a.name, aItem.Name, aItem.Version, aItem.Quantity, f.a.itemName, f.a.itemQuantityAfterAdjustment, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID+"/warranty", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own warranty block after %s's cross-tenant PUT and DELETE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aWarranty warrantyResponse
		mustDecode(t, rec, &aWarranty)
		if aWarranty.Holder != f.a.warrantyHolder || aWarranty.Version != 1 {
			t.Fatalf("%s's warranty block is now {holder:%q version:%d}, want {holder:%q version:1} -- %s's cross-tenant write answered 404 but landed anyway", f.a.name, aWarranty.Holder, aWarranty.Version, f.a.warrantyHolder, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID+"/sale", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own sale block after %s's cross-tenant PUT and DELETE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aSale saleResponse
		mustDecode(t, rec, &aSale)
		if aSale.BuyerName != f.a.saleBuyerName || aSale.Version != 1 {
			t.Fatalf("%s's sale block is now {buyer_name:%q version:%d}, want {buyer_name:%q version:1} -- %s's cross-tenant write answered 404 but landed anyway", f.a.name, aSale.BuyerName, aSale.Version, f.a.saleBuyerName, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID+"/purchase", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own purchase block after %s's cross-tenant PUT and DELETE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aPurchase purchaseResponse
		mustDecode(t, rec, &aPurchase)
		if aPurchase.Vendor != f.a.purchaseVendor || aPurchase.Version != 1 {
			t.Fatalf("%s's purchase block is now {vendor:%q version:%d}, want {vendor:%q version:1} -- %s's cross-tenant write answered 404 but landed anyway", f.a.name, aPurchase.Vendor, aPurchase.Version, f.a.purchaseVendor, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID+"/identifications", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own identification list after %s's cross-tenant PUT and DELETE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aIdentifications identificationListResponse
		mustDecode(t, rec, &aIdentifications)
		var aIdent identificationResponse
		found := false
		for _, row := range aIdentifications.Identifications {
			if row.ID == f.a.identificationID {
				found, aIdent = true, row
				break
			}
		}
		if !found {
			t.Fatalf("%s's identification row %q is gone after %s's cross-tenant DELETE attempt answered 404 -- the tombstone landed anyway: %+v", f.a.name, f.a.identificationID, f.b.name, aIdentifications.Identifications)
		}
		if aIdent.Value != f.a.identificationValue || aIdent.Version != 1 {
			t.Fatalf("%s's identification row is now {value:%q version:%d}, want {value:%q version:1} -- %s's cross-tenant PUT answered 404 but landed anyway", f.a.name, aIdent.Value, aIdent.Version, f.a.identificationValue, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/groups/detail-visibility", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own group visibility after %s's own-group toggles: status = %d, want 200: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aVisibility groupVisibilityResponse
		mustDecode(t, rec, &aVisibility)
		if aVisibility.WarrantyVisible || aVisibility.SaleVisible || aVisibility.PurchaseVisible || aVisibility.Version != 1 {
			t.Fatalf("%s's group visibility is now %+v, want {warranty_visible:false sale_visible:false purchase_visible:false version:1} -- %s's own-group toggle bled into %s's group", f.a.name, aVisibility, f.b.name, f.a.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/custom-field-defs", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own custom-field-def list after %s's cross-tenant PUT and DELETE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aCustomFieldDefs customFieldDefListResponse
		mustDecode(t, rec, &aCustomFieldDefs)
		var aCFD customFieldDefResponse
		foundCFD := false
		for _, row := range aCustomFieldDefs.CustomFieldDefs {
			if row.ID == f.a.customFieldDefID {
				foundCFD, aCFD = true, row
				break
			}
		}
		if !foundCFD {
			t.Fatalf("%s's custom field def %q is gone after %s's cross-tenant DELETE attempt answered 404 -- the tombstone landed anyway: %+v", f.a.name, f.a.customFieldDefID, f.b.name, aCustomFieldDefs.CustomFieldDefs)
		}
		if aCFD.Name != f.a.customFieldDefName || aCFD.Version != 1 {
			t.Fatalf("%s's custom field def is now {name:%q version:%d}, want {name:%q version:1} -- %s's cross-tenant PUT answered 404 but landed anyway", f.a.name, aCFD.Name, aCFD.Version, f.a.customFieldDefName, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID+"/custom-fields", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own custom field list after %s's cross-tenant PUT and DELETE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aCustomFields itemCustomFieldListResponse
		mustDecode(t, rec, &aCustomFields)
		var aCF itemCustomFieldResponse
		foundCF := false
		for _, row := range aCustomFields.CustomFields {
			if row.ID == f.a.itemCustomFieldID {
				foundCF, aCF = true, row
				break
			}
		}
		if !foundCF {
			t.Fatalf("%s's custom field %q is gone after %s's cross-tenant DELETE attempt answered 404 -- the tombstone landed anyway: %+v", f.a.name, f.a.itemCustomFieldID, f.b.name, aCustomFields.CustomFields)
		}
		if aCF.TextValue == nil || *aCF.TextValue != f.a.itemCustomFieldValue || aCF.Version != 1 {
			t.Fatalf("%s's custom field is now {text_value:%v version:%d}, want {text_value:%q version:1} -- %s's cross-tenant PUT answered 404 but landed anyway", f.a.name, aCF.TextValue, aCF.Version, f.a.itemCustomFieldValue, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID+"/stock-adjustments", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own stock-adjustment list after %s's cross-tenant CREATE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aStockAdjustments stockAdjustmentListResponse
		mustDecode(t, rec, &aStockAdjustments)
		if len(aStockAdjustments.StockAdjustments) != 1 {
			t.Fatalf("%s's stock-adjustment list has %d rows, want exactly 1 (the one provisionTenant created) -- %s's cross-tenant CREATE attempts answered 404 but a row landed anyway: %+v", f.a.name, len(aStockAdjustments.StockAdjustments), f.b.name, aStockAdjustments.StockAdjustments)
		}
		if aStockAdjustments.StockAdjustments[0].Delta != f.a.stockAdjustmentDelta || aStockAdjustments.StockAdjustments[0].ResultingQuantity != f.a.itemQuantityAfterAdjustment {
			t.Fatalf("%s's stock-adjustment row is now %+v, want {delta:%d resulting_quantity:%d}", f.a.name, aStockAdjustments.StockAdjustments[0], f.a.stockAdjustmentDelta, f.a.itemQuantityAfterAdjustment)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID, f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own item after %s's cross-tenant stock-adjustment CREATE attempts: status = %d, want 200: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aItemAfter itemResponse
		mustDecode(t, rec, &aItemAfter)
		if aItemAfter.Quantity != f.a.itemQuantityAfterAdjustment {
			t.Fatalf("%s's item quantity is now %d, want %d -- %s's cross-tenant stock-adjustment CREATE answered 404 but moved the quantity anyway", f.a.name, aItemAfter.Quantity, f.a.itemQuantityAfterAdjustment, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/labels", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own label list after %s's cross-tenant PUT and DELETE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aLabels labelListResponse
		mustDecode(t, rec, &aLabels)
		var aLabel labelResponse
		foundLabel := false
		for _, row := range aLabels.Labels {
			if row.ID == f.a.labelID {
				foundLabel, aLabel = true, row
				break
			}
		}
		if !foundLabel {
			t.Fatalf("%s's label %q is gone after %s's cross-tenant DELETE attempt answered 404 -- the tombstone landed anyway: %+v", f.a.name, f.a.labelID, f.b.name, aLabels.Labels)
		}
		if aLabel.Name != f.a.labelName || aLabel.Version != 1 {
			t.Fatalf("%s's label is now {name:%q version:%d}, want {name:%q version:1} -- %s's cross-tenant PUT answered 404 but landed anyway", f.a.name, aLabel.Name, aLabel.Version, f.a.labelName, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID+"/labels", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own item-label list after %s's cross-tenant DELETE attempt: status = %d, want 200 -- the attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aItemLabels labelListResponse
		mustDecode(t, rec, &aItemLabels)
		if len(aItemLabels.Labels) != 1 || aItemLabels.Labels[0].ID != f.a.labelID {
			t.Fatalf("%s's item carries %+v, want exactly its own label %q -- %s's cross-tenant DELETE answered 404 but the detach landed anyway", f.a.name, aItemLabels.Labels, f.a.labelID, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/locations", f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own location list after %s's cross-tenant PUT and DELETE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		var aLocations locationListResponse
		mustDecode(t, rec, &aLocations)
		var aLoc locationResponse
		foundLoc := false
		for _, row := range aLocations.Locations {
			if row.ID == f.a.locationID {
				foundLoc, aLoc = true, row
				break
			}
		}
		if !foundLoc {
			t.Fatalf("%s's location %q is gone after %s's cross-tenant DELETE attempt answered 404 -- the tombstone landed anyway: %+v", f.a.name, f.a.locationID, f.b.name, aLocations.Locations)
		}
		if aLoc.Name != f.a.locationName || aLoc.Version != 1 {
			t.Fatalf("%s's location is now {name:%q version:%d}, want {name:%q version:1} -- %s's cross-tenant PUT answered 404 but landed anyway", f.a.name, aLoc.Name, aLoc.Version, f.a.locationName, f.b.name)
		}

		rec = f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID+"/attachments/"+f.a.attachmentID, f.a.cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's own attachment after %s's cross-tenant DELETE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, rec.Code, rec.Body.String())
		}
		if !bytes.Equal(rec.Body.Bytes(), f.a.attachmentContent) {
			t.Fatalf("%s's attachment content changed after %s's cross-tenant DELETE attempts (got %d bytes, want %d) -- the tombstone-plus-unlink landed anyway despite the 404s above", f.a.name, f.b.name, rec.Body.Len(), len(f.a.attachmentContent))
		}

		thumbRec := f.srv.do(t, http.MethodGet, "/api/v1/items/"+f.a.itemID+"/attachments/"+f.a.attachmentID+"/thumbnail", f.a.cookie, nil)
		if thumbRec.Code != http.StatusOK {
			t.Fatalf("%s's own attachment thumbnail after %s's cross-tenant DELETE attempts: status = %d, want 200 -- an attempt succeeded silently: %s", f.a.name, f.b.name, thumbRec.Code, thumbRec.Body.String())
		}
	})
}

const wantCrossGroupStatus = http.StatusNotFound

const neverExistedItemID = "00000000-0000-7000-8000-000000000000"

type credential struct {
	kind string
	pick func(tn tenant) string
	send func(t *testing.T, s *liveServer, method, path, value string, body []byte) *httptest.ResponseRecorder
}

var (
	cookieCredential = credential{
		kind: "cookie",
		pick: func(tn tenant) string { return tn.cookie },
		send: func(t *testing.T, s *liveServer, method, path, value string, body []byte) *httptest.ResponseRecorder {
			t.Helper()
			return s.do(t, method, path, value, body)
		},
	}
	bearerCredential = credential{
		kind: "bearer",
		pick: func(tn tenant) string { return tn.bearer },
		send: func(t *testing.T, s *liveServer, method, path, value string, body []byte) *httptest.ResponseRecorder {
			t.Helper()
			return s.doBearer(t, method, path, value, body)
		},
	}
)

type tenant struct {
	name                        string
	groupID                     string
	userID                      string
	cookie                      string
	bearer                      string
	sessionID                   string
	deviceTokenID               string
	inviteID                    string
	inviteToken                 string
	itemID                      string
	itemName                    string
	warrantyHolder              string
	saleBuyerName               string
	purchaseVendor              string
	identificationID            string
	identificationValue         string
	customFieldDefID            string
	customFieldDefName          string
	itemCustomFieldID           string
	itemCustomFieldValue        string
	stockAdjustmentDelta        int64
	itemQuantityAfterAdjustment int64
	labelID                     string
	labelName                   string
	locationID                  string
	locationName                string
	attachmentID                string
	attachmentContent           []byte
}

type crossTenantFixture struct {
	srv  *liveServer
	a, b tenant
}

func newCrossTenantFixture(t *testing.T) *crossTenantFixture {
	t.Helper()
	srv := newLiveServer(t, true)
	f := &crossTenantFixture{srv: srv}
	f.a = provisionTenant(t, srv, "alice", "alice-password-1")
	f.b = provisionTenant(t, srv, "carol", "carol-password-1")

	if f.a.groupID == f.b.groupID {
		t.Fatalf("%s and %s landed in the SAME group (%q); this suite proves nothing without two independent households", f.a.name, f.b.name, f.a.groupID)
	}
	for _, p := range []struct{ what, x, y string }{
		{"session id", f.a.sessionID, f.b.sessionID},
		{"device token id", f.a.deviceTokenID, f.b.deviceTokenID},
		{"invite id", f.a.inviteID, f.b.inviteID},
		{"item id", f.a.itemID, f.b.itemID},
		{"identification id", f.a.identificationID, f.b.identificationID},
		{"custom field def id", f.a.customFieldDefID, f.b.customFieldDefID},
		{"item custom field id", f.a.itemCustomFieldID, f.b.itemCustomFieldID},
		{"label id", f.a.labelID, f.b.labelID},
		{"location id", f.a.locationID, f.b.locationID},
		{"attachment id", f.a.attachmentID, f.b.attachmentID},
	} {
		if p.x == p.y {
			t.Fatalf("both households got the same %s (%q); the exclusion assertions cannot distinguish them", p.what, p.x)
		}
	}
	return f
}

func provisionTenant(t *testing.T, srv *liveServer, username, password string) tenant {
	t.Helper()
	tn := tenant{name: username}

	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody(username, password)); rec.Code != http.StatusCreated {
		t.Fatalf("register %s: status = %d: %s", username, rec.Code, rec.Body.String())
	}

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody(username, password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var login loginResponse
	mustDecode(t, rec, &login)
	tn.groupID, tn.userID = login.GroupID, login.UserID
	tn.cookie = sessionCookie(t, rec)

	rec = srv.do(t, http.MethodGet, "/api/v1/auth/sessions", tn.cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s list sessions: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var sessions sessionListResponse
	mustDecode(t, rec, &sessions)
	if len(sessions.Sessions) != 1 {
		t.Fatalf("%s sessions = %+v, want exactly 1", username, sessions.Sessions)
	}
	tn.sessionID = sessions.Sessions[0].ID

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/device-tokens", tn.cookie,
		[]byte(fmt.Sprintf(`{"device_label":%q}`, username+"'s phone")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s issue device token: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var device deviceTokenIssueResponse
	mustDecode(t, rec, &device)
	tn.deviceTokenID, tn.bearer = device.ID, device.Token
	if tn.bearer == "" {
		t.Fatalf("%s's device-token issue returned no token; the bearer half of this suite would authenticate as nobody", username)
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/invites", tn.cookie, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s issue invite: status = %d (403 here would mean the registering user is not an owner): %s", username, rec.Code, rec.Body.String())
	}
	var inv inviteCreateResponse
	mustDecode(t, rec, &inv)
	tn.inviteID, tn.inviteToken = inv.ID, inv.Token

	tn.itemName = username + "'s stepladder"
	rec = srv.do(t, http.MethodPost, "/api/v1/items", tn.cookie, itemCreateBody(tn.itemName))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create item: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var it itemResponse
	mustDecode(t, rec, &it)
	tn.itemID = it.ID
	if tn.itemID == "" {
		t.Fatalf("%s's item create returned no id; every cross-group item probe below would name the empty string", username)
	}
	if it.Version != 1 {
		t.Fatalf("%s's freshly created item has version %d, want 1; the cross-group PUT probe below sends version 1 and would stop testing scoping", username, it.Version)
	}

	tn.warrantyHolder = username + "'s warranty holder"
	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+tn.itemID+"/warranty", tn.cookie,
		warrantyCreateBody(tn.warrantyHolder))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create warranty on own item: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var war warrantyResponse
	mustDecode(t, rec, &war)
	if war.Version != 1 {
		t.Fatalf("%s's freshly created warranty block has version %d, want 1; the cross-group PUT probe below sends version 1 and would stop testing scoping", username, war.Version)
	}
	if war.Holder != tn.warrantyHolder {
		t.Fatalf("%s's warranty block came back with holder %q, want %q; group_a_resources_untouched compares this value", username, war.Holder, tn.warrantyHolder)
	}

	tn.saleBuyerName = username + "'s sale buyer"
	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+tn.itemID+"/sale", tn.cookie,
		saleCreateBody(tn.saleBuyerName))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create sale on own item: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var sale saleResponse
	mustDecode(t, rec, &sale)
	if sale.Version != 1 {
		t.Fatalf("%s's freshly created sale block has version %d, want 1; the cross-group PUT probe below sends version 1 and would stop testing scoping", username, sale.Version)
	}
	if sale.BuyerName != tn.saleBuyerName {
		t.Fatalf("%s's sale block came back with buyer_name %q, want %q; group_a_resources_untouched compares this value", username, sale.BuyerName, tn.saleBuyerName)
	}

	tn.purchaseVendor = username + "'s purchase vendor"
	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+tn.itemID+"/purchase", tn.cookie,
		purchaseCreateBody(tn.purchaseVendor))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create purchase on own item: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var purchase purchaseResponse
	mustDecode(t, rec, &purchase)
	if purchase.Version != 1 {
		t.Fatalf("%s's freshly created purchase block has version %d, want 1; the cross-group PUT probe below sends version 1 and would stop testing scoping", username, purchase.Version)
	}
	if purchase.Vendor != tn.purchaseVendor {
		t.Fatalf("%s's purchase block came back with vendor %q, want %q; group_a_resources_untouched compares this value", username, purchase.Vendor, tn.purchaseVendor)
	}

	tn.identificationValue = username + "'s serial number"
	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+tn.itemID+"/identifications", tn.cookie,
		identificationCreateBody("serial", tn.identificationValue))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create identification on own item: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var ident identificationResponse
	mustDecode(t, rec, &ident)
	if ident.Version != 1 {
		t.Fatalf("%s's freshly created identification has version %d, want 1; the cross-group PUT probe below sends version 1 and would stop testing scoping", username, ident.Version)
	}
	if ident.Value != tn.identificationValue {
		t.Fatalf("%s's identification came back with value %q, want %q; group_a_resources_untouched compares this value", username, ident.Value, tn.identificationValue)
	}
	if ident.ID == "" {
		t.Fatalf("%s's identification create returned no id; every PUT/DELETE probe below would name the empty string", username)
	}
	tn.identificationID = ident.ID

	tn.customFieldDefName = username + "'s custom field"
	rec = srv.do(t, http.MethodPost, "/api/v1/custom-field-defs", tn.cookie,
		customFieldDefCreateBody(tn.customFieldDefName, "text", 0))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create custom field def: status = %d (403 here would mean the registering user is not an owner): %s", username, rec.Code, rec.Body.String())
	}
	var cfd customFieldDefResponse
	mustDecode(t, rec, &cfd)
	if cfd.Version != 1 {
		t.Fatalf("%s's freshly created custom field def has version %d, want 1; the cross-group PUT probe below sends version 1 and would stop testing scoping", username, cfd.Version)
	}
	if cfd.Name != tn.customFieldDefName {
		t.Fatalf("%s's custom field def came back with name %q, want %q; group_a_resources_untouched compares this value", username, cfd.Name, tn.customFieldDefName)
	}
	if cfd.ID == "" {
		t.Fatalf("%s's custom field def create returned no id; every PUT/DELETE probe below would name the empty string", username)
	}
	tn.customFieldDefID = cfd.ID

	tn.itemCustomFieldValue = username + "'s colour"
	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+tn.itemID+"/custom-fields", tn.cookie,
		itemCustomFieldCreateBody("Colour", tn.itemCustomFieldValue))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create custom field on own item: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var cf itemCustomFieldResponse
	mustDecode(t, rec, &cf)
	if cf.Version != 1 {
		t.Fatalf("%s's freshly created custom field has version %d, want 1; the cross-group PUT probe below sends version 1 and would stop testing scoping", username, cf.Version)
	}
	if cf.TextValue == nil || *cf.TextValue != tn.itemCustomFieldValue {
		t.Fatalf("%s's custom field came back with text_value %v, want %q; group_a_resources_untouched compares this value", username, cf.TextValue, tn.itemCustomFieldValue)
	}
	if cf.ID == "" {
		t.Fatalf("%s's custom field create returned no id; every PUT/DELETE probe below would name the empty string", username)
	}
	tn.itemCustomFieldID = cf.ID

	tn.stockAdjustmentDelta = 3
	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+tn.itemID+"/stock-adjustments", tn.cookie,
		stockAdjustmentCreateBody(tn.stockAdjustmentDelta, "initial stock", username+"'s stock note"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create stock adjustment on own item: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var sa stockAdjustmentResponse
	mustDecode(t, rec, &sa)
	if sa.Version != 1 {
		t.Fatalf("%s's freshly created stock adjustment has version %d, want 1", username, sa.Version)
	}
	if sa.Delta != tn.stockAdjustmentDelta {
		t.Fatalf("%s's stock adjustment came back with delta %d, want %d; group_a_resources_untouched compares this value", username, sa.Delta, tn.stockAdjustmentDelta)
	}
	if sa.ResultingQuantity != tn.stockAdjustmentDelta {
		t.Fatalf("%s's stock adjustment resulting_quantity = %d, want %d (a freshly created item starts at quantity 0)", username, sa.ResultingQuantity, tn.stockAdjustmentDelta)
	}
	tn.itemQuantityAfterAdjustment = sa.ResultingQuantity

	tn.labelName = username + "'s label"
	rec = srv.do(t, http.MethodPost, "/api/v1/labels", tn.cookie, labelCreateBody(tn.labelName, "#123456"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create label: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var lbl labelResponse
	mustDecode(t, rec, &lbl)
	if lbl.Version != 1 {
		t.Fatalf("%s's freshly created label has version %d, want 1; the cross-group PUT probe below sends version 1 and would stop testing scoping", username, lbl.Version)
	}
	if lbl.Name != tn.labelName {
		t.Fatalf("%s's label came back with name %q, want %q; group_a_resources_untouched compares this value", username, lbl.Name, tn.labelName)
	}
	if lbl.ID == "" {
		t.Fatalf("%s's label create returned no id; every PUT/DELETE probe below would name the empty string", username)
	}
	tn.labelID = lbl.ID

	rec = srv.do(t, http.MethodPut, "/api/v1/items/"+tn.itemID+"/labels/"+tn.labelID, tn.cookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("%s attach label to item: status = %d, want 204: %s", username, rec.Code, rec.Body.String())
	}

	tn.locationName = username + "'s garage"
	rec = srv.do(t, http.MethodPost, "/api/v1/locations", tn.cookie, locationCreateBody(tn.locationName, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create location: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var loc locationResponse
	mustDecode(t, rec, &loc)
	if loc.Version != 1 {
		t.Fatalf("%s's freshly created location has version %d, want 1; the cross-group PUT probe below sends version 1 and would stop testing scoping", username, loc.Version)
	}
	if loc.Name != tn.locationName {
		t.Fatalf("%s's location came back with name %q, want %q; group_a_resources_untouched compares this value", username, loc.Name, tn.locationName)
	}
	if loc.ID == "" {
		t.Fatalf("%s's location create returned no id; every GET/PUT/DELETE probe below would name the empty string", username)
	}
	tn.locationID = loc.ID

	tn.attachmentContent = testImageJPEGBytes(t)
	body, contentType := attachmentUploadMultipartBody(t, attachments.CategoryImage, username+"-photo.jpg", tn.attachmentContent)
	rec = srv.doMultipart(t, http.MethodPost, "/api/v1/items/"+tn.itemID+"/attachments", tn.cookie, body, contentType)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s upload attachment on own item: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var att attachmentResponse
	mustDecode(t, rec, &att)
	if !att.HasThumbnail {
		t.Fatalf("%s's uploaded attachment has has_thumbnail = false, want true -- testImageJPEGBytes must be a genuinely decodable image for the thumbnail-download probe to have something real to fetch: %+v", username, att)
	}
	if att.ID == "" {
		t.Fatalf("%s's attachment upload returned no id; every GET/GET-thumbnail/DELETE probe below would name the empty string", username)
	}
	tn.attachmentID = att.ID

	return tn
}

type doomedItem struct {
	itemID            string
	identificationID  string
	itemCustomFieldID string
}

func provisionAndDeleteDoomedItem(t *testing.T, f *crossTenantFixture) doomedItem {
	t.Helper()
	a := f.a

	rec := f.srv.do(t, http.MethodPost, "/api/v1/items", a.cookie, itemCreateBody(a.name+"'s doomed stepladder"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create doomed item: status = %d: %s", a.name, rec.Code, rec.Body.String())
	}
	var it itemResponse
	mustDecode(t, rec, &it)
	d := doomedItem{itemID: it.ID}
	if d.itemID == "" {
		t.Fatalf("%s's doomed-item create returned no id; every probe below would name the empty string", a.name)
	}

	if rec := f.srv.do(t, http.MethodPost, "/api/v1/items/"+d.itemID+"/warranty", a.cookie,
		warrantyCreateBody(a.name+"'s doomed warranty holder")); rec.Code != http.StatusCreated {
		t.Fatalf("%s create warranty on doomed item: status = %d: %s", a.name, rec.Code, rec.Body.String())
	}
	if rec := f.srv.do(t, http.MethodPost, "/api/v1/items/"+d.itemID+"/sale", a.cookie,
		saleCreateBody(a.name+"'s doomed sale buyer")); rec.Code != http.StatusCreated {
		t.Fatalf("%s create sale on doomed item: status = %d: %s", a.name, rec.Code, rec.Body.String())
	}
	if rec := f.srv.do(t, http.MethodPost, "/api/v1/items/"+d.itemID+"/purchase", a.cookie,
		purchaseCreateBody(a.name+"'s doomed purchase vendor")); rec.Code != http.StatusCreated {
		t.Fatalf("%s create purchase on doomed item: status = %d: %s", a.name, rec.Code, rec.Body.String())
	}

	rec = f.srv.do(t, http.MethodPost, "/api/v1/items/"+d.itemID+"/identifications", a.cookie,
		identificationCreateBody("serial", a.name+"'s doomed serial"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create identification on doomed item: status = %d: %s", a.name, rec.Code, rec.Body.String())
	}
	var ident identificationResponse
	mustDecode(t, rec, &ident)
	d.identificationID = ident.ID
	if d.identificationID == "" {
		t.Fatalf("%s's doomed identification create returned no id; the PUT/DELETE probes below would name the empty string", a.name)
	}

	rec = f.srv.do(t, http.MethodPost, "/api/v1/items/"+d.itemID+"/custom-fields", a.cookie,
		itemCustomFieldCreateBody("Colour", a.name+"'s doomed colour"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create custom field on doomed item: status = %d: %s", a.name, rec.Code, rec.Body.String())
	}
	var cf itemCustomFieldResponse
	mustDecode(t, rec, &cf)
	d.itemCustomFieldID = cf.ID
	if d.itemCustomFieldID == "" {
		t.Fatalf("%s's doomed custom field create returned no id; the PUT/DELETE probes below would name the empty string", a.name)
	}

	rec = f.srv.do(t, http.MethodPost, "/api/v1/items/"+d.itemID+"/stock-adjustments", a.cookie,
		stockAdjustmentCreateBody(1, "doomed stock", "doomed"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create stock adjustment on doomed item: status = %d: %s", a.name, rec.Code, rec.Body.String())
	}

	rec = f.srv.do(t, http.MethodDelete, "/api/v1/items/"+d.itemID, a.cookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("%s delete own doomed item: status = %d, want 204: %s", a.name, rec.Code, rec.Body.String())
	}

	return d
}

type reportsFixture struct {
	itemID             string
	itemName           string
	purchaseVendor     string
	purchasePriceMinor int64
	purchasedOn        string
	warrantyExpiresOn  string
}

func provisionReportsFixture(t *testing.T, srv *liveServer, tn tenant) reportsFixture {
	t.Helper()
	rf := reportsFixture{
		itemName:           tn.name + "'s report widget",
		purchaseVendor:     tn.name + "'s report vendor",
		purchasePriceMinor: 12345,
		purchasedOn:        "2026-06-15",
		warrantyExpiresOn:  time.Now().UTC().AddDate(0, 0, 10).Format("2006-01-02"),
	}

	rec := srv.do(t, http.MethodPost, "/api/v1/items", tn.cookie, itemCreateBodyWithLocation(rf.itemName, tn.locationID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create reports-fixture item: status = %d: %s", tn.name, rec.Code, rec.Body.String())
	}
	var it itemResponse
	mustDecode(t, rec, &it)
	rf.itemID = it.ID
	if rf.itemID == "" {
		t.Fatalf("%s's reports-fixture item create returned no id; every probe below would name the empty string", tn.name)
	}
	if it.LocationID != tn.locationID {
		t.Fatalf("%s's reports-fixture item landed at location %q, want %q -- reports_item_count_by_location and export_bom's location_id mode both depend on this", tn.name, it.LocationID, tn.locationID)
	}

	if rec := srv.do(t, http.MethodPut, "/api/v1/items/"+rf.itemID+"/labels/"+tn.labelID, tn.cookie, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("%s attach label to reports-fixture item: status = %d, want 204: %s", tn.name, rec.Code, rec.Body.String())
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+rf.itemID+"/warranty", tn.cookie,
		warrantyCreateBodyWithExpiry(tn.name+"'s report warranty holder", rf.warrantyExpiresOn))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create warranty on reports-fixture item: status = %d: %s", tn.name, rec.Code, rec.Body.String())
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+rf.itemID+"/purchase", tn.cookie,
		purchaseCreateBodyWithPriceAndDate(rf.purchaseVendor, rf.purchasePriceMinor, rf.purchasedOn))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s create purchase on reports-fixture item: status = %d: %s", tn.name, rec.Code, rec.Body.String())
	}
	var purchase purchaseResponse
	mustDecode(t, rec, &purchase)
	if purchase.PurchasePriceMinor != rf.purchasePriceMinor || purchase.Vendor != rf.purchaseVendor || purchase.PurchasedOn != rf.purchasedOn {
		t.Fatalf("%s's reports-fixture purchase came back as %+v, want vendor %q purchase_price_minor %d purchased_on %q", tn.name, purchase, rf.purchaseVendor, rf.purchasePriceMinor, rf.purchasedOn)
	}

	return rf
}

func stageOwnExportAsImport(t *testing.T, srv *liveServer, tn tenant) string {
	t.Helper()
	exportRec := srv.do(t, http.MethodGet, "/api/v1/export/items.csv", tn.cookie, nil)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("%s export items.csv (building an import fixture): status = %d, want 200: %s", tn.name, exportRec.Code, exportRec.Body.String())
	}
	csvBytes := append([]byte(nil), exportRec.Body.Bytes()...)

	uploadRec := srv.do(t, http.MethodPost, "/api/v1/import/native/upload", tn.cookie, csvBytes)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("%s upload native import (building an import fixture): status = %d, want 201: %s", tn.name, uploadRec.Code, uploadRec.Body.String())
	}
	var upload importUploadResponseMirror
	mustDecode(t, uploadRec, &upload)
	if upload.ImportID == "" {
		t.Fatalf("%s's import upload returned no import_id; every probe below would name the empty string", tn.name)
	}
	return upload.ImportID
}

func assertImportUnreachable(t *testing.T, srv *liveServer, cred credential, attacker, owner tenant, method, path, foreignID string) {
	t.Helper()
	rec := cred.send(t, srv, method, path, cred.pick(attacker), nil)

	switch rec.Code {
	case wantCrossGroupStatus:
	case http.StatusForbidden:
		t.Fatalf("%s (%s) %s %s -> 403; want %d. A 403 CONFIRMS the import session exists, which is "+
			"the cross-tenant existence oracle FR-008/NFR-010 forbid: %s must be unable to tell "+
			"%s's import_id from one that was never minted at all.",
			attacker.name, cred.kind, method, path, wantCrossGroupStatus, attacker.name, owner.name)
	default:
		t.Fatalf("%s (%s) %s %s -> %d; want %d (a foreign import_id and an unknown one must answer "+
			"identically). Body: %s",
			attacker.name, cred.kind, method, path, rec.Code, wantCrossGroupStatus, rec.Body.String())
	}

	if foreignID != "" && strings.Contains(rec.Body.String(), foreignID) {
		t.Fatalf("%s (%s) %s %s answered %d but echoed %s's import_id %q back in the body -- the "+
			"status hides the resource and the body reveals it: %s",
			attacker.name, cred.kind, method, path, rec.Code, owner.name, foreignID, rec.Body.String())
	}
}

func (f *crossTenantFixture) request(t *testing.T, cred credential, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	return cred.send(t, f.srv, method, path, cred.pick(f.b), body)
}

func (f *crossTenantFixture) assertCrossGroupNotFound(t *testing.T, cred credential, method, path, foreignID string) {
	t.Helper()
	f.assertCrossGroupNotFoundWithBody(t, cred, method, path, foreignID, nil)
}

func (f *crossTenantFixture) assertCrossGroupNotFoundWithBody(t *testing.T, cred credential, method, path, foreignID string, body []byte) {
	t.Helper()
	rec := f.request(t, cred, method, path, body)

	switch rec.Code {
	case wantCrossGroupStatus:
	case http.StatusForbidden:
		t.Fatalf("%s (%s) %s %s -> 403; want %d. A 403 CONFIRMS the resource exists, which is the "+
			"cross-tenant existence oracle FR-008/NFR-010 forbid: %s must be unable to tell %s's id "+
			"from an id that was never minted at all.",
			f.b.name, cred.kind, method, path, wantCrossGroupStatus, f.b.name, f.a.name)
	default:
		t.Fatalf("%s (%s) %s %s -> %d; want %d (a foreign id and an unknown id must answer "+
			"identically). Body: %s",
			f.b.name, cred.kind, method, path, rec.Code, wantCrossGroupStatus, rec.Body.String())
	}

	if foreignID != "" && strings.Contains(rec.Body.String(), foreignID) {
		t.Fatalf("%s (%s) %s %s answered %d but echoed %s's id %q back in the body -- the status "+
			"hides the resource and the body reveals it: %s",
			f.b.name, cred.kind, method, path, rec.Code, f.a.name, foreignID, rec.Body.String())
	}
}
