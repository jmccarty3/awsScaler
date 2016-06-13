package main

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	rapi "github.com/jmccarty3/awsScaler/api"
	"github.com/jmccarty3/awsScaler/api/stratagy"
	"k8s.io/kubernetes/pkg/api"
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
	client     *kclient.Client
	state      *ScheduleState
	pods       cache.StoreToPodLister
	stratageis []stratagy.RemediationStratagy

	podController *framework.Controller
}

func newKubeDataProvider(client *kclient.Client) *kubeDataProvider {
	c := &kubeDataProvider{
		client: client,
		state:  NewScheduleState(),
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

func (k *kubeDataProvider) recolmation() {
	glog.V(4).Infof("Running Recolomation over %d pods", len(k.pods.Store.List()))
	remTime := time.Now().Add(-time.Duration(*argRemediationMinutes) * time.Minute)
	glog.V(4).Infof("Pods must have been created before %v", remTime)
	// Find unscheduled pods
	pods, _ := k.pods.List(labels.Everything())
	for _, pod := range pods {
		if pod.Status.Phase == api.PodPending && pod.CreationTimestamp.Before(unversioned.NewTime(remTime)) {
			key, _ := cache.MetaNamespaceKeyFunc(pod)
			k.state.setPodState(key, pod)
		}
	}

	// Remove any lingering events related to non existant pods
	for _, pod := range k.state.getPods() {
		if p, exists, _ := k.pods.Get(pod); exists == false || p.(*api.Pod).Status.Phase != api.PodPending {
			key, _ := cache.MetaNamespaceKeyFunc(pod)
			k.state.removePod(key)
		}
	}
	glog.V(4).Info("Finished Recolomation")
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
				k.state.removePod(key)
			},
		},
	)
}

func (k *kubeDataProvider) getNeededResources(pods []*api.Pod) *rapi.Resources {
	var cpu, mem int64
	for _, pod := range pods {
		for _, c := range pod.Spec.Containers {
			cpu += c.Resources.Requests.Cpu().MilliValue()
			mem += c.Resources.Requests.Memory().Value() / (1024 * 1024) // Memory is returned as the full value. We want it truncated to Megabytes
		}
	}

	return &rapi.Resources{
		CPU:   cpu,
		MemMB: mem,
	}
}

func (k *kubeDataProvider) doWork() {
	k.recolmation()

	glog.V(4).Info("StateGraph:", k.state.failedPods)

	//TODO: Move this logic
	if len(k.state.getPods()) > 0 {
		glog.Warning("Nodes in need for remediation. Requesting response")
		podsToRemediate := k.state.getPods()
		podsCanFix := []*api.Pod{}

		for _, s := range k.stratageis {
			podsCanFix, podsToRemediate = s.FilterPods(podsToRemediate)

			if len(podsCanFix) > 0 {
				r := k.getNeededResources(podsCanFix)
				glog.Infof("Missing Resources. CPU: %d  MemMB: %d Pod Count: %d", r.CPU, r.MemMB, len(k.state.getPods()))
				if unresolved, err := s.DoRemediation(r); *unresolved == rapi.EmptyResources {
					glog.Info("Remediation request successful")
				} else {
					glog.Errorf("Remediation failed. Error: %v Leftover Resources: %v", err, unresolved)
				}
			}
		}

		if len(podsToRemediate) > 0 {
			glog.Warningf("Unable to find stratagy for %d pods\n", len(podsToRemediate))
		}
		k.state.incrementRemediations()
	}
}

func (k *kubeDataProvider) Run(stratagies []stratagy.RemediationStratagy) {

	go k.podController.Run(wait.NeverStop)
	glog.Info("Waiting for PodContoller sync")
	for k.podController.HasSynced() == false {
		time.Sleep(1 * time.Second)
	}
	glog.Info("Initial PodController sync complete")
	k.stratageis = stratagies

	if *argSyncNow {
		k.doWork()
	}

	for {
		time.Sleep(time.Minute * time.Duration(*argRemediationMinutes))
		k.doWork()

	}
}
