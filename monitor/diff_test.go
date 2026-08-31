package monitor

import (
	"reflect"
	"testing"
)

func TestDiffReportsCreatedObjects(t *testing.T) {
	before := Snapshot{}
	after := Snapshot{
		"CN=PC01,CN=Computers,DC=MANTICORE,DC=local": {"cn": {"PC01"}},
	}

	changes := Diff(before, after, IgnoredAttributes(false))

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Kind != ObjectCreated {
		t.Errorf("expected ObjectCreated, got %v", changes[0].Kind)
	}
	if changes[0].DistinguishedName != "CN=PC01,CN=Computers,DC=MANTICORE,DC=local" {
		t.Errorf("unexpected distinguished name: %s", changes[0].DistinguishedName)
	}
}

func TestDiffReportsDeletedObjects(t *testing.T) {
	before := Snapshot{
		"CN=PC01,CN=Computers,DC=MANTICORE,DC=local": {"cn": {"PC01"}},
	}
	after := Snapshot{}

	changes := Diff(before, after, IgnoredAttributes(false))

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Kind != ObjectDeleted {
		t.Errorf("expected ObjectDeleted, got %v", changes[0].Kind)
	}
}

func TestDiffReportsNothingWhenNothingChanged(t *testing.T) {
	snapshot := Snapshot{
		"CN=PC01,CN=Computers,DC=MANTICORE,DC=local": {"cn": {"PC01"}, "memberOf": {"CN=A", "CN=B"}},
	}

	changes := Diff(snapshot, snapshot, IgnoredAttributes(false))

	if len(changes) != 0 {
		t.Fatalf("expected no change, got %d: %+v", len(changes), changes)
	}
}

func TestDiffReportsChangedCreatedAndDeletedAttributes(t *testing.T) {
	dn := "CN=user1,CN=Users,DC=MANTICORE,DC=local"
	before := Snapshot{dn: {
		"badPwdCount":          {"0"},
		"servicePrincipalName": {"HOST/user1"},
	}}
	after := Snapshot{dn: {
		"badPwdCount": {"1"},
		"description": {"new description"},
	}}

	changes := Diff(before, after, IgnoredAttributes(false))

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Kind != ObjectUpdated {
		t.Fatalf("expected ObjectUpdated, got %v", changes[0].Kind)
	}

	// Attribute changes are sorted by name: badPwdCount, description, servicePrincipalName.
	expected := []AttributeChange{
		{Name: "badPwdCount", Before: []string{"0"}, After: []string{"1"}},
		{Name: "description", Before: nil, After: []string{"new description"}},
		{Name: "servicePrincipalName", Before: []string{"HOST/user1"}, After: nil},
	}
	if !reflect.DeepEqual(changes[0].Attributes, expected) {
		t.Errorf("unexpected attribute changes:\n got: %+v\nwant: %+v", changes[0].Attributes, expected)
	}
}

func TestDiffReportsMultiValuedAttributeChanges(t *testing.T) {
	dn := "CN=group1,CN=Users,DC=MANTICORE,DC=local"
	before := Snapshot{dn: {"member": {"CN=a", "CN=b"}}}
	after := Snapshot{dn: {"member": {"CN=a", "CN=b", "CN=c"}}}

	changes := Diff(before, after, IgnoredAttributes(false))

	if len(changes) != 1 || len(changes[0].Attributes) != 1 {
		t.Fatalf("expected 1 attribute change, got %+v", changes)
	}
	if !reflect.DeepEqual(changes[0].Attributes[0].After, []string{"CN=a", "CN=b", "CN=c"}) {
		t.Errorf("unexpected new values: %+v", changes[0].Attributes[0].After)
	}
}

func TestDiffIgnoresReplicationAttributesRegardlessOfCase(t *testing.T) {
	dn := "DC=MANTICORE,DC=local"
	before := Snapshot{dn: {"replUpToDateVector": {"before"}, "repsFrom": {"before"}, "dnsRecord": {"before"}}}
	after := Snapshot{dn: {"replUpToDateVector": {"after"}, "repsFrom": {"after"}, "dnsRecord": {"after"}}}

	changes := Diff(before, after, IgnoredAttributes(false))

	if len(changes) != 0 {
		t.Fatalf("expected no change, got %+v", changes)
	}
}

func TestDiffIgnoresUserLogonAttributesOnlyOnDemand(t *testing.T) {
	dn := "CN=user1,CN=Users,DC=MANTICORE,DC=local"
	before := Snapshot{dn: {"lastLogon": {"1"}, "logonCount": {"1"}}}
	after := Snapshot{dn: {"lastLogon": {"2"}, "logonCount": {"2"}}}

	if changes := Diff(before, after, IgnoredAttributes(false)); len(changes) != 1 {
		t.Fatalf("expected the logon changes to be reported, got %+v", changes)
	}
	if changes := Diff(before, after, IgnoredAttributes(true)); len(changes) != 0 {
		t.Fatalf("expected the logon changes to be ignored, got %+v", changes)
	}
}

func TestDiffDoesNotReportAnObjectWhoseOnlyChangesAreIgnored(t *testing.T) {
	dn := "CN=user1,CN=Users,DC=MANTICORE,DC=local"
	before := Snapshot{dn: {"lastLogon": {"1"}, "cn": {"user1"}}}
	after := Snapshot{dn: {"lastLogon": {"2"}, "cn": {"user1"}}}

	changes := Diff(before, after, IgnoredAttributes(true))

	if len(changes) != 0 {
		t.Fatalf("expected no change, got %+v", changes)
	}
}

func TestDiffSortsChangesByDistinguishedName(t *testing.T) {
	before := Snapshot{}
	after := Snapshot{
		"CN=c,DC=MANTICORE,DC=local": {},
		"CN=a,DC=MANTICORE,DC=local": {},
		"CN=b,DC=MANTICORE,DC=local": {},
	}

	changes := Diff(before, after, IgnoredAttributes(false))

	got := []string{}
	for _, change := range changes {
		got = append(got, change.DistinguishedName)
	}
	want := []string{"CN=a,DC=MANTICORE,DC=local", "CN=b,DC=MANTICORE,DC=local", "CN=c,DC=MANTICORE,DC=local"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unexpected order:\n got: %v\nwant: %v", got, want)
	}
}

func TestDiffTreatsAnEmptyValueListAsAnExistingAttribute(t *testing.T) {
	dn := "CN=user1,CN=Users,DC=MANTICORE,DC=local"
	before := Snapshot{dn: {"description": {}}}
	after := Snapshot{dn: {"description": {"set"}}}

	changes := Diff(before, after, IgnoredAttributes(false))

	if len(changes) != 1 || len(changes[0].Attributes) != 1 {
		t.Fatalf("expected 1 attribute change, got %+v", changes)
	}
	// The attribute was present before, so this is a change of values and not a
	// creation: Before has to stay non-nil.
	if changes[0].Attributes[0].Before == nil {
		t.Error("expected the previous values to be an empty list, got nil")
	}
}
