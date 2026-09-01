package directory

import (
	"slices"
	"sort"
	"strings"
)

// ChangeKind tells what happened to an object between two snapshots.
type ChangeKind int

const (
	// ObjectCreated is an object present in the new snapshot only.
	ObjectCreated ChangeKind = iota
	// ObjectDeleted is an object present in the old snapshot only.
	ObjectDeleted
	// ObjectUpdated is an object present in both snapshots, with at least one
	// attribute whose values differ.
	ObjectUpdated
)

// AttributeChange is one attribute of an object whose values differ between two
// snapshots. Before is nil when the attribute did not exist yet, After is nil when
// it no longer exists.
type AttributeChange struct {
	Name   string
	Before []string
	After  []string
}

// Change is everything that happened to one object between two snapshots.
type Change struct {
	Kind              ChangeKind
	DistinguishedName string
	// Attributes is filled for ObjectUpdated only, sorted by attribute name.
	Attributes []AttributeChange
}

// alwaysIgnoredAttributes are the attributes the domain controller rewrites on its
// own, on a schedule of its own: they change without anybody touching the
// directory, so reporting them would bury every real change under replication
// noise.
var alwaysIgnoredAttributes = []string{
	"dnsrecord",
	"repluptodatevector",
	"repsfrom",
}

// userLogonAttributes are the attributes every authentication in the domain
// updates. They are the point of the tool for some hunts and pure noise for
// others, so they are ignored on demand only.
var userLogonAttributes = []string{
	"lastlogon",
	"logoncount",
}

// IgnoredAttributes builds the set of attribute names whose changes are not
// reported. Names are lower-cased, since LDAP attribute names are
// case-insensitive and a domain controller is free to return either case.
//
// Parameters:
//
//	ignoreUserLogon (bool): A flag indicating whether to also ignore the attributes
//	  updated by every user logon.
//
// Returns:
//
//	The set of lower-cased attribute names to ignore.
func IgnoredAttributes(ignoreUserLogon bool) map[string]bool {
	ignored := make(map[string]bool, len(alwaysIgnoredAttributes)+len(userLogonAttributes))
	for _, name := range alwaysIgnoredAttributes {
		ignored[name] = true
	}
	if ignoreUserLogon {
		for _, name := range userLogonAttributes {
			ignored[name] = true
		}
	}
	return ignored
}

// Diff compares two snapshots and returns every change between them.
//
// The result is grouped by kind, creations first, then deletions, then updates, and
// each group is sorted by distinguished name. That ordering is what makes the output
// of a cycle reproducible, since ranging over a snapshot on its own would report the
// same set of changes in a different order every time.
//
// Parameters:
//
//	before (Snapshot): The state of the directory at the previous query.
//	after (Snapshot): The state of the directory at the latest query.
//	ignored (map[string]bool): The set of lower-cased attribute names not to report.
//
// Returns:
//
//	Every created, deleted and updated object between the two snapshots. An object
//	whose only differing attributes are ignored ones is not reported at all.
func Diff(before Snapshot, after Snapshot, ignored map[string]bool) []Change {
	changes := []Change{}

	for _, dn := range sortedKeys(after) {
		if _, exists := before[dn]; !exists {
			changes = append(changes, Change{Kind: ObjectCreated, DistinguishedName: dn})
		}
	}

	for _, dn := range sortedKeys(before) {
		if _, exists := after[dn]; !exists {
			changes = append(changes, Change{Kind: ObjectDeleted, DistinguishedName: dn})
		}
	}

	for _, dn := range sortedKeys(after) {
		previousAttributes, exists := before[dn]
		if !exists {
			continue
		}
		attributeChanges := diffAttributes(previousAttributes, after[dn], ignored)
		if len(attributeChanges) > 0 {
			changes = append(changes, Change{
				Kind:              ObjectUpdated,
				DistinguishedName: dn,
				Attributes:        attributeChanges,
			})
		}
	}

	return changes
}

// diffAttributes compares the attributes of one object between two snapshots.
//
// Parameters:
//
//	before (map[string][]string): The attributes of the object at the previous query.
//	after (map[string][]string): The attributes of the object at the latest query.
//	ignored (map[string]bool): The set of lower-cased attribute names not to report.
//
// Returns:
//
//	Every attribute whose values differ, sorted by attribute name.
func diffAttributes(before map[string][]string, after map[string][]string, ignored map[string]bool) []AttributeChange {
	names := make(map[string]bool, len(before)+len(after))
	for name := range before {
		names[name] = true
	}
	for name := range after {
		names[name] = true
	}

	attributeChanges := []AttributeChange{}
	for _, name := range sortedNames(names) {
		if ignored[strings.ToLower(name)] {
			continue
		}
		previousValues, hadValues := before[name]
		currentValues, hasValues := after[name]
		if hadValues && hasValues && slices.Equal(previousValues, currentValues) {
			continue
		}
		if !hadValues && !hasValues {
			continue
		}
		attributeChanges = append(attributeChanges, AttributeChange{
			Name:   name,
			Before: previousValues,
			After:  currentValues,
		})
	}

	return attributeChanges
}

// sortedKeys returns the distinguished names of a snapshot in ascending order.
//
// Parameters:
//
//	snapshot (Snapshot): The snapshot to read the distinguished names of.
//
// Returns:
//
//	The distinguished names, sorted.
func sortedKeys(snapshot Snapshot) []string {
	keys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedNames returns the members of a set of attribute names in ascending order.
//
// Parameters:
//
//	names (map[string]bool): The set of attribute names.
//
// Returns:
//
//	The attribute names, sorted.
func sortedNames(names map[string]bool) []string {
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	return sorted
}
