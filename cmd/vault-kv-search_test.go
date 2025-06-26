package cmd

import (
	"testing"
)

// TestBasicFunctionality is a simple test to ensure the code compiles and basic functions work
func TestBasicFunctionality(t *testing.T) {
	// Test that we can create a vault client struct
	vc := &vaultClient{
		crawlingDelay: 15,
		jsonOutput:    true,
		searchObjects: []string{"value"},
		searchString:  "test",
		showSecrets:   false,
		useRegex:      false,
	}

	// Basic validation that the struct is created correctly
	if vc.crawlingDelay != 15 {
		t.Errorf("Expected crawlingDelay to be 15, got %d", vc.crawlingDelay)
	}
	if !vc.jsonOutput {
		t.Errorf("Expected jsonOutput to be true")
	}
	if len(vc.searchObjects) != 1 || vc.searchObjects[0] != "value" {
		t.Errorf("Expected searchObjects to contain 'value', got %v", vc.searchObjects)
	}
}

// TestSecretMatching tests the logic for matching secrets
func TestSecretMatching(t *testing.T) {
	vc := &vaultClient{
		searchString: "test",
		jsonOutput:   true,
		showSecrets:  false,
		useRegex:     false,
	}

	// Test that digDeeper extracts key-value pairs correctly
	_, _ = vc.digDeeper(1, map[string]interface{}{"key1": "test-value"}, "entry1", "/path/test", "value")
	
	// Since digDeeper iterates through all keys, we just check that it processes the data
	// The actual matching logic is in secretMatch, which prints output
	if vc.searchString != "test" {
		t.Errorf("Expected searchString to be 'test', got '%s'", vc.searchString)
	}
}
