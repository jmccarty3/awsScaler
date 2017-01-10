package main

import (
	"fmt"
	"testing"

	raws "github.com/jmccarty3/awsScaler/api/remediation/remediators/aws"
	"gopkg.in/yaml.v2"
)

/*
func TestUnmarshall(t *testing.T) {
	var data = `
    foo: name
    `

	yaml.Unmarshal([]byte(data))
}
*/

type TestCondition struct {
	Data  string
	Count int
}

var testConfig = `
strategies:
- remediators:
  -  autoScalingGroup:
      names:
      - foo
`

func TestLoadConfig(t *testing.T) {
	var config Config

	err := yaml.Unmarshal([]byte(testConfig), &config)

	if err != nil {
		t.Errorf("Unexpected unmarshaling error. %v", err)
	}

	if len(config.Strategies) != 1 {
		fmt.Printf("Config.Rem: %v\n", config.Strategies)
		t.Errorf("Unexpected number of strategies. Expected %d Actual %d", 1, len(config.Strategies))
	} else {
		fmt.Printf("Len Rems: %v\n", len(config.Strategies[0].Remediators))
		fmt.Printf("Names! : %v\n", config.Strategies[0].Remediators[0].(*raws.ASGRemediator).Names)
	}

	fmt.Printf("Config.Strat %v\n", config.Strategies)
}
