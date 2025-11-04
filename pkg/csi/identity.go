package csi

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	// DriverName is the name of the CSI driver
	DriverName = "rbs.csi.servers.com"
	// DriverVersion is the version of the CSI driver
	DriverVersion = "1.0.0"
)

// IdentityService implements the CSI Identity service
type IdentityService struct {
	csi.UnimplementedIdentityServer
}

// NewIdentityService creates a new identity service
func NewIdentityService() *IdentityService {
	return &IdentityService{}
}

// GetPluginInfo returns plugin information
func (s *IdentityService) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          DriverName,
		VendorVersion: DriverVersion,
	}, nil
}

// GetPluginCapabilities returns plugin capabilities
func (s *IdentityService) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}, nil
}

// Probe checks if the plugin is ready
func (s *IdentityService) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{
		Ready: wrapperspb.Bool(true),
	}, nil
}
