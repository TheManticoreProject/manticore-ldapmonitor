package directory

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writtenSnapshot(t *testing.T, path string, snapshot Snapshot, scope Scope) {
	t.Helper()
	stored := &StoredSnapshot{Version: "test", Domain: "MANTICORE.local", Scope: scope}
	stored.SetSnapshot(snapshot)
	if err := WriteSnapshot(path, stored); err != nil {
		t.Fatalf("could not write the snapshot: %s", err)
	}
}

func TestSnapshotSurvivesAWriteAndARead(t *testing.T) {
	snapshot := Snapshot{
		"CN=PC01,DC=MANTICORE,DC=local": {
			"sAMAccountName": {"PC01$"},
			"member":         {"CN=A,DC=MANTICORE,DC=local", "CN=B,DC=MANTICORE,DC=local"},
		},
	}
	path := filepath.Join(t.TempDir(), "snapshot.json")
	writtenSnapshot(t, path, snapshot, Scope{SearchBases: []string{"DC=MANTICORE,DC=local"}})

	stored, err := ReadSnapshot(path)
	if err != nil {
		t.Fatalf("could not read the snapshot back: %s", err)
	}
	if !reflect.DeepEqual(stored.Snapshot(), snapshot) {
		t.Errorf("read back %#v, want %#v", stored.Snapshot(), snapshot)
	}
	if stored.Tool != storedTool || stored.Format != StoredFormat {
		t.Errorf("read back tool %q format %d, want %q and %d", stored.Tool, stored.Format, storedTool, StoredFormat)
	}
	if stored.TakenAt.IsZero() {
		t.Error("the reading was written with no timestamp")
	}
}

// An LDAP value is arbitrary bytes: objectSid, objectGUID and nTSecurityDescriptor
// are not text and are not valid UTF-8. Storing them as JSON strings would replace
// every invalid byte with U+FFFD, so an object would differ from itself between a
// write and a read, and every diff against a stored snapshot would report changes
// that never happened.
func TestSnapshotSurvivesValuesThatAreNotText(t *testing.T) {
	securityDescriptor := string([]byte{0x01, 0x00, 0x04, 0x9c, 0xff, 0xfe, 0x00, 0x80, 0xc0})
	objectSid := string([]byte{0x01, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, 0x15})
	snapshot := Snapshot{
		"CN=PC01,DC=MANTICORE,DC=local": {
			"nTSecurityDescriptor": {securityDescriptor},
			"objectSid":            {objectSid},
		},
	}
	path := filepath.Join(t.TempDir(), "snapshot.json")
	writtenSnapshot(t, path, snapshot, Scope{})

	stored, err := ReadSnapshot(path)
	if err != nil {
		t.Fatalf("could not read the snapshot back: %s", err)
	}
	if !reflect.DeepEqual(stored.Snapshot(), snapshot) {
		t.Errorf("read back %#v, want %#v", stored.Snapshot(), snapshot)
	}
}

func TestSnapshotKeepsAnAttributeThatHasNoValues(t *testing.T) {
	snapshot := Snapshot{"CN=PC01,DC=MANTICORE,DC=local": {"member": {}}}
	path := filepath.Join(t.TempDir(), "snapshot.json")
	writtenSnapshot(t, path, snapshot, Scope{})

	stored, err := ReadSnapshot(path)
	if err != nil {
		t.Fatalf("could not read the snapshot back: %s", err)
	}
	values, exists := stored.Snapshot()["CN=PC01,DC=MANTICORE,DC=local"]["member"]
	if !exists {
		t.Fatal("an attribute with no values was lost, which reads as a deletion")
	}
	if len(values) != 0 {
		t.Errorf("read back %#v, want no values", values)
	}
}

func TestSnapshotIsCompressedWhenTheNameAsksForIt(t *testing.T) {
	snapshot := Snapshot{"CN=PC01,DC=MANTICORE,DC=local": {"sAMAccountName": {"PC01$"}}}
	path := filepath.Join(t.TempDir(), "snapshot.json.gz")
	writtenSnapshot(t, path, snapshot, Scope{})

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read the file: %s", err)
	}
	if len(content) < 2 || content[0] != 0x1f || content[1] != 0x8b {
		t.Fatal("the file does not start with the gzip magic number")
	}

	stored, err := ReadSnapshot(path)
	if err != nil {
		t.Fatalf("could not read the snapshot back: %s", err)
	}
	if !reflect.DeepEqual(stored.Snapshot(), snapshot) {
		t.Errorf("read back %#v, want %#v", stored.Snapshot(), snapshot)
	}
}

