package directory

import (
	"strings"
	"testing"
)

func TestFormatValueKeepsPrintableText(t *testing.T) {
	if got := FormatValue("CN=PC01,CN=Computers,DC=MANTICORE,DC=local"); got != "CN=PC01,CN=Computers,DC=MANTICORE,DC=local" {
		t.Errorf("unexpected rendering: %s", got)
	}
}

func TestFormatValueHexEncodesBinaryValues(t *testing.T) {
	// The first bytes of an objectSid: a revision, a sub-authority count and an
	// authority, none of which are printable text.
	got := FormatValue("\x01\x05\x00\x00\x00\x00\x00\x05")

	if got != "0105000000000005" {
		t.Errorf("unexpected rendering: %s", got)
	}
}

func TestFormatValueTruncatesLongBinaryValues(t *testing.T) {
	value := strings.Repeat("\x00", 512)

	got := FormatValue(value)

	if !strings.HasSuffix(got, "... (512 bytes)") {
		t.Errorf("expected the length of the value to be reported, got %s", got)
	}
	if len(got) > 2*maxRawValueBytes+len("... (512 bytes)") {
		t.Errorf("expected the hex to be cut at %d bytes, got %d characters", maxRawValueBytes, len(got))
	}
}

func TestFormatValueHexEncodesControlCharacters(t *testing.T) {
	// A value carrying an ANSI escape would repaint the terminal if printed as-is.
	got := FormatValue("value\x1b[31m")

	if strings.Contains(got, "\x1b") {
		t.Errorf("expected the escape to be encoded, got %q", got)
	}
}

func TestFormatValuesRendersASingleValueOnItsOwn(t *testing.T) {
	if got := FormatValues("cn", []string{"PC01"}); got != "'PC01'" {
		t.Errorf("unexpected rendering: %s", got)
	}
}

func TestFormatValuesRendersSeveralValuesAsAList(t *testing.T) {
	if got := FormatValues("member", []string{"CN=a", "CN=b"}); got != "['CN=a', 'CN=b']" {
		t.Errorf("unexpected rendering: %s", got)
	}
}

func TestDescribeAttributeChangeNamesTheChange(t *testing.T) {
	tests := []struct {
		name            string
		attributeChange AttributeChange
		wants           []string
	}{
		{
			name:            "changed",
			attributeChange: AttributeChange{Name: "badPwdCount", Before: []string{"0"}, After: []string{"1"}},
			wants:           []string{"badPwdCount", "changed from", "'0'", "'1'"},
		},
		{
			name:            "created",
			attributeChange: AttributeChange{Name: "description", After: []string{"set"}},
			wants:           []string{"description", "'set'", "was created"},
		},
		{
			name:            "deleted",
			attributeChange: AttributeChange{Name: "description", Before: []string{"set"}},
			wants:           []string{"description", "'set'", "was deleted"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := describeAttributeChange(test.attributeChange)
			for _, want := range test.wants {
				if !strings.Contains(got, want) {
					t.Errorf("expected %q in %q", want, got)
				}
			}
		})
	}
}

func TestFormatTextKeepsReadableNames(t *testing.T) {
	name := "CN=PC01,CN=Computers,DC=MANTICORE,DC=local"
	if got := FormatText(name); got != name {
		t.Errorf("unexpected rendering: %s", got)
	}
}

func TestFormatTextEscapesTerminalEscapesInADistinguishedName(t *testing.T) {
	// An attacker with create rights on an OU picks the relative distinguished
	// name, so a DN can carry a sequence that clears the operator's screen.
	got := FormatText("CN=evil\x1b[2J,CN=Computers,DC=MANTICORE,DC=local")

	if strings.Contains(got, "\x1b") {
		t.Errorf("expected the escape to be neutralized, got %q", got)
	}
	if !strings.Contains(got, "\\x1b") {
		t.Errorf("expected the escape to be shown as \\x1b, got %q", got)
	}
	// The rest of the name has to stay readable.
	if !strings.Contains(got, "CN=Computers,DC=MANTICORE,DC=local") {
		t.Errorf("expected the name to stay readable, got %q", got)
	}
}

func TestFormatTextEscapesBytesThatAreNotValidUTF8(t *testing.T) {
	got := FormatText("CN=\xff\xfe,DC=local")

	if !strings.Contains(got, "\\xff") || !strings.Contains(got, "\\xfe") {
		t.Errorf("expected the raw bytes to be escaped, got %q", got)
	}
}

func TestFormatTextKeepsNonASCIINames(t *testing.T) {
	// A printable non-ASCII name is legitimate and has to survive untouched. The
	// accented characters are written as escapes so the source file stays ASCII.
	name := "CN=\u00c9mile Zola,CN=Users,DC=MANTICORE,DC=local"
	if got := FormatText(name); got != name {
		t.Errorf("expected the name to be kept as-is, got %q", got)
	}
}
