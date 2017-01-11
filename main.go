package main

import (
	"flag"
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v2"

	"github.com/golang/glog"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/restclient"
	kclient "k8s.io/kubernetes/pkg/client/unversioned"
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
	Scheduled       = "Scheduled"
	MaxRemediations = 5
)

var (
	argAPIServerURL       = flag.String("api-server", "", "Url endpoint of the k8s api server")
	argConfigFile         = flag.String("config", "", "Path to the configuration file")
	argRemediationMinutes = flag.Int64("remediation-timer", 5, "Time in (minutes) until remediation attempt")
	argSyncNow            = flag.Bool("sync-now", false, "Sync as soon as initial sync is complete")
	argSelfTest           = flag.Bool("self-test", false, "Startup Test")
)

func getAPIClient() (*kclient.Client, error) {
	var restConfig *restclient.Config
	var err error

	if *argAPIServerURL == "" {
		glog.Info("No API Endpoint. Using incluster config")
		restConfig, err = restclient.InClusterConfig()
	} else {
		restConfig = &restclient.Config{
			Host: *argAPIServerURL,
		}
	}

	if err != nil {
		glog.Errorf("Could not create rest config: %v", err)
		return nil, err
	}

	return kclient.New(restConfig)
}

func main() {
	flag.Parse()

	if *argSelfTest {
		fmt.Println("Started!")
		return
	}

	if *argConfigFile == "" {
		panic("No config file given")
	}

	var config Config

	configData, err := ioutil.ReadFile(*argConfigFile)
	if err != nil {
		panic(fmt.Sprintf("Error loading config file: %v", err))
	}

	err = yaml.Unmarshal(configData, &config)

	if err != nil {
		panic(fmt.Sprintf("Error parsing config file: %v", err))
	}

	kubeApiClient, _ := getAPIClient()
	version, err := kubeApiClient.ServerVersion() //Verify we can talk to the server
	if err != nil {
		panic(fmt.Sprintf("Unable to fetch server version from k8s API: %v", err))
	} else {
		fmt.Println("Server Version:", version)
	}

	provider := newKubeDataProvider(kubeApiClient)
	provider.Run(config.Strategies)

	kubeApiClient.Pods(api.NamespaceAll)

	select {}
}
