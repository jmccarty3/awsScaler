package main

import (
	"flag"
	"fmt"
	"net/url"
	"strings"

	"github.com/golang/glog"

	"k8s.io/kubernetes/pkg/api"
	kclient "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
)

/* Reasons

"no nodes available" - Scale Or Crit
"MatchNodeSelector"  - Crit"
"PodExceedsFreeCPU"  - Scale
*/

/*
 1) Keep Track of number of pods for each Reason -> Watcher Thread
 2) On Timer Check how many pods match -> Remediation Thread
 3) Rest on "Cool Down" of scaling -> Remediation Thread?

 Threshold for pod remaining after remediation (avoid infiniate remediation in impossible situation)

 map[PodName][Last Reason]
 map[Reason][failed Pod]

 Collector for all Reasons with no resolution

 Watch Pods to See if deleted. Avoid issues where pod was deleted but not scheduled
*/

const (
	FailedScheduling = "FailedScheduling"
	Scheduled        = "Scheduled"
	MaxRemediations  = 5
)

var (
	argAPIServerURL       = flag.String("api-server", "http://localhost:8080", "Url endpoint of the k8s api server")
	argASGroups           = flag.String("as-groups", "", "Comma seperated list of Autoscaling groups to use")
	argRemediationMinutes = flag.Int64("remediation-timer", 5, "Time in (minutes) until remediation attempt")
	argSelfTest           = flag.Bool("self-test", false, "Startup Test")

	EmptyResources = Resources{CPU: 0, MemMB: 0}
)

type Resources struct {
	CPU   int64
	MemMB int64
}

func getAPIServerURL() (string, error) {
	//More parsing later
	url, err := url.Parse(*argAPIServerURL)
	return url.String(), err
}

func getAPIClient() (*kclient.Client, error) {
	url, err := getAPIServerURL()

	if err != nil {
		glog.Error(err)
		return nil, err
	}
	return kclient.New(&kclient.Config{
		Host: url,
	})
}

func main() {
	flag.Parse()
	groups := strings.Split(*argASGroups, ",")

	if *argSelfTest {
		fmt.Println("Started!")
		return
	}

	if *argASGroups == "" {
		panic("No autoscaling groups given")
	}

	c, _ := getAPIClient()
	v, e := c.ServerVersion()
	fmt.Println("Version:", v)
	l, e := c.Pods("default").List(labels.Everything(), fields.Everything())

	if e != nil {
		glog.Error("Oh Noes on pod list", e)
		panic("Bail")
	} else {
		for _, p := range l.Items {
			fmt.Println("Pod:", p.Name)
		}
	}

	provider := newKubeDataProvider(c)
	provider.Run(groups)

	c.Pods(api.NamespaceAll)

	select {}
}
