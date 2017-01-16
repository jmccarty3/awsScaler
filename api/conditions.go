package api

import (
	"fmt"
	"sync"

	"gopkg.in/yaml.v2"

	kapi "k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/labels"
)

//ConditionCreator creates an object that implements PodCondition
type ConditionCreator func() PodCondition

var mapSync sync.Once
var conditionMutex sync.Mutex
var conditions = make(map[string]ConditionCreator)

//AddConditionCreator registeres a condition with the system
func AddConditionCreator(name string, creator ConditionCreator) error {
	conditionMutex.Lock()
	defer conditionMutex.Unlock()
	if _, exists := conditions[name]; exists {
		return fmt.Errorf("Condition with name %s exists", name)
	}

	conditions[name] = creator
	return nil
}

//GetConditionCreator retrieves a creation function by name
func GetConditionCreator(name string) (ConditionCreator, error) {
	conditionMutex.Lock()
	defer conditionMutex.Unlock()
	mapSync.Do(func() {
		conditions["namespace"] = func() PodCondition { return new(NamespaceCondition) }
		conditions["nodeSelector"] = func() PodCondition { return new(NodeSelectorCondition) }
	})

	if factory, exists := conditions[name]; exists {
		return factory, nil
	}

	return nil, fmt.Errorf("%s is an unknown condition", name)
}

//PodCondition conditionally matches pods
type PodCondition interface {
	MatchesPod(pod *kapi.Pod) bool
}

//NamespaceCondition ensures that a pod belongs to a valid namespace
type NamespaceCondition struct {
	Namespaces []string `yaml:"namespaces"`
}

//NodeSelectorCondition esnures that a pods nodeSelector is valid
type NodeSelectorCondition struct {
	Keys         map[string]string `yaml:",flow"`
	NodeSelector labels.Selector
}

//MatchesPod returns true iff pod meets the namespace condition
func (c *NamespaceCondition) MatchesPod(pod *kapi.Pod) bool {
	for _, namespace := range c.Namespaces {
		if namespace == pod.Namespace {
			return true
		}
	}
	return false
}

//UnmarshalFromYaml allows unmarshaling from YAMl data
func (c *NamespaceCondition) UnmarshalFromYaml(data []byte) error {
	return yaml.Unmarshal(data, c)
}

//Equal checks if two NamespaceConditions contain the name namespaces without order
func (c *NamespaceCondition) Equal(other *NamespaceCondition) bool {

	if c == other {
		return true
	}

	if len(c.Namespaces) != len(other.Namespaces) {
		return false
	}

	//We don't care about order
	for _, n := range c.Namespaces {
		found := false
		for _, n2 := range other.Namespaces {
			if n == n2 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

//TODO Resolve

//UnmarshalYAML is used perform unmarshaling of data from YAML
//Remediators will created from the the registrator registry
func (c *NodeSelectorCondition) UnmarshalYAML(unmarshal func(interface{}) error) error {
	if err := unmarshal(&c.Keys); err != nil {
		return err
	}

	c.NodeSelector = labels.SelectorFromSet(c.Keys)
	return nil
}

//Messy unmarshaling of type map[string]string. See https://github.com/go-yaml/yaml/issues/139
func cleanUpMapUnmarshal(input map[interface{}]interface{}) map[string]string {
	output := make(map[string]string)
	for k, v := range input {
		output[fmt.Sprintf("%v", k)] = fmt.Sprintf("%v", v)
	}

	return output
}

//MatchesPod verifies pod validity
func (c *NodeSelectorCondition) MatchesPod(pod *kapi.Pod) bool {
	return c.NodeSelector.Matches(labels.Set(pod.Spec.NodeSelector))
}

//UnmarshalFromYaml Allows data to be unmarshalled when loaded from yaml
func (c *NodeSelectorCondition) UnmarshalFromYaml(data []byte) error {
	return yaml.Unmarshal(data, c)
}
