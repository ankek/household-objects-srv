package storage

import (
	"errors"
	"testing"
)

func deviceTokenParamsFor(p FirstUserParams, idSuffix string) DeviceTokenParams {
	return DeviceTokenParams{
		ID:          "dvc-" + idSuffix,
		GroupID:     p.GroupID,
		UserID:      p.UserID,
		TokenHash:   "hash-" + idSuffix,
		DeviceLabel: "Test Device " + idSuffix,
		Now:         1_700_000_000_000,
	}
}

func TestCreateDeviceTokenThenDeviceTokenAuthRoundTrips(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	dp := deviceTokenParamsFor(p, "1")
	if err := s.CreateDeviceToken(t.Context(), dp); err != nil {
		t.Fatalf("CreateDeviceToken: %v", err)
	}

	got, err := s.DeviceTokenAuth(t.Context(), dp.TokenHash)
	if err != nil {
		t.Fatalf("DeviceTokenAuth: %v", err)
	}
	if got.GroupID != p.GroupID {
		t.Errorf("GroupID = %q, want %q", got.GroupID, p.GroupID)
	}
	if got.UserID != p.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, p.UserID)
	}
	if got.Role != ownerRole {
		t.Errorf("Role = %q, want %q", got.Role, ownerRole)
	}
	if got.Revoked {
		t.Error("Revoked = true for a freshly created device token, want false")
	}

	var counter int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT change_seq_counter FROM groups WHERE id = ?", p.GroupID,
	).Scan(&counter); err != nil {
		t.Fatalf("read change_seq_counter: %v", err)
	}
	if counter != 2 {
		t.Errorf("change_seq_counter after CreateDeviceToken = %d, want 2 (1 for the owner, 1 for the device token)", counter)
	}
}

func TestDeviceTokenAuthReportsUnknownToken(t *testing.T) {
	s := newTestStorage(t)
	if _, err := s.DeviceTokenAuth(t.Context(), "no-such-hash"); !errors.Is(err, ErrDeviceTokenNotFound) {
		t.Fatalf("DeviceTokenAuth(unknown): error = %v, want ErrDeviceTokenNotFound", err)
	}
}

func TestDeviceTokenAuthReportsRevokedWithoutRefusing(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	dp := deviceTokenParamsFor(p, "1")
	if err := s.CreateDeviceToken(t.Context(), dp); err != nil {
		t.Fatalf("CreateDeviceToken: %v", err)
	}
	if _, err := s.store.Writer().ExecContext(t.Context(),
		"UPDATE device_tokens SET revoked_at = 1 WHERE id = ?", dp.ID,
	); err != nil {
		t.Fatalf("revoke device token: %v", err)
	}

	got, err := s.DeviceTokenAuth(t.Context(), dp.TokenHash)
	if err != nil {
		t.Fatalf("DeviceTokenAuth(revoked): %v", err)
	}
	if !got.Revoked {
		t.Error("Revoked = false for a device token with revoked_at set, want true")
	}
}

func TestDeviceTokenAuthSkipsSoftDeletedTokensAndUsers(t *testing.T) {
	t.Run("device token tombstoned", func(t *testing.T) {
		s := newTestStorage(t)
		p := firstUserParams("a")
		if err := s.RegisterFirstUser(t.Context(), p); err != nil {
			t.Fatalf("RegisterFirstUser: %v", err)
		}
		dp := deviceTokenParamsFor(p, "1")
		if err := s.CreateDeviceToken(t.Context(), dp); err != nil {
			t.Fatalf("CreateDeviceToken: %v", err)
		}
		if _, err := s.store.Writer().ExecContext(t.Context(),
			"UPDATE device_tokens SET deleted_at = 1 WHERE id = ?", dp.ID,
		); err != nil {
			t.Fatalf("tombstone device token: %v", err)
		}
		if _, err := s.DeviceTokenAuth(t.Context(), dp.TokenHash); !errors.Is(err, ErrDeviceTokenNotFound) {
			t.Fatalf("DeviceTokenAuth(tombstoned device token): error = %v, want ErrDeviceTokenNotFound", err)
		}
	})

	t.Run("owning user tombstoned", func(t *testing.T) {
		s := newTestStorage(t)
		p := firstUserParams("a")
		if err := s.RegisterFirstUser(t.Context(), p); err != nil {
			t.Fatalf("RegisterFirstUser: %v", err)
		}
		dp := deviceTokenParamsFor(p, "1")
		if err := s.CreateDeviceToken(t.Context(), dp); err != nil {
			t.Fatalf("CreateDeviceToken: %v", err)
		}
		if _, err := s.store.Writer().ExecContext(t.Context(),
			"UPDATE users SET deleted_at = 1 WHERE id = ?", p.UserID,
		); err != nil {
			t.Fatalf("tombstone user: %v", err)
		}
		if _, err := s.DeviceTokenAuth(t.Context(), dp.TokenHash); !errors.Is(err, ErrDeviceTokenNotFound) {
			t.Fatalf("DeviceTokenAuth(tombstoned owner): error = %v, want ErrDeviceTokenNotFound", err)
		}
	})
}

func TestCreateDeviceTokenRejectsIncompleteParams(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	base := deviceTokenParamsFor(p, "1")

	cases := []struct {
		name   string
		mutate func(*DeviceTokenParams)
	}{
		{"empty ID", func(dp *DeviceTokenParams) { dp.ID = "" }},
		{"empty GroupID", func(dp *DeviceTokenParams) { dp.GroupID = "" }},
		{"empty UserID", func(dp *DeviceTokenParams) { dp.UserID = "" }},
		{"empty TokenHash", func(dp *DeviceTokenParams) { dp.TokenHash = "" }},
		{"empty DeviceLabel", func(dp *DeviceTokenParams) { dp.DeviceLabel = "" }},
		{"zero Now", func(dp *DeviceTokenParams) { dp.Now = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dp := base
			tc.mutate(&dp)
			if err := s.CreateDeviceToken(t.Context(), dp); err == nil {
				t.Errorf("CreateDeviceToken(%+v) succeeded, want a validation error", dp)
			}
		})
	}

	if got := countRows(t, s, "device_tokens"); got != 0 {
		t.Fatalf("device_tokens row count after only-invalid attempts = %d, want 0", got)
	}
}

func TestDeviceTokenMethodsRefuseUnopenedStorage(t *testing.T) {
	var s *Storage

	if err := s.CreateDeviceToken(t.Context(), DeviceTokenParams{}); !errors.Is(err, ErrNoGroup) {
		t.Errorf("CreateDeviceToken on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
	if _, err := s.DeviceTokenAuth(t.Context(), "h"); !errors.Is(err, ErrNoGroup) {
		t.Errorf("DeviceTokenAuth on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
}
