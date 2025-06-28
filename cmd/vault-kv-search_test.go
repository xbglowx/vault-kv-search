package cmd

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/vault/api"
)

// testVaultServer creates a test vault client connected to the Docker container
// and returns a configured API client and closer function.
func testVaultServer(t *testing.T) (*api.Client, func()) {
	t.Helper()

	config := api.DefaultConfig()

	// Use environment variables set by CI or default to localhost for local testing
	vaultAddr := os.Getenv("VAULT_ADDR")
	if vaultAddr == "" {
		vaultAddr = "http://localhost:8200"
	}

	vaultToken := os.Getenv("VAULT_TOKEN")
	if vaultToken == "" {
		vaultToken = "test-token"
	}

	config.Address = vaultAddr
	config.Timeout = time.Second * 30

	client, err := api.NewClient(config)
	if err != nil {
		t.Fatalf("failed to create vault client: %v", err)
	}

	client.SetToken(vaultToken)

	// Wait for vault to be ready
	for i := 0; i < 30; i++ {
		_, err := client.Sys().Health()
		if err == nil {
			break
		}
		if i == 29 {
			t.Fatalf("vault not ready after 30 attempts: %v", err)
		}
		time.Sleep(time.Second)
	}

	return client, func() {
		// Clean up any test data by unmounting test paths
		_ = client.Sys().Unmount("test-kv1")
		_ = client.Sys().Unmount("test-kv2")
	}
}

func TestListSecretsMultipleKVStores(t *testing.T) {
	client, closer := testVaultServer(t)
	defer closer()

	sysClient := client.Sys()

	// Create additional logical secret mountpoints for type KVv1
	mountInputKv1 := &api.MountInput{
		Type: "kv-v1",
	}
	err := sysClient.Mount("test-kv1", mountInputKv1)
	if err != nil {
		t.Log("Failed to mount test-kv1: ", err)
	}

	// Create additional logical secret mountpoints for KV type KVv2
	mountInputKv2 := &api.MountInput{
		Type: "kv-v2",
	}
	err = sysClient.Mount("test-kv2", mountInputKv2)
	if err != nil {
		t.Log("Failed to mount test-kv2 mount: ", err)
	}

	logical := client.Logical()

	// Write KVv1 test data to vault
	testDataKv1 := []struct {
		path  string
		key   string
		value string
	}{
		{"test-kv1/test1", "key1", "data1"},
		{"test-kv1/dir1/test1", "key1", "data1"},
	}

	for _, v := range testDataKv1 {
		data := map[string]interface{}{
			v.key: v.value,
		}
		_, err := logical.Write(v.path, data)
		if err != nil {
			t.Log("Failed to write test data to KVv1: ", err)
		}
	}

	// Write KVv2 test data to vault
	testDataKv2 := []struct {
		path  string
		key   string
		value string
	}{
		{"test-kv2/data/test1", "key1", "data1"},
		{"test-kv2/data/dir1/test1", "key1", "data1"},
	}

	for _, v := range testDataKv2 {
		data := map[string]interface{}{
			"data": map[string]interface{}{
				v.key: v.value,
			},
		}
		_, err := logical.Write(v.path, data)
		if err != nil {
			t.Log("Failed to write test data to KVv2: ", err)
		}
	}

	// Redirect stdout to a buffer
	r, w, _ := os.Pipe()
	os.Stdout = w

	args := []string{"data1"}
	crawlingDelay := 15
	jsonOutput := true
	kvVersion := 0
	searchObjects := []string{"value"}
	showSecrets := false
	useRegex := false

	// Call the function you want to test
	VaultKvSearch(args, searchObjects, showSecrets, useRegex, crawlingDelay, kvVersion, jsonOutput)

	// Read from the buffer to get the stdout output
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	// Create a slice and sort it, so that we can better compares expected vs actual.
	// The output ordering can change between runs, since we are using wait groups
	s := strings.Split(strings.TrimSpace(buf.String()), "\n")
	slices.Sort(s)
	actualOutput := strings.Join(s, ",")

	// Set expected output
	expectedOutput := fmt.Sprintf("%v,%v,%v,%v",
		`{"search":"value","path":"test-kv1/dir1/test1","key":"key1","value":"obfuscated"}`,
		`{"search":"value","path":"test-kv1/test1","key":"key1","value":"obfuscated"}`,
		`{"search":"value","path":"test-kv2/dir1/test1","key":"key1","value":"obfuscated"}`,
		`{"search":"value","path":"test-kv2/test1","key":"key1","value":"obfuscated"}`,
	)

	// Validate actual matches expected
	if actualOutput != expectedOutput {
		t.Errorf("Expected output '%s', but got '%s'", expectedOutput, actualOutput)
	}
}
