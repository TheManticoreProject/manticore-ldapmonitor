package directory

import (
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/TheManticoreProject/Manticore/logger"
)

// maxRawValueBytes is how many bytes of a non-text attribute value are shown before
// the rendering is cut short. Security descriptors and replication metadata run to
// several kilobytes, and a change is just as visible from the first bytes.
const maxRawValueBytes = 32

// Render prints one change. The line naming the object carries the timestamp of the
// query that saw it, and the attributes below it are printed without one so the
// object reads as a single event.
//
// Parameters:
//
//	change (Change): The change to print.
func Render(change Change) {
	// The distinguished name is as attacker-controlled as an attribute value is:
	// whoever can create an object picks its relative distinguished name. It goes
	// through the same sanitizing as the values so a crafted name cannot repaint the
	// operator's terminal.
	distinguishedName := FormatText(change.DistinguishedName)

	switch change.Kind {
	case ObjectCreated:
		logger.Print(fmt.Sprintf("[\x1b[1;92m+\x1b[0m] \x1b[1;92mObject created: %s\x1b[0m", distinguishedName))
	case ObjectDeleted:
		logger.Print(fmt.Sprintf("[\x1b[1;91m-\x1b[0m] \x1b[1;91mObject deleted: %s\x1b[0m", distinguishedName))
	case ObjectUpdated:
		logger.Print(fmt.Sprintf("[\x1b[1;94m~\x1b[0m] \x1b[1;94mObject updated: %s\x1b[0m", distinguishedName))
		for index, attributeChange := range change.Attributes {
			branch := "  ├── "
			if index == len(change.Attributes)-1 {
				branch = "  └── "
			}
			logger.Plain.Print(branch + describeAttributeChange(attributeChange))
		}
	}
}

// describeAttributeChange renders one attribute change as a single line of text.
//
// Parameters:
//
//	attributeChange (AttributeChange): The attribute change to render.
//
// Returns:
//
//	The line describing the change, without the tree branch in front of it.
func describeAttributeChange(attributeChange AttributeChange) string {
	name := fmt.Sprintf("\x1b[93m%s\x1b[0m", FormatText(attributeChange.Name))

	switch {
	case attributeChange.Before == nil:
		return fmt.Sprintf("Attribute \"%s\" = \x1b[92m%s\x1b[0m was created", name, FormatValues(attributeChange.Name, attributeChange.After))
	case attributeChange.After == nil:
		return fmt.Sprintf("Attribute \"%s\" = \x1b[91m%s\x1b[0m was deleted", name, FormatValues(attributeChange.Name, attributeChange.Before))
	default:
		return fmt.Sprintf(
			"Attribute \"%s\" changed from \x1b[91m%s\x1b[0m to \x1b[92m%s\x1b[0m",
			name,
			FormatValues(attributeChange.Name, attributeChange.Before),
			FormatValues(attributeChange.Name, attributeChange.After),
		)
	}
}

// FormatValues renders the values of an attribute for display: a single value on its
// own, several values as a list.
//
// Parameters:
//
//	name (string): The attribute name, which decides whether a value is a timestamp.
//	values ([]string): The values of the attribute.
//
// Returns:
//
//	The rendered values.
func FormatValues(name string, values []string) string {
	if len(values) == 1 {
		return fmt.Sprintf("'%s'", formatAttributeValue(name, values[0]))
	}

	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("'%s'", formatAttributeValue(name, value)))
	}
	return fmt.Sprintf("[%s]", strings.Join(quoted, ", "))
}

// formatAttributeValue renders one value of a named attribute.
//
// A value the directory stores as a timestamp is rendered as a date, which is what
// makes a change to whenChanged or lastLogon readable rather than a pair of 18-digit
// numbers. Everything else falls through to FormatValue.
//
// Parameters:
//
//	name (string): The attribute name.
//	value (string): The raw attribute value.
//
// Returns:
//
//	The rendered value.
func formatAttributeValue(name string, value string) string {
	if formatted, isTimestamp := FormatTimestamp(name, value); isTimestamp {
		return formatted
	}
	return FormatValue(value)
}

// FormatValue renders one attribute value for display. A value that is not printable
// text, such as an objectSid, an objectGUID or an nTSecurityDescriptor, is
// hex-encoded and cut at maxRawValueBytes: printing it as-is would send control
// characters to the terminal, and printing it in full would bury the rest of the
// change.
//
// Parameters:
//
//	value (string): The raw attribute value.
//
// Returns:
//
//	The rendered value.
func FormatValue(value string) string {
	if isPrintable(value) {
		return value
	}

	raw := []byte(value)
	if len(raw) > maxRawValueBytes {
		return fmt.Sprintf("%s... (%d bytes)", hex.EncodeToString(raw[:maxRawValueBytes]), len(raw))
	}
	return hex.EncodeToString(raw)
}

// FormatText renders a name that comes from the directory as safe terminal text.
// Unlike FormatValue it keeps the text readable rather than hex-encoding the whole
// thing, because a distinguished name or an attribute name is text that the operator
// has to be able to read: only the characters that are not printable are replaced,
// each by its \xNN escape.
//
// Parameters:
//
//	text (string): The raw name.
//
// Returns:
//
//	The name with every non-printable character escaped.
func FormatText(text string) string {
	if isPrintable(text) {
		return text
	}

	var sanitized strings.Builder
	sanitized.Grow(len(text))
	for index := 0; index < len(text); index++ {
		character, size := utf8.DecodeRuneInString(text[index:])
		// RuneError with a size of 1 is a byte that is not valid UTF-8 at all, so it
		// is escaped as the raw byte it is rather than as a rune.
		if character == utf8.RuneError && size == 1 {
			sanitized.WriteString(fmt.Sprintf("\\x%02x", text[index]))
			continue
		}
		if unicode.IsPrint(character) {
			sanitized.WriteString(text[index : index+size])
		} else {
			for _, raw := range []byte(text[index : index+size]) {
				sanitized.WriteString(fmt.Sprintf("\\x%02x", raw))
			}
		}
		index += size - 1
	}
	return sanitized.String()
}

// isPrintable reports whether a value is text that can be written to a terminal
// as-is.
//
// Parameters:
//
//	value (string): The raw attribute value.
//
// Returns:
//
//	True when the value is valid UTF-8 holding no control character, false otherwise.
func isPrintable(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if !unicode.IsPrint(character) && character != '\t' {
			return false
		}
	}
	return true
}
