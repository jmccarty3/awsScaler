package strategy

import (
	"fmt"
	"reflect"

	"gopkg.in/yaml.v2"

	"github.com/golang/glog"
	rapi "github.com/jmccarty3/awsScaler/api"
	"github.com/jmccarty3/awsScaler/api/remediation"
	//Provides access to the remediator registry
	_ "github.com/jmccarty3/awsScaler/api/remediation/remediators"
	kapi "k8s.io/kubernetes/pkg/api"
)

//RemediationStrategy represents a strategy to resolve unscheduled pods
type RemediationStrategy struct {
	Namespaces   *rapi.NamespaceCondition `yaml:",flow"`
	NodeSelector *rapi.NodeSelectorCondition
	Remediators  []remediation.Remediator
}

//remediationStrategyYaml represents a simplified representation of a RemediationStrategy used for unmarshalling
type remediationStrategyYaml struct {
	Namespaces   []string                    `yaml:"namespaces,flow"`
	NodeSelector *rapi.NodeSelectorCondition `yaml:"nodeSelector,flow"`
	Remediators  []map[string]interface{}    `yaml:"remediators"`
}

func prettyPrintValueStats(statObj reflect.Value) {
	fmt.Printf("\nValueOf Kind: %v\n", statObj.Kind())

	if statObj.Kind() == reflect.Ptr {
		fmt.Printf("ValueOf Ptr Elm: %v\n", statObj.Elem())
		fmt.Printf("ValueOf Ptr CanAddress: %v\n", statObj.Elem().CanAddr())
		fmt.Printf("ValueOf Ptr unmarshaler: %v\n", statObj.Elem().Addr().Interface().(yaml.Unmarshaler))
	} else if statObj.Kind() == reflect.Struct {
		if statObj.CanAddr() {
			statObjPtr := statObj.Addr()
			if u, ok := statObjPtr.Interface().(yaml.Unmarshaler); ok {
				fmt.Printf("ValueOf Strcut unmarshaler: %v\n", u)
			} else {
				fmt.Printf("ValueOf Struct does not implement unmarshaler\n")
			}
		} else {
			fmt.Printf("Statobj not addressable\n")
		}
	}
}

//UnmarshalYAML performs custom unmarshalling from yaml
func (s *RemediationStrategy) UnmarshalYAML(unmarshal func(interface{}) error) error {
	in := &remediationStrategyYaml{}

	if err := unmarshal(&in); err != nil {
		return err
	}

	if len(in.Namespaces) != 0 {
		s.Namespaces = &rapi.NamespaceCondition{
			Namespaces: in.Namespaces,
		}
	}

	s.NodeSelector = in.NodeSelector

	for _, remediatorMap := range in.Remediators {
		for remediatorName, remediatorData := range remediatorMap {
			create, err := remediation.GetRemediatorCreator(remediatorName)
			if err != nil {
				glog.Errorf("%v", err)
				return err
			}
			remediator := create(remediation.ConfigData{})

			//Remarshal the downstream data for processing
			reMarsh, _ := yaml.Marshal(remediatorData)
			if err = yaml.Unmarshal(reMarsh, remediator); err != nil {
				glog.Errorf("Error unmarshalling remediator %s with data %v", remediatorName, remediatorData)
				return err
			}
			s.Remediators = append(s.Remediators, remediator)
		}
	}
	s.Remediators = append(s.Remediators)
	return nil
}

//FilterPods filters the pods to find a matches that passes all conditions
// Return a list of pods able to help
func (s *RemediationStrategy) FilterPods(available []*kapi.Pod) (canRemediate, remainingPods []*kapi.Pod) {
	//TODO Consider preallocating size
	var validPods = []*kapi.Pod{}
	var invalidPods = []*kapi.Pod{}

	for _, p := range available {
		valid := true

		if s.Namespaces != nil {
			valid = valid && s.Namespaces.CheckPodValid(p)
		}

		if s.NodeSelector != nil {
			valid = valid && s.NodeSelector.CheckPodValid(p)
		}

		if valid {
			validPods = append(validPods, p)
		} else {
			invalidPods = append(invalidPods, p)
		}
	}

	return validPods, invalidPods
}

//DoRemediation attempt to do remediation
//Can only optimistically scale based on resources
func (s *RemediationStrategy) DoRemediation(resources *rapi.Resources) (remainingResources *rapi.Resources, err error) {
	remainingResources = resources
	err = nil
	success := false

	for _, r := range s.Remediators {
		glog.Infof("Calling remediator for %v resources", remainingResources)
		if success, remainingResources, _ = r.Remediate(remainingResources); success {
			glog.Info("All resourcess remediated")
			return
		}
	}

	glog.Warningf("Unable to remediate all resources. Missing: %v", remainingResources)
	return
}
