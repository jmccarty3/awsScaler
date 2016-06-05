package api

import "math"

//EmptyResources placeholder to represent no resources
var EmptyResources = Resources{CPU: 0, MemMB: 0}

//Resources represents the CPU and Memory (in MB) for an object
type Resources struct {
	CPU   int64
	MemMB int64
}

//Scale scales the resource values by given value
func (r *Resources) Scale(scaler int64) {
	r.CPU *= scaler
	r.MemMB *= scaler
}

//Remove removes the specified resource amound from the object. Returns 0 or remaining for each
func (r *Resources) Remove(toRemove *Resources) {
	r.CPU = int64(math.Max(float64(0), float64(r.CPU-toRemove.CPU)))
	r.MemMB = int64(math.Max(float64(0), float64(r.MemMB-toRemove.MemMB)))
}
