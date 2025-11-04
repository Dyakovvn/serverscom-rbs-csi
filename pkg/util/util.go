package util

import (
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"strings"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
)

// GetEnv returns environment variable value or default if not set
func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetRequiredEnv returns environment variable value or fails if not set
func GetRequiredEnv(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("environment variable %s is required", key)
	}
	return value, nil
}

// GenerateVolumeID generates a unique volume ID
func GenerateVolumeID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return fmt.Sprintf("%x", bytes), nil
}

// SanitizeName sanitizes a name to be safe for use in filesystem paths
func SanitizeName(name string) string {
	// Replace invalid characters with underscores
	sanitized := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)

	// Ensure it doesn't start with a number or special character
	if len(sanitized) > 0 && (sanitized[0] >= '0' && sanitized[0] <= '9') {
		sanitized = "v" + sanitized
	}

	// Limit length
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}

	return sanitized
}

// ValidateVolumeCapabilities validates volume capabilities
func ValidateVolumeCapabilities(capabilities []string) error {
	supportedAccessModes := map[string]bool{
		"SINGLE_NODE_WRITER":       true,
		"SINGLE_NODE_READER_ONLY":  true,
		"MULTI_NODE_READER_ONLY":   true,
		"MULTI_NODE_SINGLE_WRITER": false, // Not supported by iSCSI
		"MULTI_NODE_MULTI_WRITER":  false, // Not supported by iSCSI
	}

	for _, capability := range capabilities {
		if supported, exists := supportedAccessModes[capability]; !exists || !supported {
			return fmt.Errorf("unsupported volume capability: %s", capability)
		}
	}

	return nil
}

// ConvertBytesToGB converts bytes to gigabytes
func ConvertBytesToGB(bytes int64) int64 {
	return bytes / (1024 * 1024 * 1024)
}

// ConvertGBToBytes converts gigabytes to bytes
func ConvertGBToBytes(gb int64) int64 {
	return gb * 1024 * 1024 * 1024
}

// RoundUpToGB rounds up bytes to the nearest gigabyte
func RoundUpToGB(bytes int64) int64 {
	gb := ConvertBytesToGB(bytes)
	if bytes%ConvertGBToBytes(1) != 0 {
		gb++
	}
	if gb == 0 {
		gb = 1 // Minimum 1GB
	}
	return gb
}

// IsValidPath checks if a path is valid and safe
func IsValidPath(path string) bool {
	if path == "" {
		return false
	}

	// Check for path traversal attempts
	if strings.Contains(path, "..") {
		return false
	}

	// Must be absolute path
	if !strings.HasPrefix(path, "/") {
		return false
	}

	return true
}

// Retry executes a function with retries
func Retry(attempts int, operation func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		err = operation()
		if err == nil {
			return nil
		}
		if i < attempts-1 {
			// Add some delay between retries if needed
		}
	}
	return err
}

// CreateListener creates a listener for the given endpoint
func CreateListener(endpoint string) (net.Listener, error) {
	proto, addr, err := ParseEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse endpoint: %w", err)
	}

	if proto == "unix" {
		// Remove existing socket file if it exists
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to remove existing socket file: %w", err)
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s://%s: %w", proto, addr, err)
	}

	return listener, nil
}

// ParseEndpoint parses a CSI endpoint into protocol and address
func ParseEndpoint(endpoint string) (string, string, error) {
	if strings.HasPrefix(endpoint, "unix://") {
		return "unix", endpoint[7:], nil
	} else if strings.HasPrefix(endpoint, "tcp://") {
		return "tcp", endpoint[6:], nil
	} else if strings.HasPrefix(endpoint, "unix:") {
		return "unix", endpoint[5:], nil
	}

	// Default to unix socket
	return "unix", endpoint, nil
}

// ExtractPVCUUID extracts the UUID from volumeHandle (pvc-<uuid> -> <uuid>)
// This is used for RBS API labels to store clean UUIDs without the "pvc-" prefix
func ExtractPVCUUID(volumeHandle string) string {
	return strings.TrimPrefix(volumeHandle, "pvc-")
}

// NewScClient returns new client with specified url and token.
// If baseUrl empty returns client with default baseUrl.
func NewScClient(baseUrl, token string) *serverscom.Client {
	if baseUrl == "" {
		return serverscom.NewClient(token)
	}
	return serverscom.NewClientWithEndpoint(token, baseUrl)
}

var sensitiveKeys = []string{
	"pass",
	"password",
	"token",
}

func isSensitive(key string) bool {
	l := strings.ToLower(key)
	for _, pattern := range sensitiveKeys {
		if strings.Contains(l, pattern) {
			return true
		}
	}
	return false
}

func MaskSensitiveMap(data map[string]string) map[string]string {
	masked := make(map[string]string, len(data))
	for k, v := range data {
		if isSensitive(k) {
			masked[k] = "masked"
		} else {
			masked[k] = v
		}
	}
	return masked
}
