package main

import (
	"fmt"
	"regexp"
	"time"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	kclient "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/controller/framework"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/util"
)

const (
	NodeNodesAvailable   = "NoNodesAvailable"
	UnknownScheduleIssue = "UnknownIssue"
)

type kubeDataProvider struct {
	client *kclient.Client
	state  *scheduleState
	pods   cache.StoreToPodLister

	eventController *framework.Controller
	podController   *framework.Controller
}

func newKubeDataProvider(client *kclient.Client) *kubeDataProvider {
	c := &kubeDataProvider{
		client: client,
		state:  NewScheduleState(),
	}

	c.createEventController()
	c.createPodController()
	return c
}

func createEventListWatcher(client *kclient.Client) *cache.ListWatch {
	//s, _ := fields.ParseSelector("involvedObject.kind=Pod")
	s, _ := fields.ParseSelector("source=scheduler")
	return cache.NewListWatchFromClient(client, "events", api.NamespaceAll, s)
}

func createPodListWatcher(client *kclient.Client) *cache.ListWatch {
	return cache.NewListWatchFromClient(client, "pods", api.NamespaceAll, fields.Everything())
}

func printEvent(e *api.Event) string {
	return fmt.Sprintf("Name: %s Reason: %s Source: %s Count: %d Message: %s ", e.Name, e.Reason, e.Source, e.Count, e.Message)
}

// Remove any lingering events related to non existant pods
func (k *kubeDataProvider) recolmation() {
	glog.V(4).Info("Running Recolomation")

	for _, name := range k.state.getPods() {
		if p, exists, _ := k.pods.GetByKey(name); exists == false || p.(*api.Pod).Status.Phase != api.PodPending {
			k.state.removePod(name)
		}
	}

}

func ExtractFailureReason(reason string) string {
	re := regexp.MustCompile("Failed for reason (?P<reason>\\w+\\b)")
	results := re.FindStringSubmatch(reason)

	if len(results) > 0 {
		return results[1]
	}

	//TODO Thist is not matching
	re = regexp.MustCompile("(?P<reason>no nodes available)")
	results = re.FindStringSubmatch(reason)

	if len(results) > 1 {
		return NodeNodesAvailable
	}
	return UnknownScheduleIssue
}

func CreateMetaKeyFromEvent(event *api.Event) string {
	return fmt.Sprintf("%s/%s", event.InvolvedObject.Namespace, event.InvolvedObject.Name)
}

func (k *kubeDataProvider) createEventController() {

	_, k.eventController = framework.NewInformer(
		createEventListWatcher(k.client),
		&api.Event{},
		0,
		framework.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				glog.V(4).Info("Got Event: ", printEvent(obj.(*api.Event)))
				if obj.(*api.Event).Reason == FailedScheduling {
					k.state.setPodState(CreateMetaKeyFromEvent(obj.(*api.Event)), ExtractFailureReason(obj.(*api.Event).Message))
					fmt.Println("CurrentState:", k.state.getCurrentState())
				}
			},
		},
	)
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

func (k *kubeDataProvider) getNeededResources(pods []string) *Resources {
	var cpu, mem int64
	for _, pod := range pods {
		if p, exists, _ := k.pods.GetByKey(pod); exists {
			for _, c := range p.(*api.Pod).Spec.Containers {
				cpu += c.Resources.Requests.Cpu().MilliValue()
				mem += c.Resources.Requests.Memory().Value()
			}
		} else {
			glog.Error("Does not exist: ", pod)
		}
	}

	return &Resources{
		CPU:   cpu,
		MemMB: mem,
	}
}

func (k *kubeDataProvider) Run(groups []string) {

	go k.podController.Run(util.NeverStop)
	go k.eventController.Run(util.NeverStop)
	glog.Info("Waiting for PodContoller sync")
	for k.podController.HasSynced() == false {
		time.Sleep(1 * time.Second)
	}
	glog.Info("Initial PodController sync complete")

	rem := NewAWSRemediator(groups)

	for {
		time.Sleep(time.Minute * time.Duration(*argRemediationMinutes))
		k.recolmation()

		glog.V(4).Info("StateGraph:", k.state.failedPods, " FailedMaps:", k.state.getCurrentState())

		//TODO: Move this logic
		if len(k.state.getCurrentState()) > 0 {
			glog.Warning("Nodes in need for remediation. Requesting response")
			r := k.getNeededResources(k.state.getPods())
			glog.Infof("Missing Resources. CPU: %d  MemMB: %d", r.CPU, r.MemMB)
			if ok, err := rem.Remediate(); ok {
				glog.Info("Remediation request successful")
				k.state.incrementRemediations()
			} else {
				glog.Error("Remediation failed!", err)
			}
		}
	}
}
