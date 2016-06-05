package remediation

import (
	"fmt"
	"sync"

	"github.com/jmccarty3/awsScaler/api"
)

//Remediator is responsible for taking action to resolve resource issues
type Remediator interface {
	Remediate(resources *api.Resources) (success bool, remaining *api.Resources, err error)
	//filterAndRemediate(available []*api.Pod) (remainingResources *Resources, err error)
}

//ConfigData contains information required to configure a remediator
type ConfigData []byte

//RemediatorCreationFunc is used to create a Remediator
type RemediatorCreationFunc func(ConfigData) Remediator

var remFactoryMutex sync.Mutex
var remediators = make(map[string]RemediatorCreationFunc)

//RegisterRemediator registeres a remediator by name
func RegisterRemediator(name string, creator RemediatorCreationFunc) error {
	remFactoryMutex.Lock()
	defer remFactoryMutex.Unlock()

	if _, exists := remediators[name]; exists {
		return fmt.Errorf("%s is already registerd", name)
	}

	remediators[name] = creator
	return nil
}

//GetRemediatorCreator retrieves a creation function by name
func GetRemediatorCreator(name string) (RemediatorCreationFunc, error) {
	remFactoryMutex.Lock()
	defer remFactoryMutex.Unlock()

	if factory, exists := remediators[name]; exists {
		return factory, nil
	}

	return nil, fmt.Errorf("%s is not registered", name)
}
