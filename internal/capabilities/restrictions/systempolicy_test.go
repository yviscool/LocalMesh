package restrictions

import (
	"testing"
)

func TestCompileSystemPolicyEnableDisable(t *testing.T) {
	current := SystemPolicy{Enabled: map[SystemRestriction]bool{}}
	desired := SystemPolicy{Enabled: map[SystemRestriction]bool{
		RestrictTaskMgr: true,
	}}
	cmds := CompileSystemPolicy(current, desired)
	if len(cmds) != 1 {
		t.Fatalf("expected 1 add command, got %+v", cmds)
	}
	if cmds[0].Args[0] != "add" || cmds[0].Args[2] != "/v" || cmds[0].Args[3] != "DisableTaskMgr" {
		t.Fatalf("unexpected command: %+v", cmds[0])
	}

	// 还原：desired 清空 → delete
	cmds = CompileSystemPolicy(SystemPolicy{Enabled: map[SystemRestriction]bool{RestrictTaskMgr: true}}, SystemPolicy{Enabled: map[SystemRestriction]bool{}})
	if len(cmds) != 1 || cmds[0].Args[0] != "delete" {
		t.Fatalf("expected delete command, got %+v", cmds)
	}
}

func TestCompileSystemPolicyNoop(t *testing.T) {
	same := SystemPolicy{Enabled: map[SystemRestriction]bool{RestrictTaskMgr: true}}
	if cmds := CompileSystemPolicy(same, same); len(cmds) != 0 {
		t.Fatalf("expected no-op, got %+v", cmds)
	}
}

func TestSystemPolicyValidate(t *testing.T) {
	if err := (SystemPolicy{Enabled: map[SystemRestriction]bool{"hack": true}}).Validate(); err == nil {
		t.Fatal("expected unknown restriction error")
	}
	if err := (SystemPolicy{Enabled: map[SystemRestriction]bool{RestrictUSBStorage: true}}).Validate(); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
}
