package directory

import (
	"math"
	"regexp"
	"strconv"
	"time"
)

// generalizedTimePattern matches an LDAP generalizedTime as a domain controller
// writes it: fourteen digits, an optional fraction, and either Z or a +/-HHMM offset.
// For example "20260831140043.0Z", the form whenChanged and whenCreated come back in.
var generalizedTimePattern = regexp.MustCompile(`^(\d{14})(?:\.\d+)?(Z|[+-]\d{4})$`)

// filetimeAttributes are the attributes whose value is a Windows FILETIME: the
// number of 100-nanosecond intervals since 1601-01-01 UTC, written as a decimal
// integer.
//
// A FILETIME is indistinguishable from any other large integer by looking at the
// value, so it is recognised by attribute name instead. Converting on shape alone
// would turn uSNChanged, or any other counter that happens to be large, into a
// meaningless date. Names are lower-cased, since LDAP attribute names are
// case-insensitive.
var filetimeAttributes = map[string]bool{
	"accountexpires":                          true,
	"badpasswordtime":                         true,
	"creationtime":                            true,
	"lastlogoff":                              true,
	"lastlogon":                               true,
	"lastlogontimestamp":                      true,
	"lastsettime":                             true,
	"lockouttime":                             true,
	"ms-mcs-admpwdexpirationtime":             true,
	"msds-cachedmembershiptimestamp":          true,
	"msds-lastfailedinteractivelogontime":     true,
	"msds-lastsuccessfulinteractivelogontime": true,
	"msds-userpasswordexpirytimecomputed":     true,
	"pwdlastset":                              true,
}

// filetimeEpochOffset is the number of seconds between the FILETIME epoch
// (1601-01-01) and the Unix epoch (1970-01-01).
const filetimeEpochOffset = 11644473600

// filetimeTicksPerSecond is how many 100-nanosecond intervals make up one second.
const filetimeTicksPerSecond = 10000000

// timestampDisplayLayout is how a converted timestamp is rendered. It is always in
// UTC: a domain controller records these in UTC, and rendering them in the local
// time zone of whoever is watching would make two runs of the tool disagree about
// what the directory says.
const timestampDisplayLayout = "2006-01-02 15:04:05 UTC"

// FormatTimestamp renders an attribute value as a date when the value is one.
//
// Two encodings are recognised: the LDAP generalizedTime, which is unambiguous from
// its shape, and the Windows FILETIME, which is recognised from the attribute name.
// A FILETIME of 0, or of the largest signed 64-bit integer, is the directory's way of
// saying "never" rather than a point in time, and is rendered as such.
//
// Parameters:
//
//	name (string): The attribute name, used to recognise a FILETIME.
//	value (string): The raw attribute value.
//
// Returns:
//
//	The rendered date and true, or an empty string and false when the value is not a
//	timestamp.
func FormatTimestamp(name string, value string) (string, bool) {
	if match := generalizedTimePattern.FindStringSubmatch(value); match != nil {
		return formatGeneralizedTime(match[1], match[2])
	}

	if filetimeAttributes[lowerASCII(name)] {
		return formatFiletime(value)
	}

	return "", false
}

// formatGeneralizedTime renders the digits and zone of a generalizedTime.
//
// Parameters:
//
//	digits (string): The fourteen date and time digits.
//	zone (string): Either "Z" or a +/-HHMM offset.
//
// Returns:
//
//	The rendered date and true, or an empty string and false when the digits do not
//	form a real date.
func formatGeneralizedTime(digits string, zone string) (string, bool) {
	layout := "20060102150405-0700"
	if zone == "Z" {
		layout = "20060102150405Z"
	}

	parsed, err := time.Parse(layout, digits+zone)
	if err != nil {
		return "", false
	}
	return parsed.UTC().Format(timestampDisplayLayout), true
}

// formatFiletime renders a Windows FILETIME held in a decimal string.
//
// Parameters:
//
//	value (string): The raw attribute value.
//
// Returns:
//
//	The rendered date and true, or an empty string and false when the value is not a
//	FILETIME that maps to a real point in time.
func formatFiletime(value string) (string, bool) {
	ticks, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return "", false
	}

	// Both of these mean "not set" rather than a date: 0 on lastLogon and pwdLastSet,
	// and the largest signed 64-bit integer on accountExpires.
	if ticks == 0 || ticks == math.MaxInt64 {
		return "never", true
	}
	if ticks < 0 {
		return "", false
	}

	seconds := ticks/filetimeTicksPerSecond - filetimeEpochOffset
	nanoseconds := (ticks % filetimeTicksPerSecond) * 100

	return time.Unix(seconds, nanoseconds).UTC().Format(timestampDisplayLayout), true
}

// lowerASCII lower-cases an attribute name without allocating for the common case of
// a name that is already lower-case.
//
// Parameters:
//
//	name (string): The attribute name.
//
// Returns:
//
//	The name in lower case.
func lowerASCII(name string) string {
	needsLowering := false
	for index := 0; index < len(name); index++ {
		if name[index] >= 'A' && name[index] <= 'Z' {
			needsLowering = true
			break
		}
	}
	if !needsLowering {
		return name
	}

	lowered := []byte(name)
	for index := range lowered {
		if lowered[index] >= 'A' && lowered[index] <= 'Z' {
			lowered[index] += 'a' - 'A'
		}
	}
	return string(lowered)
}
