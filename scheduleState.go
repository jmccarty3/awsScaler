package main

import (
	"sync"

	"k8s.io/kubernetes/pkg/api"

	"github.com/golang/glog"
)

//FailedPod represents a pod that has failed to be scheduled
type FailedPod struct {
	Remediations int
	Pod          *api.Pod
}

//ScheduleState is a collection of pods that have failed to schedule
type ScheduleState struct {
	failedPods map[string]*FailedPod

	//This can likely be changed over to a channel if performance becomes an issue
	lock sync.Mutex
}

//NewScheduleState represents pods that have failed to schedule
func NewScheduleState() *ScheduleState {
	return &ScheduleState{
		failedPods: make(map[string]*FailedPod),
	}
}

func (s *ScheduleState) setPodState(name string, pod *api.Pod) {
	s.lock.Lock()
	defer s.lock.Unlock()
	glog.V(4).Infof("Adding Pod: %s", name)
	s.failedPods[name] = &FailedPod{
		Remediations: 0,
		Pod:          pod,
	}
}

func (s *ScheduleState) getPods() []*api.Pod {
	s.lock.Lock()
	defer s.lock.Unlock()

	pods := make([]*api.Pod, len(s.failedPods))
	i := 0

	for _, p := range s.failedPods {
		pods[i] = p.Pod
		i++
	}
	return pods
}

func (s *ScheduleState) removePod(name string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	glog.V(4).Infof("Removing Pod %s from State if exists", name)
	delete(s.failedPods, name)
}

func (s *ScheduleState) incrementRemediations() {
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, p := range s.failedPods {
		p.Remediations++
	}
}
