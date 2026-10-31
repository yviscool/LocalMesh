package netfilter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidPolicyMode = errors.New("invalid policy mode: must be allow_all, block_internet, or whitelist_only")
	ErrInvalidSubnet     = errors.New("invalid subnet or IP address")
	ErrInvalidPort       = errors.New("invalid port number: must be between 1 and 65535")
	ErrInvalidDomain     = errors.New("invalid domain name")
	ErrManagerClosed     = errors.New("firewall manager is closed")
)

// FilterMode defines network restriction enforcement level.
type FilterMode string

const (
	ModeAllowAll      FilterMode = "allow_all"
	ModeBlockInternet FilterMode = "block_internet"
	ModeWhitelistOnly FilterMode = "whitelist_only"
)

// NetworkPolicy specifies target restriction rules for student devices.
type NetworkPolicy struct {
	Mode           FilterMode `json:"mode"`
	AllowedSubnets []string   `json:"allowed_subnets,omitempty"`
	AllowedDomains []string   `json:"allowed_domains,omitempty"`
	BlockPorts     []int      `json:"block_ports,omitempty"`
}

// Validate checks policy structure and value validity.
func (p NetworkPolicy) Validate() error {
	switch p.Mode {
	case ModeAllowAll, ModeBlockInternet, ModeWhitelistOnly:
	default:
		return fmt.Errorf("%w: %q", ErrInvalidPolicyMode, p.Mode)
	}

	for _, sub := range p.AllowedSubnets {
		subTrim := strings.TrimSpace(sub)
		if subTrim == "" {
			return fmt.Errorf("%w: empty subnet entry", ErrInvalidSubnet)
		}
		if _, _, err := net.ParseCIDR(subTrim); err != nil {
			if ip := net.ParseIP(subTrim); ip == nil {
				return fmt.Errorf("%w: %q", ErrInvalidSubnet, sub)
			}
		}
	}

	for _, d := range p.AllowedDomains {
		domainTrim := strings.TrimSpace(d)
		if domainTrim == "" || strings.Contains(domainTrim, " ") || strings.Contains(domainTrim, "/") {
			return fmt.Errorf("%w: %q", ErrInvalidDomain, d)
		}
	}

	for _, port := range p.BlockPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%w: %d", ErrInvalidPort, port)
		}
	}

	return nil
}

// RuleDirection specifies traffic direction.
type RuleDirection string

const (
	DirectionOut RuleDirection = "out"
	DirectionIn  RuleDirection = "in"
)

// RuleAction specifies allow or block.
type RuleAction string

const (
	ActionAllow RuleAction = "allow"
	ActionBlock RuleAction = "block"
)

// RuleProtocol specifies IP transport protocol.
type RuleProtocol string

const (
	ProtocolTCP RuleProtocol = "TCP"
	ProtocolUDP RuleProtocol = "UDP"
	ProtocolAny RuleProtocol = "ANY"
)

// FirewallRule models an OS-level firewall filter rule.
type FirewallRule struct {
	Name        string        `json:"name"`
	Direction   RuleDirection `json:"direction"`
	Action      RuleAction    `json:"action"`
	Protocol    RuleProtocol  `json:"protocol,omitempty"`
	RemoteIP    string        `json:"remote_ip,omitempty"`
	RemotePort  string        `json:"remote_port,omitempty"`
	Description string        `json:"description,omitempty"`
}

// FirewallAdapter abstracts concrete operating system firewall operations.
type FirewallAdapter interface {
	ApplyRules(ctx context.Context, rules []FirewallRule) error
	ResetRules(ctx context.Context, rulePrefix string) error
	GetActiveRules(ctx context.Context) ([]FirewallRule, error)
}

// CommandRunner defines the execution hook for OS commands.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// WindowsNetshAdapter implements FirewallAdapter using Windows netsh advfirewall.
type WindowsNetshAdapter struct {
	mu     sync.RWMutex
	runner CommandRunner
	rules  []FirewallRule
}

// NewWindowsNetshAdapter creates a new adapter, optionally with a custom CommandRunner.
func NewWindowsNetshAdapter(runner ...CommandRunner) *WindowsNetshAdapter {
	r := defaultExecRunner
	if len(runner) > 0 && runner[0] != nil {
		r = runner[0]
	}
	return &WindowsNetshAdapter{
		runner: r,
		rules:  make([]FirewallRule, 0),
	}
}

