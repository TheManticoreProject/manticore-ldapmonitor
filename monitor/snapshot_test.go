package monitor

import (
	"reflect"
	"testing"

	"github.com/TheManticoreProject/Manticore/network/ldap"
	ldapv3 "github.com/go-ldap/ldap/v3"
)

func entryWithAttributes(dn string, attributes map[string][]string) *ldap.Entry {
	entry := &ldap.Entry{DN: dn}
	for name, values := range attributes {
		entry.Attributes = append(entry.Attributes, &ldapv3.EntryAttribute{Name: name, Values: values})
	}
	return entry
}

func TestAttributesOfMapsNamesToValues(t *testing.T) {
	entry := entryWithAttributes("CN=PC01,DC=MANTICORE,DC=local", map[string][]string{
		"cn":          {"PC01"},
		"description": {"a workstation"},
	})

	got := attributesOf(entry)

	want := map[string][]string{"cn": {"PC01"}, "description": {"a workstation"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unexpected attributes:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestAttributesOfSortsValues(t *testing.T) {
	entry := entryWithAttributes("CN=group1,DC=MANTICORE,DC=local", map[string][]string{
		"member": {"CN=c", "CN=a", "CN=b"},
	})

	got := attributesOf(entry)["member"]

	if !reflect.DeepEqual(got, []string{"CN=a", "CN=b", "CN=c"}) {
		t.Errorf("expected the values to be sorted, got %v", got)
	}
}

// The values of an LDAP attribute are a set and the server orders them as it likes:
// a domain controller answering a range request returns a membership in a different
// order than an untruncated read of the same attribute does. Two reads of an
// unchanged object must not be reported as a change because of that.
func TestSnapshotsOfTheSameValuesInADifferentOrderCompareEqual(t *testing.T) {
	dn := "CN=group1,CN=Users,DC=MANTICORE,DC=local"

	before := Snapshot{dn: attributesOf(entryWithAttributes(dn, map[string][]string{
		"member": {"CN=Domain Admins", "CN=Enterprise Admins", "CN=Administrator"},
	}))}
	after := Snapshot{dn: attributesOf(entryWithAttributes(dn, map[string][]string{
		"member": {"CN=Administrator", "CN=Domain Admins", "CN=Enterprise Admins"},
	}))}

	changes := Diff(before, after, IgnoredAttributes(false))

	if len(changes) != 0 {
		t.Errorf("expected the reordering not to be reported as a change, got %+v", changes)
	}
}

func TestSnapshotStillReportsARealMembershipChange(t *testing.T) {
	dn := "CN=group1,CN=Users,DC=MANTICORE,DC=local"

	before := Snapshot{dn: attributesOf(entryWithAttributes(dn, map[string][]string{
		"member": {"CN=b", "CN=a"},
	}))}
	after := Snapshot{dn: attributesOf(entryWithAttributes(dn, map[string][]string{
		"member": {"CN=c", "CN=a", "CN=b"},
	}))}

	changes := Diff(before, after, IgnoredAttributes(false))

	if len(changes) != 1 || len(changes[0].Attributes) != 1 {
		t.Fatalf("expected the added member to be reported, got %+v", changes)
	}
	if !reflect.DeepEqual(changes[0].Attributes[0].After, []string{"CN=a", "CN=b", "CN=c"}) {
		t.Errorf("unexpected new values: %v", changes[0].Attributes[0].After)
	}
}
