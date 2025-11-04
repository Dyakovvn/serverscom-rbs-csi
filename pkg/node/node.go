package node

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"

	rbscsi "github.com/serverscom/rbs-csi-driver/pkg/csi"
	"github.com/serverscom/rbs-csi-driver/pkg/util"
)

// Node represents the CSI Node service
type Node struct {
	endpoint        string
	nodeID          string
	identityService *rbscsi.IdentityService
	nodeService     *rbscsi.NodeService

	server   *grpc.Server
	listener net.Listener
}

// Config holds node configuration
type Config struct {
	Endpoint string
	NodeID   string
}

// NewNode creates a new CSI node
func NewNode(config *Config) (*Node, error) {
	if config.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}

	if config.NodeID == "" {
		return nil, fmt.Errorf("node ID is required")
	}

	// Create services
	identityService := rbscsi.NewIdentityService()
	nodeService := rbscsi.NewNodeService(config.NodeID)

	node := &Node{
		endpoint:        config.Endpoint,
		nodeID:          config.NodeID,
		identityService: identityService,
		nodeService:     nodeService,
	}

	klog.InfoS("RBS CSI node initialized", "node", config.NodeID)
	return node, nil
}

// Run starts the CSI node
func (n *Node) Run(ctx context.Context) error {
	klog.InfoS("Starting RBS CSI node", "endpoint", n.endpoint)

	// Create listener
	listener, err := createListener(n.endpoint)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	n.listener = listener

	// Create gRPC server
	n.server = grpc.NewServer()

	// Register only Identity and Node services
	csi.RegisterIdentityServer(n.server, n.identityService)
	csi.RegisterNodeServer(n.server, n.nodeService)

	klog.InfoS("Node is ready to serve requests")

	// Start gRPC server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := n.server.Serve(n.listener); err != nil {
			errChan <- fmt.Errorf("gRPC server failed: %w", err)
		}
	}()

	// Wait for context cancellation, interrupt signal, or server error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		klog.InfoS("Context cancelled, shutting down node")
	case sig := <-sigChan:
		klog.InfoS("Received signal, shutting down node", "signal", sig)
	case err := <-errChan:
		klog.ErrorS(err, "gRPC server error")
		return err
	}

	return n.stop()
}

// stop gracefully stops the node
func (n *Node) stop() error {
	klog.InfoS("Stopping RBS CSI node")

	if n.server != nil {
		klog.InfoS("Gracefully stopping gRPC server")
		n.server.GracefulStop()
	}

	if n.listener != nil {
		n.listener.Close()
	}

	klog.InfoS("RBS CSI node stopped")
	return nil
}

// createListener creates a listener for the given endpoint
func createListener(endpoint string) (net.Listener, error) {
	return util.CreateListener(endpoint)
}
