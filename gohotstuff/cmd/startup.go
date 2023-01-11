package cmd

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"time"

	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/node"
	"github.com/spf13/cobra"
)

type StartupCmd struct {
	Cmd *cobra.Command
}

func GetStartCmd() *StartupCmd {
	cmd := new(StartupCmd)
	var envCfgPath string

	cmd.Cmd = &cobra.Command{
		Use:           "start",
		Short:         "Start up the hotstuff node.",
		Example:       "gohotstuff start --conf /home/rd/gohotstuff/conf",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return StartHotstuff(envCfgPath)
		},
	}

	cmd.Cmd.Flags().StringVarP(&envCfgPath, "conf", "c", "",
		"engine environment config file path")

	return cmd
}

func StartHotstuff(envCfgPath string) error {
	if len(envCfgPath) <= 0 {
		cfgFile := filepath.Join(libs.GetCurRootDir(), "conf/conf.yaml")
		if !libs.FileIsExist(cfgFile) {
			panic("conf file doesn't exist, must mkdir conf in output path")
		}
		envCfgPath = cfgFile
	} else {
		libs.SetRootDir(envCfgPath)
		envCfgPath = filepath.Join(envCfgPath, "conf.yaml")
	}

	cfg, err := libs.GetConfig(envCfgPath)
	if err != nil {
		panic(fmt.Errorf("load configuration failed, err: %v", err))
	}

	n, err := node.NewNode(cfg)
	if err != nil {
		panic(fmt.Errorf("new a node failed, cfg: %+v, err: %v", cfg, err))
	}
	n.Start()
	for {
		rand.Seed(time.Now().UnixNano())
		time.Sleep(time.Duration(rand.Intn(1000) * int(time.Millisecond)))
		continue
	}
}
