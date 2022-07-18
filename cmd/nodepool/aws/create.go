package aws

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	hyperv1 "github.com/openshift/hypershift/api/v1alpha1"
	"github.com/openshift/hypershift/cmd/nodepool/core"
	"github.com/openshift/hypershift/support/releaseinfo"
	"github.com/spf13/cobra"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type AWSPlatformCreateOptions struct {
	InstanceProfile    string
	SubnetID           string
	SecurityGroupID    string
	InstanceType       string
	RootVolumeType     string
	RootVolumeIOPS     int64
	RootVolumeSize     int64
	PullSecretFile     string
	AWSCredentialsFile string
}

func NewCreateCommand(coreOpts *core.CreateNodePoolOptions) *cobra.Command {
	platformOpts := &AWSPlatformCreateOptions{
		InstanceType:   "m5.large",
		RootVolumeType: "gp3",
		RootVolumeSize: 120,
		RootVolumeIOPS: 0,
	}
	cmd := &cobra.Command{
		Use:          "aws",
		Short:        "Creates basic functional NodePool resources for AWS platform",
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&platformOpts.InstanceType, "instance-type", platformOpts.InstanceType, "The AWS instance type of the NodePool")
	cmd.Flags().StringVar(&platformOpts.SubnetID, "subnet-id", platformOpts.SubnetID, "The AWS subnet ID in which to create the NodePool")
	cmd.Flags().StringVar(&platformOpts.SecurityGroupID, "securitygroup-id", platformOpts.SecurityGroupID, "The AWS security group in which to create the NodePool")
	cmd.Flags().StringVar(&platformOpts.InstanceProfile, "instance-profile", platformOpts.InstanceProfile, "The AWS instance profile for the NodePool")
	cmd.Flags().StringVar(&platformOpts.RootVolumeType, "root-volume-type", platformOpts.RootVolumeType, "The type of the root volume (e.g. gp3, io2) for machines in the NodePool")
	cmd.Flags().Int64Var(&platformOpts.RootVolumeIOPS, "root-volume-iops", platformOpts.RootVolumeIOPS, "The iops of the root volume for machines in the NodePool")
	cmd.Flags().Int64Var(&platformOpts.RootVolumeSize, "root-volume-size", platformOpts.RootVolumeSize, "The size of the root volume (min: 8) for machines in the NodePool")
	cmd.Flags().StringVar(&platformOpts.PullSecretFile, "pull-secret", platformOpts.PullSecretFile, "Path to a pull secret")
	cmd.Flags().StringVar(&platformOpts.AWSCredentialsFile, "aws-creds", platformOpts.AWSCredentialsFile, "File with AWS credentials")

	cmd.MarkFlagRequired("aws-creds")
	cmd.MarkFlagRequired("pull-secret")

	cmd.RunE = coreOpts.CreateRunFunc(platformOpts)

	return cmd
}

