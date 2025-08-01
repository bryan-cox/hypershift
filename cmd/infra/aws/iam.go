package aws

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/go-logr/logr"
)

const (
	ROSAWorkerRoleNameSuffix = "ROSA-Worker-Role"
)

type policyBinding struct {
	name                 string
	serviceAccounts      []string
	policy               string
	allowAssumeRole      bool
	rosaManagedPolicyARN string
}

type sharedVPCPolicyBinding struct {
	name         string
	policy       string
	allowedRoles []string
}

const allowAssumeRolePolicy = `{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": "sts:AssumeRole",
			"Resource": "*"
        }
    ]
}`

var (
	imageRegistryPermPolicy = policyBinding{
		name: "openshift-image-registry",
		serviceAccounts: []string{
			"system:serviceaccount:openshift-image-registry:cluster-image-registry-operator",
			"system:serviceaccount:openshift-image-registry:registry",
		},
		policy: `{
	"Version": "2012-10-17",
	"Statement": [
		{
			"Effect": "Allow",
			"Action": [
				"s3:CreateBucket",
				"s3:DeleteBucket",
				"s3:PutBucketTagging",
				"s3:GetBucketTagging",
				"s3:PutBucketPublicAccessBlock",
				"s3:GetBucketPublicAccessBlock",
				"s3:PutEncryptionConfiguration",
				"s3:GetEncryptionConfiguration",
				"s3:PutLifecycleConfiguration",
				"s3:GetLifecycleConfiguration",
				"s3:GetBucketLocation",
				"s3:ListBucket",
				"s3:GetObject",
				"s3:PutObject",
				"s3:DeleteObject",
				"s3:ListBucketMultipartUploads",
				"s3:AbortMultipartUpload",
				"s3:ListMultipartUploadParts"
			],
			"Resource": "*"
		}
	]
}`,
		rosaManagedPolicyARN: "arn:aws:iam::aws:policy/service-role/ROSAImageRegistryOperatorPolicy",
	}

	awsEBSCSIPermPolicy = policyBinding{
		name:            "aws-ebs-csi-driver-controller",
		serviceAccounts: []string{"system:serviceaccount:openshift-cluster-csi-drivers:aws-ebs-csi-driver-controller-sa"},
		policy: `{
	"Version": "2012-10-17",
	"Statement": [
		{
			"Effect": "Allow",
			"Action": [
				"ec2:AttachVolume",
				"ec2:CreateSnapshot",
				"ec2:CreateTags",
				"ec2:CreateVolume",
				"ec2:DeleteSnapshot",
				"ec2:DeleteTags",
				"ec2:DeleteVolume",
				"ec2:DescribeInstances",
				"ec2:DescribeSnapshots",
				"ec2:DescribeTags",
				"ec2:DescribeVolumes",
				"ec2:DescribeVolumesModifications",
				"ec2:DetachVolume",
				"ec2:ModifyVolume"
			],
			"Resource": "*"
		},
		{
			"Effect": "Allow",
			"Action": [
				"kms:Decrypt",
				"kms:Encrypt",
				"kms:GenerateDataKey",
				"kms:GenerateDataKeyWithoutPlainText",
				"kms:DescribeKey"
			],
			"Resource": "*"
		},
        {
            "Effect": "Allow",
            "Action": [
                "kms:RevokeGrant",
                "kms:CreateGrant",
                "kms:ListGrants"
            ],
            "Resource": "*",
            "Condition": {
                "Bool": {
                    "kms:GrantIsForAWSResource": true
                }
            }
        }
	]
}`,
		rosaManagedPolicyARN: "arn:aws:iam::aws:policy/service-role/ROSAAmazonEBSCSIDriverOperatorPolicy",
	}

	kubeControllerPolicy = policyBinding{
		name:            "cloud-controller",
		serviceAccounts: []string{"system:serviceaccount:kube-system:kube-controller-manager"},
		policy: `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "autoscaling:DescribeAutoScalingGroups",
        "autoscaling:DescribeLaunchConfigurations",
        "autoscaling:DescribeTags",
        "ec2:DescribeAvailabilityZones",
        "ec2:DescribeInstances",
        "ec2:DescribeImages",
        "ec2:DescribeRegions",
        "ec2:DescribeRouteTables",
        "ec2:DescribeSecurityGroups",
        "ec2:DescribeSubnets",
        "ec2:DescribeVolumes",
        "ec2:CreateSecurityGroup",
        "ec2:CreateTags",
        "ec2:CreateVolume",
        "ec2:ModifyInstanceAttribute",
        "ec2:ModifyVolume",
        "ec2:AttachVolume",
        "ec2:AuthorizeSecurityGroupIngress",
        "ec2:CreateRoute",
        "ec2:DeleteRoute",
        "ec2:DeleteSecurityGroup",
        "ec2:DeleteVolume",
        "ec2:DetachVolume",
        "ec2:RevokeSecurityGroupIngress",
        "ec2:DescribeVpcs",
        "elasticloadbalancing:AddTags",
        "elasticloadbalancing:AttachLoadBalancerToSubnets",
        "elasticloadbalancing:ApplySecurityGroupsToLoadBalancer",
        "elasticloadbalancing:CreateLoadBalancer",
        "elasticloadbalancing:CreateLoadBalancerPolicy",
        "elasticloadbalancing:CreateLoadBalancerListeners",
        "elasticloadbalancing:ConfigureHealthCheck",
        "elasticloadbalancing:DeleteLoadBalancer",
        "elasticloadbalancing:DeleteLoadBalancerListeners",
        "elasticloadbalancing:DescribeLoadBalancers",
        "elasticloadbalancing:DescribeLoadBalancerAttributes",
        "elasticloadbalancing:DetachLoadBalancerFromSubnets",
        "elasticloadbalancing:DeregisterInstancesFromLoadBalancer",
        "elasticloadbalancing:ModifyLoadBalancerAttributes",
        "elasticloadbalancing:RegisterInstancesWithLoadBalancer",
        "elasticloadbalancing:SetLoadBalancerPoliciesForBackendServer",
        "elasticloadbalancing:AddTags",
        "elasticloadbalancing:CreateListener",
        "elasticloadbalancing:CreateTargetGroup",
        "elasticloadbalancing:DeleteListener",
        "elasticloadbalancing:DeleteTargetGroup",
        "elasticloadbalancing:DeregisterTargets",
        "elasticloadbalancing:DescribeListeners",
        "elasticloadbalancing:DescribeLoadBalancerPolicies",
        "elasticloadbalancing:DescribeTargetGroups",
        "elasticloadbalancing:DescribeTargetHealth",
        "elasticloadbalancing:ModifyListener",
        "elasticloadbalancing:ModifyTargetGroup",
        "elasticloadbalancing:RegisterTargets",
        "elasticloadbalancing:SetLoadBalancerPoliciesOfListener",
        "iam:CreateServiceLinkedRole",
        "kms:DescribeKey"
      ],
      "Resource": [
        "*"
      ],
      "Effect": "Allow"
    }
  ]
}`,
		rosaManagedPolicyARN: "arn:aws:iam::aws:policy/service-role/ROSAKubeControllerPolicy",
	}

	//   {
	// 	"Action": [
	// 	  "*"
	// 	],
	// 	"Resource": [
	// 	  "*"
	// 	],
	// 	"Effect": "Allow"
	//   }
	karpenterPolicy = policyBinding{
		name:            "karpenter",
		serviceAccounts: []string{"system:serviceaccount:kube-system:karpenter"},
		policy: `{
			"Version": "2012-10-17",
			"Statement": [
			  {
				"Sid": "AllowScopedEC2InstanceAccessActions",
				"Effect": "Allow",
				"Resource": [
					"arn:*:ec2:*::image/*",
					"arn:*:ec2:*::snapshot/*",
					"arn:*:ec2:*:*:security-group/*",
					"arn:*:ec2:*:*:subnet/*"
				],
				"Action": [
					"ec2:RunInstances",
					"ec2:CreateFleet"
				]
			  },
			  {
				"Sid": "AllowScopedEC2LaunchTemplateAccessActions",
				"Effect": "Allow",
				"Resource": "arn:*:ec2:*:*:launch-template/*",
				"Action": [
					"ec2:RunInstances",
					"ec2:CreateFleet"
				]
			  },
			  {
				"Sid": "AllowScopedEC2InstanceActionsWithTags",
				"Effect": "Allow",
				"Resource": [
					"arn:*:ec2:*:*:fleet/*",
					"arn:*:ec2:*:*:instance/*",
					"arn:*:ec2:*:*:volume/*",
					"arn:*:ec2:*:*:network-interface/*",
					"arn:*:ec2:*:*:launch-template/*",
					"arn:*:ec2:*:*:spot-instances-request/*"
				],
				"Action": [
					"ec2:RunInstances",
					"ec2:CreateFleet",
					"ec2:CreateLaunchTemplate"
				],
				"Condition": {
					"StringLike": {
					"aws:RequestTag/karpenter.sh/nodepool": "*"
					}
				}
			  },
			  {
				"Sid": "AllowScopedResourceCreationTagging",
				"Effect": "Allow",
				"Resource": [
					"arn:*:ec2:*:*:fleet/*",
					"arn:*:ec2:*:*:instance/*",
					"arn:*:ec2:*:*:volume/*",
					"arn:*:ec2:*:*:network-interface/*",
					"arn:*:ec2:*:*:launch-template/*",
					"arn:*:ec2:*:*:spot-instances-request/*"
				],
				"Action": "ec2:CreateTags",
				"Condition": {
					"StringEquals": {
					"ec2:CreateAction": [
						"RunInstances",
						"CreateFleet",
						"CreateLaunchTemplate"
					]
					},
					"StringLike": {
					"aws:RequestTag/karpenter.sh/nodepool": "*"
					}
				}
			  },
			  {
				"Sid": "AllowScopedResourceTagging",
				"Effect": "Allow",
				"Resource": "arn:*:ec2:*:*:instance/*",
				"Action": "ec2:CreateTags",
				"Condition": {
					"StringLike": {
					"aws:ResourceTag/karpenter.sh/nodepool": "*"
					}
				}
			  },
			  {
				"Sid": "AllowScopedDeletion",
				"Effect": "Allow",
				"Resource": [
					"arn:*:ec2:*:*:instance/*",
					"arn:*:ec2:*:*:launch-template/*"
				],
				"Action": [
					"ec2:TerminateInstances",
					"ec2:DeleteLaunchTemplate"
				],
				"Condition": {
					"StringLike": {
					"aws:ResourceTag/karpenter.sh/nodepool": "*"
					}
				}
			  },
			{
				"Sid": "AllowRegionalReadActions",
				"Effect": "Allow",
				"Resource": "*",
				"Action": [
					"ec2:DescribeImages",
					"ec2:DescribeInstances",
					"ec2:DescribeInstanceTypeOfferings",
					"ec2:DescribeInstanceTypes",
					"ec2:DescribeLaunchTemplates",
					"ec2:DescribeSecurityGroups",
					"ec2:DescribeSpotPriceHistory",
					"ec2:DescribeSubnets"
				]
			  },
			  {
				"Sid": "AllowSSMReadActions",
				"Effect": "Allow",
				"Resource": "arn:*:ssm:*::parameter/aws/service/*",
				"Action": "ssm:GetParameter"
			  },
			  {
				"Sid": "AllowPricingReadActions",
				"Effect": "Allow",
				"Resource": "*",
				"Action": "pricing:GetProducts"
			  },
			  {
				"Sid": "AllowInterruptionQueueActions",
				"Effect": "Allow",
				"Resource": "*",
				"Action": [
					"sqs:DeleteMessage",
					"sqs:GetQueueUrl",
					"sqs:ReceiveMessage"
				]
			  },
			  {
				"Sid": "AllowPassingInstanceRole",
				"Effect": "Allow",
				"Resource": "arn:*:iam::*:role/*",
				"Action": "iam:PassRole",
				"Condition": {
					"StringEquals": {
					"iam:PassedToService": [
						"ec2.amazonaws.com",
						"ec2.amazonaws.com.cn"
					]
					}
				}
			  },
			  {
				"Sid": "AllowScopedInstanceProfileCreationActions",
				"Effect": "Allow",
				"Resource": "arn:*:iam::*:instance-profile/*",
				"Action": [
					"iam:CreateInstanceProfile"
				],
				"Condition": {
					"StringLike": {
					"aws:RequestTag/karpenter.k8s.aws/ec2nodeclass": "*"
					}
				}
			  },
			{
				"Sid": "AllowScopedInstanceProfileTagActions",
				"Effect": "Allow",
				"Resource": "arn:*:iam::*:instance-profile/*",
				"Action": [
					"iam:TagInstanceProfile"
				],
				"Condition": {
					"StringLike": {
					"aws:ResourceTag/karpenter.k8s.aws/ec2nodeclass": "*",
					"aws:RequestTag/karpenter.k8s.aws/ec2nodeclass": "*"
					}
				}
			  },
			  {
				"Sid": "AllowScopedInstanceProfileActions",
				"Effect": "Allow",
				"Resource": "arn:*:iam::*:instance-profile/*",
				"Action": [
					"iam:AddRoleToInstanceProfile",
					"iam:RemoveRoleFromInstanceProfile",
					"iam:DeleteInstanceProfile"
				],
				"Condition": {
					"StringLike": {
					"aws:ResourceTag/karpenter.k8s.aws/ec2nodeclass": "*"
					}
				}
			  },
			  {
				"Sid": "AllowInstanceProfileReadActions",
				"Effect": "Allow",
				"Resource": "arn:*:iam::*:instance-profile/*",
				"Action": "iam:GetInstanceProfile"
			  }
			]
		  }`,
	}

	nodePoolPolicy = policyBinding{
		name:            "node-pool",
		serviceAccounts: []string{"system:serviceaccount:kube-system:capa-controller-manager"},
		policy: `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "ec2:AssociateRouteTable",
        "ec2:AttachInternetGateway",
        "ec2:AuthorizeSecurityGroupIngress",
        "ec2:CreateInternetGateway",
        "ec2:CreateNatGateway",
        "ec2:CreateRoute",
        "ec2:CreateRouteTable",
        "ec2:CreateSecurityGroup",
        "ec2:CreateSubnet",
        "ec2:CreateTags",
        "ec2:DeleteInternetGateway",
        "ec2:DeleteNatGateway",
        "ec2:DeleteRouteTable",
        "ec2:DeleteSecurityGroup",
        "ec2:DeleteSubnet",
        "ec2:DeleteTags",
        "ec2:DescribeAccountAttributes",
        "ec2:DescribeAddresses",
        "ec2:DescribeAvailabilityZones",
        "ec2:DescribeImages",
        "ec2:DescribeInstances",
        "ec2:DescribeInternetGateways",
        "ec2:DescribeNatGateways",
        "ec2:DescribeNetworkInterfaces",
        "ec2:DescribeNetworkInterfaceAttribute",
        "ec2:DescribeRouteTables",
        "ec2:DescribeSecurityGroups",
        "ec2:DescribeSubnets",
        "ec2:DescribeVpcs",
        "ec2:DescribeDhcpOptions",
        "ec2:DescribeVpcAttribute",
        "ec2:DescribeVolumes",
        "ec2:DetachInternetGateway",
        "ec2:DisassociateRouteTable",
        "ec2:DisassociateAddress",
        "ec2:ModifyInstanceAttribute",
        "ec2:ModifyNetworkInterfaceAttribute",
        "ec2:ModifySubnetAttribute",
        "ec2:RevokeSecurityGroupIngress",
        "ec2:RunInstances",
        "ec2:TerminateInstances",
        "tag:GetResources",
        "ec2:CreateLaunchTemplate",
        "ec2:CreateLaunchTemplateVersion",
        "ec2:DescribeLaunchTemplates",
        "ec2:DescribeLaunchTemplateVersions",
        "ec2:DeleteLaunchTemplate",
        "ec2:DeleteLaunchTemplateVersions"
      ],
      "Resource": [
        "*"
      ],
      "Effect": "Allow"
    },
    {
      "Condition": {
        "StringLike": {
          "iam:AWSServiceName": "elasticloadbalancing.amazonaws.com"
        }
      },
      "Action": [
        "iam:CreateServiceLinkedRole"
      ],
      "Resource": [
        "arn:*:iam::*:role/aws-service-role/elasticloadbalancing.amazonaws.com/AWSServiceRoleForElasticLoadBalancing"
      ],
      "Effect": "Allow"
    },
    {
      "Action": [
        "iam:PassRole"
      ],
      "Resource": [
        "arn:*:iam::*:role/*-worker-role"
      ],
      "Effect": "Allow"
    },
	{
		"Effect": "Allow",
		"Action": [
			"kms:Decrypt",
			"kms:ReEncrypt",
			"kms:GenerateDataKeyWithoutPlainText",
			"kms:DescribeKey"
		],
		"Resource": "*"
	},
	{
		"Effect": "Allow",
		"Action": [
			"kms:CreateGrant"
		],
		"Resource": "*",
		"Condition": {
			"Bool": {
				"kms:GrantIsForAWSResource": true
			}
		}
	}
  ]
}`,
		rosaManagedPolicyARN: "arn:aws:iam::aws:policy/service-role/ROSANodePoolManagementPolicy",
	}

	cloudNetworkConfigControllerPolicy = policyBinding{
		name:            "cloud-network-config-controller",
		serviceAccounts: []string{"system:serviceaccount:openshift-cloud-network-config-controller:cloud-network-config-controller"},
		policy: `{
	"Version": "2012-10-17",
	"Statement": [
		{
			"Effect": "Allow",
			"Action": [
				"ec2:DescribeInstances",
        "ec2:DescribeInstanceStatus",
        "ec2:DescribeInstanceTypes",
        "ec2:UnassignPrivateIpAddresses",
        "ec2:AssignPrivateIpAddresses",
        "ec2:UnassignIpv6Addresses",
        "ec2:AssignIpv6Addresses",
        "ec2:DescribeSubnets",
        "ec2:DescribeNetworkInterfaces"
			],
			"Resource": "*"
		}
	]
}`,
		rosaManagedPolicyARN: "arn:aws:iam::aws:policy/service-role/ROSACloudNetworkConfigOperatorPolicy",
	}
)

