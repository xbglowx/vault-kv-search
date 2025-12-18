//go:build integration

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/vault/api"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testVaultServerWithTestcontainers creates a test vault client connected to a Vault container
// started by testcontainers-go.
func testVaultServerWithTestcontainers(t *testing.T) (*api.Client, func()) {
	t.Helper()

	ctx := context.Background()

	// Use generic container instead of vault module to avoid initialization issues
	req := testcontainers.ContainerRequest{
		Image:        "hashicorp/vault:1.15.6",
		ExposedPorts: []string{"8200/tcp"},
		Env: map[string]string{
			"VAULT_DEV_ROOT_TOKEN_ID":  "test-token",
			"VAULT_DEV_LISTEN_ADDRESS": "0.0.0.0:8200",
		},
		Cmd:        []string{"vault", "server", "-dev"},
		WaitingFor: wait.ForHTTP("/v1/sys/health").WithPort("8200/tcp"),
	}

	vaultContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start vault container: %v", err)
	}

	// Get the container's mapped port and build HTTP address
	mappedPort, err := vaultContainer.MappedPort(ctx, "8200")
	if err != nil {
		t.Fatalf("failed to get mapped port: %v", err)
	}

	host, err := vaultContainer.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}

	config := api.DefaultConfig()
	config.Address = fmt.Sprintf("http://%s:%s", host, mappedPort.Port())
	config.Timeout = time.Second * 30

	client, err := api.NewClient(config)
	if err != nil {
		t.Fatalf("failed to create vault client: %v", err)
	}

	client.SetToken("test-token")

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
		if err := vaultContainer.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate vault container: %v", err)
		}
	}
}

