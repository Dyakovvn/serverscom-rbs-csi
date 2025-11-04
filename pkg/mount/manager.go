package mount

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
	utilexec "k8s.io/utils/exec"
)

//go:generate mockgen --destination ../mocks/mount_manager.go --package=mocks --source manager.go
type MountManager interface {
	FormatAndMountDevice(ctx context.Context, devicePath, targetPath, fsType string, mountOptions []string) error
	Mount(ctx context.Context, devicePath, targetPath, fsType string, options []string) error
	Unmount(ctx context.Context, targetPath string) error
	IsMounted(targetPath string) (bool, error)
	GetMountInfo(targetPath string) (*MountInfo, error)
	BindMount(ctx context.Context, sourcePath, targetPath string, options []string) error
	ResizeFilesystem(ctx context.Context, devicePath, fsType string) error
	ResizeFilesystemAtPath(ctx context.Context, mountPath, fsType string) error
}

// Manager handles filesystem operations and mounting
type Manager struct {
	mounter    *mount.SafeFormatAndMount
	k8sMounter mount.Interface
	exec       utilexec.Interface
}

// NewManager creates a new mount manager
func NewManager() MountManager {
	exec := utilexec.New()
	k8sMounter := mount.New("")

	return &Manager{
		mounter: mount.NewSafeFormatAndMount(
			k8sMounter,
			exec,
			mount.WithMaxConcurrentFormat(1, 5*time.Minute), // Max 1 concurrent format, 5min timeout
		),
		k8sMounter: k8sMounter,
		exec:       exec,
	}
}

// FormatAndMountDevice formats (if needed) and mounts a block device
func (m *Manager) FormatAndMountDevice(ctx context.Context, devicePath, targetPath, fsType string, mountOptions []string) error {
	if fsType == "" {
		fsType = "ext4"
	}

	klog.V(1).InfoS("Starting to format and mount device",
		"device_path", devicePath,
		"target_path", targetPath,
		"fs_type", fsType)

	startTime := time.Now()

	formatArgs := []string{}

	switch fsType {
	case "ext4", "ext3":
		formatArgs = []string{"-E", "nodiscard"}
	case "btrfs":
		formatArgs = []string{"-K"}
	case "xfs":
		formatArgs = []string{"-K"}
	}

	// Use SafeFormatAndMount for all filesystems
	// Note: The semaphore inside SafeFormatAndMount only blocks mkfs operations,
	// not GetDiskFormat checks or mounting of already-formatted devices
	if err := m.mounter.FormatAndMountSensitiveWithFormatOptions(devicePath, targetPath, fsType, mountOptions, nil, formatArgs); err != nil {
		duration := time.Since(startTime)
		klog.ErrorS(err, "FormatAndMount device failed",
			"device_path", devicePath,
			"target_path", targetPath,
			"fs_type", fsType,
			"duration", duration)

		return fmt.Errorf("failed to format and mount device %s: %w", devicePath, err)
	}

	duration := time.Since(startTime)
	klog.V(1).InfoS("FormatAndMount device completed",
		"device_path", devicePath,
		"target_path", targetPath,
		"fs_type", fsType,
		"duration", duration)

	return nil
}

// Mount mounts a device to the specified target path
func (m *Manager) Mount(ctx context.Context, devicePath, targetPath, fsType string, options []string) error {
	// Create target directory if it doesn't exist
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", targetPath, err)
	}

	// Use k8s mount interface
	if err := m.k8sMounter.Mount(devicePath, targetPath, fsType, options); err != nil {
		return fmt.Errorf("failed to mount %s to %s: %w", devicePath, targetPath, err)
	}

	return nil
}

// Unmount unmounts the specified path
func (m *Manager) Unmount(ctx context.Context, targetPath string) error {
	// Use k8s unmount interface
	if err := m.k8sMounter.Unmount(targetPath); err != nil {
		return fmt.Errorf("failed to unmount %s: %w", targetPath, err)
	}

	return nil
}