func ingressPermPolicy(publicZone, privateZone string, sharedVPC bool) policyBinding {
	publicZone = ensureHostedZonePrefix(publicZone)
	privateZone = ensureHostedZonePrefix(privateZone)

	var policy string
	if sharedVPC {
		policy = fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Action": [
						"elasticloadbalancing:DescribeLoadBalancers",
						"tag:GetResources",
						"route53:ListHostedZones"
					],
					"Resource": "*"
				},
				{
					"Effect": "Allow",
					"Action": [
						"route53:ChangeResourceRecordSets"
					],
					"Resource": [
						"arn:aws:route53:::%s"
					]
				}
			]
		}`, publicZone)
	} else {
		policy = fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Action": [
						"elasticloadbalancing:DescribeLoadBalancers",
						"tag:GetResources",
						"route53:ListHostedZones"
					],
					"Resource": "*"
				},
				{
					"Effect": "Allow",
					"Action": [
						"route53:ChangeResourceRecordSets"
					],
					"Resource": [
						"arn:aws:route53:::%s",
						"arn:aws:route53:::%s"
					]
				}
			]
		}`, publicZone, privateZone)
	}

	return policyBinding{
		name:                 "openshift-ingress",
		serviceAccounts:      []string{"system:serviceaccount:openshift-ingress-operator:ingress-operator"},
		policy:               policy,
		allowAssumeRole:      sharedVPC,
		rosaManagedPolicyARN: "arn:aws:iam::aws:policy/service-role/ROSAIngressOperatorPolicy",
	}
}

