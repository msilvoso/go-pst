package pst

import (
	"os"
	"testing"

	"github.com/msilvoso/go-pst/v6/pkg/properties"
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

// TestWalkMessagesANSI checks that String8 properties are decoded and populated:
// the message class (selecting the properties struct) and the string fields
// themselves, which the generated decoders only know under their Unicode keys.
func TestWalkMessagesANSI(t *testing.T) {
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

	var messages []*Message

	if err := pstFile.WalkFolders(func(folder *Folder) error {
		messageIterator, err := folder.GetMessageIterator()

		if err != nil {
			// Folders without messages are checked by TestWalkFoldersANSI.
			return nil
		}

		for messageIterator.Next() {
			messages = append(messages, messageIterator.Value())
		}

		return messageIterator.Err()
	}); err != nil {
		t.Fatalf("WalkFolders failed: %+v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	// The message class "IPM.Appointment" is stored as String8.
	if _, ok := messages[0].Properties.(*properties.Appointment); !ok {
		t.Fatalf("expected *properties.Appointment, got %T", messages[0].Properties)
	}

	// The appointment's common properties are not part of the Appointment
	// struct; populate a Message to check the String8 fields.
	messageProperties := &properties.Message{}

	if err := messages[0].PropertyContext.Populate(messageProperties, messages[0].LocalDescriptors); err != nil {
		t.Fatalf("Populate failed: %+v", err)
	}

	if expected := "Olympus training for new hires"; messageProperties.GetConversationTopic() != expected {
		t.Fatalf("expected conversation topic %q, got %q", expected, messageProperties.GetConversationTopic())
	}

	if expected := "Cyndy Foulkrod"; messageProperties.GetSentRepresentingName() != expected {
		t.Fatalf("expected sent representing name %q, got %q", expected, messageProperties.GetSentRepresentingName())
	}
}