// BuildAddRuleArgs constructs netsh command arguments for adding a rule.
func BuildAddRuleArgs(rule FirewallRule) []string {
	args := []string{
		"advfirewall", "firewall", "add", "rule",
		"name=" + rule.Name,
		"dir=" + string(rule.Direction),
		"action=" + string(rule.Action),
	}
	if rule.Protocol != "" && rule.Protocol != ProtocolAny {
		args = append(args, "protocol="+string(rule.Protocol))
	}
	if rule.RemoteIP != "" {
		args = append(args, "remoteip="+rule.RemoteIP)
	}
	if rule.RemotePort != "" {
		args = append(args, "remoteport="+rule.RemotePort)
	}
	return args
}

// BuildDeleteRuleArgs constructs netsh command arguments for deleting a rule by name.
func BuildDeleteRuleArgs(ruleName string) []string {
	return []string{
		"advfirewall", "firewall", "delete", "rule",
		"name=" + ruleName,
	}
}

// ApplyRules removes previous rules and executes netsh to add each new rule.
func (a *WindowsNetshAdapter) ApplyRules(ctx context.Context, rules []FirewallRule) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Clean up previously active rules first
	for _, old := range a.rules {
		deleteArgs := BuildDeleteRuleArgs(old.Name)
		_, _ = a.runner(ctx, "netsh", deleteArgs...)
	}
	a.rules = nil

	// Add each new rule
	applied := make([]FirewallRule, 0, len(rules))
	for _, rule := range rules {
		addArgs := BuildAddRuleArgs(rule)
		out, err := a.runner(ctx, "netsh", addArgs...)
		if err != nil {
			// Rollback applied rules on error
			for _, rollback := range applied {
				delArgs := BuildDeleteRuleArgs(rollback.Name)
				_, _ = a.runner(ctx, "netsh", delArgs...)
			}
			return fmt.Errorf("failed adding firewall rule %q: %w (output: %s)", rule.Name, err, string(out))
		}
		applied = append(applied, rule)
	}

	a.rules = applied
	return nil
}

// ResetRules deletes all active rules from netsh and clears state.
func (a *WindowsNetshAdapter) ResetRules(ctx context.Context, rulePrefix string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var firstErr error
	for _, rule := range a.rules {
		deleteArgs := BuildDeleteRuleArgs(rule.Name)
		if _, err := a.runner(ctx, "netsh", deleteArgs...); err != nil && firstErr == nil {
			// Ignore if not found, but note first failure if any
			outputStr := err.Error()
			if !strings.Contains(outputStr, "No rules match") {
				firstErr = err
			}
		}
	}

	if rulePrefix != "" {
		// Attempt prefix delete as well
		prefixArgs := BuildDeleteRuleArgs(rulePrefix)
		_, _ = a.runner(ctx, "netsh", prefixArgs...)
	}

	a.rules = nil
	return firstErr
}

// GetActiveRules returns a copy of currently tracked active rules.
func (a *WindowsNetshAdapter) GetActiveRules(ctx context.Context) ([]FirewallRule, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]FirewallRule, len(a.rules))
	copy(out, a.rules)
	return out, nil
}

// MockFirewallAdapter provides an in-memory test double for FirewallAdapter.
type MockFirewallAdapter struct {
	mu             sync.RWMutex
	rules          []FirewallRule
	appliedBatches [][]FirewallRule
	resetCount     int
	FailOnApply    error
	FailOnReset    error
}

// NewMockFirewallAdapter initializes a fresh MockFirewallAdapter.
func NewMockFirewallAdapter() *MockFirewallAdapter {
	return &MockFirewallAdapter{
		rules:          make([]FirewallRule, 0),
		appliedBatches: make([][]FirewallRule, 0),
	}
}

// ApplyRules updates mock active rules.
func (m *MockFirewallAdapter) ApplyRules(ctx context.Context, rules []FirewallRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.FailOnApply != nil {
		return m.FailOnApply
	}

	copied := make([]FirewallRule, len(rules))
	copy(copied, rules)
	m.rules = copied
	m.appliedBatches = append(m.appliedBatches, copied)
	return nil
}

