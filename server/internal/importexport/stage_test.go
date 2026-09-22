package importexport

import (
	"bytes"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stageTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := datadir.Ensure(root); err != nil {
		t.Fatalf("datadir.Ensure(%s): %v", root, err)
	}
	return root
}

func validHeaderCSV(dataRow string) []byte {
	header := strings.Join(FixedColumns, ",")
	return []byte(header + "\n" + dataRow + "\n")
}

func tmpFileCount(t *testing.T, dataDir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dataDir, datadir.TmpSubdir))
	if err != nil {
		t.Fatalf("ReadDir(tmp): %v", err)
	}
	return len(entries)
}

func TestStageWritesTheExactBytesAtTheDocumentedPath(t *testing.T) {
	root := stageTestRoot(t)
	content := validHeaderCSV("item-1,Lawnmower,,1,,,SHORT1,,,,,,,,,,,,,,,,,,")

	if err := Stage(root, "group-a", "import-1", bytes.NewReader(content), DefaultMaxUploadBytes); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	wantPath := StagingPath(root, "group-a", "import-1")
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", wantPath, err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("staged bytes = %q, want %q (identical to the uploaded content)", got, content)
	}

	if n := tmpFileCount(t, root); n != 1 {
		t.Errorf("tmp/ has %d top-level entries after a successful Stage, want exactly 1 (imports/)", n)
	}
}

func TestStageAcceptsAWellFormedHeaderWithAnInvalidDataRow(t *testing.T) {
	root := stageTestRoot(t)
	content := validHeaderCSV("item-1,Lawnmower,,not-a-number,,,SHORT1,,,,,,,,,,,,,,,,,,")

	if err := Stage(root, "group-a", "import-1", bytes.NewReader(content), DefaultMaxUploadBytes); err != nil {
		t.Fatalf("Stage rejected a valid header solely because a data row was invalid: %v", err)
	}
}

func TestStageRejectsAnEmptyUpload(t *testing.T) {
	root := stageTestRoot(t)

	err := Stage(root, "group-a", "import-1", bytes.NewReader(nil), DefaultMaxUploadBytes)
	if !errors.Is(err, ErrInvalidCSV) {
		t.Fatalf("Stage(empty) error = %v, want ErrInvalidCSV", err)
	}

	if _, statErr := os.Stat(StagingPath(root, "group-a", "import-1")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a staged file exists after a rejected empty upload")
	}
	if n := tmpFileCount(t, root); n != 0 {
		t.Errorf("tmp/ has %d leftover entries after a rejected upload, want 0", n)
	}
}

func TestStageRejectsAMalformedHeader(t *testing.T) {
	root := stageTestRoot(t)
	content := []byte("not,a,real,header\nfoo,bar,baz,qux\n")

	err := Stage(root, "group-a", "import-1", bytes.NewReader(content), DefaultMaxUploadBytes)
	if !errors.Is(err, ErrInvalidCSV) {
		t.Fatalf("Stage(malformed header) error = %v, want ErrInvalidCSV", err)
	}

	if _, statErr := os.Stat(StagingPath(root, "group-a", "import-1")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a staged file exists after a rejected malformed-header upload")
	}
	if n := tmpFileCount(t, root); n != 0 {
		t.Errorf("tmp/ has %d leftover entries after a rejected upload, want 0", n)
	}
}

func TestStageRejectsAnOversizeUpload(t *testing.T) {
	root := stageTestRoot(t)
	content := validHeaderCSV("item-1,Lawnmower,,1,,,SHORT1,,,,,,,,,,,,,,,,,,")

	err := Stage(root, "group-a", "import-1", bytes.NewReader(content), int64(len(content)-1))
	if !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("Stage(oversize) error = %v, want ErrUploadTooLarge", err)
	}

	if _, statErr := os.Stat(StagingPath(root, "group-a", "import-1")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a staged file exists after a rejected oversize upload")
	}
	if n := tmpFileCount(t, root); n != 0 {
		t.Errorf("tmp/ has %d leftover entries after a rejected upload, want 0", n)
	}
}

func TestStageAcceptsAFileAtExactlyMaxBytes(t *testing.T) {
	root := stageTestRoot(t)
	content := validHeaderCSV("item-1,Lawnmower,,1,,,SHORT1,,,,,,,,,,,,,,,,,,")

	if err := Stage(root, "group-a", "import-1", bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("Stage(exactly maxBytes): %v", err)
	}
}

func TestStageRejectsEmptyIdentifiers(t *testing.T) {
	root := stageTestRoot(t)
	content := validHeaderCSV("item-1,Lawnmower,,1,,,SHORT1,,,,,,,,,,,,,,,,,,")

	cases := []struct {
		name                       string
		dataDir, groupID, importID string
		maxBytes                   int64
	}{
		{"empty dataDir", "", "group-a", "import-1", DefaultMaxUploadBytes},
		{"empty groupID", root, "", "import-1", DefaultMaxUploadBytes},
		{"empty importID", root, "group-a", "", DefaultMaxUploadBytes},
		{"non-positive maxBytes", root, "group-a", "import-1", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Stage(tc.dataDir, tc.groupID, tc.importID, bytes.NewReader(content), tc.maxBytes); err == nil {
				t.Error("Stage succeeded, want an error")
			}
		})
	}
}

func TestRemoveStagedRemovesAStagedFile(t *testing.T) {
	root := stageTestRoot(t)
	content := validHeaderCSV("item-1,Lawnmower,,1,,,SHORT1,,,,,,,,,,,,,,,,,,")
	if err := Stage(root, "group-a", "import-1", bytes.NewReader(content), DefaultMaxUploadBytes); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	if err := RemoveStaged(root, "group-a", "import-1"); err != nil {
		t.Fatalf("RemoveStaged: %v", err)
	}
	if _, statErr := os.Stat(StagingPath(root, "group-a", "import-1")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("staged file still exists after RemoveStaged")
	}
}

func TestRemoveStagedIsANoOpWhenNothingIsStaged(t *testing.T) {
	root := stageTestRoot(t)
	if err := RemoveStaged(root, "group-a", "no-such-import"); err != nil {
		t.Errorf("RemoveStaged(nothing staged) = %v, want nil", err)
	}
}
