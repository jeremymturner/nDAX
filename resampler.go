package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

type Resampler struct {
	latencyTarget float64
	tolerance     float64
	latHist       [60]uint64
	histIndex     int
	wrapped       bool
	accum         float64
	dropped       int
	padded        int
	minLatency    uint64
	maxLatency    uint64
	lastSample    float32
	holdoff       int
	Fast          bool
}

// TODO: parametrize this on sample rate and packet size, so the control loop isn't powered by magic numbers.
func NewResampler(target, tolerance uint64) *Resampler {
	return &Resampler{
		latencyTarget: float64(target),
		tolerance:     float64(tolerance),
		minLatency:    ^uint64(0),
	}
}

func interpolateSample(prev, next float32) float32 {
	return (prev + next) / 2
}

func (r *Resampler) ResamplePacket(in []byte, latency uint64) []float32 {
	if latency < r.minLatency {
		r.minLatency = latency
	}
	if latency > r.maxLatency {
		r.maxLatency = latency
	}

	out := make([]float32, len(in)/4)
	b := bytes.NewReader(in)
	binary.Read(b, binary.BigEndian, out)

	err := float64(latency) - r.latencyTarget
	err *= math.Abs(err / (r.tolerance + math.Abs(err)))

	r.accum += err

	if r.holdoff > 0 {
		r.holdoff -= 1
	} else if r.accum > 12*r.latencyTarget { // Drop one sample
		out = out[1:]
		r.dropped += 1
		r.accum -= 11 * r.latencyTarget
		r.holdoff = 10
	} else if r.accum < -12*r.latencyTarget { // Interpolate one sample
		samp := interpolateSample(r.lastSample, out[0])
		out = append([]float32{samp}, out...)
		r.padded += 1
		r.accum += 11 * r.latencyTarget
		r.holdoff = 10
	}

	r.accum *= 0.9999 // Let the integrator leak
	r.lastSample = out[len(out)-1]

	return out
}

func (r *Resampler) Stats(latency uint64) string {
	diff := int64(latency - r.latHist[r.histIndex])

	r.latHist[r.histIndex] = latency
	r.histIndex = (r.histIndex + 1) % len(r.latHist)
	if r.histIndex == 0 && !r.wrapped {
		r.wrapped = true
	}

	msg := fmt.Sprintf("%7d %7d %11.1f +%-3d -%-3d", r.minLatency, r.maxLatency, r.accum, r.padded, r.dropped)
	if r.wrapped {
		rate := float64(diff) / float64(len(r.latHist))
		msg += fmt.Sprintf(" %8.3f %11.5f", rate, (1+rate/1e6)*48000)
	}

	// Reset stats for next time
	r.minLatency = ^uint64(0)
	r.maxLatency = 0
	r.padded = 0
	r.dropped = 0

	return msg
}
