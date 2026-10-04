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
