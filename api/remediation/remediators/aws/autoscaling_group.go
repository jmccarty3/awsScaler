package aws

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/golang/glog"
	"github.com/jmccarty3/awsScaler/api"
	rem "github.com/jmccarty3/awsScaler/api/remediation"
)

//ScalerPriotiryTagKey tag to use to attempt to order autoscaling groups
const ScalerPriotiryTagKey = "scaler_priority"

//RemediatorName name to use when registering the remediator
const RemediatorName = "autoScalingGroup"

//ASGConfig used for marshalling data to/from yaml
type ASGConfig struct {
	Names    []string `yaml:"names"`
	Tags     map[string]string
	SelfTags []string `yaml:"selfTags"`
}

//ASGRemediator attempts to remediate scheduling issues using AutoScalingGroups
type ASGRemediator struct {
	Names    []string `yaml:"names"`
	Tags     map[string]string
	SelfTags []string `yaml:"selfTags"`
	client   *autoscaling.AutoScaling
}

func newASGRemediator(config rem.ConfigData) rem.Remediator {
	return &ASGRemediator{
		client: autoscaling.New(session.New(&aws.Config{
			Credentials: getAWSCredentials(),
			Region:      aws.String(getRegion()),
		})),
	}
}

//UnmarshalYAML is used to unmarshal the remediator from yaml config
func (asgRemediator *ASGRemediator) UnmarshalYAML(unmarshal func(interface{}) error) error {
	config := &ASGConfig{}
	err := unmarshal(&config)
	asgRemediator.Names = config.Names
	asgRemediator.Tags = config.Tags
	asgRemediator.SelfTags = config.SelfTags
	return err
}

func init() {
	rem.RegisterRemediator(RemediatorName, newASGRemediator)
}

//mergeTags merges the second map into the first.
func mergeTags(primary, secondary map[string]string) map[string]string {
	for k, v := range primary {
		secondary[k] = v
	}

	return secondary
}

//Remediate will attempt to increase autoscaling groups to resolve the failed pods
func (asgRemediator *ASGRemediator) Remediate(neededResources *api.Resources) (bool, *api.Resources, error) {
	success := false
	var err error

	tags := asgRemediator.Tags
	if len(asgRemediator.SelfTags) != 0 {
		tagsForSelf := asgRemediator.getSelfTags()
		tags = mergeTags(tags, tagsForSelf)
	}

	groups, err := asgRemediator.getAllAutoscalingGroups(&asgRemediator.Names, &tags)

	if len(groups) == 0 {
		glog.Error("No autoscaling groups found.")
		return false, neededResources, nil
	}

	groups = sortAutoScalingGroups(groups)

	for _, group := range groups {
		glog.Info("Attempting to Remediate using group: ", *group.AutoScalingGroupName)
		if success, neededResources, err = asgRemediator.attemptRemediate(group, neededResources); success {
			if *neededResources != api.EmptyResources {
				glog.Infof("Autoscaling group %s did not fully meet resource need. NeededResources %v", group, neededResources)
				success = false
				continue
			}
			glog.Info("Remediation successful")
			break
		}
		glog.Warning("Failed remediation. Error: ", err)
	}

	return success, neededResources, err
}

//TODO condense br returning error and having the calling function panic
func (asgRemediator *ASGRemediator) getSelfTags() (tags map[string]string) {
	metaData := getMetadataClient()

	if !metaData.Available() {
		panic("Metadata service not available. Possibly not running in AWS. Please check configuration")
	}
	var instanceID string
	if doc, err := metaData.GetInstanceIdentityDocument(); err != nil {
		panic(fmt.Sprintf("Unable to fetch instance id. %v", err))
	} else {
		instanceID = doc.InstanceID
	}

	output, err := asgRemediator.client.DescribeAutoScalingInstances(&autoscaling.DescribeAutoScalingInstancesInput{
		InstanceIds: []*string{aws.String(instanceID)},
	})

	if err != nil {
		panic(fmt.Sprintf("Unabele to describe autoscaling instances. Error: %v", err))
	}

	if len(output.AutoScalingInstances) != 1 {
		panic("Incorrect number of autoscaling groups for self")
	}

	group, _ := asgRemediator.getAutoscalingGroup(*output.AutoScalingInstances[0].AutoScalingGroupName)
	tags = make(map[string]string)
	for _, tag := range group.Tags {
		if stringSliceContains(asgRemediator.SelfTags, *tag.Key) {
			tags[*tag.Key] = *tag.Value
		}
	}

	if len(tags) != len(asgRemediator.SelfTags) {
		panic("Not all self tags found")
	}
	return
}

