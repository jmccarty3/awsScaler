package main

import (
	"sync"

	"github.com/golang/glog"
)

type failedPod struct {
	Reason       string
	Remediations int
}

type scheduleState struct {
	failedPods map[string]*failedPod

	//This can likely be changed over to a channel if performance becomes an issue
	lock sync.Mutex
}

func NewScheduleState() *scheduleState {
	return &scheduleState{
		failedPods: make(map[string]*failedPod),
	}
}

func (s *scheduleState) setPodState(name, reason string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	glog.V(4).Infof("Adding Pod: %s with Reason: %s to state", name, reason)
	s.failedPods[name] = &failedPod{
		Reason:       reason,
		Remediations: 0,
	}
}

func (s *scheduleState) getPods() []string {
	s.lock.Lock()
	defer s.lock.Unlock()

	keys := make([]string, len(s.failedPods))
	i := 0

	for k := range s.failedPods {
		keys[i] = k
		i++
	}
	return keys
}

func (s *scheduleState) removePod(name string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	glog.V(4).Infof("Removing Pod %s from State if exists", name)
	delete(s.failedPods, name)
}

func (s *scheduleState) getCurrentState() map[string]int {
	states := make(map[string]int)
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, p := range s.failedPods {
		if p.Remediations < MaxRemediations {
			states[p.Reason] = states[p.Reason] + 1
		}
	}
	return states
}

func (s *scheduleState) incrementRemediations() {
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, p := range s.failedPods {
		p.Remediations++
	}
}