// ResetRules clears mock active rules.
func (m *MockFirewallAdapter) ResetRules(ctx context.Context, rulePrefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.FailOnReset != nil {
		return m.FailOnReset
	}

	m.resetCount++
	m.rules = nil
	return nil
}

// GetActiveRules returns the current active rules.
func (m *MockFirewallAdapter) GetActiveRules(ctx context.Context) ([]FirewallRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]FirewallRule, len(m.rules))
	copy(out, m.rules)
	return out, nil
}

// ResetCount returns the number of times ResetRules was called.
func (m *MockFirewallAdapter) ResetCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.resetCount
}

// AuditAction defines netfilter audit event types.
type AuditAction string

const (
	AuditNetfilterPolicyApplied AuditAction = "netfilter.policy_applied"
	AuditNetfilterReset         AuditAction = "netfilter.reset"
	AuditNetfilterShutdown      AuditAction = "netfilter.shutdown"
	AuditNetfilterError         AuditAction = "netfilter.error"
)

// AuditEvent represents a security record emitted during firewall state changes.
type AuditEvent struct {
	Timestamp time.Time   `json:"timestamp"`
	Action    AuditAction `json:"action"`
	Mode      FilterMode  `json:"mode,omitempty"`
	RuleCount int         `json:"rule_count,omitempty"`
	Error     string      `json:"error,omitempty"`
	Details   string      `json:"details,omitempty"`
}

// AuditSink receives audit events for compliance and logging.
type AuditSink interface {
	Record(ctx context.Context, event AuditEvent) error
}

// DomainResolver abstracts domain-to-IP resolution for whitelisting.
type DomainResolver func(ctx context.Context, domain string) ([]string, error)

func defaultDomainResolver(ctx context.Context, domain string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, domain)
}

// ManagerOption configures the Manager.
type ManagerOption func(*Manager)

// WithAuditSink injects an audit logger.
func WithAuditSink(sink AuditSink) ManagerOption {
	return func(m *Manager) {
		m.auditSink = sink
	}
}

// WithDomainResolver injects a custom DNS resolver.
func WithDomainResolver(resolver DomainResolver) ManagerOption {
	return func(m *Manager) {
		m.resolver = resolver
	}
}

// WithRulePrefix sets the rule naming prefix.
func WithRulePrefix(prefix string) ManagerOption {
	return func(m *Manager) {
		m.prefix = prefix
	}
}

// DefaultLANSubnets provides RFC1918 private subnets and localhost.
var DefaultLANSubnets = []string{
	"127.0.0.1/32",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
}

// Manager orchestrates firewall policies, fail-safe cleanup, and audit logging.
type Manager struct {
	mu        sync.RWMutex
	adapter   FirewallAdapter
	policy    NetworkPolicy
	prefix    string
	resolver  DomainResolver
	auditSink AuditSink
	closed    bool
}