func (asgRemediator *ASGRemediator) getAllAutoscalingGroups(names *[]string, tags *map[string]string) ([]*autoscaling.Group, error) {
	glog.Info("Fetching all autoscaling groups")
	resp, err := asgRemediator.client.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{})

	if err != nil {
		glog.Errorf("Error fetching autoscaling groups. %v", err)
		return nil, err
	}

	groups := []*autoscaling.Group{}

	for _, asg := range resp.AutoScalingGroups {
		if stringSliceContains(*names, *asg.AutoScalingGroupName) {
			glog.Infof("Found matching autoscaling group name %s", *asg.AutoScalingGroupName)
			groups = append(groups, asg)
			continue
		}

		if allTagsPresent(asg.Tags, *tags) {
			glog.Infof("Found autoscaling group %s matching all tags", *asg.AutoScalingGroupName)
			groups = append(groups, asg)
			continue
		}
	}

	return groups, nil
}

//ByTag implements sort.Interface for []*autoscaling.Group based on tag
type ByTag []*autoscaling.Group

func (a ByTag) Len() int      { return len(a) }
func (a ByTag) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByTag) Less(i, j int) bool {
	return extractPriority(a[i].Tags) < extractPriority(a[j].Tags)
}

func extractPriority(tags []*autoscaling.TagDescription) int {
	for _, t := range tags {
		if *t.Key == ScalerPriotiryTagKey {
			p, err := strconv.Atoi(*t.Value)
			if err != nil {
				glog.Errorf("Error parsing %s to int\n", *t.Value)
				break
			}
			return p
		}
	}

	return 0
}

func sortAutoScalingGroups(toSort []*autoscaling.Group) []*autoscaling.Group {
	//TODO Put un prioritized names in order?
	glog.V(4).Info("Sorting groups")
	sort.Sort(sort.Reverse(ByTag(toSort)))
	return toSort
}

func allTagsPresent(toSearch []*autoscaling.TagDescription, toFind map[string]string) bool {
	if len(toFind) == 0 {
		return false //If there are no tags, we can't match
	}
	foundCount := 0
	//Looks terrible but prevents continuously iterating over all tags
	for _, t := range toSearch {
		if value, exists := toFind[*t.Key]; exists {
			if value == *t.Value {
				foundCount++
			} else {
				return false
			}
		}
	}
	return foundCount == len(toFind)
}

func (asgRemediator *ASGRemediator) isGroupValid(group *autoscaling.Group) bool {
	if stringSliceContains(asgRemediator.Names, *group.AutoScalingGroupName) {
		return true
	}

	return false
}

func (asgRemediator *ASGRemediator) getAutoscalingGroup(asGroup string) (*autoscaling.Group, error) {
	params := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			aws.String(asGroup),
		},
		MaxRecords: aws.Int64(1),
	}

	resp, err := asgRemediator.client.DescribeAutoScalingGroups(params)

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

