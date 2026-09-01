package directory

import (
	"fmt"
	"slices"

	"github.com/TheManticoreProject/Manticore/logger"
	"github.com/TheManticoreProject/Manticore/network/ldap"
)

// DefaultLDAPFilter matches every object of a search base, the same way the
// directory itself enumerates them.
const DefaultLDAPFilter = "(objectClass=*)"

// Scope is what to read: where, and which objects.
type Scope struct {
	// SearchBases are the distinguished names to enumerate, each as a whole subtree.
	SearchBases []string `json:"searchBases"`
	// LDAPFilter restricts which objects are read. Empty means DefaultLDAPFilter.
	LDAPFilter string `json:"ldapFilter"`
}

// Filter returns the LDAP filter of the scope, falling back to the default.
//
// Returns:
//
//	The filter to send.
func (scope Scope) Filter() string {
	if scope.LDAPFilter == "" {
		return DefaultLDAPFilter
	}
	return scope.LDAPFilter
}

// Snapshot is the state of the monitored objects at one point in time: the
// distinguished name of every object mapped to its attributes, and each attribute
// mapped to its values.
type Snapshot map[string]map[string][]string

// TakeSnapshot reads every object in scope and returns their current state.
//
// Parameters:
//
//	ldapSession (*ldap.Session): The connected LDAP session to query.
//	scope (Scope): The search bases to enumerate and the filter to enumerate them with.
//	debug (bool): A flag indicating whether to print debug information.
//
// Returns:
//
//	The state of every object found, or an error if a search failed.
func TakeSnapshot(ldapSession *ldap.Session, scope Scope, debug bool) (Snapshot, error) {
	snapshot := make(Snapshot)
	filter := scope.Filter()

	for _, searchBase := range scope.SearchBases {
		entries, err := ldapSession.QueryWholeSubtree(searchBase, filter, []string{"*"})
		if err != nil {
			return nil, fmt.Errorf("error querying search base '%s': %w", searchBase, err)
		}

		if debug {
			logger.Debug(fmt.Sprintf("Search base '%s' returned %d objects", searchBase, len(entries)))
		}

		for _, entry := range entries {
			// A distinguished name is unique in the directory, so two search bases
			// returning the same one means they overlap: the second copy is the same
			// object and is skipped rather than diffed against itself.
			if _, exists := snapshot[entry.DN]; exists {
				if debug {
					logger.Debug(fmt.Sprintf("Object '%s' already in the snapshot, search bases overlap", entry.DN))
				}
				continue
			}
			attributes := attributesOf(entry)

			// A domain controller returns at most MaxValRange values of one
			// attribute per search (1500 by default) and renames the attribute when
			// it truncates it, so the rest has to be fetched with follow-up queries.
			// This costs extra round-trips only for the objects that actually hold a
			// truncated attribute.
			if hasRangedAttribute(attributes) {
				resolveRangedAttributes(ldapSession, entry.DN, attributes, filter, debug)
			}

			snapshot[entry.DN] = attributes
		}
	}

	return snapshot, nil
}

// hasRangedAttribute reports whether any attribute of an object was truncated by the
// server.
//
// Parameters:
//
//	attributes (map[string][]string): The attributes of the object.
//
// Returns:
//
//	True when at least one attribute name carries a range option, false otherwise.
func hasRangedAttribute(attributes map[string][]string) bool {
	for name := range attributes {
		if HasRangeOption(name) {
			return true
		}
	}
	return false
}

// attributesOf collects the attributes of an LDAP entry into a map, with the values
// of each attribute sorted.
//
// The values of an LDAP attribute are a set, and a server is free to return them in
// any order: a domain controller answering a range request returns the same
// membership in a different order than it does for an untruncated read, and the order
// is not guaranteed to hold between two searches either. Sorting them here is what
// makes a comparison between two snapshots a comparison of the values rather than of
// the order the server happened to send them in, and what makes the rendered output
// stable.
//
// Parameters:
//
//	entry (*ldap.Entry): The entry to read the attributes of.
//
// Returns:
//
//	The attributes of the entry, mapped from attribute name to sorted values.
func attributesOf(entry *ldap.Entry) map[string][]string {
	attributes := make(map[string][]string, len(entry.Attributes))
	for _, attribute := range entry.Attributes {
		// The entry is discarded right after this, so its slice is sorted in place
		// rather than copied.
		slices.Sort(attribute.Values)
		attributes[attribute.Name] = attribute.Values
	}
	return attributes
}
