package aws

import (
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

// AutoscalingClient performs operations of autoscaling.Autoscaling
type AutoscalingClient interface {
	DescribeScalingActivities(*autoscaling.DescribeScalingActivitiesInput) (*autoscaling.DescribeScalingActivitiesOutput, error)
	DescribeLaunchConfigurations(*autoscaling.DescribeLaunchConfigurationsInput) (*autoscaling.DescribeLaunchConfigurationsOutput, error)
	DescribeAutoScalingInstances(*autoscaling.DescribeAutoScalingInstancesInput) (*autoscaling.DescribeAutoScalingInstancesOutput, error)
	DescribeAutoScalingGroups(*autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
	SetDesiredCapacity(input *autoscaling.SetDesiredCapacityInput) (*autoscaling.SetDesiredCapacityOutput, error)
}
