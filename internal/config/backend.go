package config

// Backend types.
const (
	BackendSOCKS = "socks"
	BackendSSH   = "ssh"
)

// BackendConfig defines a backend service.
type BackendConfig struct {
	Tag     string       `json:"tag"`
	Type    string       `json:"type"`
	Address string       `json:"address"`
	SOCKS   *SOCKSConfig `json:"socks,omitempty"`
}

// SOCKSConfig holds SOCKS-specific settings.
type SOCKSConfig struct {
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}
