package aws

import (
	"bytes"
	"html/template"
	"math"
	"reflect"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"

	"gopkg.in/yaml.v2"
)

func compareNames(a, b []string) bool {
	if a == nil && b == nil {
		return true
	}

	if len(a) != len(b) {
		return false
	}
	//Order doesn't matter
	for _, n := range a {
		found := false
		for _, bv := range b {
			if bv == n {
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

func compareTags(a, b map[string]string) bool {
	if a == nil && b == nil {
		return true
	}

	if len(a) != len(b) {
		return false
	}

	for ak, av := range a {
		if bv, exists := b[ak]; !exists {
			return false
		} else if av != bv {
			return false
		}
	}

	return true
}

func TestUnmarshalYaml(t *testing.T) {
	var templateYaml = `
{{- with .Names}}names:
{{ range .}}- {{.}}
{{end}}{{end}}
{{- with .Tags }}tags:
{{  range $Key, $Value := .}} {{$Key}}: {{$Value}}
{{end}}{{end}}
{{- with .MaxMachines }}maxMachines: {{.}}
{{end -}}
  `

	maxMachines := [...]int{1, 100, 25}
	tests := []struct {
		Names       []string
		Tags        map[string]string
		MaxMachines *int
	}{
		{
			Names: []string{"foo", "bar"},
			Tags:  map[string]string{"one": "two", "three": "four"},
		},
		{
			Names: []string{"foo", "bar"},
			Tags:  nil,
		},
		{
			Names: nil,
			Tags:  map[string]string{"one": "two"},
		},
		{
			Names:       nil,
			Tags:        map[string]string{"one": ""},
			MaxMachines: nil,
		},
		{
			Names:       []string{"foo", "bar"},
			Tags:        map[string]string{"one": "two", "three": "four"},
			MaxMachines: &maxMachines[0],
		},
		{
			Names:       []string{"foo", "bar"},
			Tags:        nil,
			MaxMachines: &maxMachines[1],
		},
		{
			Names:       nil,
			Tags:        map[string]string{"one": "two"},
			MaxMachines: &maxMachines[2],
		},
	}
	temp := template.Must(template.New("asg").Parse(templateYaml))

	for _, test := range tests {
		var buffer bytes.Buffer
		temp.Execute(&buffer, test)

		asg := &ASGRemediator{}
		if err := yaml.Unmarshal([]byte(buffer.String()), &asg); err != nil {
			t.Errorf("Could not unmarshal: %s  Error: %v", buffer.String(), err)
		} else {
			if !compareNames(test.Names, asg.Names) {
				t.Errorf("Names Expected %v Actual %v", test.Names, asg.Names)
			}

			if !compareTags(test.Tags, asg.Tags) {
				t.Errorf("Tags Expected %v Actual %v", test.Tags, asg.Tags)
			}
		}

	}
}

func createAutoScalingGroup(name string, order int) *autoscaling.Group {
	group := &autoscaling.Group{
		AutoScalingGroupName: aws.String(name),
		Tags:                 []*autoscaling.TagDescription{},
	}

	if order != 0 {
		group.Tags = append(group.Tags, &autoscaling.TagDescription{
			Key:   aws.String(ScalerPriorityTagKey),
			Value: aws.String(strconv.Itoa(order)),
		})
	}

	return group
}

func TestSortGroups(t *testing.T) {

	tests := []struct {
		Groups        []*autoscaling.Group
		ExpectedOrder []string
	}{
		{
			Groups: []*autoscaling.Group{
				createAutoScalingGroup("1", 1),
				createAutoScalingGroup("2", 2),
				createAutoScalingGroup("3", 3),
			},
			ExpectedOrder: []string{"3", "2", "1"},
		},
		{
			Groups: []*autoscaling.Group{
				createAutoScalingGroup("1", 1),
				createAutoScalingGroup("0", 0),
				createAutoScalingGroup("3", 3),
			},
			ExpectedOrder: []string{"3", "1", "0"},
		},
	}

	extract := func(group *autoscaling.Group) string {
		return *group.AutoScalingGroupName
	}

	for _, test := range tests {
		results := sortAutoScalingGroups(test.Groups)
		actual := make([]string, len(test.Groups))
		for i, r := range results {
			actual[i] = extract(r)
		}

		if !reflect.DeepEqual(actual, test.ExpectedOrder) {
			t.Errorf("Expected %v Actual %v", test.ExpectedOrder, actual)
		}
	}

}

func TestGetMaxMachineCount(t *testing.T) {
	getIntPtr := func(v int) *int {
		return &v
	}
	getInt64Ptr := func(v int64) *int64 {
		return &v
	}
	type expectation struct {
		Val    int64
		HasErr bool
	}
	type test struct {
		Rem            *ASGRemediator
		Group          *autoscaling.Group
		ExpectedResult expectation
	}
	createTest := func(configMaxMachines *int, groupMaxSize *int64, expectedVal int64, expectedHasErr bool) test {
		return test{
			Rem:            &ASGRemediator{ASGConfig: ASGConfig{MaxMachines: configMaxMachines}},
			Group:          &autoscaling.Group{MaxSize: groupMaxSize},
			ExpectedResult: expectation{Val: expectedVal, HasErr: expectedHasErr},
		}
	}
	tests := []test{
		createTest(nil, getInt64Ptr(10), 10, false),
		createTest(getIntPtr(5), getInt64Ptr(10), 5, false),
		createTest(getIntPtr(5), getInt64Ptr(4), 4, false),
		createTest(getIntPtr(5), nil, 5, false),
		createTest(nil, nil, math.MaxInt32, true),
		createTest(getIntPtr(-30), nil, math.MaxInt32, true),
		createTest(getIntPtr(-30), getInt64Ptr(5), 5, true),
	}

	for _, test := range tests {
		size, err := test.Rem.getMaxMachineCount(test.Group)
		if size != test.ExpectedResult.Val {
			t.Errorf("getMaxMachineCount failed to get correct size. Expected %d, got %d", test.ExpectedResult.Val, size)
		}
		if hasErr := err != nil; hasErr != test.ExpectedResult.HasErr {
			t.Errorf("getMaxMachineCount failed to get correct error value. Expected error: %v, got error: %v", test.ExpectedResult.HasErr, hasErr)
		}
	}
}
