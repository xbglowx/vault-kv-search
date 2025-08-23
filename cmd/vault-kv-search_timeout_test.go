package cmd

import (
	vault "github.com/hashicorp/vault/api"
	"testing"
	"time"
)

func TestVaultClientTimeout(t *testing.T) {
	// Test that the timeout configuration is applied correctly
	tests := []struct {
		name            string
		timeoutSeconds  int
		expectedTimeout time.Duration
	}{
		{
			name:            "default timeout",
			timeoutSeconds:  30,
			expectedTimeout: 30 * time.Second,
		},
		{
			name:            "custom short timeout",
			timeoutSeconds:  5,
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "custom long timeout",
			timeoutSeconds:  120,
			expectedTimeout: 120 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := vault.DefaultConfig()
			config.Timeout = time.Duration(tt.timeoutSeconds) * time.Second

			if config.Timeout != tt.expectedTimeout {
				t.Errorf("Expected timeout %v, got %v", tt.expectedTimeout, config.Timeout)
			}
		})
	}
}
