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
	"github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/audit"
	"github.com/hashicorp/vault/builtin/logical/database"
	"github.com/hashicorp/vault/builtin/logical/pki"
	"github.com/hashicorp/vault/builtin/logical/transit"
	"github.com/hashicorp/vault/helper/builtinplugins"
	"github.com/hashicorp/vault/sdk/logical"
	"github.com/hashicorp/vault/vault"

	auditFile "github.com/hashicorp/vault/builtin/audit/file"
	credUserpass "github.com/hashicorp/vault/builtin/credential/userpass"
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
		DisableMlock: true,
		DisableCache: true,
		CredentialBackends: map[string]logical.Factory{
			"userpass": credUserpass.Factory,
		},
		AuditBackends: map[string]audit.Factory{
			"file": auditFile.Factory,
		},
		LogicalBackends: map[string]logical.Factory{
			"database":       database.Factory,
			"generic-leased": vault.LeasedPassthroughBackendFactory,
			"pki":            pki.Factory,
			"transit":        transit.Factory,
		},
		BuiltinRegistry: builtinplugins.Registry,
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

	// Create additional logical secret mountpoints for KV type KVv2
	mountPointsKv := []string{"test0-kv", "test1-kv", "test2-kv"}
	mountPointOptionsKv := api.MountInput{
		Type: "kv",
	}

	for _, mount := range mountPointsKv {
		sysClient.Mount(mount, &mountPointOptionsKv)
	}

	// Create additional logical secret mountpoints for Generic type KVv1
	mountPointsGeneric := []string{"test0-generic", "test1-generic", "test2-generic"}
	mountPointOptionsGeneric := api.MountInput{
		Type: "generic",
	}

	for _, mount := range mountPointsGeneric {
		sysClient.Mount(mount, &mountPointOptionsGeneric)
	}

	testData := []struct {
		path  string
		key   string
		value string
	}{
		{"test0-kv/data/test0", "key0", "data0"},
		{"test1-kv/data/test1", "key1", "data0"},
		{"test2-kv/data/test2", "key2", "data2"},
		{"test0-generic/data/test0", "key0", "data0"},
		{"test1-generic/data/test1", "key1", "data0"},
		{"test2-generic/data/test2", "key2", "data2"},
	}

	// Create some test data
	for _, v := range testData {
		logical := client.Logical()
		data := map[string]interface{}{
			v.key: v.value,
		}
		logical.Write(v.path, data)
	}

	// Redirect stdout to a buffer
	r, w, _ := os.Pipe()
	os.Stdout = w

	args := []string{"data0"}
	searchObjects := []string{"value"}
	showSecrets := false
	useRegex := false
	crawlingDelay := 15
	version := 0
	jsonOutput := true

	os.Setenv("VAULT_TOKEN", client.Token())
	os.Setenv("VAULT_ADDR", client.Address())
	os.Setenv("VAULT_SKIP_VERIFY", "true")

	// Call the function you want to test
	VaultKvSearch(args, searchObjects, showSecrets, useRegex, crawlingDelay, version, jsonOutput)

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
		`{"search":"value","path":"test0-generic/data/test0","key":"key0","value":"obfuscated"}`,
		`{"search":"value","path":"test0-kv/data/test0","key":"key0","value":"obfuscated"}`,
		`{"search":"value","path":"test1-generic/data/test1","key":"key1","value":"obfuscated"}`,
		`{"search":"value","path":"test1-kv/data/test1","key":"key1","value":"obfuscated"}`,
	)

	// Validate actual matches expected
	if actualOutput != expectedOutput {
		t.Errorf("Expected output '%s', but got '%s'", expectedOutput, actualOutput)
	}
}
