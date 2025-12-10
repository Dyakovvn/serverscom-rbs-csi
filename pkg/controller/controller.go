package controller

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
	"github.com/serverscom/rbs-csi-driver/pkg/rbs"
	"github.com/serverscom/rbs-csi-driver/pkg/util"
	"k8s.io/client-go/kubernetes"
)

// Controller represents the CSI Controller service
type Controller struct {
	endpoint          string
	rbsService        rbs.RBSService
	identityService   *rbscsi.IdentityService
	controllerService *rbscsi.ControllerService

	server   *grpc.Server
	listener net.Listener
}

// Config holds controller configuration
type Config struct {
	Endpoint    string
	RBSAPIUrl   string
	RBSAPIToken string
	KubeClient  *kubernetes.Clientset
}

// NewController creates a new CSI controller
func NewController(config *Config) (*Controller, error) {
	scClient := util.NewScClient(config.RBSAPIUrl, config.RBSAPIToken)
	rbsService := rbs.NewRBSService(scClient)

	// Create services
	identityService := rbscsi.NewIdentityService()
	controllerService := rbscsi.NewControllerService(rbsService, config.KubeClient)

	controller := &Controller{
		endpoint:          config.Endpoint,
		rbsService:        rbsService,
		identityService:   identityService,
		controllerService: controllerService,
	}

	klog.V(1).InfoS("RBS CSI controller initialized")
	return controller, nil
}

// Run starts the CSI controller
func (c *Controller) Run(ctx context.Context) error {
	klog.InfoS("Starting RBS CSI controller", "endpoint", c.endpoint)

	// Create listener
	listener, err := createListener(c.endpoint)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	c.listener = listener

	// Create gRPC server
	c.server = grpc.NewServer()

	// Register only Identity and Controller services
	csi.RegisterIdentityServer(c.server, c.identityService)
	csi.RegisterControllerServer(c.server, c.controllerService)

	klog.InfoS("Controller is ready to serve requests")

	// Start gRPC server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := c.server.Serve(c.listener); err != nil {
			errChan <- fmt.Errorf("gRPC server failed: %w", err)
		}
	}()

	// Wait for context cancellation, interrupt signal, or server error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		klog.InfoS("Context cancelled, shutting down controller")
	case sig := <-sigChan:
		klog.InfoS("Received signal, shutting down controller", "signal", sig)
	case err := <-errChan:
		klog.ErrorS(err, "gRPC server error")
		return err
	}

	return c.stop()
}

// stop gracefully stops the controller
func (c *Controller) stop() error {
	klog.InfoS("Stopping RBS CSI controller")

	if c.server != nil {
		klog.InfoS("Gracefully stopping gRPC server")
		c.server.GracefulStop()
	}

	if c.listener != nil {
		c.listener.Close()
	}

	klog.InfoS("RBS CSI controller stopped")
	return nil
}

// createListener creates a listener for the given endpoint
func createListener(endpoint string) (net.Listener, error) {
	return util.CreateListener(endpoint)
}
