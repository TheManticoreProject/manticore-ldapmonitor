package monitor

import (
	"strings"
	"testing"
)

func TestFormatTimestampRendersGeneralizedTime(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		// The form a domain controller writes whenChanged and whenCreated in.
		{"whenChanged", "20260831140043.0Z", "2026-08-31 14:00:43 UTC"},
		{"whenCreated", "20260831140043Z", "2026-08-31 14:00:43 UTC"},
		{"whenChanged", "20260831140043.123Z", "2026-08-31 14:00:43 UTC"},
		// An offset is normalized to UTC, so two domain controllers reporting the
		// same instant render identically.
		{"whenChanged", "20260831160043.0+0200", "2026-08-31 14:00:43 UTC"},
		{"whenChanged", "20260831120043.0-0200", "2026-08-31 14:00:43 UTC"},
		// dSCorePropagationData carries the FILETIME epoch as a generalizedTime.
		{"dSCorePropagationData", "16010101000000.0Z", "1601-01-01 00:00:00 UTC"},
	}

	for _, test := range tests {
		got, ok := FormatTimestamp(test.name, test.value)
		if !ok {
			t.Errorf("FormatTimestamp(%q, %q) did not recognize a timestamp", test.name, test.value)
			continue
		}
		if got != test.want {
			t.Errorf("FormatTimestamp(%q, %q) = %q, want %q", test.name, test.value, got, test.want)
		}
	}
}

func TestFormatTimestampRendersFiletime(t *testing.T) {
	// 133707974417531238 ticks since 1601-01-01, the shape lastLogon comes back in.
	got, ok := FormatTimestamp("lastLogon", "133707974417531238")

	if !ok {
		t.Fatal("expected lastLogon to be recognized as a FILETIME")
	}
	if got != "2024-09-14 14:24:01 UTC" {
		t.Errorf("unexpected rendering: %s", got)
	}
}

func TestFormatTimestampRecognizesFiletimeAttributesCaseInsensitively(t *testing.T) {
	for _, name := range []string{"pwdLastSet", "PWDLASTSET", "pwdlastset"} {
		if _, ok := FormatTimestamp(name, "133707974417531238"); !ok {
			t.Errorf("expected %q to be recognized as a FILETIME attribute", name)
		}
	}
}

func TestFormatTimestampRendersTheNeverSentinels(t *testing.T) {
	// 0 on lastLogon or pwdLastSet, and the largest signed 64-bit integer on
	// accountExpires, both mean "not set" rather than a date.
	for _, test := range []struct{ name, value string }{
		{"lastLogon", "0"},
		{"pwdLastSet", "0"},
		{"accountExpires", "9223372036854775807"},
	} {
		got, ok := FormatTimestamp(test.name, test.value)
		if !ok || got != "never" {
			t.Errorf("FormatTimestamp(%q, %q) = %q, %v; want \"never\", true", test.name, test.value, got, ok)
		}
	}
}

// A FILETIME cannot be told from any other large integer by looking at it, so an
// attribute that is not a timestamp has to be left alone however large its value.
func TestFormatTimestampLeavesOtherLargeIntegersAlone(t *testing.T) {
	for _, name := range []string{"uSNChanged", "uSNCreated", "msDS-KeyVersionNumber"} {
		if got, ok := FormatTimestamp(name, "133707974417531238"); ok {
			t.Errorf("FormatTimestamp(%q, ...) converted a counter to %q", name, got)
		}
	}
}

func TestFormatTimestampRejectsValuesThatAreNotTimestamps(t *testing.T) {
	for _, test := range []struct{ name, value string }{
		{"lastLogon", "not a number"},
		{"lastLogon", "-5"},
		{"whenChanged", "20261332140043.0Z"}, // month 13, day 32
		{"whenChanged", "2026083114004.0Z"},  // thirteen digits
		{"whenChanged", "20260831140043"},    // no zone
		{"cn", "PC01"},
	} {
		if got, ok := FormatTimestamp(test.name, test.value); ok {
			t.Errorf("FormatTimestamp(%q, %q) = %q, want it rejected", test.name, test.value, got)
		}
	}
}

func TestFormatValuesRendersTimestampsThroughTheAttributeName(t *testing.T) {
	if got := FormatValues("whenChanged", []string{"20260831140043.0Z"}); got != "'2026-08-31 14:00:43 UTC'" {
		t.Errorf("unexpected rendering: %s", got)
	}
	if got := FormatValues("uSNChanged", []string{"290945"}); got != "'290945'" {
		t.Errorf("unexpected rendering: %s", got)
	}
}

func TestDescribeAttributeChangeRendersTimestampsAsDates(t *testing.T) {
	line := describeAttributeChange(AttributeChange{
		Name:   "whenChanged",
		Before: []string{"20260831135153.0Z"},
		After:  []string{"20260831140043.0Z"},
	})

	for _, want := range []string{"2026-08-31 13:51:53 UTC", "2026-08-31 14:00:43 UTC"} {
		if !strings.Contains(line, want) {
			t.Errorf("expected %q in %q", want, line)
		}
	}
}
