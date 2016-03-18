package main

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func buildLaunchConfig(instanceType string) *autoscaling.LaunchConfiguration {
	return &autoscaling.LaunchConfiguration{
		InstanceType: aws.String(instanceType),
	}
}

func makeResources(cpu, mem int64) *Resources {
	return &Resources{
		CPU:   cpu,
		MemMB: mem,
	}
}

func TestCalculateServers(t *testing.T) {
	tests := []struct {
		config          *autoscaling.LaunchConfiguration
		resourcesNeeded *Resources
		expected        int
		test            string
	}{
		{
			config:          buildLaunchConfig(ec2.InstanceTypeM42xlarge),
			resourcesNeeded: makeResources(0, 0),
			expected:        1,
			test:            "Blank Resource Requests",
		},
		{
			config:          buildLaunchConfig(ec2.InstanceTypeM42xlarge),
			resourcesNeeded: makeResources(1, 0),
			expected:        1,
			test:            "Blank CPU Requests",
		},
		{
			config:          buildLaunchConfig(ec2.InstanceTypeM42xlarge),
			resourcesNeeded: makeResources(0, 1000),
			expected:        1,
			test:            "Blank Memory Requests",
		},
		{
			config:          buildLaunchConfig(ec2.InstanceTypeM42xlarge),
			resourcesNeeded: makeResources(1, 1000),
			expected:        1,
			test:            "Small Requests",
		},
		{
			config:          buildLaunchConfig(ec2.InstanceTypeM42xlarge),
			resourcesNeeded: makeResources(1, 33000),
			expected:        2,
			test:            "Memory Exceeds Limit",
		},
		{
			config:          buildLaunchConfig(ec2.InstanceTypeM42xlarge),
			resourcesNeeded: makeResources(1, 320000),
			expected:        10,
			test:            "Memory Greatly Exceeds Limit",
		},
		{
			config:          buildLaunchConfig(ec2.InstanceTypeM42xlarge),
			resourcesNeeded: makeResources(8001, 1),
			expected:        2,
			test:            "CPU Exceeds Limit",
		},
		{
			config:          buildLaunchConfig(ec2.InstanceTypeM42xlarge),
			resourcesNeeded: makeResources(80000, 1),
			expected:        10,
			test:            "CPU Greatly Exceeds Limit",
		},
		{
			config:          buildLaunchConfig("Unknown"),
			resourcesNeeded: makeResources(0, 0),
			expected:        1,
			test:            "Unknown Type",
		},
	}

	for _, test := range tests {
		if actual, _ := calculatedNeededServersForConfig(test.config, test.resourcesNeeded); actual != test.expected {
			t.Errorf("%s Failed. Expected: %d, Got: %d", test.test, test.expected, actual)
		}
	}
}
