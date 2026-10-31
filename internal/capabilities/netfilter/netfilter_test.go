package netfilter

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type recordedAuditSink struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (s *recordedAuditSink) Record(ctx context.Context, event AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *recordedAuditSink) Events() []AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AuditEvent, len(s.events))
	copy(out, s.events)
	return out
}

func TestNetworkPolicyValidation(t *testing.T) {
	tests := []struct {
		name        string
		policy      NetworkPolicy
		expectError bool
	}{
		{
			name:        "valid allow all",
			policy:      NetworkPolicy{Mode: ModeAllowAll},
			expectError: false,
		},
		{
			name: "valid block internet",
			policy: NetworkPolicy{
				Mode:           ModeBlockInternet,
				AllowedSubnets: []string{"192.168.1.0/24", "10.0.0.1"},
				BlockPorts:     []int{80, 443, 8080},
			},
			expectError: false,
		},
		{
			name: "valid whitelist only",
			policy: NetworkPolicy{
				Mode:           ModeWhitelistOnly,
				AllowedSubnets: []string{"10.10.0.0/16"},
				AllowedDomains: []string{"exam.school.edu", "cdn.exam.org"},
				BlockPorts:     []int{22, 3389},
			},
			expectError: false,
		},
		{
			name:        "invalid mode",
			policy:      NetworkPolicy{Mode: "custom_filter"},
			expectError: true,
		},
		{
			name: "invalid subnet",
			policy: NetworkPolicy{
				Mode:           ModeBlockInternet,
				AllowedSubnets: []string{"not-an-ip"},
			},
			expectError: true,
		},
		{
			name: "invalid domain with space",
			policy: NetworkPolicy{
				Mode:           ModeWhitelistOnly,
				AllowedDomains: []string{"bad domain.com"},
			},
			expectError: true,
		},
		{
			name: "invalid port zero",
			policy: NetworkPolicy{
				Mode:       ModeBlockInternet,
				BlockPorts: []int{0},
			},
			expectError: true,
		},
		{
			name: "invalid port high",
			policy: NetworkPolicy{
				Mode:       ModeBlockInternet,
				BlockPorts: []int{70000},
			},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if tc.expectError && err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !tc.expectError && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestWindowsNetshAdapter_CommandGeneration(t *testing.T) {
	rule1 := FirewallRule{
		Name:        "Test-Allow-LAN",
		Direction:   DirectionOut,
		Action:      ActionAllow,
		Protocol:    ProtocolAny,
		RemoteIP:    "192.168.1.0/24",
		Description: "Allow LAN",
	}

	args1 := BuildAddRuleArgs(rule1)
	expectedArgs1 := []string{
		"advfirewall", "firewall", "add", "rule",
		"name=Test-Allow-LAN",
		"dir=out",
		"action=allow",
		"remoteip=192.168.1.0/24",
	}
	if strings.Join(args1, " ") != strings.Join(expectedArgs1, " ") {
		t.Fatalf("args mismatch:\nexpected: %v\ngot:      %v", expectedArgs1, args1)
	}

	rule2 := FirewallRule{
		Name:        "Test-Block-Port-80",
		Direction:   DirectionOut,
		Action:      ActionBlock,
		Protocol:    ProtocolTCP,
		RemotePort:  "80",
		Description: "Block HTTP",
	}

	args2 := BuildAddRuleArgs(rule2)
	expectedArgs2 := []string{
		"advfirewall", "firewall", "add", "rule",
		"name=Test-Block-Port-80",
		"dir=out",
		"action=block",
		"protocol=TCP",
		"remoteport=80",
	}
	if strings.Join(args2, " ") != strings.Join(expectedArgs2, " ") {
		t.Fatalf("args mismatch:\nexpected: %v\ngot:      %v", expectedArgs2, args2)
	}

	deleteArgs := BuildDeleteRuleArgs("Test-Rule")
	expectedDelete := []string{"advfirewall", "firewall", "delete", "rule", "name=Test-Rule"}
	if strings.Join(deleteArgs, " ") != strings.Join(expectedDelete, " ") {
		t.Fatalf("delete args mismatch: %v", deleteArgs)
	}
}

func TestWindowsNetshAdapter_ExecutionAndRollback(t *testing.T) {
	var mu sync.Mutex
	executedCommands := make([][]string, 0)
	failOnRuleName := ""

	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()

		cmd := append([]string{name}, args...)
		executedCommands = append(executedCommands, cmd)

		for _, arg := range args {
			if failOnRuleName != "" && strings.Contains(arg, failOnRuleName) && strings.Contains(cmd[3], "add") {
				return []byte("Access denied or syntax error"), errors.New("command failed")
			}
		}
		return []byte("Ok."), nil
	}

	adapter := NewWindowsNetshAdapter(mockRunner)

	rules := []FirewallRule{
		{Name: "Rule-1", Direction: DirectionOut, Action: ActionAllow, RemoteIP: "10.0.0.1"},
		{Name: "Rule-2", Direction: DirectionOut, Action: ActionBlock, RemoteIP: "any"},
	}

	// Successful application
	ctx := context.Background()
	if err := adapter.ApplyRules(ctx, rules); err != nil {
		t.Fatalf("unexpected error applying rules: %v", err)
	}

	active, err := adapter.GetActiveRules(ctx)
	if err != nil || len(active) != 2 {
		t.Fatalf("expected 2 active rules, got %d (err: %v)", len(active), err)
	}

	// Now test error rollback: fail on Rule-2
	mu.Lock()
	failOnRuleName = "Rule-2"
	executedCommands = nil
	mu.Unlock()

	err = adapter.ApplyRules(ctx, rules)
	if err == nil {
		t.Fatal("expected error applying rules with failure injected, got nil")
	}

	// Verify rollback command was executed for Rule-1
	mu.Lock()
	rollbackFound := false
	for _, cmd := range executedCommands {
		if len(cmd) >= 6 && cmd[1] == "advfirewall" && cmd[2] == "firewall" && cmd[3] == "delete" && strings.Contains(cmd[5], "Rule-1") {
			rollbackFound = true
			break
		}
	}
	mu.Unlock()

	if !rollbackFound {
		t.Fatal("expected rollback delete command for Rule-1 upon Rule-2 failure")
	}

	// Test ResetRules
	if err := adapter.ResetRules(ctx, "TestPrefix"); err != nil {
		t.Fatalf("unexpected error resetting rules: %v", err)
	}
	active, _ = adapter.GetActiveRules(ctx)
	if len(active) != 0 {
		t.Fatalf("expected 0 active rules after reset, got %d", len(active))
	}
}

func TestManager_PolicyOrchestrationAndAudit(t *testing.T) {
	mockAdapter := NewMockFirewallAdapter()
	auditSink := &recordedAuditSink{}

	mockResolver := func(ctx context.Context, domain string) ([]string, error) {
		if domain == "exam.school.edu" {
			return []string{"198.51.100.10", "198.51.100.11"}, nil
		}
		return nil, errors.New("domain not found")
	}

	mgr := NewManager(
		mockAdapter,
		WithAuditSink(auditSink),
		WithDomainResolver(mockResolver),
		WithRulePrefix("ExamPrefix"),
	)

	ctx := context.Background()

	// 1. Initial State
	if mgr.GetPolicy().Mode != ModeAllowAll {
		t.Fatalf("expected initial mode ModeAllowAll, got %s", mgr.GetPolicy().Mode)
	}

	// 2. Apply ModeBlockInternet
	blockPolicy := NetworkPolicy{
		Mode:           ModeBlockInternet,
		AllowedSubnets: []string{"192.168.100.0/24"},
		BlockPorts:     []int{8080},
	}
	if err := mgr.ApplyPolicy(ctx, blockPolicy); err != nil {
		t.Fatalf("failed applying ModeBlockInternet: %v", err)
	}

	if mgr.GetPolicy().Mode != ModeBlockInternet {
		t.Fatalf("expected current policy ModeBlockInternet, got %s", mgr.GetPolicy().Mode)
	}

	activeRules, err := mgr.GetActiveRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(activeRules) == 0 {
		t.Fatal("expected active rules to be populated for ModeBlockInternet")
	}

	// Check that LAN subnets, block-internet, and blocked ports exist
	hasBlockInternet := false
	hasPort8080TCP := false
	hasAllowedCustomSubnet := false

	for _, r := range activeRules {
		if strings.Contains(r.Name, "block-internet") {
			hasBlockInternet = true
		}
		if strings.Contains(r.Name, "block-port-8080-tcp") && r.RemotePort == "8080" {
			hasPort8080TCP = true
		}
		if r.RemoteIP == "192.168.100.0/24" && r.Action == ActionAllow {
			hasAllowedCustomSubnet = true
		}
	}

	if !hasBlockInternet {
		t.Error("missing block-internet rule")
	}
	if !hasPort8080TCP {
		t.Error("missing port 8080 block rule")
	}
	if !hasAllowedCustomSubnet {
		t.Error("missing allowed custom subnet rule")
	}

	// 3. Apply ModeWhitelistOnly with domain resolution
	whitelistPolicy := NetworkPolicy{
		Mode:           ModeWhitelistOnly,
		AllowedSubnets: []string{"10.50.0.0/16"},
		AllowedDomains: []string{"exam.school.edu"},
		BlockPorts:     []int{23},
	}
	if err := mgr.ApplyPolicy(ctx, whitelistPolicy); err != nil {
		t.Fatalf("failed applying ModeWhitelistOnly: %v", err)
	}

	activeRules, _ = mgr.GetActiveRules(ctx)
	hasDNSUDP := false
	hasDNSTCP := false
	hasDomainIP1 := false
	hasDomainIP2 := false

	for _, r := range activeRules {
		if strings.Contains(r.Name, "allow-dns-udp") && r.RemotePort == "53" {
			hasDNSUDP = true
		}
		if strings.Contains(r.Name, "allow-dns-tcp") && r.RemotePort == "53" {
			hasDNSTCP = true
		}
		if r.RemoteIP == "198.51.100.10" {
			hasDomainIP1 = true
		}
		if r.RemoteIP == "198.51.100.11" {
			hasDomainIP2 = true
		}
	}

	if !hasDNSUDP || !hasDNSTCP {
		t.Error("missing DNS allow rules in whitelist mode")
	}
	if !hasDomainIP1 || !hasDomainIP2 {
		t.Errorf("missing resolved domain IP rules: ip1=%v, ip2=%v", hasDomainIP1, hasDomainIP2)
	}

	// 4. Test Reset
	if err := mgr.Reset(ctx); err != nil {
		t.Fatalf("failed to reset manager: %v", err)
	}
	if mgr.GetPolicy().Mode != ModeAllowAll {
		t.Fatalf("expected policy to be ModeAllowAll after reset, got %s", mgr.GetPolicy().Mode)
	}
	activeRules, _ = mgr.GetActiveRules(ctx)
	if len(activeRules) != 0 {
		t.Fatalf("expected 0 active rules after reset, got %d", len(activeRules))
	}

	// 5. Test Fail-safe Cleanup on Close
	// Re-apply a blocking policy before close
	if err := mgr.ApplyPolicy(ctx, blockPolicy); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Close(ctx); err != nil {
		t.Fatalf("failed to close manager: %v", err)
	}

	// Verify rules are cleared on close
	activeRules, _ = mgr.GetActiveRules(ctx)
	if len(activeRules) != 0 {
		t.Fatalf("expected 0 active rules after Close (fail-safe cleanup), got %d", len(activeRules))
	}

	// Subsequent operations should fail with ErrManagerClosed
	err = mgr.ApplyPolicy(ctx, blockPolicy)
	if err != ErrManagerClosed {
		t.Fatalf("expected ErrManagerClosed after Close, got %v", err)
	}

	// Verify Audit Events
	events := auditSink.Events()
	var actions []AuditAction
	for _, ev := range events {
		actions = append(actions, ev.Action)
	}

	expectedActions := []AuditAction{
		AuditNetfilterPolicyApplied, // block internet
		AuditNetfilterPolicyApplied, // whitelist
		AuditNetfilterReset,         // reset
		AuditNetfilterPolicyApplied, // block internet again
		AuditNetfilterShutdown,      // close
	}

	for _, exp := range expectedActions {
		found := false
		for _, a := range actions {
			if a == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected audit action %s in emitted events %v", exp, actions)
		}
	}
}

func TestManager_AdapterFailureHandling(t *testing.T) {
	mockAdapter := NewMockFirewallAdapter()
	mockAdapter.FailOnApply = errors.New("simulated firewall permission failure")

	auditSink := &recordedAuditSink{}
	mgr := NewManager(mockAdapter, WithAuditSink(auditSink))

	ctx := context.Background()
	policy := NetworkPolicy{
		Mode: ModeBlockInternet,
	}

	err := mgr.ApplyPolicy(ctx, policy)
	if err == nil {
		t.Fatal("expected error when adapter ApplyRules fails, got nil")
	}

	// Verify error audit event was recorded
	events := auditSink.Events()
	foundErrorEvent := false
	for _, ev := range events {
		if ev.Action == AuditNetfilterError {
			foundErrorEvent = true
			if !strings.Contains(ev.Error, "simulated firewall permission failure") {
				t.Fatalf("expected error message in audit event, got: %s", ev.Error)
			}
		}
	}
	if !foundErrorEvent {
		t.Fatal("expected AuditNetfilterError event to be recorded on failure")
	}
}
