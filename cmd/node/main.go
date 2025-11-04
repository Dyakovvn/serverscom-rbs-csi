package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/serverscom/rbs-csi-driver/pkg/node"
	"github.com/serverscom/rbs-csi-driver/pkg/util"
	"k8s.io/klog/v2"
)

var (
	endpoint  = flag.String("endpoint", "unix:///var/lib/csi/sockets/pluginproxy/csi.sock", "CSI endpoint")
	nodeID    = flag.String("nodeid", "", "node ID")
	version   = "dev"
	gitCommit = "none"
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	defer klog.Flush()

	if *nodeID == "" {
		*nodeID = os.Getenv(util.RbsCsiNodeIdEnvKey)
	}
	if *nodeID == "" {
		klog.Fatal("Node ID is required for node service")
	}

	klog.V(1).InfoS("Starting RBS CSI Node",
		"csi_endpoint", endpoint,
		"node_id", nodeID,
		"version", version,
		"commit", gitCommit,
	)

	config := &node.Config{
		Endpoint: *endpoint,
		NodeID:   *nodeID,
	}

	n, err := node.NewNode(config)
	if err != nil {
		klog.Fatalf("Failed to create node: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		klog.Info("Received shutdown signal")
		cancel()
	}()

	klog.Info("Starting CSI node service...")
	if err := n.Run(ctx); err != nil {
		klog.Fatalf("Failed to run CSI node: %v", err)
	}
}
