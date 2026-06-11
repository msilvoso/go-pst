package pst

import (
	"os"
	"testing"
)

// TestWalkFoldersANSI walks data/32-bit.pst, which stores folder names as String8.
func TestWalkFoldersANSI(t *testing.T) {
	reader, err := os.Open("../data/32-bit.pst")

	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	pstFile, err := New(reader)

	if err != nil {
		t.Fatal(err)
	}
	defer pstFile.Cleanup()

	var folderNames []string

	if err := pstFile.WalkFolders(func(folder *Folder) error {
		folderNames = append(folderNames, folder.Name)
		return nil
	}); err != nil {
		t.Fatalf("WalkFolders failed: %+v", err)
	}

	expectedFolderNames := []string{"ROOT_FOLDER", "Top of Personal Folders", "Deleted Items", "Calendar", "Search Root"}

	if len(folderNames) != len(expectedFolderNames) {
		t.Fatalf("expected %d folders, got %d: %v", len(expectedFolderNames), len(folderNames), folderNames)
	}

	for i, expectedFolderName := range expectedFolderNames {
		if folderNames[i] != expectedFolderName {
			t.Fatalf("expected folder %q at index %d, got %q", expectedFolderName, i, folderNames[i])
		}
	}
}