func TestListSecretsMultipleKVStores(t *testing.T) {
	client, closer := testVaultServerWithTestcontainers(t)
	defer closer()

	sysClient := client.Sys()

	// Create additional logical secret mountpoints for type KVv1
	mountInputKv1 := &api.MountInput{
		Type: "kv",
		Options: map[string]string{
			"version": "1",
		},
	}
	err := sysClient.Mount("test-kv1", mountInputKv1)
	if err != nil {
		t.Fatalf("Failed to mount test-kv1: %v", err)
	}

	// Create additional logical secret mountpoints for KV type KVv2
	mountInputKv2 := &api.MountInput{
		Type: "kv",
		Options: map[string]string{
			"version": "2",
		},
	}
	err = sysClient.Mount("test-kv2", mountInputKv2)
	if err != nil {
		t.Fatalf("Failed to mount test-kv2 mount: %v", err)
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
			t.Fatalf("Failed to write test data to KVv1: %v", err)
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
			t.Fatalf("Failed to write test data to KVv2: %v", err)
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

	// Configure the vault client environment variables
	if err := os.Setenv("VAULT_TOKEN", client.Token()); err != nil {
		t.Fatalf("failed to set VAULT_TOKEN: %v", err)
	}
	if err := os.Setenv("VAULT_ADDR", client.Address()); err != nil {
		t.Fatalf("failed to set VAULT_ADDR: %v", err)
	}
	if err := os.Setenv("VAULT_SKIP_VERIFY", "true"); err != nil {
		t.Fatalf("failed to set VAULT_SKIP_VERIFY: %v", err)
	}

	// Call the function you want to test
	VaultKvSearch(args, searchObjects, showSecrets, useRegex, crawlingDelay, kvVersion, jsonOutput, 30)

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

func TestListSecretsMultipleKVStoresWithRegex(t *testing.T) {
	client, closer := testVaultServerWithTestcontainers(t)
	defer closer()

	sysClient := client.Sys()

	// Create additional logical secret mountpoints for type KVv1
	mountInputKv1 := &api.MountInput{
		Type: "kv",
		Options: map[string]string{
			"version": "1",
		},
	}
	err := sysClient.Mount("test-kv1", mountInputKv1)
	if err != nil {
		t.Log("Failed to mount test-kv1: ", err)
	}

	logical := client.Logical()

	// Write KVv1 test data to vault
	testDataKv1 := []struct {
		path  string
		key   string
		value string
	}{
		{"test-kv1/test1", "key1", "data1"},
		{"test-kv1/dir1/test1", "key1", "foo-data-bar"},
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

	// Redirect stdout to a buffer
	r, w, _ := os.Pipe()
	os.Stdout = w

	args := []string{"^foo-"}
	crawlingDelay := 15
	jsonOutput := true
	kvVersion := 1
	searchObjects := []string{"value"}
	showSecrets := false
	useRegex := true

	// Configure the vault client
	if err := os.Setenv("VAULT_TOKEN", client.Token()); err != nil {
		t.Fatalf("failed to set VAULT_TOKEN: %v", err)
	}
	if err := os.Setenv("VAULT_ADDR", client.Address()); err != nil {
		t.Fatalf("failed to set VAULT_ADDR: %v", err)
	}
	if err := os.Setenv("VAULT_SKIP_VERIFY", "true"); err != nil {
		t.Fatalf("failed to set VAULT_SKIP_VERIFY: %v", err)
	}

	// Call the function you want to test
	VaultKvSearch(args, searchObjects, showSecrets, useRegex, crawlingDelay, kvVersion, jsonOutput, 30)

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
	expectedOutput := `{"search":"value","path":"test-kv1/dir1/test1","key":"key1","value":"obfuscated"}`

	// Validate actual matches expected
	if actualOutput != expectedOutput {
		t.Errorf("Expected output '%s', but got '%s'", expectedOutput, actualOutput)
	}
}

func TestNestedMapSearch(t *testing.T) {
	client, closer := testVaultServerWithTestcontainers(t)
	defer closer()

	sysClient := client.Sys()

	// Create a KVv2 mount for testing nested maps with a unique name
	mountPath := fmt.Sprintf("test-nested-%d", time.Now().Unix())
	mountInputKv2 := &api.MountInput{
		Type: "kv",
		Options: map[string]string{
			"version": "2",
		},
	}
	err := sysClient.Mount(mountPath, mountInputKv2)
	if err != nil {
		t.Fatalf("Failed to mount %s: %v", mountPath, err)
	}

	logical := client.Logical()

	// Write nested map data similar to Spring configuration
	nestedData := map[string]interface{}{
		"data": map[string]interface{}{
			"mongodb": map[string]interface{}{
				"key1": map[string]interface{}{
					"database": "databasename1",
					"uri":      "connectionstring2.database.org",
				},
				"key2": map[string]interface{}{
					"database": "databasename2",
					"uri":      "connectionstring2.database.org",
				},
			},
		},
	}

	secretPath := fmt.Sprintf("%s/data/config", mountPath)
	_, err = logical.Write(secretPath, nestedData)
	if err != nil {
		t.Fatalf("Failed to write nested test data: %v", err)
	}

	// Redirect stdout to a buffer
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Search for "connectionstring2" which should be found in both key1.uri and key2.uri
	args := []string{mountPath + "/", "connectionstring2"}
	crawlingDelay := 15
	jsonOutput := true
	kvVersion := 2
	searchObjects := []string{"value"}
	showSecrets := true
	useRegex := false

	// Configure the vault client environment variables
	if err := os.Setenv("VAULT_TOKEN", client.Token()); err != nil {
		t.Fatalf("failed to set VAULT_TOKEN: %v", err)
	}
	if err := os.Setenv("VAULT_ADDR", client.Address()); err != nil {
		t.Fatalf("failed to set VAULT_ADDR: %v", err)
	}
	if err := os.Setenv("VAULT_SKIP_VERIFY", "true"); err != nil {
		t.Fatalf("failed to set VAULT_SKIP_VERIFY: %v", err)
	}

	// Call the function you want to test
	VaultKvSearch(args, searchObjects, showSecrets, useRegex, crawlingDelay, kvVersion, jsonOutput, 30)

	// Read from the buffer to get the stdout output
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	actualOutput := strings.TrimSpace(buf.String())

	// Log the actual output to see what we're getting
	t.Logf("Actual output: %s", actualOutput)

	// We should find "connectionstring2" in both uri fields
	// The exact output format will depend on how the search traverses the nested structure
	if !strings.Contains(actualOutput, "connectionstring2") {
		t.Errorf("Expected to find 'connectionstring2' in nested map search, but got: %s", actualOutput)
	}

	// Check that we get results (not empty)
	if actualOutput == "" {
		t.Error("Expected search results but got empty output")
	}

	// Count how many matches we should have - there should be 2 uri fields with connectionstring2
	matches := strings.Count(actualOutput, "connectionstring2")
	if matches < 2 {
		t.Errorf("Expected at least 2 matches for 'connectionstring2' but got %d. Output: %s", matches, actualOutput)
	}
}
