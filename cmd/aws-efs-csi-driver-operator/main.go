package main

import (
	"context"
	"os"

	"github.com/openshift/csi-operator/pkg/driver/aws-efs"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"

	"github.com/openshift/csi-operator/pkg/operator"
	"github.com/openshift/csi-operator/pkg/version"
)

func main() {
	command := NewOperatorCommand()
	code := cli.Run(command)
	os.Exit(code)
}

var guestKubeconfig *string

func NewOperatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aws-efs-csi-driver-operator",
		Short: "OpenShift AWS EFS CSI Driver Operator",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
			os.Exit(1)
		},
	}

	ctrlCmd := controllercmd.NewControllerCommandConfig(
		"aws-efs-csi-driver-operator",
		version.Get(),
		runCSIDriverOperator,
	).NewCommand()

	guestKubeconfig = ctrlCmd.Flags().String("guest-kubeconfig", "", "Path to the guest kubeconfig file. This flag enables hypershift integration.")

	ctrlCmd.Use = "start"
	ctrlCmd.Short = "Start the AWS EFS CSI Driver Operator"

	cmd.AddCommand(ctrlCmd)

	return cmd
}

func runCSIDriverOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
	opConfig := aws_efs.GetAWSEFSOperatorConfig()
	return operator.RunOperator(ctx, controllerConfig, *guestKubeconfig, opConfig)
}
