package strategy

import (
	"testing"

	"gopkg.in/yaml.v2"

	rapi "github.com/jmccarty3/awsScaler/api"
	"github.com/jmccarty3/awsScaler/api/remediation"
	kapi "k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/labels"
)

type failPodCondition struct {
	rapi.PodCondition
}

func (f failPodCondition) CheckPodValid(pod *kapi.Pod) bool {
	return false
}

func (f *failPodCondition) UnmarshalFromYaml(data []byte) error {
	return yaml.Unmarshal(data, f)
}

type successPodCondition struct {
	rapi.PodCondition
}

func (f successPodCondition) CheckPodValid(pod *kapi.Pod) bool {
	return true
}

func (f *successPodCondition) UnmarshalFromYaml(data []byte) error {
	return yaml.Unmarshal(data, f)
}

type failRemediator struct {
	remediation.Remediator
}

type successRemediator struct {
	remediation.Remediator
}

type testCondition struct {
}

func (t *testCondition) CheckPodValid(pod *kapi.Pod) bool {
	return true
}

func (t *testCondition) UnmarshalFromYaml(data []byte) error {
	return yaml.Unmarshal(data, t)
}

func TestRemediationStrategyUnmarshal(t *testing.T) {
	var testConfig = `
namespaces:
- foo
- bar
nodeSelector:
 foo: bar
remediators:
- autoScalingGroup:
    names:
    - foo
`
	var strat RemediationStrategy
	err := yaml.Unmarshal([]byte(testConfig), &strat)

	if err != nil {
		t.Errorf("Unexpected unmarshaling error %v", err)
	}

	if len(strat.Namespaces.Namespaces) != 2 {
		t.Errorf("Got %d conditions. Expected : %d", len(strat.Namespaces.Namespaces), 2)
		t.FailNow()
	}

	if strat.NodeSelector == nil {
		t.Error("Nil NodeSelector")
	}

	if strat.NodeSelector.NodeSelector == nil {
		t.Error("Nil Real selector")
	}

	if len(strat.Remediators) != 1 {
		t.Errorf("Incorrect number of remediators. Got %v", len(strat.Remediators))
	}
}

func TestFilterPods(t *testing.T) {
	podList := []*kapi.Pod{
		&kapi.Pod{
			ObjectMeta: kapi.ObjectMeta{
				Namespace: "pass",
			},
		},
		&kapi.Pod{
			ObjectMeta: kapi.ObjectMeta{
				Namespace: "pass",
			},
		},
	}

	tests := []struct {
		strategy     RemediationStrategy
		successCount int
		failCount    int
	}{
		{
			strategy: RemediationStrategy{
				Namespaces: &rapi.NamespaceCondition{
					Namespaces: []string{"pass"},
				},
			},
			successCount: len(podList),
			failCount:    0,
		},
		{
			strategy: RemediationStrategy{
				Namespaces: &rapi.NamespaceCondition{
					Namespaces: []string{"fail"},
				},
			},
			successCount: 0,
			failCount:    len(podList),
		},
		{
			strategy: RemediationStrategy{
				Namespaces: &rapi.NamespaceCondition{
					Namespaces: []string{"pass"},
				},
				NodeSelector: &rapi.NodeSelectorCondition{
					NodeSelector: labels.SelectorFromSet(labels.Set(map[string]string{"foo": "bar"})),
				},
			},
			successCount: 0,
			failCount:    len(podList),
		},
	}

	for _, test := range tests {
		success, fail := test.strategy.FilterPods(podList)
		if len(success) != test.successCount && len(fail) != test.failCount {
			t.Errorf("Expected: Success %d Fail %d  Actual: Success %d Fail %d", test.successCount, test.failCount, len(success), len(fail))
		}
	}
}