// NewManager creates a firewall manager with fail-safe capabilities.
func NewManager(adapter FirewallAdapter, opts ...ManagerOption) *Manager {
	m := &Manager{
		adapter:  adapter,
		policy:   NetworkPolicy{Mode: ModeAllowAll},
		prefix:   "LocalMesh-Exam",
		resolver: defaultDomainResolver,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// emitAudit logs an audit event if a sink is configured.
func (m *Manager) emitAudit(ctx context.Context, event AuditEvent) {
	if m.auditSink != nil {
		if event.Timestamp.IsZero() {
			event.Timestamp = time.Now()
		}
		_ = m.auditSink.Record(ctx, event)
	}
}

// GenerateRules synthesizes the list of FirewallRule entries for a given policy.
func GenerateRules(policy NetworkPolicy, prefix string, resolvedDomains map[string][]string) []FirewallRule {
	if policy.Mode == ModeAllowAll {
		return nil
	}

	rules := make([]FirewallRule, 0)

	// Combine default LAN subnets with user-specified allowed subnets
	lanSet := make(map[string]struct{})
	for _, s := range DefaultLANSubnets {
		lanSet[s] = struct{}{}
	}
	for _, s := range policy.AllowedSubnets {
		lanSet[s] = struct{}{}
	}

	switch policy.Mode {
	case ModeBlockInternet:
		// 1. Allow local LAN & classroom communication
		i := 0
		for sub := range lanSet {
			rules = append(rules, FirewallRule{
				Name:        fmt.Sprintf("%s-allow-lan-%d", prefix, i),
				Direction:   DirectionOut,
				Action:      ActionAllow,
				Protocol:    ProtocolAny,
				RemoteIP:    sub,
				Description: fmt.Sprintf("Allow local subnet %s", sub),
			})
			i++
		}

		// 2. Block general outbound internet
		rules = append(rules, FirewallRule{
			Name:        fmt.Sprintf("%s-block-internet", prefix),
			Direction:   DirectionOut,
			Action:      ActionBlock,
			Protocol:    ProtocolAny,
			RemoteIP:    "any",
			Description: "Block general outbound internet",
		})

		// 3. Block specific restricted ports (e.g. game or chat ports)
		for _, port := range policy.BlockPorts {
			portStr := strconv.Itoa(port)
			rules = append(rules,
				FirewallRule{
					Name:        fmt.Sprintf("%s-block-port-%d-tcp", prefix, port),
					Direction:   DirectionOut,
					Action:      ActionBlock,
					Protocol:    ProtocolTCP,
					RemotePort:  portStr,
					Description: fmt.Sprintf("Block port %d TCP", port),
				},
				FirewallRule{
					Name:        fmt.Sprintf("%s-block-port-%d-udp", prefix, port),
					Direction:   DirectionOut,
					Action:      ActionBlock,
					Protocol:    ProtocolUDP,
					RemotePort:  portStr,
					Description: fmt.Sprintf("Block port %d UDP", port),
				},
			)
		}

	case ModeWhitelistOnly:
		// 1. Block all outbound by default
		rules = append(rules, FirewallRule{
			Name:        fmt.Sprintf("%s-block-all", prefix),
			Direction:   DirectionOut,
			Action:      ActionBlock,
			Protocol:    ProtocolAny,
			RemoteIP:    "any",
			Description: "Block all outbound traffic",
		})

		// 2. Allow DNS lookup (so allowed domain names can resolve)
		rules = append(rules,
			FirewallRule{
				Name:        fmt.Sprintf("%s-allow-dns-udp", prefix),
				Direction:   DirectionOut,
				Action:      ActionAllow,
				Protocol:    ProtocolUDP,
				RemotePort:  "53",
				Description: "Allow DNS UDP",
			},
			FirewallRule{
				Name:        fmt.Sprintf("%s-allow-dns-tcp", prefix),
				Direction:   DirectionOut,
				Action:      ActionAllow,
				Protocol:    ProtocolTCP,
				RemotePort:  "53",
				Description: "Allow DNS TCP",
			},
		)

		// 3. Allow LAN and whitelisted subnets
		i := 0
		for sub := range lanSet {
			rules = append(rules, FirewallRule{
				Name:        fmt.Sprintf("%s-allow-sub-%d", prefix, i),
				Direction:   DirectionOut,
				Action:      ActionAllow,
				Protocol:    ProtocolAny,
				RemoteIP:    sub,
				Description: fmt.Sprintf("Allow whitelisted subnet %s", sub),
			})
			i++
		}

		// 4. Allow resolved domains
		dIdx := 0
		for domain, ips := range resolvedDomains {
			for ipIdx, ip := range ips {
				rules = append(rules, FirewallRule{
					Name:        fmt.Sprintf("%s-allow-dom-%d-%d", prefix, dIdx, ipIdx),
					Direction:   DirectionOut,
					Action:      ActionAllow,
					Protocol:    ProtocolAny,
					RemoteIP:    ip,
					Description: fmt.Sprintf("Allow whitelisted domain %s (%s)", domain, ip),
				})
			}
			dIdx++
		}

		// 5. Block ports (take precedence on restricted services)
		for _, port := range policy.BlockPorts {
			portStr := strconv.Itoa(port)
			rules = append(rules,
				FirewallRule{
					Name:        fmt.Sprintf("%s-block-port-%d-tcp", prefix, port),
					Direction:   DirectionOut,
					Action:      ActionBlock,
					Protocol:    ProtocolTCP,
					RemotePort:  portStr,
					Description: fmt.Sprintf("Block port %d TCP", port),
				},
				FirewallRule{
					Name:        fmt.Sprintf("%s-block-port-%d-udp", prefix, port),
					Direction:   DirectionOut,
					Action:      ActionBlock,
					Protocol:    ProtocolUDP,
					RemotePort:  portStr,
					Description: fmt.Sprintf("Block port %d UDP", port),
				},
			)
		}
	}

	return rules
}

// ApplyPolicy validates, generates, and enforces a new firewall configuration.
func (m *Manager) ApplyPolicy(ctx context.Context, policy NetworkPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrManagerClosed
	}

	if err := policy.Validate(); err != nil {
		m.emitAudit(ctx, AuditEvent{
			Action:  AuditNetfilterError,
			Mode:    policy.Mode,
			Error:   err.Error(),
			Details: "policy validation failed",
		})
		return err
	}

	// If AllowAll, simply reset rules
	if policy.Mode == ModeAllowAll {
		if err := m.adapter.ResetRules(ctx, m.prefix); err != nil {
			m.emitAudit(ctx, AuditEvent{
				Action:  AuditNetfilterError,
				Mode:    policy.Mode,
				Error:   err.Error(),
				Details: "failed to reset firewall rules during allow_all",
			})
			return err
		}
		m.policy = policy
		m.emitAudit(ctx, AuditEvent{
			Action:    AuditNetfilterPolicyApplied,
			Mode:      ModeAllowAll,
			RuleCount: 0,
			Details:   "reset firewall to allow all traffic",
		})
		return nil
	}

	// Resolve allowed domains if any
	resolved := make(map[string][]string)
	for _, domain := range policy.AllowedDomains {
		ips, err := m.resolver(ctx, domain)
		if err != nil {
			// If resolution fails, record warning and continue
			continue
		}
		if len(ips) > 0 {
			resolved[domain] = ips
		}
	}

	rules := GenerateRules(policy, m.prefix, resolved)

	if err := m.adapter.ApplyRules(ctx, rules); err != nil {
		m.emitAudit(ctx, AuditEvent{
			Action:  AuditNetfilterError,
			Mode:    policy.Mode,
			Error:   err.Error(),
			Details: "failed applying rules to firewall adapter",
		})
		return err
	}

	m.policy = policy
	m.emitAudit(ctx, AuditEvent{
		Action:    AuditNetfilterPolicyApplied,
		Mode:      policy.Mode,
		RuleCount: len(rules),
		Details:   fmt.Sprintf("enforced policy %s with %d rules", policy.Mode, len(rules)),
	})
	return nil
}

// Reset clears all active firewall rules and sets policy back to ModeAllowAll.
func (m *Manager) Reset(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrManagerClosed
	}

	if err := m.adapter.ResetRules(ctx, m.prefix); err != nil {
		m.emitAudit(ctx, AuditEvent{
			Action:  AuditNetfilterError,
			Error:   err.Error(),
			Details: "failed to reset rules",
		})
		return err
	}

	m.policy = NetworkPolicy{Mode: ModeAllowAll}
	m.emitAudit(ctx, AuditEvent{
		Action:  AuditNetfilterReset,
		Mode:    ModeAllowAll,
		Details: "firewall rules reset to allow all",
	})
	return nil
}

// Close executes fail-safe cleanup, clearing any installed firewall rules.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true

	// Strict fail-safe cleanup: remove rules so student device is not permanently trapped
	err := m.adapter.ResetRules(ctx, m.prefix)
	m.emitAudit(ctx, AuditEvent{
		Action:  AuditNetfilterShutdown,
		Details: "firewall manager closed; fail-safe cleanup executed",
	})
	return err
}

// GetPolicy returns the currently active policy.
func (m *Manager) GetPolicy() NetworkPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.policy
}

// GetActiveRules returns rules currently tracked by the underlying adapter.
func (m *Manager) GetActiveRules(ctx context.Context) ([]FirewallRule, error) {
	return m.adapter.GetActiveRules(ctx)
}
