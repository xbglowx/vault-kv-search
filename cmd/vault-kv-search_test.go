package cmd

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	kv "github.com/hashicorp/vault-plugin-secrets-kv"
	"github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/sdk/logical"
	"github.com/hashicorp/vault/vault"

	vaulthttp "github.com/hashicorp/vault/http"
)

// testVaultServer creates a test vault cluster and returns a configured API
// client and closer function.
func testVaultServer(t *testing.T) (*api.Client, func()) {
	t.Helper()

	client, _, closer := testVaultServerUnseal(t)
	return client, closer
}

// testVaultServerUnseal creates a test vault cluster and returns a configured
// API client, list of unseal keys (as strings), and a closer function.
func testVaultServerUnseal(t *testing.T) (*api.Client, []string, func()) {
	t.Helper()

	return testVaultServerCoreConfig(t, &vault.CoreConfig{
		LogicalBackends: map[string]logical.Factory{
			"kv":    kv.Factory,
			"kv-v2": kv.VersionedKVFactory,
		},
		Logger: hclog.New(&hclog.LoggerOptions{
			Level: hclog.Off,
		}),
	})
}

// testVaultServerCoreConfig creates a new vault cluster with the given core
// configuration. This is a lower-level test helper.
func testVaultServerCoreConfig(t *testing.T, coreConfig *vault.CoreConfig) (*api.Client, []string, func()) {
	t.Helper()

	cluster := vault.NewTestCluster(t, coreConfig, &vault.TestClusterOptions{
		HandlerFunc: vaulthttp.Handler,
	})
	cluster.Start()

	// Make it easy to get access to the active
	core := cluster.Cores[0].Core
	vault.TestWaitActive(t, core)

	// Get the client already setup for us!
	client := cluster.Cores[0].Client
	client.SetToken(cluster.RootToken)

	// Convert the unseal keys to base64 encoded, since these are how the user
	// will get them.
	unsealKeys := make([]string, len(cluster.BarrierKeys))
	for i := range unsealKeys {
		unsealKeys[i] = base64.StdEncoding.EncodeToString(cluster.BarrierKeys[i])
	}

	return client, unsealKeys, func() { defer cluster.Cleanup() }
}

func TestListSecretsMultipleKVStores(t *testing.T) {
	client, closer := testVaultServer(t)
	defer closer()

	sysClient := client.Sys()

	// Create additional logical secret mountpoints for type KVv1
	mountInputKv1 := &api.MountInput{
		Type: "kv-v1",
	}
	sysClient.Mount("test-kv1", mountInputKv1)

	// Create additional logical secret mountpoints for KV type KVv2
	mountInputKv2 := &api.MountInput{
		Type: "kv-v2",
	}
	sysClient.Mount("test-kv2", mountInputKv2)

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
		logical.Write(v.path, data)
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
		logical.Write(v.path, data)
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
	os.Setenv("VAULT_TOKEN", client.Token())
	os.Setenv("VAULT_ADDR", client.Address())
	os.Setenv("VAULT_SKIP_VERIFY", "true")

	// Call the function you want to test
	VaultKvSearch(args, searchObjects, showSecrets, useRegex, crawlingDelay, kvVersion, jsonOutput)

	// Read from the buffer to get the stdout output
	w.Close()
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