func (asgRemediator *ASGRemediator) attemptRemediate(asGroup *autoscaling.Group, neededResources *api.Resources) (bool, *api.Resources, error) {
	if *asGroup.DesiredCapacity == *asGroup.MaxSize {
		glog.Warning("Autoscaling group already at max size")
		return false, neededResources, errors.New("Failed to scale")
	}

	if activity, err := asgRemediator.getCurrentActivity(*asGroup.AutoScalingGroupName); err == nil {
		//TODO Probably a good idea to look at errors
		if *activity.StatusCode == autoscaling.ScalingActivityStatusCodeFailed && int(*asGroup.DesiredCapacity) > len(asGroup.Instances) {
			return false, neededResources, errors.New("Autoscaling group last activity failed and desired count exceeds current count. Assuming the worst")
		}

		if asgRemediator.checkPreInService(activity) {
			return false, neededResources, errors.New("Autoscaling group in pre service")
		}

		if asgRemediator.checkIsWaitingForSpot(activity) {
			spotTimeout := 2 //TODO Make configurable if stays
			glog.Info("Autoscaling group is cluster waiting on spot work. Giving ", spotTimeout, " for instance increase")
			i := len(asGroup.Instances)
			time.Sleep(time.Duration(spotTimeout) * time.Minute)
			asGroup, err = asgRemediator.getAutoscalingGroup(*asGroup.AutoScalingGroupName)
			if i >= len(asGroup.Instances) {
				glog.Error("Instance group has not increased members. Assuming the worst")
				return false, neededResources, errors.New("Spot cluster increase seems to have failed")
			}

		}
	} else {
		glog.Error("Could not get current ASG activity", err)
	}

	//Determine how many servers we should
	launchConfig, _ := getLaunchConfig(asgRemediator.client, *asGroup.LaunchConfigurationName)
	neededCount, resourcePerMachine := calculatedNeededServersForConfig(launchConfig, neededResources)

	glog.Infof("Need %v servers from group %s", neededCount, *asGroup.AutoScalingGroupName)

	sizeToScaleTo := len(asGroup.Instances) + neededCount
	if sizeToScaleTo > int(*asGroup.MaxSize) {
		neededCount = int(*asGroup.MaxSize) - len(asGroup.Instances)
		sizeToScaleTo = int(*asGroup.MaxSize) //No risk of truncate since Max Size cannot be anywhere near max int
		glog.Info("Desired capacity too large. Setting to Max.")
	}

	err := asgRemediator.scaleGroup(*asGroup.AutoScalingGroupName, int64(sizeToScaleTo))
	glog.Info("Requested group capacity increase for:", *asGroup.AutoScalingGroupName)

	if err != nil {
		return false, neededResources, err
	}

	resourcePerMachine.Scale(int64(neededCount))

	if resourcePerMachine == api.EmptyResources {
		glog.Warning("Unable to determine now many resources were created. Optimistically assuming everything is fixed")
		return true, &api.EmptyResources, nil
	}

	remainingNeed := neededResources
	remainingNeed.Remove(&resourcePerMachine)
	return true, remainingNeed, nil
}

func (asgRemediator *ASGRemediator) scaleGroup(name string, size int64) error {
	params := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(name),
		DesiredCapacity:      aws.Int64(size),
		HonorCooldown:        aws.Bool(false), //TODO Make this settable
	}

	_, err := asgRemediator.client.SetDesiredCapacity(params)
	glog.Infof("Requested AS Group %s be set to capacity %v", name, size)

	return err
}

func (asgRemediator *ASGRemediator) groupIsSpotCluster(clusterName string) (bool, error) {
	config, err := getLaunchConfig(asgRemediator.client, clusterName)

	if err != nil {
		return false, err
	}

	return isSpotConfig(config), nil
}

func isSpotConfig(config *autoscaling.LaunchConfiguration) bool {
	return config.SpotPrice != nil
}

func (asgRemediator *ASGRemediator) getCurrentActivity(asgName string) (*autoscaling.Activity, error) {
	params := &autoscaling.DescribeScalingActivitiesInput{
		AutoScalingGroupName: aws.String(asgName),
		MaxRecords:           aws.Int64(1), //Only want the last/current action
	}

	resp, err := asgRemediator.client.DescribeScalingActivities(params)

	if err != nil {
		return nil, err
	}

	if len(resp.Activities) == 0 {
		return nil, fmt.Errorf("No activities for: %s", asgName)
	}

	return resp.Activities[0], nil
}

func (asgRemediator *ASGRemediator) checkPreInService(activity *autoscaling.Activity) bool {
	return *activity.StatusCode == autoscaling.ScalingActivityStatusCodePreInService
}

func (asgRemediator *ASGRemediator) checkIsWaitingForSpot(activity *autoscaling.Activity) bool {
	return *activity.StatusCode == autoscaling.ScalingActivityStatusCodePendingSpotBidPlacement ||
		*activity.StatusCode == autoscaling.ScalingActivityStatusCodeWaitingForSpotInstanceId ||
		*activity.StatusCode == autoscaling.ScalingActivityStatusCodeWaitingForSpotInstanceRequestId
}
