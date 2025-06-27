package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/vault"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/vault"
)

// testVaultServer creates a test vault cluster and returns a configured API
// client and closer function.
func testVaultServer(t *testing.T) (*api.Client, func()) {
	t.Helper()

	ctx := context.Background()
	vaultContainer, err := vault.RunContainer(ctx,
		testcontainers.WithImage("hashicorp/vault:1.20.0"),
		vault.WithToken("root"),
	)
	require.NoError(t, err)

	// Create a new Vault API client
	client, err := api.NewClient(nil)
	require.NoError(t, err)

	// Set the address and token
	require.NoError(t, client.SetAddress(vaultContainer.Address))
	client.SetToken(vaultContainer.Token)

	return client, func() {
		if err := vaultContainer.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate container: %s", err)
		}
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
