package classroom

import "testing"

func TestClassroomControlRequiresMembershipAndCapability(t *testing.T) {
	id, err := NewClassroomID("CLASS-2026-001")
	if err != nil {
		t.Fatal(err)
	}
	c := New(id, "C++ 基础班")
	if err := c.AddMember(Member{ID: "T-001", Kind: MemberTeacher}); err != nil {
		t.Fatal(err)
	}
	if c.CanControl("T-001", map[string]bool{}) {
		t.Fatal("membership must not grant control capability")
	}
	if !c.CanControl("T-001", map[string]bool{"classroom.control": true}) {
		t.Fatal("authorized classroom member should be able to control")
	}
	if c.CanControl("T-999", map[string]bool{"classroom.control": true}) {
		t.Fatal("unknown member must not control classroom")
	}
}
