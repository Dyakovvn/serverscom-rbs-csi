package csi

import (
	"context"
	"errors"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/serverscom/rbs-csi-driver/pkg/mocks"
	serverscom "github.com/serverscom/serverscom-go-client/pkg"
)

func TestCreateVolume_SuccessWithIDs(t *testing.T) {
	g := NewGomegaWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRBSService(ctrl)
	svc := NewControllerService(mockSvc)

	ctx := context.Background()
	req := &csi.CreateVolumeRequest{
		Name: "test-vol",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10 * 1024 * 1024 * 1024, // 10GB
		},
		Parameters: map[string]string{
			"rbs.csi.servers.com/location": "1",
			"rbs.csi.servers.com/flavor":   "2",
		},
	}

	vol := &serverscom.RemoteBlockStorageVolume{
		ID:         "vol-1",
		Name:       "test-vol",
		LocationID: 1,
		FlavorID:   2,
		Size:       10,
		Status:     "active",
	}

	mockSvc.EXPECT().FindVolumeByLabel(gomock.Any(), "rbs.csi.servers.com/pvc-uuid", gomock.Any()).
		Return(nil, errors.New("not found"))
	mockSvc.EXPECT().CreateVolume(gomock.Any(), gomock.Any()).
		Return(vol, nil)
	mockSvc.EXPECT().GetVolume(gomock.Any(), "vol-1").AnyTimes().
		Return(vol, nil)

	resp, err := svc.CreateVolume(ctx, req)
	g.Expect(err).To(BeNil())
	g.Expect(resp).NotTo(BeNil())
	g.Expect(resp.Volume.VolumeId).To(Equal("vol-1"))
	g.Expect(resp.Volume.VolumeContext["location-id"]).To(Equal("1"))
	g.Expect(resp.Volume.VolumeContext["flavor-id"]).To(Equal("2"))
}

func TestCreateVolume_SuccessWithNames(t *testing.T) {
	g := NewGomegaWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRBSService(ctrl)
	svc := NewControllerService(mockSvc)

	ctx := context.Background()
	req := &csi.CreateVolumeRequest{
		Name: "test-vol",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10 * 1024 * 1024 * 1024, // 10GB
		},
		Parameters: map[string]string{
			"rbs.csi.servers.com/location": "test-location",
			"rbs.csi.servers.com/flavor":   "test-flavor",
		},
	}

	vol := &serverscom.RemoteBlockStorageVolume{
		ID:         "vol-1",
		Name:       "test-vol",
		LocationID: 1,
		FlavorID:   2,
		Size:       10,
		Status:     "active",
	}

	mockSvc.EXPECT().FindLocationByName(gomock.Any(), "test-location").
		Return(&serverscom.Location{ID: 1}, nil)
	mockSvc.EXPECT().FindFlavorByName(gomock.Any(), 1, "test-flavor").
		Return(&serverscom.RemoteBlockStorageFlavor{ID: 2}, nil)
	mockSvc.EXPECT().FindVolumeByLabel(gomock.Any(), "rbs.csi.servers.com/pvc-uuid", gomock.Any()).
		Return(nil, errors.New("not found"))
	mockSvc.EXPECT().CreateVolume(gomock.Any(), gomock.Any()).
		Return(vol, nil)
	mockSvc.EXPECT().GetVolume(gomock.Any(), "vol-1").AnyTimes().
		Return(vol, nil)

	resp, err := svc.CreateVolume(ctx, req)
	g.Expect(err).To(BeNil())
	g.Expect(resp).NotTo(BeNil())
	g.Expect(resp.Volume.VolumeId).To(Equal("vol-1"))
	g.Expect(resp.Volume.VolumeContext["location-id"]).To(Equal("1"))
	g.Expect(resp.Volume.VolumeContext["flavor-id"]).To(Equal("2"))
}

func TestDeleteVolume_Success(t *testing.T) {
	g := NewGomegaWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRBSService(ctrl)
	svc := NewControllerService(mockSvc)
	ctx := context.Background()

	vol := &serverscom.RemoteBlockStorageVolume{
		ID:     "vol-1",
		Status: "active",
	}

	mockSvc.EXPECT().GetVolume(ctx, "vol-1").Return(vol, nil)
	mockSvc.EXPECT().DeleteVolume(ctx, "vol-1").Return(nil)

	req := &csi.DeleteVolumeRequest{VolumeId: "vol-1"}
	resp, err := svc.DeleteVolume(ctx, req)
	g.Expect(err).To(BeNil())
	g.Expect(resp).NotTo(BeNil())
}

