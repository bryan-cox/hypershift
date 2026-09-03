package dump

import (
	"github.com/openshift/hypershift/cmd/cluster/core"
	"github.com/openshift/hypershift/cmd/util"

	"github.com/spf13/cobra"
)

func NewCommand(clientProviders ...*util.ClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "dump",
		Short:        "Commands for dumping resources for debugging",
		SilenceUsage: true,
	}

	clientProvider := core.DefaultClientProvider()
	if len(clientProviders) > 0 && clientProviders[0] != nil {
		clientProvider = clientProviders[0]
	}
	cmd.AddCommand(core.NewDumpCommand(core.DumpClusterWithRetry, clientProvider))

	return cmd
}
