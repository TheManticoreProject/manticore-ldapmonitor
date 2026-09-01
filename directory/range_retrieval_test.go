package directory

import (
	"reflect"
	"testing"

	"github.com/TheManticoreProject/Manticore/network/ldap"
	ldapv3 "github.com/go-ldap/ldap/v3"
)

func TestHasRangeOption(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"member", false},
		{"member;range=0-1499", true},
		{"member;range=1500-*", true},
		{"member;RANGE=0-1499", true},
		{"member;binary", false},
		{"msDS-AllowedToDelegateTo", false},
		// A name that mentions a range without it being the option is not one.
		{"rangeAttribute", false},
		{"member;range=0-", false},
	}

	for _, test := range tests {
		if got := HasRangeOption(test.name); got != test.want {
			t.Errorf("HasRangeOption(%q) = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestHasRangedAttribute(t *testing.T) {
	if hasRangedAttribute(map[string][]string{"cn": {"group1"}, "member": {"CN=a"}}) {
		t.Error("expected no ranged attribute")
	}
	if !hasRangedAttribute(map[string][]string{"cn": {"group1"}, "member;range=0-1499": {"CN=a"}}) {
		t.Error("expected a ranged attribute")
	}
}

// entryWithAttribute builds the kind of response a domain controller sends for a
// follow-up range query.
func entryWithAttribute(name string, values []string) *ldap.Entry {
	return &ldap.Entry{
		DN:         "CN=group1,CN=Users,DC=MANTICORE,DC=local",
		Attributes: []*ldapv3.EntryAttribute{{Name: name, Values: values}},
	}
}

func TestFindRangedChunkReadsAnIntermediateChunk(t *testing.T) {
	entry := entryWithAttribute("member;range=1500-2999", []string{"CN=b"})

	values, upperBound, found := findRangedChunk(entry, "member")

	if !found {
		t.Fatal("expected the chunk to be found")
	}
	if upperBound != "2999" {
		t.Errorf("unexpected upper bound: %s", upperBound)
	}
	if !reflect.DeepEqual(values, []string{"CN=b"}) {
		t.Errorf("unexpected values: %v", values)
	}
}

func TestFindRangedChunkReadsTheFinalChunk(t *testing.T) {
	entry := entryWithAttribute("member;range=1500-*", []string{"CN=b", "CN=c"})

	_, upperBound, found := findRangedChunk(entry, "member")

	if !found {
		t.Fatal("expected the chunk to be found")
	}
	if upperBound != "*" {
		t.Errorf("expected the final chunk, got upper bound %s", upperBound)
	}
}

func TestFindRangedChunkTreatsAnUnrangedAnswerAsFinal(t *testing.T) {
	// A server that can return the rest of the values in one go drops the option.
	entry := entryWithAttribute("member", []string{"CN=b"})

	_, upperBound, found := findRangedChunk(entry, "member")

	if !found {
		t.Fatal("expected the chunk to be found")
	}
	if upperBound != "*" {
		t.Errorf("expected the answer to be final, got upper bound %s", upperBound)
	}
}

func TestFindRangedChunkMatchesTheAttributeNameCaseInsensitively(t *testing.T) {
	entry := entryWithAttribute("Member;Range=1500-*", []string{"CN=b"})

	if _, _, found := findRangedChunk(entry, "member"); !found {
		t.Error("expected the chunk to be found whatever the case of the name")
	}
}

func TestFindRangedChunkIgnoresOtherAttributes(t *testing.T) {
	entry := entryWithAttribute("memberOf", []string{"CN=somegroup"})

	if _, _, found := findRangedChunk(entry, "member"); found {
		t.Error("expected 'memberOf' not to be matched as a chunk of 'member'")
	}
}
