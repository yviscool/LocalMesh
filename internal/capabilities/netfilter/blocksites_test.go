package netfilter

import "testing"

// TestGenerateRulesBlockSites verifies blacklist mode creates block rules
// only for the resolved blacklisted domains and leaves general traffic alone.
func TestGenerateRulesBlockSites(t *testing.T) {
	policy := NetworkPolicy{
		Mode:           ModeBlockSites,
		BlockedDomains: []string{"game.com", "video.example.com"},
	}
	resolved := map[string][]string{
		"game.com":            {"93.184.216.34"},
		"video.example.com":   {"93.184.216.35", "93.184.216.36"},
		"allowed.example.com": {"1.2.3.4"}, // resolved but NOT blacklisted
	}

	rules := GenerateRules(policy, "Test-", resolved)

	if len(rules) != 3 {
		t.Fatalf("expected 3 block rules, got %d: %+v", len(rules), rules)
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if r.Action != ActionBlock || r.Direction != DirectionOut {
			t.Fatalf("expected outbound block rule, got %+v", r)
		}
		seen[r.RemoteIP] = true
	}
	for _, ip := range []string{"93.184.216.34", "93.184.216.35", "93.184.216.36"} {
		if !seen[ip] {
			t.Fatalf("expected block rule for %s", ip)
		}
	}
	if seen["1.2.3.4"] {
		t.Fatal("non-blacklisted domain must not be blocked")
	}

	// 无黑名单条目 → 零规则
	empty := GenerateRules(NetworkPolicy{Mode: ModeBlockSites}, "Test-", nil)
	if len(empty) != 0 {
		t.Fatalf("expected no rules for empty blacklist, got %d", len(empty))
	}
}

// TestNetworkPolicyBlockSitesValidation covers the new mode + domain checks.
func TestNetworkPolicyBlockSitesValidation(t *testing.T) {
	if err := (NetworkPolicy{Mode: ModeBlockSites, BlockedDomains: []string{"game.com"}}).Validate(); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	if err := (NetworkPolicy{Mode: ModeBlockSites, BlockedDomains: []string{"bad domain"}}).Validate(); err == nil {
		t.Fatal("expected error for invalid domain")
	}
	if err := (NetworkPolicy{Mode: "nonsense"}).Validate(); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}
