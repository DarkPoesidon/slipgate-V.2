package handlers

import (
	"testing"

	"github.com/anonvector/slipgate/internal/config"
)

func TestAutomaticTunnelDomain(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		backend   string
		want      string
	}{
		{"dnstt socks", config.TransportDNSTT, config.BackendSOCKS, "t.example.com"},
		{"dnstt ssh", config.TransportDNSTT, config.BackendSSH, "ts.example.com"},
		{"slipstream socks", config.TransportSlipstream, config.BackendSOCKS, "s.example.com"},
		{"slipstream ssh", config.TransportSlipstream, config.BackendSSH, "ss.example.com"},
		{"vaydns socks", config.TransportVayDNS, config.BackendSOCKS, "v.example.com"},
		{"vaydns ssh", config.TransportVayDNS, config.BackendSSH, "vs.example.com"},
		{"naive apex", config.TransportNaive, config.BackendSOCKS, "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := automaticTunnelDomain(tt.transport, tt.backend, "Example.COM.")
			if got != tt.want {
				t.Fatalf("automaticTunnelDomain() = %q, want %q", got, tt.want)
			}
		})
	}
}
