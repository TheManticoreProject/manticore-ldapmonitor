package directory

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/TheManticoreProject/Manticore/logger"
	"github.com/TheManticoreProject/Manticore/network/ldap"
)

// rangeSuffixPattern matches the range option a domain controller appends to an
// attribute name when it returns only part of the values of that attribute, as in
// "member;range=0-1499". The upper bound is "*" on the last chunk.
//
// See MS-ADTS 3.1.1.3.1.3.3 (Incremental Retrieval of Multi-valued Properties).
var rangeSuffixPattern = regexp.MustCompile(`(?i);range=(\d+)-(\d+|\*)$`)

// maxRangeRequests caps how many follow-up queries are spent on one attribute. At the
// default MaxValRange of 1500 values per chunk this covers 1.5 million values, well
// past anything a directory holds, and it guarantees the loop ends even if a server
// answers in a way that never reaches the final chunk.
const maxRangeRequests = 1000

// HasRangeOption reports whether an attribute name carries a range option, and so
// holds only part of the values of that attribute.
//
// The cheap check for a semicolon comes first: an attribute name normally has no
// option at all, and this runs once per attribute of every object of every snapshot.
//
// Parameters:
//
//	name (string): The attribute name as the server returned it.
//
// Returns:
//
//	True when the name carries a range option, false otherwise.
func HasRangeOption(name string) bool {
	if !strings.Contains(name, ";") {
		return false
	}
	return rangeSuffixPattern.MatchString(name)
}

// resolveRangedAttributes replaces every range-limited attribute of an object with
// its complete set of values.
//
// A domain controller caps how many values of one attribute a search returns
// (MaxValRange, 1500 by default). Past that cap it renames the attribute in the
// response, from "member" to "member;range=0-1499", and the remaining values have to
// be fetched with follow-up queries. Without this, a group with more than 1500
// members would report no membership change past the first chunk, and the attribute
// would be keyed under a name that changes as the group grows.
//
// Parameters:
//
//	ldapSession (*ldap.Session): The connected LDAP session to query.
//	distinguishedName (string): The distinguished name of the object being read.
//	attributes (map[string][]string): The attributes of the object, modified in place.
//	filter (string): The LDAP filter the object was read with.
//	debug (bool): A flag indicating whether to print debug information.
func resolveRangedAttributes(ldapSession *ldap.Session, distinguishedName string, attributes map[string][]string, filter string, debug bool) {
	// The names to work on are collected before the map is modified, rather than
	// deleting and inserting keys while ranging over it.
	rangedNames := []string{}
	for name := range attributes {
		if HasRangeOption(name) {
			rangedNames = append(rangedNames, name)
		}
	}

	for _, name := range rangedNames {
		values := attributes[name]
		match := rangeSuffixPattern.FindStringSubmatch(name)
		if match == nil {
			continue
		}

		baseName := name[:len(name)-len(match[0])]
		upperBound := match[2]

		// The ranged name is dropped whatever happens next: keying the attribute
		// under a name that moves with the number of values would report a change of
		// the range window as a change of the attribute.
		delete(attributes, name)

		complete, err := fetchRemainingValues(ldapSession, distinguishedName, baseName, values, upperBound, filter, debug)
		if err != nil {
			// A truncated attribute is still worth reporting: dropping the object
			// entirely would hide every other change on it. The values gathered so
			// far are kept and the shortfall is logged.
			logger.Warn(fmt.Sprintf("Could not read all values of '%s' on '%s': %s", baseName, distinguishedName, err))
		}
		// Sorted like every other attribute, so that a chunked read and an
		// untruncated read of the same values compare equal: the server returns the
		// chunks in an order of its own.
		slices.Sort(complete)
		attributes[baseName] = complete
	}
}

// fetchRemainingValues walks the remaining chunks of a range-limited attribute.
//
// Parameters:
//
//	ldapSession (*ldap.Session): The connected LDAP session to query.
//	distinguishedName (string): The distinguished name of the object being read.
//	baseName (string): The attribute name without its range option.
//	values ([]string): The values already returned by the first chunk.
//	upperBound (string): The upper bound of the first chunk, "*" when it was the last one.
//	filter (string): The LDAP filter the object was read with.
//	debug (bool): A flag indicating whether to print debug information.
//
// Returns:
//
//	Every value of the attribute, and an error if a chunk could not be read, in
//	which case the values gathered so far are still returned.
func fetchRemainingValues(ldapSession *ldap.Session, distinguishedName string, baseName string, values []string, upperBound string, filter string, debug bool) ([]string, error) {
	complete := make([]string, 0, len(values))
	complete = append(complete, values...)

	for requests := 0; upperBound != "*"; requests++ {
		if requests >= maxRangeRequests {
			return complete, fmt.Errorf("gave up after %d range requests", maxRangeRequests)
		}

		lastReturned, err := strconv.Atoi(upperBound)
		if err != nil {
			return complete, fmt.Errorf("unreadable range upper bound '%s': %w", upperBound, err)
		}

		// Asking for "<next>-*" lets the server return as many values as it is
		// willing to, and name the window it actually used in its answer.
		rangedName := fmt.Sprintf("%s;range=%d-*", baseName, lastReturned+1)
		if debug {
			logger.Debug(fmt.Sprintf("Retrieving '%s' of '%s'", rangedName, distinguishedName))
		}

		entries, err := ldapSession.QueryBaseObject(distinguishedName, filter, []string{rangedName})
		if err != nil {
			return complete, fmt.Errorf("error querying '%s': %w", rangedName, err)
		}
		if len(entries) == 0 {
			// The object was deleted between two queries. Whatever was read stands,
			// and the deletion itself is reported by the next diff.
			return complete, nil
		}

		chunkValues, chunkUpperBound, found := findRangedChunk(entries[0], baseName)
		if !found {
			return complete, fmt.Errorf("server returned no chunk for '%s'", rangedName)
		}

		complete = append(complete, chunkValues...)

		// A server that answers with the window it was already asked about, and no
		// new values, would otherwise loop forever.
		if chunkUpperBound != "*" && len(chunkValues) == 0 {
			return complete, fmt.Errorf("server returned no values for '%s'", rangedName)
		}
		upperBound = chunkUpperBound
	}

	return complete, nil
}

// findRangedChunk locates, in a follow-up response, the chunk of the attribute that
// was asked for.
//
// The server answers with its own range option rather than the one in the request,
// for instance "member;range=1500-2999", or "member;range=1500-*" for the last chunk,
// so the attribute is matched by name instead of by the requested window. An
// attribute small enough to fit in one chunk comes back with no range option at all.
//
// Parameters:
//
//	entry (*ldap.Entry): The entry returned by the follow-up query.
//	baseName (string): The attribute name without its range option.
//
// Returns:
//
//	The values of the chunk, its upper bound ("*" on the last chunk), and whether a
//	matching attribute was found at all.
func findRangedChunk(entry *ldap.Entry, baseName string) ([]string, string, bool) {
	for _, attribute := range entry.Attributes {
		if !strings.EqualFold(attribute.Name, baseName) &&
			!strings.HasPrefix(strings.ToLower(attribute.Name), strings.ToLower(baseName)+";range=") {
			continue
		}

		match := rangeSuffixPattern.FindStringSubmatch(attribute.Name)
		if match == nil {
			// No range option: the server returned the rest of the values in one go.
			return attribute.Values, "*", true
		}
		return attribute.Values, match[2], true
	}

	return nil, "", false
}