func controlPlaneOperatorPolicy(hostedZone string, sharedVPC bool) policyBinding {
	hostedZone = ensureHostedZonePrefix(hostedZone)
	var policy string
	if sharedVPC {
		policy = `{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Action": [
					    "ec2:DescribeVpcEndpoints",
						"ec2:CreateTags",
						"ec2:CreateSecurityGroup",
						"ec2:AuthorizeSecurityGroupIngress",
						"ec2:AuthorizeSecurityGroupEgress",
						"ec2:DeleteSecurityGroup",
						"ec2:RevokeSecurityGroupIngress",
						"ec2:RevokeSecurityGroupEgress",
						"ec2:DescribeSecurityGroups",
						"ec2:DescribeVpcs"
					],
					"Resource": "*"
				}
			]
		}`
	} else {
		policy = fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Action": [
						"ec2:CreateVpcEndpoint",
						"ec2:DescribeVpcEndpoints",
						"ec2:ModifyVpcEndpoint",
						"ec2:DeleteVpcEndpoints",
						"ec2:CreateTags",
						"route53:ListHostedZones",
						"ec2:CreateSecurityGroup",
						"ec2:AuthorizeSecurityGroupIngress",
						"ec2:AuthorizeSecurityGroupEgress",
						"ec2:DeleteSecurityGroup",
						"ec2:RevokeSecurityGroupIngress",
						"ec2:RevokeSecurityGroupEgress",
						"ec2:DescribeSecurityGroups",
						"ec2:DescribeVpcs"
					],
					"Resource": "*"
				},
				{
					"Effect": "Allow",
					"Action": [
						"route53:ChangeResourceRecordSets",
						"route53:ListResourceRecordSets"
					],
					"Resource": "arn:aws:route53:::%s"
				}
			]
		}`, hostedZone)
	}
	return policyBinding{
		name:                 "control-plane-operator",
		serviceAccounts:      []string{"system:serviceaccount:kube-system:control-plane-operator"},
		policy:               policy,
		allowAssumeRole:      sharedVPC,
		rosaManagedPolicyARN: "arn:aws:iam::aws:policy/service-role/ROSAControlPlaneOperatorPolicy",
	}
}

func sharedVPCRoute53Role(allowedRoles []string) sharedVPCPolicyBinding {
	policy := `{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "route53:ListHostedZones",
                "route53:ListHostedZonesByName",
                "route53:ChangeTagsForResource",
                "route53:GetAccountLimit",
                "route53:GetChange",
                "route53:GetHostedZone",
                "route53:ListTagsForResource",
                "route53:UpdateHostedZoneComment",
                "tag:GetResources",
                "tag:UntagResources",
				"route53:ChangeResourceRecordSets",
				"route53:ListResourceRecordSets"
            ],
            "Resource": "*"
        }
    ]
}`
	return sharedVPCPolicyBinding{
		name:         "shared-vpc-route53",
		policy:       policy,
		allowedRoles: allowedRoles,
	}
}

func sharedVPCEndpointRole(controlPlaneRoleARN string) sharedVPCPolicyBinding {
	policy := `{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Action": [
						"ec2:CreateVpcEndpoint",
						"ec2:DescribeVpcEndpoints",
						"ec2:ModifyVpcEndpoint",
						"ec2:DeleteVpcEndpoints",
						"ec2:CreateTags",
						"ec2:CreateSecurityGroup",
						"ec2:AuthorizeSecurityGroupIngress",
						"ec2:AuthorizeSecurityGroupEgress",
						"ec2:DeleteSecurityGroup",
						"ec2:RevokeSecurityGroupIngress",
						"ec2:RevokeSecurityGroupEgress",
						"ec2:DescribeSecurityGroups",
						"ec2:DescribeVpcs"
					],
					"Resource": "*"
				}
			]
		}`
	return sharedVPCPolicyBinding{
		name:         "shared-vpc-endpoint",
		policy:       policy,
		allowedRoles: []string{controlPlaneRoleARN},
	}
}

func kmsProviderPolicy(kmsKeyARN string) policyBinding {
	return policyBinding{
		name:            "kms-provider",
		serviceAccounts: []string{"system:serviceaccount:kube-system:kms-provider"},
		policy: fmt.Sprintf(`{
	"Version": "2012-10-17",
	"Statement": [
    	{
			"Effect": "Allow",
			"Action": [
				"kms:Encrypt",
				"kms:Decrypt",
				"kms:ReEncrypt*",
				"kms:GenerateDataKey*",
				"kms:DescribeKey"
			],
			"Resource": %q
		}
	]
}`, kmsKeyARN),
		rosaManagedPolicyARN: "arn:aws:iam::aws:policy/service-role/ROSAKMSProviderPolicy",
	}
}

func ensureHostedZonePrefix(hostedZone string) string {
	if !strings.HasPrefix(hostedZone, "hostedzone/") {
		hostedZone = "hostedzone/" + hostedZone
	}
	return hostedZone
}

func DefaultProfileName(infraID string) string {
	return infraID + "-worker"
}

// inputs: none
// outputs rsa keypair
func (o *CreateIAMOptions) CreateOIDCResources(ctx context.Context, iamClient iamiface.IAMAPI, logger logr.Logger, sharedVPC bool) (*CreateIAMOutput, error) {
	var providerName string
	var providerARN string
	if o.IssuerURL == "" {
		o.IssuerURL = oidcDiscoveryURL(o.OIDCStorageProviderS3BucketName, o.OIDCStorageProviderS3Region, o.InfraID)
		logger.Info("Detected Issuer URL", "issuer", o.IssuerURL)

		providerName = strings.TrimPrefix(o.IssuerURL, "https://")

		// Create the OIDC provider
		arn, err := o.CreateOIDCProvider(iamClient, logger)
		if err != nil {
			return nil, err
		}
		providerARN = arn
	} else {
		providerName = strings.TrimPrefix(o.IssuerURL, "https://")
		oidcProviderList, err := iamClient.ListOpenIDConnectProviders(&iam.ListOpenIDConnectProvidersInput{})
		if err != nil {
			return nil, err
		}

		for _, provider := range oidcProviderList.OpenIDConnectProviderList {
			if strings.Contains(*provider.Arn, providerName) {
				providerARN = *provider.Arn
				break
			}
		}

		if providerARN == "" {
			return nil, fmt.Errorf("OIDC provider with issuer URL %s was not found", o.IssuerURL)
		}
	}

	output := &CreateIAMOutput{
		Region:    o.Region,
		InfraID:   o.InfraID,
		IssuerURL: o.IssuerURL,
	}

	// TODO: The policies and secrets for these roles can be extracted from the
	// release payload, avoiding this current hardcoding.
	bindings := map[*string]policyBinding{
		&output.Roles.IngressARN:              ingressPermPolicy(o.PublicZoneID, o.PrivateZoneID, sharedVPC),
		&output.Roles.ImageRegistryARN:        imageRegistryPermPolicy,
		&output.Roles.StorageARN:              awsEBSCSIPermPolicy,
		&output.Roles.KubeCloudControllerARN:  kubeControllerPolicy,
		&output.Roles.NodePoolManagementARN:   nodePoolPolicy,
		&output.Roles.ControlPlaneOperatorARN: controlPlaneOperatorPolicy(o.LocalZoneID, sharedVPC),
		&output.Roles.NetworkARN:              cloudNetworkConfigControllerPolicy,
	}

	if o.CreateKarpenterRoleARN {
		bindings[&output.KarpenterRoleARN] = karpenterPolicy

	}

	if len(o.KMSKeyARN) > 0 {
		bindings[&output.KMSProviderRoleARN] = kmsProviderPolicy(o.KMSKeyARN)
	}

	for into, binding := range bindings {
		trustPolicy := oidcTrustPolicy(providerARN, providerName, binding.serviceAccounts...)
		arn, err := o.CreateOIDCRole(ctx, iamClient, binding, trustPolicy, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create OIDC Role %q: with trust policy %s and permission policy %s: %v", binding.name, trustPolicy, binding.policy, err)
		}
		*into = arn
	}

	if o.UseROSAManagedPolicies {
		// When using ROSA managed policies, some permissions are scoped to specific resource names,
		// which might not match the generated resources by the CLI which differs based on user input.
		//
		// Currently ingress operator managed policy is scoped to specific DNS domain names,
		// which is why we need to create an additional policy on top to allow it to manage routes with the DNS basedomain specified by the CLI user.
		policy := fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Action": [
						"route53:ChangeResourceRecordSets"
					],
					"Resource": "*",
					"Condition": {
						"ForAllValues:StringLike": {
							"route53:ChangeResourceRecordSetsNormalizedRecordNames": [
								"*.%s"
							]
						}
					}
				}
			]
		}`, o.BaseDomain)

		ingressRoleName := output.Roles.IngressARN[strings.LastIndex(output.Roles.IngressARN, "/")+1:]
		if _, err := iamClient.PutRolePolicyWithContext(ctx, &iam.PutRolePolicyInput{
			PolicyName:     aws.String(ingressRoleName),
			RoleName:       aws.String(ingressRoleName),
			PolicyDocument: aws.String(policy),
		}); err != nil {
			return nil, fmt.Errorf("failed to create role policy %q: with permission policy %s: %v", ingressRoleName, policy, err)
		}
	}

	return output, nil
}

