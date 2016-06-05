package aws

import (
	"errors"
	"math"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"
	"github.com/jmccarty3/awsScaler/api"
)

const defaultRegion = "us-east-1"
const envRegionName = "AWS_DEFAULT_REGION"

func stringSliceContains(toSearch []string, value string) bool {
	for _, v := range toSearch {
		if v == value {
			return true
		}
	}

	return false
}

//TODO Consider abstracting over these for testing purposes

func getAWSCredentials() *credentials.Credentials {
	return credentials.NewChainCredentials(
		[]credentials.Provider{
			&credentials.EnvProvider{},
			&ec2rolecreds.EC2RoleProvider{
				Client: ec2metadata.New(session.New(&aws.Config{})),
			},
			&credentials.SharedCredentialsProvider{},
		})
}

func getMetadataClient() *ec2metadata.EC2Metadata {
	return ec2metadata.New(session.New(&aws.Config{}))
}

func getRegion() string {
	//Check the environment first
	if region, found := os.LookupEnv(envRegionName); found {
		if region != "" {
			glog.Infof("Using region %s from environment", region)
			return region
		}
	}
	//Attempt to check the metadata service
	client := getMetadataClient()
	if client.Available() {
		region, err := client.Region()
		if err == nil {
			glog.Info("Metadata service returned %s region", region)
			return region
		}
	}

	glog.Warningf("Unable to find region from Metadata service or the environment. Using default %s", defaultRegion)
	//Give up. Use default
	return defaultRegion
}

var mapSync sync.Once
var resources map[string]api.Resources

func getResourceForInstanceType(instanceType *string) api.Resources {
	mapSync.Do(func() {
		resources = make(map[string]api.Resources)
		resources[ec2.InstanceTypeC42xlarge] = api.Resources{
			CPU:   8000,
			MemMB: 15000,
		}
		resources[ec2.InstanceTypeC44xlarge] = api.Resources{
			CPU:   16000,
			MemMB: 30000,
		}
		resources[ec2.InstanceTypeM42xlarge] = api.Resources{
			CPU:   8000,
			MemMB: 32000,
		}
		resources[ec2.InstanceTypeM44xlarge] = api.Resources{
			CPU:   16000,
			MemMB: 64000,
		}
	})

	if r, exists := resources[*instanceType]; exists {
		return r
	}

	glog.Warning("Could not find instance type:", *instanceType)
	return api.EmptyResources
}

func calculatedNeededServersForConfig(config *autoscaling.LaunchConfiguration, resources *api.Resources) (int, api.Resources) {
	congfigResources := getResourceForInstanceType(config.InstanceType)

	if congfigResources == api.EmptyResources {
		return 1, api.EmptyResources
	}

	byCPU := math.Ceil(float64(resources.CPU) / float64(congfigResources.CPU))
	byMem := math.Ceil(float64(resources.MemMB) / float64(congfigResources.MemMB))

	val := int(math.Max(byCPU, byMem))

	if val == 0 {
		return 1, api.EmptyResources
	}

	return val, congfigResources
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
