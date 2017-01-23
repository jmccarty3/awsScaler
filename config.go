package main

import (
	"fmt"

	"github.com/jmccarty3/awsScaler/api/strategy"

	"gopkg.in/yaml.v2"
)

//Config represents configuration information for the scaler
type Config struct {
	Strategies []strategy.RemediationStrategy `yaml:"strategies"`
}

//TODO Remove this
func prettyPrintMap(m map[interface{}]interface{}) {
	for n, v := range m {
		fmt.Printf("Key: %v Val %v \n", n, v)
	}
}

//TODO Remove this
func prettyPrintMapSlice(m yaml.MapSlice) {
	for n, v := range m {
		fmt.Printf("Key: %v Val %v \n", n, v)
	}
}

//TODO Remove this
func prettyPrintMapItem(m yaml.MapItem) {
	fmt.Printf("Key: %v Val %v \n", m.Key, m.Value)
}
