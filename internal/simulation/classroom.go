package simulation

import (
	"fmt"
	"net/netip"
	"sync"
	"time"

	"localmesh/internal/domain/discovery"
	"localmesh/internal/domain/identity"
	"localmesh/internal/domain/session"
)

type Agent struct {
	DeviceID identity.DeviceID
	Session  *session.Session
	Address  netip.AddrPort
}

type Report struct {
	Agents            int
	Discovered        int
	ActiveSessions    int
	Reconnected       int
	StableDeviceIDs   bool
	DuplicateSessions bool
}

func Run(agentCount int) (Report, error) {
	if agentCount < 1 {
		return Report{}, fmt.Errorf("agent count must be positive")
	}
	now := time.Unix(100, 0)
	registry := discovery.NewRegistry()
	agents := make([]Agent, agentCount)
	var wg sync.WaitGroup
	for i := range agents {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			deviceID, err := identity.NewDeviceID(fmt.Sprintf("CPC-%04d-AGENT", index))
			if err != nil {
				return
			}
			s, err := session.New(fmt.Sprintf("session-%04d", index), deviceID, now, time.Minute)
			if err != nil {
				return
			}
			_ = s.Pair(now)
			_ = s.Activate(now)
			agents[index] = Agent{DeviceID: deviceID, Session: s, Address: netip.MustParseAddrPort(fmt.Sprintf("192.168.1.%d:7443", index+1))}
			registry.Observe(discovery.Observation{DeviceID: deviceID, Address: agents[index].Address, SeenAt: now, Reachable: true})
		}(i)
	}
	wg.Wait()

	initialIDs := make(map[string]struct{}, agentCount)
	for _, agent := range agents {
		if agent.DeviceID != "" {
			initialIDs[string(agent.DeviceID)] = struct{}{}
		}
	}
	report := Report{Agents: agentCount, Discovered: len(registry.Candidates(now, time.Minute)), StableDeviceIDs: len(initialIDs) == agentCount}
	sessionIDs := make(map[string]struct{}, agentCount)
	for i := range agents {
		if agents[i].Session == nil {
			continue
		}
		if err := agents[i].Session.Heartbeat(now.Add(10 * time.Second)); err == nil {
			report.ActiveSessions++
		}
		// DHCP changes the address, but the stable DeviceID and session remain unchanged.
		newAddress := netip.MustParseAddrPort(fmt.Sprintf("10.20.0.%d:7443", i+1))
		registry.Observe(discovery.Observation{DeviceID: agents[i].DeviceID, Address: newAddress, SeenAt: now.Add(10 * time.Second), Reachable: true})
	}
	for i := range agents {
		if agents[i].Session == nil {
			continue
		}
		agents[i].Session.Close(now.Add(20 * time.Second))
		newSession, err := session.New(fmt.Sprintf("reconnect-%04d", i), agents[i].DeviceID, now.Add(20*time.Second), time.Minute)
		if err != nil {
			continue
		}
		_ = newSession.Pair(now.Add(20 * time.Second))
		_ = newSession.Activate(now.Add(20 * time.Second))
		agents[i].Session = newSession
		report.Reconnected++
		if _, exists := sessionIDs[newSession.ID]; exists {
			report.DuplicateSessions = true
		}
		sessionIDs[newSession.ID] = struct{}{}
	}
	if report.Discovered != agentCount || report.ActiveSessions != agentCount || report.Reconnected != agentCount {
		return report, fmt.Errorf("simulation incomplete: %+v", report)
	}
	return report, nil
}
