package network

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

var ErrDropped = errors.New("network packet dropped")

type Config struct {
	Latency    time.Duration
	Loss       float64
	Duplicates float64
	Seed       int64
}

type Delivery struct {
	Payload []byte
	Delay   time.Duration
}

type Scenario struct {
	Loss       float64
	Latency    time.Duration
	Duplicates float64
	Seed       int64
}

func RunScenario(ctx context.Context, config Config, messages [][]byte) (delivered, dropped, duplicated int, err error) {
	injector, err := New(config)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, message := range messages {
		items, transmitErr := injector.Transmit(ctx, message)
		if errors.Is(transmitErr, ErrDropped) {
			dropped++
			continue
		}
		if transmitErr != nil {
			return delivered, dropped, duplicated, transmitErr
		}
		delivered += len(items)
		if len(items) > 1 {
			duplicated += len(items) - 1
		}
	}
	return delivered, dropped, duplicated, nil
}

type Router struct {
	classrooms map[string]map[string]struct{}
	violations int
}

func NewRouter() *Router { return &Router{classrooms: make(map[string]map[string]struct{})} }

func (r *Router) Add(classroom, device string) {
	if r.classrooms[classroom] == nil {
		r.classrooms[classroom] = make(map[string]struct{})
	}
	r.classrooms[classroom][device] = struct{}{}
}

func (r *Router) Route(classroom, device string) bool {
	allowed := false
	if members := r.classrooms[classroom]; members != nil {
		_, allowed = members[device]
	}
	if !allowed {
		r.violations++
	}
	return allowed
}

func (r *Router) IsolationViolations() int { return r.violations }

type FaultInjector struct {
	config Config
	random *rand.Rand
}

func New(config Config) (*FaultInjector, error) {
	if config.Loss < 0 || config.Loss > 1 || config.Duplicates < 0 || config.Duplicates > 1 || config.Latency < 0 {
		return nil, errors.New("invalid network fault configuration")
	}
	return &FaultInjector{config: config, random: rand.New(rand.NewSource(config.Seed))}, nil
}

func (f *FaultInjector) Transmit(ctx context.Context, payload []byte) ([]Delivery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.random.Float64() < f.config.Loss {
		return nil, ErrDropped
	}
	copyPayload := append([]byte(nil), payload...)
	result := []Delivery{{Payload: copyPayload, Delay: f.config.Latency}}
	if f.random.Float64() < f.config.Duplicates {
		result = append(result, Delivery{Payload: append([]byte(nil), payload...), Delay: f.config.Latency})
	}
	return result, nil
}
