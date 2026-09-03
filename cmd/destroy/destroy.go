package destroy

import (
	"github.com/openshift/hypershift/cmd/bastion"
	"github.com/openshift/hypershift/cmd/cluster"
	"github.com/openshift/hypershift/cmd/cluster/core"
	"github.com/openshift/hypershift/cmd/infra"
	"github.com/openshift/hypershift/cmd/util"

	"github.com/spf13/cobra"
)

func NewCommand(clientProviders ...*util.ClientProvider) *cobra.Command {
	clientProvider := core.DefaultClientProvider()
	if len(clientProviders) > 0 && clientProviders[0] != nil {
		clientProvider = clientProviders[0]
	}
	destroyCmd := &cobra.Command{
		Use:          "destroy",
		Short:        "Commands for destroying HyperShift resources",
		SilenceUsage: true,
	}

	destroyCmd.AddCommand(cluster.NewDestroyCommands(clientProvider))
	destroyCmd.AddCommand(infra.NewDestroyCommand())
	destroyCmd.AddCommand(infra.NewDestroyIAMCommand())
	destroyCmd.AddCommand(bastion.NewDestroyCommand(clientProvider))

	return destroyCmd
}