// A snapshot is recognized by its own bytes, so that one saved without the .gz
// suffix still reads back.
func TestACompressedSnapshotIsRecognizedWhateverItIsCalled(t *testing.T) {
	snapshot := Snapshot{"CN=PC01,DC=MANTICORE,DC=local": {"sAMAccountName": {"PC01$"}}}
	directory := t.TempDir()
	compressed := filepath.Join(directory, "snapshot.json.gz")
	writtenSnapshot(t, compressed, snapshot, Scope{})

	renamed := filepath.Join(directory, "snapshot.json")
	if err := os.Rename(compressed, renamed); err != nil {
		t.Fatalf("could not rename the snapshot: %s", err)
	}

	stored, err := ReadSnapshot(renamed)
	if err != nil {
		t.Fatalf("could not read the snapshot back: %s", err)
	}
	if !reflect.DeepEqual(stored.Snapshot(), snapshot) {
		t.Errorf("read back %#v, want %#v", stored.Snapshot(), snapshot)
	}
}

func TestReadSnapshotRefusesAFileFromAnotherTool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, []byte(`{"format":1,"tool":"something-else","objects":{}}`), 0o600); err != nil {
		t.Fatalf("could not write the file: %s", err)
	}

	_, err := ReadSnapshot(path)
	if err == nil {
		t.Fatal("a snapshot from another tool was accepted")
	}
	if !strings.Contains(err.Error(), "something-else") {
		t.Errorf("the error does not name the tool that wrote the file: %s", err)
	}
}

func TestReadSnapshotRefusesAnUnknownFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	content := `{"format":99,"tool":"` + storedTool + `","objects":{}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("could not write the file: %s", err)
	}

	_, err := ReadSnapshot(path)
	if err == nil {
		t.Fatal("a snapshot in an unknown format was accepted")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("the error does not name the format of the file: %s", err)
	}
}

func TestWriteSnapshotLeavesNothingBehindWhenItCannotWrite(t *testing.T) {
	directory := t.TempDir()
	stored := &StoredSnapshot{}
	stored.SetSnapshot(Snapshot{})

	if err := WriteSnapshot(filepath.Join(directory, "missing", "snapshot.json"), stored); err == nil {
		t.Fatal("writing into a directory that does not exist was reported as a success")
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("could not list the directory: %s", err)
	}
	if len(entries) != 0 {
		t.Errorf("a partial file was left behind: %v", entries)
	}
}

func TestScopeMismatchIsSilentOnTwoReadingsOfTheSameGround(t *testing.T) {
	scope := Scope{SearchBases: []string{"DC=MANTICORE,DC=local", "CN=Configuration,DC=MANTICORE,DC=local"}}
	other := Scope{SearchBases: []string{"CN=Configuration,DC=MANTICORE,DC=local", "DC=MANTICORE,DC=local"}}

	differences := ScopeMismatch(&StoredSnapshot{Scope: scope}, &StoredSnapshot{Scope: other})
	if len(differences) != 0 {
		t.Errorf("the same search bases in another order were reported as a mismatch: %v", differences)
	}
}

func TestScopeMismatchReportsDifferentSearchBasesAndFilters(t *testing.T) {
	before := &StoredSnapshot{Scope: Scope{SearchBases: []string{"DC=MANTICORE,DC=local"}}}
	after := &StoredSnapshot{Scope: Scope{
		SearchBases: []string{"OU=Servers,DC=MANTICORE,DC=local"},
		LDAPFilter:  "(objectClass=user)",
	}}

	differences := ScopeMismatch(before, after)
	if len(differences) != 2 {
		t.Fatalf("reported %d differences, want 2: %v", len(differences), differences)
	}
	if !strings.Contains(differences[0], "search bases") {
		t.Errorf("the first difference is not about the search bases: %s", differences[0])
	}
	if !strings.Contains(differences[1], "(objectClass=user)") {
		t.Errorf("the second difference does not name the filter: %s", differences[1])
	}
}

// An unset filter and the default written out are the same reading, and reporting
// them as a mismatch would put a warning on every diff of two default captures.
func TestScopeMismatchTreatsAnEmptyFilterAsTheDefault(t *testing.T) {
	before := &StoredSnapshot{Scope: Scope{LDAPFilter: ""}}
	after := &StoredSnapshot{Scope: Scope{LDAPFilter: DefaultLDAPFilter}}

	if differences := ScopeMismatch(before, after); len(differences) != 0 {
		t.Errorf("an unset filter and the default were reported as a mismatch: %v", differences)
	}
}
