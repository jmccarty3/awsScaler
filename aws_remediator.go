package main

import (
	"errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/golang/glog"
)

type AWSRemediator struct {
	client   *autoscaling.AutoScaling
	asGroups []string
}

func NewAWSRemediator(asGroups []string) *AWSRemediator {
	return &AWSRemediator{
		client: autoscaling.New(session.New(&aws.Config{
			Region: aws.String("us-east-1"),
		})),
		asGroups: asGroups,
	}
}

func (rem *AWSRemediator) Remediate() (bool, error) {
	success := false
	var err error
	for _, group := range rem.asGroups {
		glog.Info("Attempting to Remediate using group: ", group)
		if success, err = rem.attempRemediate(group); success {
			glog.Info("Remediation successful")
			break
		}
		glog.Warning("Failed remediation. Error: ", err)
	}

	return success, err
}

func (rem *AWSRemediator) attempRemediate(asGroup string) (bool, error) {
	params := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			aws.String(asGroup),
		},
		MaxRecords: aws.Int64(1),
	}

	resp, err := rem.client.DescribeAutoScalingGroups(params)

	if err != nil {
		glog.Error("Error fetching Autoscaling group:", asGroup, " Error:", err)
		return false, err
	}
	//We only Get a single AutoScalingGroup
	//TODO Iterate over multiple
	as := resp.AutoScalingGroups[0]

	if *as.DesiredCapacity == *as.MaxSize {
		glog.Warning("Autoscaling group already at max size")
		return false, errors.New("Failed to scale")
	}

	if activity, err := rem.getCurrentActivity(asGroup); err == nil {
		//TODO Probably a good idea to look at errors
		if rem.checkPreInService(activity) {
			return false, errors.New("Autoscaling group in pre service")
		}

		if rem.checkIsWaitingForSpot(activity) {
			glog.Info("Autoscaling group is cluster waiting on spot work")
		}
	} else {
		glog.Error("Could not get current ASG activity", err)
	}

	err = rem.scaleGroup(asGroup, (*as.DesiredCapacity + 1))
	glog.Info("Requested group capacity increase for:", asGroup)
	return err == nil, err
}

func (rem *AWSRemediator) scaleGroup(name string, size int64) error {
	params := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(name),
		DesiredCapacity:      aws.Int64(size),
		HonorCooldown:        aws.Bool(false), //TODO Make this settable
	}

	_, err := rem.client.SetDesiredCapacity(params)

	return err
}

func (rem *AWSRemediator) checkIsSpotCluster(configName string) (bool, error) {
	params := &autoscaling.DescribeLaunchConfigurationsInput{
		LaunchConfigurationNames: []*string{
			aws.String(configName),
		},
		MaxRecords: aws.Int64(1),
	}

	resp, err := rem.client.DescribeLaunchConfigurations(params)

	if err != nil {
		glog.Error("Error getting LaunchConfig: ", configName, " Error:", err)
		return false, err
	}

	if len(resp.LaunchConfigurations) == 0 {
		glog.Error("Launch Config?", resp, "Name??", configName)
	}

	return resp.LaunchConfigurations[0].SpotPrice == nil, nil
}

func (rem *AWSRemediator) getCurrentActivity(asg_name string) (*autoscaling.Activity, error) {
	params := &autoscaling.DescribeScalingActivitiesInput{
		AutoScalingGroupName: aws.String(asg_name),
		MaxRecords:           aws.Int64(1), //Only want the last/current action
	}

	resp, err := rem.client.DescribeScalingActivities(params)

	if err != nil {
		return nil, err
	}

	return resp.Activities[0], nil
}

func (rem *AWSRemediator) checkPreInService(activity *autoscaling.Activity) bool {
	return *activity.StatusCode == autoscaling.ScalingActivityStatusCodePreInService
}

func (rem *AWSRemediator) checkIsWaitingForSpot(activity *autoscaling.Activity) bool {
	return *activity.StatusCode == autoscaling.ScalingActivityStatusCodePendingSpotBidPlacement ||
		*activity.StatusCode == autoscaling.ScalingActivityStatusCodeWaitingForSpotInstanceId ||
		*activity.StatusCode == autoscaling.ScalingActivityStatusCodeWaitingForSpotInstanceRequestId
}
