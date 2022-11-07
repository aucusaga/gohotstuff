package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/node"
)

func main() {
	cfg, err := libs.GetConfig("./conf/conf.yaml")
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
