package main

import (
	"fmt"

	"github.com/aucusaga/gohotstuff/gohotstuff/cmd"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd, err := NewServiceCommand()
	if err != nil {
		fmt.Print("start service failed\n")
		return
	}

	if err = rootCmd.Execute(); err != nil {
		fmt.Printf("cmd fail, err: %v\n", err)
		return
	}
}

func NewServiceCommand() (*cobra.Command, error) {
	rootCmd := &cobra.Command{
		Use:           "gohotstuff <command> [arguments]",
		Short:         "gohotstuff is a service which implements the chained-hotStuff consensus protocol.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example:       "gohotstuff start --conf /home/rd/gohotstuff/conf",
	}

	// cmd service
	rootCmd.AddCommand(cmd.GetStartCmd().Cmd)
	rootCmd.AddCommand(cmd.GetKeyCmd().Cmd)
	rootCmd.AddCommand(cmd.GetAddressCmd().Cmd)

	return rootCmd, nil
}
