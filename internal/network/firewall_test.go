package network

import "testing"

func TestResolverPointsAtStub(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"stub only", "nameserver 127.0.0.53\n", true},
		{"stub with options", "nameserver 127.0.0.53\noptions edns0 trust-ad\n", true},
		{"public resolvers", "nameserver 1.1.1.1\nnameserver 8.8.8.8\n", false},
		{"similar address", "nameserver 127.0.0.54\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolverPointsAtStub(tt.content); got != tt.want {
				t.Fatalf("resolverPointsAtStub() = %v, want %v", got, tt.want)
			}
		})
	}
}