func (o *CreateIAMOptions) CreateOIDCProvider(iamClient iamiface.IAMAPI, logger logr.Logger) (string, error) {
	oidcProviderList, err := iamClient.ListOpenIDConnectProviders(&iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return "", err
	}

	providerName := strings.TrimPrefix(o.IssuerURL, "https://")
	for _, provider := range oidcProviderList.OpenIDConnectProviderList {
		if strings.Contains(*provider.Arn, providerName) {
			_, err := iamClient.DeleteOpenIDConnectProvider(&iam.DeleteOpenIDConnectProviderInput{
				OpenIDConnectProviderArn: provider.Arn,
			})
			if err != nil {
				logger.Error(err, "Failed to remove existing OIDC provider", "provider", *provider.Arn)
				return "", err
			}
			logger.Info("Removing existing OIDC provider", "provider", *provider.Arn)
			break
		}
	}

	oidcOutput, err := iamClient.CreateOpenIDConnectProvider(&iam.CreateOpenIDConnectProviderInput{
		ClientIDList: []*string{
			aws.String("openshift"),
			aws.String("sts.amazonaws.com"),
		},
		// The AWS console mentions that this will be ignored for S3 buckets but creation fails if we don't
		// pass a thumbprint.
		ThumbprintList: []*string{
			aws.String("A9D53002E97E00E043244F3D170D6F4C414104FD"), // root CA thumbprint for s3 (DigiCert)
		},
		Url:  aws.String(o.IssuerURL),
		Tags: o.additionalIAMTags,
	})
	if err != nil {
		return "", err
	}

	providerARN := *oidcOutput.OpenIDConnectProviderArn
	logger.Info("Created OIDC provider", "provider", providerARN)

	return providerARN, nil
}

