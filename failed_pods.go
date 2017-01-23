package main

import (
	"sync"

	"k8s.io/kubernetes/pkg/api"

	"github.com/golang/glog"
)

//FailedPod represents a pod that has failed to be scheduled
type FailedPod struct {
	Remediations int //TODO: I don't see where this is ever used
	Pod          *api.Pod
}

//FailedPods is a collection of pods that have failed to schedule
type FailedPods struct {
	failedPods map[string]*FailedPod

	//This can likely be changed over to a channel if performance becomes an issue
	lock sync.Mutex
}

//NewFailedPods represents pods that have failed to schedule
func NewFailedPods() *FailedPods {
	return &FailedPods{
		failedPods: make(map[string]*FailedPod),
	}
}

func (f *FailedPods) addPod(name string, pod *api.Pod) {
	f.lock.Lock()
	defer f.lock.Unlock()
	glog.V(4).Infof("Adding Pod: %s", name)
	f.failedPods[name] = &FailedPod{
		Remediations: 0, //TODO: Should this be reset to zero if the pod's already present in the list?
		Pod:          pod,
	}
}

func (f *FailedPods) getPods() []*api.Pod {
	f.lock.Lock()
	defer f.lock.Unlock()

	pods := make([]*api.Pod, len(f.failedPods))
	i := 0

	for _, p := range f.failedPods {
		pods[i] = p.Pod
		i++
	}
	return pods
}

func (f *FailedPods) removePod(name string) {
	f.lock.Lock()
	defer f.lock.Unlock()
	glog.V(4).Infof("Removing Pod %s from State if exists", name)
	delete(f.failedPods, name)
}

func (f *FailedPods) incrementRemediations() {
	f.lock.Lock()
	defer f.lock.Unlock()
	for _, p := range f.failedPods {
		p.Remediations++
	}
}
