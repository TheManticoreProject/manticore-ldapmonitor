package directory

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// StoredFormat is the version of the snapshot file layout. A file written by a
// version the reader does not know is refused by name rather than half-parsed.
const StoredFormat = 1

// storedTool is written into every file so that a snapshot from another tool is
// refused with a sentence instead of a type error.
const storedTool = "manticore-ldapmonitor"

// StoredSnapshot is a reading as it is written to a file: the objects, and the scope
// the reading was taken with, so that a diff can tell whether the two files it was
// handed cover the same ground.
//
// The values are held as bytes rather than as the strings the rest of the package
// works with, and that is not cosmetic: an LDAP value is arbitrary bytes, and
// objectSid, objectGUID, nTSecurityDescriptor and logonHours are none of them valid
// UTF-8. encoding/json replaces every invalid byte of a Go string with U+FFFD, so
// storing them as strings would quietly corrupt exactly the attributes worth
// watching, and every object holding one would differ from itself across a
// write-then-read. As bytes they are base64-encoded and come back unchanged.
type StoredSnapshot struct {
	Format           int       `json:"format"`
	Tool             string    `json:"tool"`
	Version          string    `json:"version"`
	TakenAt          time.Time `json:"takenAt"`
	Domain           string    `json:"domain,omitempty"`
	DomainController string    `json:"domainController,omitempty"`
	Scope            Scope     `json:"scope"`

	Objects map[string]map[string][][]byte `json:"objects"`
}

// SetSnapshot fills in the objects of a stored snapshot from a reading.
//
// Parameters:
//
//	snapshot (Snapshot): The reading to store.
func (stored *StoredSnapshot) SetSnapshot(snapshot Snapshot) {
	objects := make(map[string]map[string][][]byte, len(snapshot))
	for distinguishedName, attributes := range snapshot {
		storedAttributes := make(map[string][][]byte, len(attributes))
		for name, values := range attributes {
			storedValues := make([][]byte, 0, len(values))
			for _, value := range values {
				storedValues = append(storedValues, []byte(value))
			}
			storedAttributes[name] = storedValues
		}
		objects[distinguishedName] = storedAttributes
	}
	stored.Objects = objects
}

// Snapshot returns the reading held in a stored snapshot.
//
// Returns:
//
//	The reading, in the form the rest of the package compares and renders.
func (stored *StoredSnapshot) Snapshot() Snapshot {
	snapshot := make(Snapshot, len(stored.Objects))
	for distinguishedName, storedAttributes := range stored.Objects {
		attributes := make(map[string][]string, len(storedAttributes))
		for name, storedValues := range storedAttributes {
			// An attribute that exists with no value is not the same thing as an
			// absent attribute, and Diff tells them apart by the presence of the key,
			// so the empty list has to come back as an empty list.
			values := make([]string, 0, len(storedValues))
			for _, value := range storedValues {
				values = append(values, string(value))
			}
			attributes[name] = values
		}
		snapshot[distinguishedName] = attributes
	}
	return snapshot
}

// WriteSnapshot writes a reading to a file.
//
// The file is gzipped when its name ends in .gz. A snapshot holds every attribute of
// every object of the search bases, which on a domain of any size runs to hundreds of
// megabytes of highly repetitive text, so that is worth having on anything but a
// small scope.
//
// Parameters:
//
//	path (string): Where to write.
//	stored (*StoredSnapshot): The reading to write, whose Format, Tool and TakenAt
//	  are filled in here.
//
// Returns:
//
//	An error if the file could not be written.
func WriteSnapshot(path string, stored *StoredSnapshot) error {
	stored.Format = StoredFormat
	stored.Tool = storedTool
	if stored.TakenAt.IsZero() {
		stored.TakenAt = time.Now().UTC()
	}

	// The file is written to a temporary name in the same directory and moved into
	// place, so that an interrupted write does not leave a half-written snapshot
	// under the name the operator will later diff against.
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, filepath.Base(path)+".partial-*")
	if err != nil {
		return fmt.Errorf("error creating the snapshot file in '%s': %w", directory, err)
	}
	temporaryPath := temporary.Name()

	if err := encodeSnapshot(temporary, stored, isGzipPath(path)); err != nil {
		temporary.Close()
		os.Remove(temporaryPath)
		return err
	}

	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return fmt.Errorf("error closing the snapshot file: %w", err)
	}

	if err := os.Rename(temporaryPath, path); err != nil {
		os.Remove(temporaryPath)
		return fmt.Errorf("error writing the snapshot to '%s': %w", path, err)
	}

	return nil
}