// CreateOIDCRole create an IAM Role with a trust policy for the OIDC provider
func (o *CreateIAMOptions) CreateOIDCRole(ctx context.Context, client iamiface.IAMAPI, binding policyBinding, trustPolicy string, logger logr.Logger) (string, error) {
	createIAMRoleOpts := CreateIAMRoleOptions{
		RoleName:          fmt.Sprintf("%s-%s", o.InfraID, binding.name),
		TrustPolicy:       trustPolicy,
		PermissionsPolicy: binding.policy,
		additionalIAMTags: o.additionalIAMTags,
		AllowAssume:       binding.allowAssumeRole,
	}

	if o.UseROSAManagedPolicies && binding.rosaManagedPolicyARN != "" {
		return createIAMRoleOpts.CreateRoleWithManagedPolicy(ctx, client, binding.rosaManagedPolicyARN, logger)
	}
	return createIAMRoleOpts.CreateRoleWithInlinePolicy(ctx, client, logger)
}

func (o *CreateIAMOptions) CreateWorkerInstanceProfile(client iamiface.IAMAPI, profileName string, logger logr.Logger) error {
	const (
		assumeRolePolicy = `{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Action": "sts:AssumeRole",
            "Principal": {
                "Service": "ec2.amazonaws.com"
            },
            "Effect": "Allow",
            "Sid": ""
        }
    ]
}`
		workerPolicy = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "ec2:DescribeRegions"
      ],
      "Resource": "*"
    }
  ]
}`
	)

	roleName := fmt.Sprintf("%s-role", profileName)
	if o.UseROSAManagedPolicies {
		roleName = fmt.Sprintf("%s-%s", profileName, ROSAWorkerRoleNameSuffix)
	}

	role, err := existingRole(client, roleName)
	if err != nil {
		return err
	}
	if role == nil {
		_, err := client.CreateRole(&iam.CreateRoleInput{
			AssumeRolePolicyDocument: aws.String(assumeRolePolicy),
			Path:                     aws.String("/"),
			RoleName:                 aws.String(roleName),
			Tags:                     o.additionalIAMTags,
		})
		if err != nil {
			return fmt.Errorf("cannot create worker role: %w", err)
		}
		logger.Info("Created role", "name", roleName)
	} else {
		logger.Info("Found existing role", "name", roleName)
	}
	instanceProfile, err := existingInstanceProfile(client, profileName)
	if err != nil {
		return err
	}
	if instanceProfile == nil {
		result, err := client.CreateInstanceProfile(&iam.CreateInstanceProfileInput{
			InstanceProfileName: aws.String(profileName),
			Path:                aws.String("/"),
			Tags:                o.additionalIAMTags,
		})
		if err != nil {
			return fmt.Errorf("cannot create instance profile: %w", err)
		}
		instanceProfile = result.InstanceProfile
		logger.Info("Created instance profile", "name", profileName)
	} else {
		logger.Info("Found existing instance profile", "name", profileName)
	}
	hasRole := false
	for _, role := range instanceProfile.Roles {
		if aws.StringValue(role.RoleName) == roleName {
			hasRole = true
		}
	}
	if !hasRole {
		_, err = client.AddRoleToInstanceProfile(&iam.AddRoleToInstanceProfileInput{
			InstanceProfileName: aws.String(profileName),
			RoleName:            aws.String(roleName),
		})
		if err != nil {
			return fmt.Errorf("cannot add role to instance profile: %w", err)
		}
		logger.Info("Added role to instance profile", "role", roleName, "profile", profileName)
	}

	if o.UseROSAManagedPolicies {
		_, err = client.AttachRolePolicy(&iam.AttachRolePolicyInput{
			PolicyArn: aws.String("arn:aws:iam::aws:policy/service-role/ROSAWorkerInstancePolicy"),
			RoleName:  aws.String(roleName),
		})
		if err != nil {
			return fmt.Errorf("failed to attach managed policy to worker instance profile: %w", err)
		}
		return nil
	}

	rolePolicyName := fmt.Sprintf("%s-policy", profileName)
	hasPolicy, err := existingRolePolicy(client, roleName, rolePolicyName)
	if err != nil {
		return err
	}
	if !hasPolicy {
		_, err = client.PutRolePolicy(&iam.PutRolePolicyInput{
			PolicyName:     aws.String(rolePolicyName),
			PolicyDocument: aws.String(workerPolicy),
			RoleName:       aws.String(roleName),
		})
		if err != nil {
			return fmt.Errorf("cannot create profile policy: %w", err)
		}
		logger.Info("Created role policy", "name", rolePolicyName)
	}
	return nil
}

type CreateIAMRoleOptions struct {
	RoleName          string
	TrustPolicy       string
	PermissionsPolicy string
	AllowAssume       bool

	additionalIAMTags []*iam.Tag
}

func (o *CreateIAMRoleOptions) CreateRole(ctx context.Context, client iamiface.IAMAPI, logger logr.Logger) (string, error) {
	role, err := existingRole(client, o.RoleName)
	if err != nil {
		return "", err
	}

	if role != nil {
		logger.Info("Found existing role", "name", o.RoleName)
		return *role.Arn, nil
	}

	output, err := client.CreateRoleWithContext(ctx, &iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String(o.TrustPolicy),
		RoleName:                 aws.String(o.RoleName),
		Tags:                     o.additionalIAMTags,
	})
	if err != nil {
		return "", err
	}
	logger.Info("Created role", "name", o.RoleName)
	return *output.Role.Arn, nil
}

func (o *CreateIAMRoleOptions) CreateRoleWithInlinePolicy(ctx context.Context, client iamiface.IAMAPI, logger logr.Logger) (string, error) {
	arn, err := o.CreateRole(ctx, client, logger)
	if err != nil {
		return "", err
	}

	rolePolicyName := o.RoleName
	_, err = client.PutRolePolicyWithContext(ctx, &iam.PutRolePolicyInput{
		PolicyName:     aws.String(rolePolicyName),
		PolicyDocument: aws.String(o.PermissionsPolicy),
		RoleName:       aws.String(o.RoleName),
	})
	if err != nil {
		return "", err
	}

	logger.Info("Added/Updated role policy", "name", rolePolicyName)

	if o.AllowAssume {
		rolePolicyName = fmt.Sprintf("%s-assume", o.RoleName)
		_, err = client.PutRolePolicyWithContext(ctx, &iam.PutRolePolicyInput{
			PolicyName:     aws.String(rolePolicyName),
			PolicyDocument: aws.String(allowAssumeRolePolicy),
			RoleName:       aws.String(o.RoleName),
		})
		if err != nil {
			return "", err
		}
		logger.Info("Added/Updated role policy", "name", rolePolicyName)
	}

	return arn, nil
}

func (o *CreateIAMRoleOptions) CreateRoleWithManagedPolicy(ctx context.Context, client iamiface.IAMAPI, managedPolicyARN string, logger logr.Logger) (string, error) {
	arn, err := o.CreateRole(ctx, client, logger)
	if err != nil {
		return "", err
	}

	_, err = client.AttachRolePolicyWithContext(ctx, &iam.AttachRolePolicyInput{
		PolicyArn: aws.String(managedPolicyARN),
		RoleName:  aws.String(o.RoleName),
	})
	if err != nil {
		return "", err
	}
	logger.Info("Attached role policy", "arn", managedPolicyARN)

	if o.AllowAssume {
		rolePolicyName := fmt.Sprintf("%s-assume", o.RoleName)
		_, err = client.PutRolePolicyWithContext(ctx, &iam.PutRolePolicyInput{
			PolicyName:     aws.String(rolePolicyName),
			PolicyDocument: aws.String(allowAssumeRolePolicy),
			RoleName:       aws.String(o.RoleName),
		})
		if err != nil {
			return "", err
		}
		logger.Info("Added/Updated role policy", "name", rolePolicyName)
	}

	return arn, nil
}

func (o *CreateIAMOptions) CreateSharedVPCEndpointRole(ctx context.Context, iamClient iamiface.IAMAPI, logger logr.Logger, controlPlaneRole string) (string, error) {
	return o.createSharedVPCRole(ctx, iamClient, logger, sharedVPCEndpointRole(controlPlaneRole))
}

func (o *CreateIAMOptions) CreateSharedVPCRoute53Role(ctx context.Context, iamClient iamiface.IAMAPI, logger logr.Logger, ingressRole, controlPlaneRole string) (string, error) {
	return o.createSharedVPCRole(ctx, iamClient, logger, sharedVPCRoute53Role([]string{ingressRole, controlPlaneRole}))
}

func (o *CreateIAMOptions) createSharedVPCRole(ctx context.Context, iamClient iamiface.IAMAPI, logger logr.Logger, binding sharedVPCPolicyBinding) (string, error) {
	trustPolicy := sharedVPCRoleTrustPolicy(binding.allowedRoles)
	backoff := wait.Backoff{
		Steps:    10,
		Duration: 10 * time.Second,
		Factor:   1.0,
		Jitter:   0.1,
	}
	var arn string
	if err := retry.OnError(backoff, func(error) bool { return true }, func() error {
		createIAMRoleOpts := CreateIAMRoleOptions{
			RoleName:          fmt.Sprintf("%s-%s", o.InfraID, binding.name),
			TrustPolicy:       trustPolicy,
			PermissionsPolicy: binding.policy,
			additionalIAMTags: o.additionalIAMTags,
		}
		var err error
		arn, err = createIAMRoleOpts.CreateRoleWithInlinePolicy(ctx, iamClient, logger)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return "", err
	}
	return arn, nil
}

func existingRole(client iamiface.IAMAPI, roleName string) (*iam.Role, error) {
	result, err := client.GetRole(&iam.GetRoleInput{RoleName: aws.String(roleName)})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == iam.ErrCodeNoSuchEntityException {
				return nil, nil
			}
		}
		return nil, fmt.Errorf("cannot get existing role: %w", err)
	}
	return result.Role, nil
}

func existingInstanceProfile(client iamiface.IAMAPI, profileName string) (*iam.InstanceProfile, error) {
	result, err := client.GetInstanceProfile(&iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(profileName),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == iam.ErrCodeNoSuchEntityException {
				return nil, nil
			}
		}
		return nil, fmt.Errorf("cannot get existing instance profile: %w", err)
	}
	return result.InstanceProfile, nil
}

func existingRolePolicy(client iamiface.IAMAPI, roleName, policyName string) (bool, error) {
	result, err := client.GetRolePolicy(&iam.GetRolePolicyInput{
		RoleName:   aws.String(roleName),
		PolicyName: aws.String(policyName),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == iam.ErrCodeNoSuchEntityException {
				return false, nil
			}
		}
		return false, fmt.Errorf("cannot get existing role policy: %w", err)
	}

	return aws.StringValue(result.PolicyName) == policyName, nil
}

type oidcTrustPolicyParams struct {
	ProviderARN     string
	ProviderName    string
	ServiceAccounts string
}

const (
	oidcTrustPolicyTemplate = `{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": {
					"Federated": "{{ .ProviderARN }}"
				},
					"Action": "sts:AssumeRoleWithWebIdentity",
				"Condition": {
					"StringEquals": {
						"{{ .ProviderName }}:sub": {{ .ServiceAccounts }}
					}
				}
			}
		]
	}`
)

func oidcTrustPolicy(providerARN, providerName string, serviceAccounts ...string) string {
	params := oidcTrustPolicyParams{
		ProviderARN:  providerARN,
		ProviderName: providerName,
	}
	if len(serviceAccounts) == 1 {
		params.ServiceAccounts = fmt.Sprintf("%q", serviceAccounts[0])
	} else {
		sas := &bytes.Buffer{}
		fmt.Fprintf(sas, "[")
		for i, sa := range serviceAccounts {
			fmt.Fprintf(sas, "%q", sa)
			if i < len(serviceAccounts)-1 {
				fmt.Fprintf(sas, ", ")
			}
		}
		fmt.Fprintf(sas, "]")
		params.ServiceAccounts = sas.String()
	}

	tmpl, err := template.New("oidcTrustPolicy").Parse(oidcTrustPolicyTemplate)
	if err != nil {
		panic(fmt.Sprintf("programmer error, oidcTrustPolicyTemplate failed to parse: %v", err))
	}
	b := &bytes.Buffer{}
	if err = tmpl.Execute(b, params); err != nil {
		panic(fmt.Sprintf("failed to execute oidcTrustPolicyTemplate: %v", err))
	}
	return b.String()
}

func sharedVPCRoleTrustPolicy(trustedRoles []string) string {
	var allowedString string
	switch len(trustedRoles) {
	case 1:
		allowedString = fmt.Sprintf("%q", trustedRoles[0])
	case 2:
		allowedString = fmt.Sprintf("[ %q, %q ]", trustedRoles[0], trustedRoles[1])
	default:
		panic("not supported")
	}

	policy := `{
  "Version": "2012-10-17",
  "Statement": [
    {
	  "Sid": "Statement1",
	  "Effect": "Allow",
	  "Principal": {
	  	"AWS": %s
	  },
	  "Action": "sts:AssumeRole"
	}
  ]
}`
	return fmt.Sprintf(policy, allowedString)
}
