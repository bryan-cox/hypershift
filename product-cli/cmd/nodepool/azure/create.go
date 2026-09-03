package azure

import (
	hypershiftazure "github.com/openshift/hypershift/cmd/nodepool/azure"
	"github.com/openshift/hypershift/cmd/nodepool/core"
	"github.com/openshift/hypershift/cmd/util"

	"github.com/spf13/cobra"
)

func NewCreateCommand(coreOpts *core.CreateNodePoolOptions, clientProviders ...*util.ClientProvider) *cobra.Command {
	clientProvider := util.DefaultClientProvider()
	if len(clientProviders) > 0 && clientProviders[0] != nil {
		clientProvider = clientProviders[0]
	}
	platformOpts := hypershiftazure.DefaultOptions()
	cmd := &cobra.Command{
		Use:          "azure",
		Short:        "Creates basic functional NodePool resources for Azure platform",
		SilenceUsage: true,
	}

	hypershiftazure.BindProductFlags(platformOpts, cmd.Flags())

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		validOpts, err := platformOpts.Validate(ctx, coreOpts)
		if err != nil {
			return err
		}
		opts, err := validOpts.Complete(ctx, coreOpts)
		if err != nil {
			return err
		}
		return coreOpts.CreateRunFunc(opts, clientProvider)(cmd, args)
	}

	return cmd
}
