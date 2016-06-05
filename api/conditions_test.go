package api

import (
	"reflect"
	"testing"

	"k8s.io/kubernetes/pkg/labels"

	"gopkg.in/yaml.v2"
)

func TestNameSpaceUnMarshal(t *testing.T) {

	var data = `
  namespaces:
    - foo
    - bar
  `

	var condition NamespaceCondition
	yaml.Unmarshal([]byte(data), &condition)

	if len(condition.Namespaces) != 2 {
		t.Error("Incorrect number of namespaces")
	}

	expected := []string{"foo", "bar"}

	for i := range expected {
		if expected[i] != condition.Namespaces[i] {
			t.Errorf("Expected: %v Actual %v", expected, condition.Namespaces)
			return
		}
	}
}

func TestNamespaceEquality(t *testing.T) {
	tests := []struct {
		ValA  NamespaceCondition
		ValB  NamespaceCondition
		Equal bool
	}{
		{
			ValA: NamespaceCondition{
				Namespaces: []string{"Foo", "Bar"},
			},
			ValB: NamespaceCondition{
				Namespaces: []string{"Foo", "Bar"},
			},
			Equal: true,
		},
		{
			ValA: NamespaceCondition{
				Namespaces: []string{"Bar", "Foo"},
			},
			ValB: NamespaceCondition{
				Namespaces: []string{"Foo", "Bar"},
			},
			Equal: true,
		},
		{
			ValA: NamespaceCondition{
				Namespaces: []string{},
			},
			ValB: NamespaceCondition{
				Namespaces: []string{"Foo", "Bar"},
			},
			Equal: false,
		},
	}

	for _, test := range tests {
		if test.ValA.Equal(&test.ValB) != test.Equal {
			t.Errorf("Equallity Error. ValA: %v ValB: %v Expected: %v", test.ValA, test.ValB, test.Equal)
		}
	}
}

func TestNodeLabelSelectorUnMarshal(t *testing.T) {
	var data = `
  foo: bar
  key: value
  `
	expected := labels.SelectorFromSet(map[string]string{"foo": "bar", "key": "value"})

	var condition NodeSelectorCondition
	if err := yaml.Unmarshal([]byte(data), &condition); err != nil {
		t.Errorf("Unexpected error during unmarshal %v", err)
	}

	if !reflect.DeepEqual(labels.SelectorFromSet(condition.Keys), expected) {
		t.Errorf("Expected: %v Actual: %v", expected, labels.SelectorFromSet(condition.Keys))
	}
}
