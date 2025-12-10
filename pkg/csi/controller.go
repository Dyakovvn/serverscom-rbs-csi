package csi

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	"maps"

	"github.com/serverscom/rbs-csi-driver/pkg/rbs"
	"github.com/serverscom/rbs-csi-driver/pkg/util"
	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	"k8s.io/client-go/kubernetes"
)

const (
	waitForVolumeBecomeActive  = 5 * time.Minute
	waitForVolumeBecomeDeleted = 5 * time.Minute

	// Volume status constants
	volumeStatusCreating = "creating"
	volumeStatusPending  = "pending"
	volumeStatusActive   = "active"
	volumeStatusRemoving = "removing"
)

// ControllerService implements the CSI Controller service
type ControllerService struct {
	csi.UnimplementedControllerServer
	rbsService rbs.RBSService
	kubeClient kubernetes.Interface
}

// NewControllerService creates a new controller service
func NewControllerService(s rbs.RBSService, kubeClient kubernetes.Interface) *ControllerService {
	return &ControllerService{
		rbsService: s,
		kubeClient: kubeClient,
	}
}

// CreateVolume creates a new volume
func (s *ControllerService) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(2).InfoS("CreateVolume called",
		"name", req.GetName(),
		"size_bytes", req.GetCapacityRange().GetRequiredBytes(),
	)

	// Parse parameters
	locationID, err := s.getLocationID(ctx, req.GetParameters())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to get location ID: %v", err)
	}

	flavorID, err := s.getFlavorID(ctx, req.GetParameters(), locationID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to get flavor ID: %v", err)
	}

	// Get PVC metadata from parameters (passed by external-provisioner with --extra-create-metadata)
	pvcName := req.GetParameters()["csi.storage.k8s.io/pvc/name"]
	pvcNamespace := req.GetParameters()["csi.storage.k8s.io/pvc/namespace"]

	// req.GetName() returns pvc-<uuid> which we use for idempotency
	volumeHandle := req.GetName()

	// Use PVC name as volume name, fallback to volumeHandle if not available
	volumeName := pvcName
	if volumeName == "" {
		volumeName = volumeHandle
	}
	klog.V(2).InfoS("Creating volume",
		"name", volumeName,
		"pvc_name", pvcName,
		"pvc_namespace", pvcNamespace,
		"volume_handle", volumeHandle,
	)
	// Build labels with priority order:
	// 1. Labels from StorageClass parameters (rbs.csi.servers.com/labels)
	// 2. Labels from PVC metadata
	// 3. System labels (rbs.csi.servers.com/pvc-uuid, pvc-namespace) - highest priority

	labels := make(map[string]string)

	// 1. Get labels from StorageClass parameters
	storageClassLabels := s.getLabels(req.GetParameters())
	maps.Copy(labels, storageClassLabels)

	// 2. Get labels from PVC
	pvcLabels, err := s.getPVCLabels(ctx, pvcNamespace, pvcName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get PVC labels: %v", err)
	}
	maps.Copy(labels, pvcLabels)

	// 3. Add system labels (these override everything)
	pvcUUID := util.ExtractPVCUUID(volumeHandle)
	labels["rbs.csi.servers.com/pvc-uuid"] = pvcUUID
	if pvcNamespace != "" {
		labels["rbs.csi.servers.com/pvc-namespace"] = pvcNamespace
	}

	// Convert size from bytes to GB
	sizeBytes := req.GetCapacityRange().GetRequiredBytes()
	sizeGB := sizeBytes / (1024 * 1024 * 1024)
	if sizeGB == 0 {
		sizeGB = 1 // Minimum 1GB
	}

	// Create volume request
	createReq := serverscom.RemoteBlockStorageVolumeCreateInput{
		Name:       volumeName,
		Size:       sizeGB,
		LocationID: locationID,
		FlavorID:   flavorID,
		Labels:     labels,
	}

	// Check if volume already exists (idempotency)
	// Search by label rbs.csi.servers.com/pvc-uuid to ensure uniqueness
	var volume *serverscom.RemoteBlockStorageVolume
	existingVolume, err := s.rbsService.FindVolumeByLabel(ctx, "rbs.csi.servers.com/pvc-uuid", pvcUUID)
	if err == nil {
		// Volume already exists
		klog.V(1).InfoS("Volume already exists",
			"name", existingVolume.Name,
			"id", existingVolume.ID,
			"pvc_namespace", pvcNamespace,
			"pvc_name", pvcName,
			"handle", volumeHandle,
		)

		volume = existingVolume
	} else {
		// Volume doesn't exist, create it
		volume, err = s.rbsService.CreateVolume(ctx, createReq)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create volume: %v", err)
		}
		klog.V(1).InfoS("Created new volume",
			"name", volumeName,
			"id", volume.ID,
			"pvc_namespace", pvcNamespace,
			"pvc_name", pvcName,
			"handle", volumeHandle,
		)
	}

	// Wait for volume to become active
	volume, err = s.waitForVolumeActive(ctx, volume.ID, waitForVolumeBecomeActive)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to wait for volume to become active: %v", err)
	}

	// Prepare response
	volumeContext := map[string]string{
		"volume-id":                           volume.ID,
		"rbs.csi.servers.com/rbs-volume-id":   volume.ID,
		"rbs.csi.servers.com/rbs-volume-name": volume.Name,
		"location-id":                         strconv.Itoa(volume.LocationID),
		"flavor-id":                           strconv.Itoa(volume.FlavorID),
	}

	// Add PVC metadata to volume context
	if pvcName != "" {
		volumeContext["rbs.csi.servers.com/pvc-name"] = pvcName
	}
	if pvcNamespace != "" {
		volumeContext["rbs.csi.servers.com/pvc-namespace"] = pvcNamespace
	}
	volumeContext["rbs.csi.servers.com/pvc-uuid"] = pvcUUID

	if volume.IPAddress != nil {
		volumeContext["ip-address"] = *volume.IPAddress
	}
	if volume.TargetIQN != nil {
		volumeContext["target-iqn"] = *volume.TargetIQN
	}

	response := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volume.ID,
			CapacityBytes: volume.Size * 1024 * 1024 * 1024, // Convert GB to bytes
			VolumeContext: volumeContext,
		},
	}
	klog.InfoS("Volume created successfully",
		"rbs_id", volume.ID,
		"name", volume.Name,
		"pvc_namespace", pvcNamespace,
		"pvc_name", pvcName,
	)
	return response, nil
}

