package aws

import (
	"bytes"
	"fmt"
	"html/template"
	"reflect"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	"github.com/jmccarty3/awsScaler/api"

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
{{- with .MaxMachineIncrement }}maxMachineIncrement: {{.}}
{{end -}}
{{- with .StopIfMaximallyIncremented }}stopIfMaximallyIncremented: {{.}}
{{end -}}
  `

	tests := []struct {
		Names                      []string
		Tags                       map[string]string
		MaxMachineIncrement        *int
		StopIfMaximallyIncremented *bool
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
			Names:               nil,
			Tags:                map[string]string{"one": ""},
			MaxMachineIncrement: nil,
		},
		{
			Names:                      []string{"foo", "bar"},
			Tags:                       map[string]string{"one": "two", "three": "four"},
			MaxMachineIncrement:        aws.Int(1),
			StopIfMaximallyIncremented: aws.Bool(true),
		},
		{
			Names:                      []string{"foo", "bar"},
			Tags:                       nil,
			MaxMachineIncrement:        aws.Int(100),
			StopIfMaximallyIncremented: aws.Bool(false),
		},
		{
			Names:                      nil,
			Tags:                       map[string]string{"one": "two"},
			MaxMachineIncrement:        aws.Int(25),
			StopIfMaximallyIncremented: nil,
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

			valueErr := false
			switch {
			case test.MaxMachineIncrement == nil && asg.MaxMachineIncrement == nil:
			case test.MaxMachineIncrement == nil, asg.MaxMachineIncrement == nil:
				valueErr = true
			default:
				valueErr = *test.MaxMachineIncrement != *asg.MaxMachineIncrement
			}
			if valueErr {
				t.Errorf("MaxMachineIncrement Expected %v Actual %v", asg.MaxMachineIncrement, test.MaxMachineIncrement)
			}

			valueErr = false
			switch {
			case test.StopIfMaximallyIncremented == nil && !asg.StopIfMaximallyIncremented:
			case test.StopIfMaximallyIncremented == nil:
				valueErr = true
			default:
				valueErr = *test.StopIfMaximallyIncremented != asg.StopIfMaximallyIncremented
			}
			if valueErr {
				t.Errorf("StopIfMaximallyIncremented Expected %v Actual %v", asg.StopIfMaximallyIncremented, test.StopIfMaximallyIncremented)
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

func getInstanceList(length int) []*autoscaling.Instance {
	instances := []*autoscaling.Instance{}
	for i := 0; i < length; i++ {
		instances = append(instances, nil)
	}
	return instances
}

func TestAttemptRemediate(t *testing.T) {
	type inputs struct {
		configMaxMachineIncrement *int
		configStopIfMaxIncrement  bool
		asgDesiredCapactiy        int64
		asgMaxSize                int64
		asgCurrentNumInstances    int
		activityStatusCode        string
		instanceType              string
		neededResources           api.Resources
	}

	type expectedResults struct {
		shouldDescribeScalingActivities    bool
		shouldDescribeLaunchConfigurations bool
		shouldSetDesiredCapacity           bool
		setDesiredCapacity                 int64
		remainingNeededResources           api.Resources
		err                                error
	}
	name := aws.String("blah")
	tests := map[inputs]expectedResults{
		inputs{ // Request 6 additional machines; limit 5 by MaxMachineIncrement;
			configMaxMachineIncrement: aws.Int(5),
			configStopIfMaxIncrement:  false,
			asgDesiredCapactiy:        5,
			asgMaxSize:                10,
			asgCurrentNumInstances:    5,
			activityStatusCode:        autoscaling.ScalingActivityStatusCodeSuccessful,
			instanceType:              ec2.InstanceTypeM44xlarge,
			neededResources: api.Resources{
				CPU:   96000, // 6x instanceType's CPU
				MemMB: 64000,
			},
		}: expectedResults{
			shouldDescribeScalingActivities:    true,
			shouldDescribeLaunchConfigurations: true,
			shouldSetDesiredCapacity:           true,
			setDesiredCapacity:                 10,
			remainingNeededResources:           api.Resources{CPU: 16000}, // failed to get 1 ec2 instance's worth of CPU
		},

		inputs{ // Request 6 additional machines; limit 5 by MaxMachineIncrement; return empty resources due to StopIfMaximallyIncremented
			configMaxMachineIncrement: aws.Int(5),
			configStopIfMaxIncrement:  true,
			asgDesiredCapactiy:        5,
			asgMaxSize:                10,
			asgCurrentNumInstances:    5,
			activityStatusCode:        autoscaling.ScalingActivityStatusCodeSuccessful,
			instanceType:              ec2.InstanceTypeM44xlarge,
			neededResources: api.Resources{
				CPU:   96000, // 6x instanceType's CPU
				MemMB: 64000,
			},
		}: expectedResults{
			shouldDescribeScalingActivities:    true,
			shouldDescribeLaunchConfigurations: true,
			shouldSetDesiredCapacity:           true,
			setDesiredCapacity:                 10,
			remainingNeededResources:           api.EmptyResources,
		},

		inputs{ // limited by asgMaxSize
			configStopIfMaxIncrement: true,
			asgDesiredCapactiy:       5,
			asgMaxSize:               10,
			asgCurrentNumInstances:   5,
			activityStatusCode:       autoscaling.ScalingActivityStatusCodeSuccessful,
			instanceType:             ec2.InstanceTypeM44xlarge,
			neededResources: api.Resources{
				CPU:   96000, // 6x instanceType's CPU
				MemMB: 100000,
			},
		}: expectedResults{
			shouldDescribeScalingActivities:    true,
			shouldDescribeLaunchConfigurations: true,
			shouldSetDesiredCapacity:           true,
			setDesiredCapacity:                 10,
			remainingNeededResources:           api.Resources{CPU: 16000},
		},

		inputs{ // resources not limited; should add 6
			asgDesiredCapactiy:     5,
			asgMaxSize:             15,
			asgCurrentNumInstances: 5,
			activityStatusCode:     autoscaling.ScalingActivityStatusCodeSuccessful,
			instanceType:           ec2.InstanceTypeM44xlarge,
			neededResources: api.Resources{
				CPU:   96000, // 6x instanceType's CPU
				MemMB: 10,
			},
		}: expectedResults{
			shouldDescribeScalingActivities:    true,
			shouldDescribeLaunchConfigurations: true,
			shouldSetDesiredCapacity:           true,
			setDesiredCapacity:                 11,
			remainingNeededResources:           api.EmptyResources,
		},

		// initial desired exceeds max size -> error
		inputs{
			asgDesiredCapactiy:     16,
			asgMaxSize:             15,
			asgCurrentNumInstances: 5,
			activityStatusCode:     autoscaling.ScalingActivityStatusCodeSuccessful,
			instanceType:           ec2.InstanceTypeM44xlarge,
			neededResources:        api.Resources{CPU: 96000, MemMB: 10},
		}: expectedResults{
			shouldDescribeScalingActivities: false,
			remainingNeededResources:        api.Resources{CPU: 96000, MemMB: 10},
			err: fmt.Errorf("Failed to scale.  Autoscaling group blah at max size."),
		},

		// PreInService ScalingActivityStatusCodePreInService -> error
		inputs{
			asgDesiredCapactiy:     5,
			asgMaxSize:             15,
			asgCurrentNumInstances: 5,
			activityStatusCode:     autoscaling.ScalingActivityStatusCodePreInService,
			instanceType:           ec2.InstanceTypeM44xlarge,
			neededResources:        api.Resources{CPU: 96000, MemMB: 10},
		}: expectedResults{
			shouldDescribeScalingActivities:    true,
			shouldDescribeLaunchConfigurations: false,
			remainingNeededResources:           api.Resources{CPU: 96000, MemMB: 10},
			err: fmt.Errorf("Autoscaling group in pre service"),
		},
	}

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockAutoscalingClient := NewMockAutoscalingClient(mockCtrl)
	asgRemediator := &ASGRemediator{client: mockAutoscalingClient}
	for in, out := range tests {
		asgRemediator.MaxMachineIncrement = in.configMaxMachineIncrement
		asgRemediator.StopIfMaximallyIncremented = in.configStopIfMaxIncrement
		asGroup := &autoscaling.Group{
			AutoScalingGroupName:    name,
			LaunchConfigurationName: name,
			DesiredCapacity:         &in.asgDesiredCapactiy,
			MaxSize:                 &in.asgMaxSize,
			Instances:               getInstanceList(in.asgCurrentNumInstances),
		}

		if out.shouldDescribeScalingActivities {
			first := mockAutoscalingClient.EXPECT().DescribeScalingActivities(gomock.Any()).Return(
				&autoscaling.DescribeScalingActivitiesOutput{
					Activities: []*autoscaling.Activity{&autoscaling.Activity{
						StatusCode: &in.activityStatusCode,
					},
					}}, nil)

			if out.shouldDescribeLaunchConfigurations {
				second := mockAutoscalingClient.EXPECT().DescribeLaunchConfigurations(gomock.Any()).Return(
					&autoscaling.DescribeLaunchConfigurationsOutput{
						LaunchConfigurations: []*autoscaling.LaunchConfiguration{&autoscaling.LaunchConfiguration{
							InstanceType: &in.instanceType}},
					}, nil).After(first)

				if out.shouldSetDesiredCapacity {
					mockAutoscalingClient.EXPECT().SetDesiredCapacity(&autoscaling.SetDesiredCapacityInput{
						AutoScalingGroupName: name,
						DesiredCapacity:      &out.setDesiredCapacity,
						HonorCooldown:        aws.Bool(false),
					}).After(second)
				}
			}
		}

		remainingNeeded, err := asgRemediator.attemptRemediate(asGroup, &in.neededResources)
		if (err == nil) != (out.err == nil) {
			t.Errorf("Expected error %v but got error %v when attempting to remediate", out.err, err)
		}
		if *remainingNeeded != out.remainingNeededResources {
			t.Errorf("Expected %v resources after attempt remediate, but got %v", out.remainingNeededResources, *remainingNeeded)
		}
	}

}