func (o *AWSPlatformCreateOptions) UpdateNodePool(ctx context.Context, nodePool *hyperv1.NodePool, hcluster *hyperv1.HostedCluster, client crclient.Client) error {
	if len(o.InstanceProfile) == 0 {
		o.InstanceProfile = fmt.Sprintf("%s-worker", hcluster.Spec.InfraID)
	}
	if len(o.SubnetID) == 0 {
		if hcluster.Spec.Platform.AWS.CloudProviderConfig.Subnet.ID != nil {
			o.SubnetID = *hcluster.Spec.Platform.AWS.CloudProviderConfig.Subnet.ID
		} else {
			return fmt.Errorf("subnet ID was not specified and cannot be determined from HostedCluster")
		}
	}
	if len(o.SecurityGroupID) == 0 {
		nodePoolList := &hyperv1.NodePoolList{}
		if err := client.List(ctx, nodePoolList, &crclient.ListOptions{Namespace: hcluster.Namespace}); err != nil {
			return fmt.Errorf("security group ID was not specified and cannot be determined from default nodepool: %v", err)
		}
		var defaultNodePool *hyperv1.NodePool
		for i, nodePool := range nodePoolList.Items {
			if nodePool.Spec.ClusterName == hcluster.Name {
				defaultNodePool = &nodePoolList.Items[i]
				break
			}
		}
		if defaultNodePool == nil {
			return fmt.Errorf("--securitygroup-id flag is required when there are no existing nodepools")
		}
		if defaultNodePool.Spec.Platform.AWS == nil || len(defaultNodePool.Spec.Platform.AWS.SecurityGroups) == 0 ||
			defaultNodePool.Spec.Platform.AWS.SecurityGroups[0].ID == nil {
			return fmt.Errorf("security group ID was not specified and cannot be determined from default nodepool")
		}
		o.SecurityGroupID = *defaultNodePool.Spec.Platform.AWS.SecurityGroups[0].ID
	}
	nodePool.Spec.Platform.AWS = &hyperv1.AWSNodePoolPlatform{
		InstanceType:    o.InstanceType,
		InstanceProfile: o.InstanceProfile,
		Subnet: &hyperv1.AWSResourceReference{
			ID: &o.SubnetID,
		},
		SecurityGroups: []hyperv1.AWSResourceReference{
			{
				ID: &o.SecurityGroupID,
			},
		},
		RootVolume: &hyperv1.Volume{
			Type: o.RootVolumeType,
			Size: o.RootVolumeSize,
			IOPS: o.RootVolumeIOPS,
		},
	}

	pullSecretBytes, err := os.ReadFile(o.PullSecretFile)
	if err != nil {
		return fmt.Errorf("cannot read pull secret file %s: %w", o.PullSecretFile, err)
	}

	riprovider := &releaseinfo.RegistryClientProvider{}
	releaseImage, err := releaseinfo.Provider.Lookup(riprovider, ctx, nodePool.Spec.Release.Image, pullSecretBytes)

	if err != nil {
		return fmt.Errorf("failed to pull the instance architecture type, %s: %v", o.InstanceType, err)
	}

	if err != nil {
		return fmt.Errorf("failed to pull the instance architecture type, %s: %v", o.InstanceType, err)
	} else {
		releaseImageArch, err := getInstanceTypeArch(o.InstanceType, hcluster.Spec.Platform.AWS.Region, releaseImage)

		if err != nil {
			return fmt.Errorf("failed to pull the instance architecture type, %s: %v", o.InstanceType, err)
		}

		regionData, hasRegionData := releaseImageArch.Images.AWS.Regions[hcluster.Spec.Platform.AWS.Region]
		if !hasRegionData {
			return fmt.Errorf("couldn't find AWS image for region %q", hcluster.Spec.Platform.AWS.Region)
		}
		if len(regionData.Image) == 0 {
			return fmt.Errorf("release image metadata has no image for region %q", hcluster.Spec.Platform.AWS.Region)
		}

		nodePool.Spec.Platform.AWS.AMI = regionData.Image
	}
	return nil
}

func (o *AWSPlatformCreateOptions) Type() hyperv1.PlatformType {
	return hyperv1.AWSPlatform
}

func getInstanceTypeArch(instanceType string, region string, releaseImage *releaseinfo.ReleaseImage) (arch releaseinfo.CoreOSArchitecture, err error) {
	mySession := session.Must(session.NewSession())
	svc := ec2.New(mySession, aws.NewConfig().WithRegion(region))
	input := &ec2.DescribeInstanceTypesInput{
		InstanceTypes: []*string{aws.String(instanceType)},
	}

	result, err := svc.DescribeInstanceTypes(input)
	if err != nil {
		return releaseinfo.CoreOSArchitecture{}, fmt.Errorf("error occurred trying to find instance type, %s, in region, %s: %s", instanceType, region, err.Error())
	}

	var archPtr []*string = nil
	instanceArch := ""
	if result != nil && result.InstanceTypes != nil {
		archPtr = result.InstanceTypes[0].ProcessorInfo.SupportedArchitectures
	} else {
		return releaseinfo.CoreOSArchitecture{}, fmt.Errorf("could not find instance type in region, %s, for instance type, %q", region, instanceType)
	}

	if archPtr != nil {
		instanceArch = *archPtr[0]

		if instanceArch == "arm64" {
			instanceArch = "aarch64"
		}
	} else {
		return releaseinfo.CoreOSArchitecture{}, fmt.Errorf("couldn't find architecture type for instance type %s", instanceType)
	}

	arch, foundArch := releaseImage.StreamMetadata.Architectures[instanceArch]
	if !foundArch {
		return releaseinfo.CoreOSArchitecture{}, fmt.Errorf("couldn't find OS metadata for architecture %q", instanceArch)
	}

	return arch, nil
}