func TestDeleteVolume_NotFound(t *testing.T) {
	g := NewGomegaWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRBSService(ctrl)
	svc := NewControllerService(mockSvc)
	ctx := context.Background()

	mockSvc.EXPECT().GetVolume(ctx, "vol-1").
		Return(nil, &serverscom.NotFoundError{})

	req := &csi.DeleteVolumeRequest{VolumeId: "vol-1"}
	resp, err := svc.DeleteVolume(ctx, req)
	g.Expect(err).To(BeNil())
	g.Expect(resp).NotTo(BeNil())
}

func TestControllerPublishVolume_Success(t *testing.T) {
	g := NewGomegaWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRBSService(ctrl)
	svc := NewControllerService(mockSvc)
	ctx := context.Background()

	vol := &serverscom.RemoteBlockStorageVolume{ID: "vol-1"}
	iqn := "iqn.2025-11.test"
	ip := "10.0.0.1"
	creds := &serverscom.RemoteBlockStorageVolumeCredentials{
		Username:  "user",
		Password:  "pass",
		TargetIQN: &iqn,
		IPAddress: &ip,
	}

	mockSvc.EXPECT().GetVolume(ctx, "vol-1").Return(vol, nil)
	mockSvc.EXPECT().GetVolumeCredentials(ctx, "vol-1").Return(creds, nil)

	req := &csi.ControllerPublishVolumeRequest{
		VolumeId: "vol-1",
		NodeId:   "node-1",
	}

	resp, err := svc.ControllerPublishVolume(ctx, req)
	g.Expect(err).To(BeNil())
	g.Expect(resp).NotTo(BeNil())
	g.Expect(resp.PublishContext["username"]).To(Equal("user"))
	g.Expect(resp.PublishContext["target-iqn"]).To(Equal(iqn))
}

func TestControllerUnpublishVolume(t *testing.T) {
	g := NewGomegaWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	svc := NewControllerService(nil)
	ctx := context.Background()
	req := &csi.ControllerUnpublishVolumeRequest{
		VolumeId: "vol-1",
		NodeId:   "node-1",
	}

	resp, err := svc.ControllerUnpublishVolume(ctx, req)
	g.Expect(err).To(BeNil())
	g.Expect(resp).NotTo(BeNil())
}

func TestControllerExpandVolume_Success(t *testing.T) {
	g := NewGomegaWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRBSService(ctrl)
	svc := NewControllerService(mockSvc)
	ctx := context.Background()

	vol := &serverscom.RemoteBlockStorageVolume{
		ID:     "vol-1",
		Size:   5,
		Status: "active",
	}

	newVol := &serverscom.RemoteBlockStorageVolume{
		ID:     "vol-1",
		Size:   10,
		Status: "active",
	}

	mockSvc.EXPECT().GetVolume(ctx, "vol-1").Return(vol, nil)
	mockSvc.EXPECT().UpdateVolume(ctx, "vol-1", gomock.Any()).Return(newVol, nil)
	mockSvc.EXPECT().GetVolume(gomock.Any(), "vol-1").AnyTimes().Return(newVol, nil)

	req := &csi.ControllerExpandVolumeRequest{
		VolumeId: "vol-1",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10 * 1024 * 1024 * 1024,
		},
	}

	resp, err := svc.ControllerExpandVolume(ctx, req)
	g.Expect(err).To(BeNil())
	g.Expect(resp.CapacityBytes).To(Equal(int64(10 * 1024 * 1024 * 1024)))
}

func TestControllerGetCapabilities(t *testing.T) {
	g := NewGomegaWithT(t)
	svc := NewControllerService(nil)
	resp, err := svc.ControllerGetCapabilities(context.Background(), &csi.ControllerGetCapabilitiesRequest{})
	g.Expect(err).To(BeNil())
	g.Expect(resp.Capabilities).NotTo(BeEmpty())
}

func TestValidateVolumeCapabilities(t *testing.T) {
	g := NewGomegaWithT(t)
	svc := NewControllerService(nil)
	req := &csi.ValidateVolumeCapabilitiesRequest{
		VolumeId: "vol-1",
		VolumeCapabilities: []*csi.VolumeCapability{
			{AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER}},
		},
	}
	resp, err := svc.ValidateVolumeCapabilities(context.Background(), req)
	g.Expect(err).To(BeNil())
	g.Expect(resp.Confirmed).NotTo(BeNil())
}
