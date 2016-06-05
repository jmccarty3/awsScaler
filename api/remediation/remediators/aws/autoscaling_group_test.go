package aws

import (
	"bytes"
	"html/template"
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
{{end}}{{end -}}
  `

	tests := []struct {
		Names []string
		Tags  map[string]string
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
			Names: nil,
			Tags:  map[string]string{"one": ""},
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

func CreateAutoScalingGroup(name string, order int) *autoscaling.Group {
	group := &autoscaling.Group{
		AutoScalingGroupName: aws.String(name),
		Tags:                 []*autoscaling.TagDescription{},
	}

	if order != 0 {
		group.Tags = append(group.Tags, &autoscaling.TagDescription{
			Key:   aws.String(ScalerPriotiryTagKey),
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
				CreateAutoScalingGroup("1", 1),
				CreateAutoScalingGroup("2", 2),
				CreateAutoScalingGroup("3", 3),
			},
			ExpectedOrder: []string{"3", "2", "1"},
		},
		{
			Groups: []*autoscaling.Group{
				CreateAutoScalingGroup("1", 1),
				CreateAutoScalingGroup("0", 0),
				CreateAutoScalingGroup("3", 3),
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
