package main

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	rapi "github.com/jmccarty3/awsScaler/api"
	"github.com/jmccarty3/awsScaler/api/strategy"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/resource"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/client/cache"
	kclient "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/controller/framework"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/util/wait"
)

const (
	NodeNodesAvailable   = "NoNodesAvailable"
	UnknownScheduleIssue = "UnknownIssue"
)

type kubeDataProvider struct {
	client      *kclient.Client
	failingPods *FailedPods
	pods        cache.StoreToPodLister
	strategies  []strategy.RemediationStrategy

	podController *framework.Controller
}

func newKubeDataProvider(client *kclient.Client) *kubeDataProvider {
	c := &kubeDataProvider{
		client:      client,
		failingPods: NewFailedPods(),
	}

	c.createPodController()
	return c
}

func createEventListWatcher(client *kclient.Client) *cache.ListWatch {
	//s, _ := fields.ParseSelector("involvedObject.kind=Pod")
	//s, _ := fields.ParseSelector("source=scheduler")
	s := fields.Everything()
	return cache.NewListWatchFromClient(client, "events", api.NamespaceAll, s)
}

func createPodListWatcher(client *kclient.Client) *cache.ListWatch {
	return cache.NewListWatchFromClient(client, "pods", api.NamespaceAll, fields.Everything())
}

func printEvent(e *api.Event) string {
	return fmt.Sprintf("Name: %s Reason: %s Source: %s Count: %d Message: %s ", e.Name, e.Reason, e.Source, e.Count, e.Message)
}

func isPodStatusFine(pod *api.Pod) bool {
	if pod.Status.Phase != api.PodPending {
		return true
	}

	//Pod can still be pending but in the creating state which means its been scheduled
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "ContainerCreating" {
			return true
		}
	}

	return false
}

// updateFailingPods updates the kubeDataProvider's FailedPods
func (k *kubeDataProvider) updateFailingPods() {
	glog.V(4).Infof("Running Recolomation over %d pods", len(k.pods.Store.List())) // TODO: "Reclamation"? Not sure if that's an accurate description of what's happening here...
	remTime := time.Now().Add(-time.Duration(*argRemediationMinutes) * time.Minute)
	glog.V(4).Infof("Pods must have been created before %v", remTime)
	// Find unscheduled pods
	pods, _ := k.pods.List(labels.Everything())
	for _, pod := range pods {
		if pod.Status.Phase == api.PodPending && pod.CreationTimestamp.Before(unversioned.NewTime(remTime)) {
			if !isPodStatusFine(pod) {
				key, _ := cache.MetaNamespaceKeyFunc(pod)
				k.failingPods.addPod(key, pod)
			}
		}
	}

	// Remove any lingering events related to non existant pods
	for _, pod := range k.failingPods.getPods() {
		if p, exists, _ := k.pods.Get(pod); !exists || isPodStatusFine(p.(*api.Pod)) {
			key, _ := cache.MetaNamespaceKeyFunc(pod)
			k.failingPods.removePod(key)
		}
	}
	glog.V(4).Info("Finished Recolomation") // TODO: clarify this statement
}

func (k *kubeDataProvider) createPodController() {
	k.pods.Store, k.podController = framework.NewInformer(
		createPodListWatcher(k.client),
		&api.Pod{},
		0,
		framework.ResourceEventHandlerFuncs{
			DeleteFunc: func(oldObj interface{}) {
				glog.V(4).Info("Deleting pod: ", oldObj.(*api.Pod).Name)
				key, _ := cache.MetaNamespaceKeyFunc(oldObj.(*api.Pod))
				k.failingPods.removePod(key)
			},
		},
	)
}

func getResourceMem(mem *api.ResourceRequirements) int64 {
	if (*mem.Limits.Cpu() != resource.Quantity{} && mem.Limits.Memory().Value() > 0) {
		return mem.Limits.Memory().Value() / (1024 * 1024) // Memory is returned as the full value. We want it truncated to Megabytes
	}
	return mem.Requests.Memory().Value() / (1024 * 1024) // Memory is returned as the full value. We want it truncated to Megabytes
}

func getResourceCPU(cpu *api.ResourceRequirements) int64 {
	if (*cpu.Limits.Cpu() != resource.Quantity{} && cpu.Limits.Cpu().MilliValue() > 0) {
		return cpu.Limits.Cpu().MilliValue()
	}
	return cpu.Requests.Cpu().MilliValue()
}

func (k *kubeDataProvider) getNeededResources(pods []*api.Pod) *rapi.Resources {
	var cpu, mem int64
	for _, pod := range pods {
		for _, c := range pod.Spec.Containers {
			cpu += getResourceCPU(&c.Resources)
			mem += getResourceMem(&c.Resources)
		}
	}

	return &rapi.Resources{
		CPU:   cpu,
		MemMB: mem,
	}
}

// remediateFailingPods applies its remediation strategies to the currently failing pods
func (k *kubeDataProvider) remediateFailingPods() {
	k.updateFailingPods()

	glog.V(4).Info("StateGraph:", k.failingPods.failedPods)

	//TODO: Move this logic
	if len(k.failingPods.getPods()) > 0 {
		glog.Warning("Nodes in need of remediation. Requesting response")
		podsToRemediate := k.failingPods.getPods()
		var podsCanFix []*api.Pod

		for _, stratgy := range k.strategies {
			podsCanFix, podsToRemediate = stratgy.FilterPods(podsToRemediate)

			if len(podsCanFix) > 0 {
				resources := k.getNeededResources(podsCanFix)
				glog.Infof("Missing Resources. CPU: %d  MemMB: %d Pod Count: %d", resources.CPU, resources.MemMB, len(k.failingPods.getPods()))
				if unresolved, err := stratgy.DoRemediation(resources); *unresolved == rapi.EmptyResources {
					glog.Info("Remediation request successful")
				} else {
					glog.Errorf("Remediation failed. Error: %v Leftover Resources: %v", err, unresolved)
				}
			}
		}

		if len(podsToRemediate) > 0 {
			glog.Warningf("Unable to find strategy for %d pods\n", len(podsToRemediate))
		}
		k.failingPods.incrementRemediations()
	}
}

func (k *kubeDataProvider) Run(strategies []strategy.RemediationStrategy) {

	go k.podController.Run(wait.NeverStop)
	glog.Info("Waiting for PodContoller sync")
	for k.podController.HasSynced() == false {
		time.Sleep(1 * time.Second)
	}
	glog.Info("Initial PodController sync complete")
	k.strategies = strategies

	if *argSyncNow {
		k.remediateFailingPods()
	}

	for {
		time.Sleep(time.Minute * time.Duration(*argRemediationMinutes))
		k.remediateFailingPods()

	}
}
