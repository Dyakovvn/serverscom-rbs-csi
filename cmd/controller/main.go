package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/serverscom/rbs-csi-driver/pkg/controller"
	"github.com/serverscom/rbs-csi-driver/pkg/util"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

var (
	endpoint  = flag.String("endpoint", "unix:///var/lib/csi/sockets/pluginproxy/csi.sock", "CSI endpoint")
	version   = "dev"
	gitCommit = "none"
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	defer klog.Flush()

	apiToken := os.Getenv(util.ScTokenEnvKey)
	if apiToken == "" {
		klog.Fatalf("environment variable %q is required", util.ScTokenEnvKey)
	}

	apiBaseURL := os.Getenv(util.ScBaseUrlEnvKey)

	klog.V(1).InfoS("Starting RBS CSI Controller",
		"csi_endpoint", endpoint,
		"api_base_url", apiBaseURL,
		"version", version,
		"commit", gitCommit,
	)

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("get in cluster config failed: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("create new k8s clientset failed: %v", err)
	}

	config := &controller.Config{
		Endpoint:    *endpoint,
		RBSAPIUrl:   apiBaseURL,
		RBSAPIToken: apiToken,
		KubeClient:  kubeClient,
		UserAgent:   "serverscom-rbs-csi/" + version,
	}

	ctrl, err := controller.NewController(config)
	if err != nil {
		klog.Fatalf("Failed to create controller: %v", err)
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

	klog.Info("Starting CSI controller service...")
	if err := ctrl.Run(ctx); err != nil {
		klog.Fatalf("Failed to run CSI controller: %v", err)
	}
}
