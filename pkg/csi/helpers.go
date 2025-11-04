package csi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	"k8s.io/klog/v2"
)

// getLocationID gets location ID from parameters, supporting both ID and name
func (s *ControllerService) getLocationID(ctx context.Context, parameters map[string]string) (int, error) {
	locationStr, ok := parameters["rbs.csi.servers.com/location"]
	if !ok {
		return 0, fmt.Errorf("location parameter is required (rbs.csi.servers.com/location)")
	}

	if locationID, err := strconv.Atoi(locationStr); err == nil {
		klog.V(3).Infof("Using location ID: %d", locationID)
		return locationID, nil
	}

	klog.V(3).Infof("Location value '%s' is not numeric, searching by name", locationStr)
	location, err := s.rbsService.FindLocationByName(ctx, locationStr)
	if err != nil {
		return 0, fmt.Errorf("failed to find location by name '%s': %w", locationStr, err)
	}

	klog.V(3).Infof("Found location '%s' with ID: %d", locationStr, location.ID)
	return int(location.ID), nil
}

// getFlavorID gets flavor ID from parameters, supporting both ID and name
func (s *ControllerService) getFlavorID(ctx context.Context, parameters map[string]string, locationID int) (int, error) {
	flavorStr, ok := parameters["rbs.csi.servers.com/flavor"]
	if !ok {
		return 0, fmt.Errorf("flavor parameter is required (rbs.csi.servers.com/flavor)")
	}

	if flavorID, err := strconv.Atoi(flavorStr); err == nil {
		klog.V(3).Infof("Using flavor ID: %d", flavorID)
		return flavorID, nil
	}

	klog.V(3).Infof("Flavor value '%s' is not numeric, searching by name in location %d", flavorStr, locationID)
	flavor, err := s.rbsService.FindFlavorByName(ctx, locationID, flavorStr)
	if err != nil {
		return 0, fmt.Errorf("failed to find flavor by name '%s' in location %d: %w", flavorStr, locationID, err)
	}

	klog.V(3).Infof("Found flavor '%s' with ID: %d", flavorStr, flavor.ID)
	return int(flavor.ID), nil
}

// getLabels extracts labels from params
func (s *ControllerService) getLabels(parameters map[string]string) map[string]string {
	labelsStr, ok := parameters["rbs.csi.servers.com/labels"]
	if !ok {
		return nil
	}

	var labels map[string]string
	if err := json.Unmarshal([]byte(labelsStr), &labels); err != nil {
		klog.ErrorS(err, "failed to parse labels parameter")
		return nil
	}

	return labels
}

// getPVCLabels extracts PVC labels from CSI parameters
// External-provisioner with --extra-create-metadata passes PVC labels as:
// csi.storage.k8s.io/pvc/labels.<label-key> = <label-value>
func (s *ControllerService) getPVCLabels(parameters map[string]string) map[string]string {
	const pvcLabelPrefix = "csi.storage.k8s.io/pvc/labels."
	labels := make(map[string]string)

	for key, value := range parameters {
		if len(key) > len(pvcLabelPrefix) && key[:len(pvcLabelPrefix)] == pvcLabelPrefix {
			labelKey := key[len(pvcLabelPrefix):]
			labels[labelKey] = value
		}
	}

	if len(labels) == 0 {
		return nil
	}

	return labels
}

// waitForVolumeActive polls every 5 seconds volume status until it gets active
func (s *ControllerService) waitForVolumeActive(ctx context.Context, volumeID string, timeout time.Duration) (*serverscom.RemoteBlockStorageVolume, error) {
	const pollInterval = 5 * time.Second
	klog.V(1).InfoS("Waiting for volume to become active", "volumeID", volumeID)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		volume, err := s.rbsService.GetVolume(ctx, volumeID)
		if err != nil {
			return nil, err
		}

		switch volume.Status {
		case volumeStatusActive:
			klog.V(1).InfoS("Volume is now active", "volumeID", volumeID)
			return volume, nil
		case volumeStatusCreating, volumeStatusPending:
			klog.V(1).InfoS("Volume still in intermediate status, waiting...", "volumeID", volumeID, "status", volume.Status)
		default:
			return nil, fmt.Errorf("volume %s is in unexpected status: %s", volumeID, volume.Status)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for volume %s to become active: %w", volumeID, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}
