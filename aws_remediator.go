package main

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"
)

var mapSync sync.Once
var resources map[string]Resources

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

func getResourceForInstanceType(instance_type *string) Resources {
	mapSync.Do(func() {
		resources = make(map[string]Resources)
		resources[ec2.InstanceTypeC42xlarge] = Resources{
			CPU:   8000,
			MemMB: 15000,
		}
		resources[ec2.InstanceTypeC44xlarge] = Resources{
			CPU:   16000,
			MemMB: 30000,
		}
		resources[ec2.InstanceTypeM42xlarge] = Resources{
			CPU:   8000,
			MemMB: 32000,
		}
		resources[ec2.InstanceTypeM44xlarge] = Resources{
			CPU:   16000,
			MemMB: 64000,
		}
	})

	if r, exists := resources[*instance_type]; exists {
		return r
	}

	glog.Warning("Could not find instance type:", instance_type)
	return EmptyResources
}

func (rem *AWSRemediator) Remediate(neededResources Resources) (bool, error) {
	success := false
	var err error
	for _, group := range rem.asGroups {
		glog.Info("Attempting to Remediate using group: ", group)
		if success, neededResources, err = rem.attemptRemediate(group, neededResources); success {
			if neededResources != EmptyResources {
				glog.Infof("Autoscaling group %s did not fully meet resource need. NeededResources %v", group, neededResources)
				success = false
				continue
			}
			glog.Info("Remediation successful")
			break
		}
		glog.Warning("Failed remediation. Error: ", err)
	}

	return success, err
}

func (rem *AWSRemediator) getAutoscalingGroup(asGroup string) (*autoscaling.Group, error) {
	params := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			aws.String(asGroup),
		},
		MaxRecords: aws.Int64(1),
	}

	resp, err := rem.client.DescribeAutoScalingGroups(params)

	if err != nil {
		glog.Error("Error fetching Autoscaling group:", asGroup, " Error:", err)
		return nil, err
	}

	if len(resp.AutoScalingGroups) == 0 {
		glog.Error("Autoscaling group with name ", asGroup, " does not exist")
		return nil, errors.New("Auoscaling group does not exist")
	}

	return resp.AutoScalingGroups[0], nil
}

func (rem *AWSRemediator) attemptRemediate(asGroup string, neededResources Resources) (bool, Resources, error) {
	//We only Get a single AutoScalingGroup
	as, err := rem.getAutoscalingGroup(asGroup)

	if err != nil {
		return false, neededResources, err
	}

	if *as.DesiredCapacity == *as.MaxSize {
		glog.Warning("Autoscaling group already at max size")
		return false, neededResources, errors.New("Failed to scale")
	}

	if activity, err := rem.getCurrentActivity(asGroup); err == nil {
		//TODO Probably a good idea to look at errors
		if *activity.StatusCode == autoscaling.ScalingActivityStatusCodeFailed && int(*as.DesiredCapacity) > len(as.Instances) {
			return false, neededResources, errors.New("Autoscaling group last activity failed and desired count exceeds current count. Assuming the worst")
		}

		if rem.checkPreInService(activity) {
			return false, neededResources, errors.New("Autoscaling group in pre service")
		}

		if rem.checkIsWaitingForSpot(activity) {
			spotTimeout := 2 //TODO Make configurable if stays
			glog.Info("Autoscaling group is cluster waiting on spot work. Giving ", spotTimeout, " for instance increase")
			i := len(as.Instances)
			time.Sleep(time.Duration(spotTimeout) * time.Minute)
			as, err = rem.getAutoscalingGroup(asGroup)
			if i >= len(as.Instances) {
				glog.Error("Instance group as not increased members. Assuming the worst")
				return false, neededResources, errors.New("Spot cluster increase seems to have failed")
			}

		}
	} else {
		glog.Error("Could not get current ASG activity", err)
	}

	//Determine how many servers we should
	launchConfig, _ := getLaunchConfig(rem.client, *as.LaunchConfigurationName)
	neededCount, resourcePerMachine := calculatedNeededServersForConfig(launchConfig, &neededResources)

	glog.Infof("Need %v servers from group %s", neededCount, asGroup)

	sizeToScaleTo := len(as.Instances) + neededCount
	if sizeToScaleTo > int(*as.MaxSize) {
		neededCount = int(*as.MaxSize) - len(as.Instances)
		sizeToScaleTo = int(*as.MaxSize) //No risk of truncate since Max Size cannot be anywhere near max int
		glog.Info("Desired capacity too large. Setting to Max.")
	}

	err = rem.scaleGroup(asGroup, int64(sizeToScaleTo))
	glog.Info("Requested group capacity increase for:", asGroup)

	if err != nil {
		return false, neededResources, err
	}

	resourcePerMachine.Scale(int64(neededCount))

	if resourcePerMachine == EmptyResources {
		glog.Warning("Unable to determine now many resources were created. Optimistically assuming everything is fixed")
		return true, EmptyResources, nil
	}

	remainingNeed := neededResources
	remainingNeed.Remove(&resourcePerMachine)
	return true, remainingNeed, nil
}

func (rem *AWSRemediator) scaleGroup(name string, size int64) error {
	params := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(name),
		DesiredCapacity:      aws.Int64(size),
		HonorCooldown:        aws.Bool(false), //TODO Make this settable
	}

	_, err := rem.client.SetDesiredCapacity(params)
	glog.Infof("Requested AS Group %s be set to capacity %v", name, size)

	return err
}

func getLaunchConfig(client *autoscaling.AutoScaling, configName string) (*autoscaling.LaunchConfiguration, error) {
	params := &autoscaling.DescribeLaunchConfigurationsInput{
		LaunchConfigurationNames: []*string{
			aws.String(configName),
		},
		MaxRecords: aws.Int64(1),
	}

	resp, err := client.DescribeLaunchConfigurations(params)

	if err != nil {
		glog.Error("Error getting LaunchConfig: ", configName, " Error:", err)
		return nil, err
	}

	if len(resp.LaunchConfigurations) == 0 {
		glog.Error("Launch Config?", resp, "Name??", configName)
		return nil, errors.New("Successful response but no launch configuration found")
	}

	return resp.LaunchConfigurations[0], nil
}

func calculatedNeededServersForConfig(config *autoscaling.LaunchConfiguration, resources *Resources) (int, Resources) {
	congfigResources := getResourceForInstanceType(config.InstanceType)

	if congfigResources == EmptyResources {
		return 1, EmptyResources
	}

	byCPU := math.Ceil(float64(resources.CPU) / float64(congfigResources.CPU))
	byMem := math.Ceil(float64(resources.MemMB) / float64(congfigResources.MemMB))

	val := int(math.Max(byCPU, byMem))

	if val == 0 {
		return 1, EmptyResources
	}

	return val, congfigResources
}

func (rem *AWSRemediator) groupIsSpotCluster(clusterName string) (bool, error) {
	config, err := getLaunchConfig(rem.client, clusterName)

	if err != nil {
		return false, err
	}

	return isSpotConfig(config), nil
}

func isSpotConfig(config *autoscaling.LaunchConfiguration) bool {
	return config.SpotPrice != nil
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

	if len(resp.Activities) == 0 {
		return nil, errors.New(fmt.Sprintf("No activities for: %s", asg_name))
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