// encodeSnapshot writes the JSON body of a snapshot, gzipped or not.
//
// Parameters:
//
//	file (*os.File): The file to write to.
//	stored (*StoredSnapshot): The reading to write.
//	compress (bool): Whether to gzip the body.
//
// Returns:
//
//	An error if encoding failed.
func encodeSnapshot(file *os.File, stored *StoredSnapshot, compress bool) error {
	if !compress {
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(stored); err != nil {
			return fmt.Errorf("error encoding the snapshot: %w", err)
		}
		return nil
	}

	compressor := gzip.NewWriter(file)
	encoder := json.NewEncoder(compressor)
	if err := encoder.Encode(stored); err != nil {
		compressor.Close()
		return fmt.Errorf("error encoding the snapshot: %w", err)
	}
	if err := compressor.Close(); err != nil {
		return fmt.Errorf("error compressing the snapshot: %w", err)
	}
	return nil
}

// ReadSnapshot reads a reading back from a file.
//
// Parameters:
//
//	path (string): The file to read.
//
// Returns:
//
//	The stored reading, or an error if the file could not be read, is not a snapshot
//	of this tool, or was written by a format this version does not know.
func ReadSnapshot(path string) (*StoredSnapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening the snapshot '%s': %w", path, err)
	}
	defer file.Close()

	// The file is recognized by its own bytes rather than by its name, so that a
	// gzipped snapshot saved without the .gz suffix still reads back.
	var reader interface{ Read([]byte) (int, error) } = file
	compressed, err := looksGzipped(file)
	if err != nil {
		return nil, fmt.Errorf("error reading the snapshot '%s': %w", path, err)
	}
	if compressed {
		decompressor, err := gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("error decompressing the snapshot '%s': %w", path, err)
		}
		defer decompressor.Close()
		reader = decompressor
	}

	stored := &StoredSnapshot{}
	if err := json.NewDecoder(reader).Decode(stored); err != nil {
		return nil, fmt.Errorf("error reading the snapshot '%s': %w", path, err)
	}

	if stored.Tool != storedTool {
		return nil, fmt.Errorf("'%s' was not written by %s (it says it was written by '%s')", path, storedTool, stored.Tool)
	}
	if stored.Format != StoredFormat {
		return nil, fmt.Errorf("'%s' is in snapshot format %d, this build reads format %d", path, stored.Format, StoredFormat)
	}

	return stored, nil
}

// looksGzipped reports whether a file starts with the gzip magic number, leaving the
// read offset back at the start either way.
//
// Parameters:
//
//	file (*os.File): The file to sniff.
//
// Returns:
//
//	True when the file is gzipped, and an error if it could not be read or rewound.
func looksGzipped(file *os.File) (bool, error) {
	magic := make([]byte, 2)
	read, err := file.Read(magic)
	if err != nil && read == 0 {
		// An empty file is not gzipped; the JSON decoder gives the better error.
		if _, seekErr := file.Seek(0, 0); seekErr != nil {
			return false, seekErr
		}
		return false, nil
	}
	if _, err := file.Seek(0, 0); err != nil {
		return false, err
	}
	return read == 2 && magic[0] == 0x1f && magic[1] == 0x8b, nil
}

// isGzipPath reports whether a path asks for a compressed file.
//
// Parameters:
//
//	path (string): The path to check.
//
// Returns:
//
//	True when the path ends in .gz.
func isGzipPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".gz")
}

// ScopeMismatch describes how the scopes of two stored readings differ.
//
// It matters because an object that one reading never looked at is absent from it,
// and absent is what a deleted object looks like: without this, narrowing the scope
// between two captures would report the whole difference as objects disappearing.
//
// Parameters:
//
//	before (*StoredSnapshot): The older reading.
//	after (*StoredSnapshot): The newer reading.
//
// Returns:
//
//	One sentence per difference, empty when the two readings cover the same ground.
func ScopeMismatch(before *StoredSnapshot, after *StoredSnapshot) []string {
	differences := []string{}

	previousBases := slices.Clone(before.Scope.SearchBases)
	currentBases := slices.Clone(after.Scope.SearchBases)
	slices.Sort(previousBases)
	slices.Sort(currentBases)
	if !slices.Equal(previousBases, currentBases) {
		differences = append(differences, fmt.Sprintf(
			"the search bases differ: [%s] then [%s]",
			strings.Join(previousBases, ", "), strings.Join(currentBases, ", ")))
	}

	if before.Scope.Filter() != after.Scope.Filter() {
		differences = append(differences, fmt.Sprintf(
			"the LDAP filters differ: '%s' then '%s'",
			before.Scope.Filter(), after.Scope.Filter()))
	}

	return differences
}