// DeleteVolume deletes a volume
func (s *ControllerService) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.V(2).InfoS("DeleteVolume called", "volume_id", req.GetVolumeId())

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		klog.Error("DeleteVolume called with empty volume ID")
		return nil, fmt.Errorf("volume ID missing")
	}

	// Check if volume exists
	volume, err := s.rbsService.GetVolume(ctx, volumeID)
	if err != nil {
		if _, ok := err.(*serverscom.NotFoundError); ok {
			klog.V(1).InfoS("Volume not found, considering it already deleted", "volume_id", req.GetVolumeId())
			return &csi.DeleteVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}

	if volume.Status == volumeStatusRemoving {
		klog.V(1).InfoS("Volume in removing state, treating as success delete", "volume_id", req.GetVolumeId())
		return &csi.DeleteVolumeResponse{}, nil

	}
	// Delete volume via RBS API
	err = s.rbsService.DeleteVolume(ctx, volumeID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete volume: %v", err)
	}

	klog.InfoS("Volume deleted successfully", "volume_id", req.GetVolumeId())
	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume publishes a volume to a node
func (s *ControllerService) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	klog.V(2).InfoS("ControllerPublishVolume called",
		"volume_id", req.GetVolumeId(),
		"node_id", req.GetNodeId(),
	)

	// Get volume information
	volume, err := s.rbsService.GetVolume(ctx, req.GetVolumeId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}

	// Get iSCSI credentials
	credentials, err := s.rbsService.GetVolumeCredentials(ctx, req.GetVolumeId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get volume credentials: %v", err)
	}

	// Prepare publish context with iSCSI connection information
	publishContext := map[string]string{
		"username":    credentials.Username,
		"password":    credentials.Password,
		"volume-id":   volume.ID,
		"device-path": "", // Will be determined on the node
	}
	if credentials.TargetIQN != nil {
		publishContext["target-iqn"] = *credentials.TargetIQN
	}
	if credentials.IPAddress != nil {
		publishContext["ip-address"] = *credentials.IPAddress
	}

	response := &csi.ControllerPublishVolumeResponse{
		PublishContext: publishContext,
	}

	klog.InfoS("Volume published successfully", "volume_id", req.GetVolumeId())
	return response, nil
}