// IsMounted checks if a path is mounted
func (m *Manager) IsMounted(targetPath string) (bool, error) {
	// Use k8s IsMountPoint
	notMnt, err := m.k8sMounter.IsLikelyNotMountPoint(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !notMnt, nil
}

// GetFilesystemType detects the filesystem type of a device
func (m *Manager) GetFilesystemType(devicePath string) (string, error) {
	// Use blkid to detect filesystem
	cmd := m.exec.Command("blkid", "-p", "-s", "TYPE", "-o", "value", devicePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Device might not be formatted
		return "", nil
	}

	fsType := strings.TrimSpace(string(output))
	return fsType, nil
}

// IsFormatted checks if a device is formatted with a filesystem
func (m *Manager) IsFormatted(devicePath string) (bool, error) {
	fsType, err := m.GetFilesystemType(devicePath)
	if err != nil {
		// If blkid fails, assume device is not formatted
		return false, nil
	}

	return fsType != "", nil
}

// ResizeFilesystem resizes a filesystem to fill the underlying device
func (m *Manager) ResizeFilesystem(ctx context.Context, devicePath, fsType string) error {
	var cmdName string
	var args []string

	switch fsType {
	case "ext4", "ext3":
		cmdName = "resize2fs"
		args = []string{devicePath}
	case "xfs":
		// For XFS, we need the mount point, not the device
		// This should be called after the device is mounted
		return fmt.Errorf("XFS resize requires mount point, use ResizeFilesystemAtPath instead")
	default:
		return fmt.Errorf("unsupported filesystem type for resize: %s", fsType)
	}

	cmd := m.exec.Command(cmdName, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to resize %s filesystem on %s: %w, output: %s",
			fsType, devicePath, err, string(output))
	}

	return nil
}

// ResizeFilesystemAtPath resizes a filesystem at the given mount path
func (m *Manager) ResizeFilesystemAtPath(ctx context.Context, mountPath, fsType string) error {
	var cmdName string
	var args []string

	switch fsType {
	case "xfs":
		cmdName = "xfs_growfs"
		args = []string{mountPath}
	default:
		return fmt.Errorf("filesystem type %s not supported for path-based resize", fsType)
	}

	cmd := m.exec.Command(cmdName, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to resize %s filesystem at %s: %w, output: %s",
			fsType, mountPath, err, string(output))
	}

	return nil
}

// GetDeviceSize returns the size of a block device in bytes
func (m *Manager) GetDeviceSize(devicePath string) (int64, error) {
	file, err := os.Open(devicePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open device %s: %w", devicePath, err)
	}
	defer file.Close()

	size, err := file.Seek(0, 2) // Seek to end
	if err != nil {
		return 0, fmt.Errorf("failed to seek device %s: %w", devicePath, err)
	}

	return size, nil
}

// SafePathJoin safely joins path components and prevents path traversal
func SafePathJoin(base string, components ...string) (string, error) {
	path := filepath.Join(base, filepath.Join(components...))
	cleanPath := filepath.Clean(path)

	// Ensure the resulting path is still under the base directory
	relPath, err := filepath.Rel(base, cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path traversal detected: %s", relPath)
	}

	return cleanPath, nil
}

// CreateDirectory creates a directory with the specified mode
func CreateDirectory(path string, mode os.FileMode) error {
	return os.MkdirAll(path, mode)
}

// RemoveDirectory removes a directory and all its contents
func RemoveDirectory(path string) error {
	return os.RemoveAll(path)
}

// IsDirectoryEmpty checks if a directory is empty
func IsDirectoryEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// GetMountInfo gets mount information for a path
type MountInfo struct {
	Device     string
	MountPoint string
	FSType     string
	Options    []string
}

// GetMountInfo returns mount information for the given path
func (m *Manager) GetMountInfo(targetPath string) (*MountInfo, error) {
	cmd := m.exec.Command("findmnt", "-n", "-o", "SOURCE,TARGET,FSTYPE,OPTIONS", targetPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get mount info for %s: %w", targetPath, err)
	}

	line := strings.TrimSpace(string(output))
	if line == "" {
		return nil, fmt.Errorf("no mount info found for %s", targetPath)
	}

	fields := strings.Fields(line)
	if len(fields) < 4 {
		return nil, fmt.Errorf("invalid mount info format: %s", line)
	}

	info := &MountInfo{
		Device:     fields[0],
		MountPoint: fields[1],
		FSType:     fields[2],
		Options:    strings.Split(fields[3], ","),
	}

	return info, nil
}

// BindMount creates a bind mount
func (m *Manager) BindMount(ctx context.Context, sourcePath, targetPath string, options []string) error {
	// Create target directory if it doesn't exist
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", targetPath, err)
	}

	// Use k8s mount with bind option
	bindOptions := append([]string{"bind"}, options...)
	if err := m.k8sMounter.Mount(sourcePath, targetPath, "", bindOptions); err != nil {
		return fmt.Errorf("failed to bind mount %s to %s: %w", sourcePath, targetPath, err)
	}

	return nil
}

// GetDiskUsage returns disk usage statistics for a path
type DiskUsage struct {
	Total     uint64
	Available uint64
	Used      uint64
}

// GetDiskUsage returns disk usage for the given path
func (m *Manager) GetDiskUsage(path string) (*DiskUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("failed to get disk usage for %s: %w", path, err)
	}

	total := uint64(stat.Blocks) * uint64(stat.Bsize)
	available := uint64(stat.Bavail) * uint64(stat.Bsize)
	used := total - available

	return &DiskUsage{
		Total:     total,
		Available: available,
		Used:      used,
	}, nil
}
