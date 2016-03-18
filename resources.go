package main

import "math"

var EmptyResources = Resources{CPU: 0, MemMB: 0}

type Resources struct {
	CPU   int64
	MemMB int64
}

func (r *Resources) Scale(scaler int64) {
	r.CPU *= scaler
	r.MemMB *= scaler
}

func (r *Resources) Remove(toRemove *Resources) {
	r.CPU = int64(math.Max(float64(0), float64(r.CPU-toRemove.CPU)))
	r.MemMB = int64(math.Max(float64(0), float64(r.MemMB-toRemove.MemMB)))
}