// ControllerUnpublishVolume unpublishes a volume from a node
func (s *ControllerService) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	klog.V(2).InfoS("ControllerUnpublishVolume called",
		"volume_id", req.GetVolumeId(),
		"node_id", req.GetNodeId(),
	)

	// For iSCSI, unpublishing is typically handled on the node side
	// The controller doesn't need to do anything special here

	klog.InfoS("Volume unpublished successfully", "volume_id", req.GetVolumeId())
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

// ControllerExpandVolume expands a volume
func (s *ControllerService) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	klog.V(2).InfoS("ControllerExpandVolume called",
		"volume_id", req.GetVolumeId(),
		"new_size_bytes", req.GetCapacityRange().GetRequiredBytes(),
	)

	// Convert size from bytes to GB
	newSizeBytes := req.GetCapacityRange().GetRequiredBytes()
	newSizeGB := newSizeBytes / (1024 * 1024 * 1024)
	if newSizeGB == 0 {
		newSizeGB = 1 // Minimum 1GB
	}

	// Get current volume info
	volume, err := s.rbsService.GetVolume(ctx, req.GetVolumeId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}

	// Check if resize is already in progress
	if volume.Status == volumeStatusPending {
		klog.V(1).InfoS("Volume is already in pending state, waiting for resize to complete",
			"volume_id", volume.ID,
		)
		// Just wait for it to become active, don't send another update request
		volume, err = s.waitForVolumeActive(ctx, volume.ID, 5*time.Minute)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to wait for volume resize to complete: %v", err)
		}
	} else if volume.Size >= newSizeGB {
		// Volume is already at or larger than requested size
		klog.V(1).InfoS("Volume already has size, skipping resize",
			"volume_id", volume.ID,
			"current_size_gb", volume.Size,
			"requested_size_gb", newSizeGB,
		)
	} else {
		// Need to resize the volume
		klog.V(1).InfoS("Resizing volume",
			"volume_id", volume.ID,
			"from_gb", volume.Size,
			"to_gb", newSizeGB,
		)
		updateReq := serverscom.RemoteBlockStorageVolumeUpdateInput{
			Size: newSizeGB,
		}

		volume, err = s.rbsService.UpdateVolume(ctx, req.GetVolumeId(), updateReq)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update volume size: %v", err)
		}

		// Wait for volume to become active after resize
		volume, err = s.waitForVolumeActive(ctx, volume.ID, 5*time.Minute)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to wait for volume resize to complete: %v", err)
		}
	}

	response := &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         volume.Size * 1024 * 1024 * 1024, // Convert GB to bytes
		NodeExpansionRequired: true,                             // Filesystem resize needed on the node
	}

	klog.InfoS("Volume expanded successfully",
		"volume_id", req.GetVolumeId(),
	)
	return response, nil
}

// ControllerGetCapabilities returns the capabilities of the controller
func (s *ControllerService) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	klog.V(2).Info("ControllerGetCapabilities called")

	capabilities := []*csi.ControllerServiceCapability{
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
				},
			},
		},
	}

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: capabilities,
	}, nil
}

// ValidateVolumeCapabilities validates volume capabilities
func (s *ControllerService) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	klog.V(2).InfoS("ValidateVolumeCapabilities called",
		"volume_id", req.GetVolumeId(),
	)

	// For now, support filesystem access mode
	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.GetVolumeCapabilities(),
		},
	}, nil
}
