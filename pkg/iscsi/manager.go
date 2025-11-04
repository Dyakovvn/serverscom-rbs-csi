package iscsi

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:generate mockgen --destination ../mocks/iscsi_manager.go --package=mocks --source manager.go
type ISCSIManager interface {
	IsLoggedIn(ctx context.Context, target *TargetInfo) (bool, error)
	DiscoverTargets(ctx context.Context, portal string) ([]string, error)
	Login(ctx context.Context, target *TargetInfo) error
	Logout(ctx context.Context, target *TargetInfo) error
	GetDevice(ctx context.Context, target *TargetInfo) (string, error)
	WaitForDevice(ctx context.Context, devicePath string, timeout time.Duration) error
	RescanDevice(ctx context.Context, devicePath string) error
	CleanupTarget(ctx context.Context, target *TargetInfo) error
}

// Manager handles iSCSI operations
type Manager struct {
	iscsiadmPath string
}

// NewManager creates a new iSCSI manager
func NewManager() ISCSIManager {
	return &Manager{
		iscsiadmPath: "/usr/sbin/iscsiadm",
	}
}

// TargetInfo represents iSCSI target information
type TargetInfo struct {
	Portal    string
	IQN       string
	Username  string
	Password  string
	Device    string
	Multipath bool
}

// DiscoverTargets discovers iSCSI targets on the given portal
func (m *Manager) DiscoverTargets(ctx context.Context, portal string) ([]string, error) {
	cmd := exec.CommandContext(ctx, m.iscsiadmPath, "-m", "discovery", "-t", "st", "-p", portal)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to discover targets on portal %s: %w, output: %s", portal, err, string(output))
	}

	var targets []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Parse output format: "portal,target_iqn"
		parts := strings.Split(line, " ")
		if len(parts) >= 2 {
			targets = append(targets, parts[1])
		}
	}

	return targets, nil
}

// Login logs into an iSCSI target
func (m *Manager) Login(ctx context.Context, target *TargetInfo) error {
	// Set CHAP authentication if provided
	if target.Username != "" && target.Password != "" {
		if err := m.setCHAPAuth(ctx, target.Portal, target.IQN, target.Username, target.Password); err != nil {
			return fmt.Errorf("failed to set CHAP authentication: %w", err)
		}
	}

	// Login to target
	cmd := exec.CommandContext(ctx, m.iscsiadmPath, "-m", "node", "-T", target.IQN, "-p", target.Portal, "--login")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to login to target %s: %w, output: %s", target.IQN, err, string(output))
	}

	return nil
}

// Logout logs out from an iSCSI target
func (m *Manager) Logout(ctx context.Context, target *TargetInfo) error {
	cmd := exec.CommandContext(ctx, m.iscsiadmPath, "-m", "node", "-T", target.IQN, "-p", target.Portal, "--logout")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to logout from target %s: %w", target.IQN, err)
	}

	return nil
}

// GetDevice returns the device path for a logged-in iSCSI target
func (m *Manager) GetDevice(ctx context.Context, target *TargetInfo) (string, error) {
	// Wait for device to appear
	timeout := 30 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		devices, err := m.findDevicesByIQN(target.IQN)
		if err != nil {
			return "", err
		}

		if len(devices) > 0 {
			// Return the first device found
			return devices[0], nil
		}

		time.Sleep(1 * time.Second)
	}

	return "", fmt.Errorf("timeout waiting for device to appear for target %s", target.IQN)
}

// IsLoggedIn checks if we're logged into the target
func (m *Manager) IsLoggedIn(ctx context.Context, target *TargetInfo) (bool, error) {
	cmd := exec.CommandContext(ctx, m.iscsiadmPath, "-m", "session")
	output, err := cmd.Output()
	if err != nil {
		// If no sessions exist, iscsiadm returns exit code 21
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 21 {
			return false, nil
		}
		// Also check for "No active sessions" in error message
		if strings.Contains(err.Error(), "No active sessions") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check sessions: %w", err)
	}

	return strings.Contains(string(output), target.IQN), nil
}

// setCHAPAuth sets CHAP authentication for a target
func (m *Manager) setCHAPAuth(ctx context.Context, portal, iqn, username, password string) error {
	// Set authentication method to CHAP
	cmd := exec.CommandContext(ctx, m.iscsiadmPath, "-m", "node", "-T", iqn, "-p", portal,
		"--op=update", "--name", "node.session.auth.authmethod", "--value", "CHAP")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set auth method: %w", err)
	}

	// Set username
	cmd = exec.CommandContext(ctx, m.iscsiadmPath, "-m", "node", "-T", iqn, "-p", portal,
		"--op=update", "--name", "node.session.auth.username", "--value", username)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set username: %w", err)
	}

	// Set password
	cmd = exec.CommandContext(ctx, m.iscsiadmPath, "-m", "node", "-T", iqn, "-p", portal,
		"--op=update", "--name", "node.session.auth.password", "--value", password)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set password: %w", err)
	}

	return nil
}

// findDevicesByIQN finds block devices associated with an iSCSI target IQN
func (m *Manager) findDevicesByIQN(iqn string) ([]string, error) {
	var devices []string

	// Look for devices in /dev/disk/by-path/ that match our IQN
	byPathDir := "/dev/disk/by-path"
	entries, err := os.ReadDir(byPathDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", byPathDir, err)
	}

	// Create regex pattern to match iSCSI devices with our IQN
	pattern := fmt.Sprintf(`ip-.*-iscsi-%s-lun-\d+`, regexp.QuoteMeta(iqn))
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to compile regex pattern: %w", err)
	}

	for _, entry := range entries {
		if regex.MatchString(entry.Name()) {
			// Resolve symlink to get actual device
			devicePath := filepath.Join(byPathDir, entry.Name())
			realDevice, err := filepath.EvalSymlinks(devicePath)
			if err != nil {
				continue
			}
			devices = append(devices, realDevice)
		}
	}

	return devices, nil
}

// WaitForDevice waits for a device to become available
func (m *Manager) WaitForDevice(ctx context.Context, devicePath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(devicePath); err == nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("timeout waiting for device %s", devicePath)
}

// CleanupTarget removes iSCSI target configuration
func (m *Manager) CleanupTarget(ctx context.Context, target *TargetInfo) error {
	// First try to logout if we're logged in
	if loggedIn, err := m.IsLoggedIn(ctx, target); err == nil && loggedIn {
		if err := m.Logout(ctx, target); err != nil {
			return fmt.Errorf("failed to logout from target: %w", err)
		}
	}

	// Remove target node configuration
	cmd := exec.CommandContext(ctx, m.iscsiadmPath, "-m", "node", "-T", target.IQN, "-p", target.Portal, "-o", "delete")
	if err := cmd.Run(); err != nil {
		// Don't fail if the node doesn't exist
		if !strings.Contains(err.Error(), "No records found") {
			return fmt.Errorf("failed to delete target node: %w", err)
		}
	}

	return nil
}

// RescanDevice rescans an iSCSI device to detect size changes
func (m *Manager) RescanDevice(ctx context.Context, devicePath string) error {
	// Extract device name from path (e.g., /dev/sde -> sde)
	deviceName := filepath.Base(devicePath)

	// Rescan the SCSI device
	rescanPath := fmt.Sprintf("/sys/block/%s/device/rescan", deviceName)
	if err := os.WriteFile(rescanPath, []byte("1"), 0644); err != nil {
		return fmt.Errorf("failed to rescan device %s: %w", devicePath, err)
	}

	// Wait a bit for the kernel to process the rescan
	time.Sleep(2 * time.Second)

	return nil
}
