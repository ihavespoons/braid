package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DockerNetworkMode controls outbound network policy for Docker sandboxes.
type DockerNetworkMode string

const (
	DockerNetworkRestricted   DockerNetworkMode = "restricted"
	DockerNetworkUnrestricted DockerNetworkMode = "unrestricted"
)

// DockerConfig is loaded from .braid/docker.json.
type DockerConfig struct {
	Network DockerNetwork `json:"network"`
}

// DockerNetwork holds the network-level policy.
type DockerNetwork struct {
	Mode         DockerNetworkMode `json:"mode"`
	AllowedHosts []string          `json:"allowedHosts"`
}

// DefaultDockerConfig returns the built-in docker defaults.
func DefaultDockerConfig() DockerConfig {
	return DockerConfig{
		Network: DockerNetwork{
			Mode:         DockerNetworkRestricted,
			AllowedHosts: []string{},
		},
	}
}

type rawDockerConfig struct {
	Network struct {
		Mode         string   `json:"mode"`
		AllowedHosts []string `json:"allowedHosts"`
	} `json:"network"`
}

// LoadDocker reads .braid/docker.json from projectRoot.
func LoadDocker(projectRoot string) (DockerConfig, error) {
	cfg := DefaultDockerConfig()
	path := filepath.Join(projectRoot, ".braid", "docker.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}

	var raw rawDockerConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("malformed .braid/docker.json: %w", err)
	}

	if raw.Network.Mode == string(DockerNetworkUnrestricted) {
		cfg.Network.Mode = DockerNetworkUnrestricted
	}
	if raw.Network.AllowedHosts != nil {
		hosts := make([]string, 0, len(raw.Network.AllowedHosts))
		for _, h := range raw.Network.AllowedHosts {
			if h != "" {
				hosts = append(hosts, h)
			}
		}
		cfg.Network.AllowedHosts = hosts
	}

	return cfg, nil
}
