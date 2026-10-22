package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/time/rate"
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
	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()
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
	if err := VaultKvSearch(args, searchObjects, showSecrets, useRegex, crawlingDelay, kvVersion, jsonOutput, 30); err != nil {
		t.Fatalf("VaultKvSearch failed: %v", err)
	}

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
	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()
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
	if err := VaultKvSearch(args, searchObjects, showSecrets, useRegex, crawlingDelay, kvVersion, jsonOutput, 30); err != nil {
		t.Fatalf("VaultKvSearch failed: %v", err)
	}

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
	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()
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
	if err := VaultKvSearch(args, searchObjects, showSecrets, useRegex, crawlingDelay, kvVersion, jsonOutput, 30); err != nil {
		t.Fatalf("VaultKvSearch failed: %v", err)
	}

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

func TestIgnoreCaseAndExitCode(t *testing.T) {
	client, closer := testVaultServerWithTestcontainers(t)
	defer closer()

	mountPath := "test-ic-kv1"
	if err := client.Sys().Mount(mountPath, &api.MountInput{
		Type:    "kv",
		Options: map[string]string{"version": "1"},
	}); err != nil {
		t.Fatalf("Failed to mount %s: %v", mountPath, err)
	}

	if _, err := client.Logical().Write(mountPath+"/AppConfig", map[string]interface{}{
		"ApiKey": "SecRetValUe123",
	}); err != nil {
		t.Fatalf("Failed to write test secret: %v", err)
	}

	t.Setenv("VAULT_TOKEN", client.Token())
	t.Setenv("VAULT_ADDR", client.Address())
	t.Setenv("VAULT_SKIP_VERIFY", "true")

	discovered := discoverMountsForInteractive(30)
	if len(discovered) == 0 {
		t.Error("Expected discoverMountsForInteractive to return at least 1 mount")
	}

	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	r, w, _ := os.Pipe()
	os.Stdout = w

	err := VaultKvSearchWithOptions(SearchOptions{
		Args:           []string{mountPath + "/", "secretvalue123"},
		SearchObjects:  []string{"value"},
		ShowSecrets:    true,
		IgnoreCase:     true,
		Concurrency:    4,
		KvVersion:      1,
		JSONOutput:     true,
		TimeoutSeconds: 30,
		ExitCode:       true,
	})
	_ = w.Close()
	if err != nil {
		t.Fatalf("Expected case-insensitive search to succeed, got: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if !strings.Contains(buf.String(), `"value":"SecRetValUe123"`) {
		t.Errorf("Expected matched secret in output, got: %s", buf.String())
	}

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	err = VaultKvSearchWithOptions(SearchOptions{
		Args:           []string{"/" + mountPath, "^apikey|appconfig$"},
		SearchObjects:  []string{"key", "path"},
		ShowSecrets:    true,
		UseRegex:       true,
		IgnoreCase:     true,
		Concurrency:    2,
		KvVersion:      0,
		JSONOutput:     false,
		ForceColor:     true,
		TimeoutSeconds: 30,
		ExitCode:       true,
	})
	_ = wOut.Close()
	_ = wErr.Close()
	if err != nil {
		t.Fatalf("Expected regex case-insensitive search to succeed, got: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	_, _ = outBuf.ReadFrom(rOut)
	_, _ = errBuf.ReadFrom(rErr)
	if !strings.Contains(outBuf.String(), ansiBoldRed+"ApiKey"+ansiReset) {
		t.Errorf("Expected highlighted key match in stdout, got: %s", outBuf.String())
	}
	if !strings.Contains(errBuf.String(), "Summary: 2 match(es) across 1 secret(s)") {
		t.Errorf("Expected completion summary on stderr, got: %s", errBuf.String())
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	os.Stderr = origStderr
	err = VaultKvSearchWithOptions(SearchOptions{
		Args:           []string{mountPath + "/", "nonexistent-value"},
		SearchObjects:  []string{"value"},
		IgnoreCase:     true,
		Concurrency:    4,
		KvVersion:      1,
		JSONOutput:     true,
		TimeoutSeconds: 30,
		ExitCode:       true,
	})
	_ = w2.Close()
	_, _ = io.Copy(io.Discard, r2)
	if !errors.Is(err, ErrNoMatches) {
		t.Errorf("Expected ErrNoMatches when ExitCode is true and no matches found, got: %v", err)
	}
}

func TestGracefulPermissionDenied(t *testing.T) {
	client, closer := testVaultServerWithTestcontainers(t)
	defer closer()

	mountPath := "test-perm-kv2"
	if err := client.Sys().Mount(mountPath, &api.MountInput{
		Type:    "kv",
		Options: map[string]string{"version": "2"},
	}); err != nil {
		t.Fatalf("Failed to mount %s: %v", mountPath, err)
	}

	if _, err := client.Logical().Write(mountPath+"/data/allowed", map[string]interface{}{
		"data": map[string]interface{}{"key1": "findme"},
	}); err != nil {
		t.Fatalf("Failed to write allowed secret: %v", err)
	}
	if _, err := client.Logical().Write(mountPath+"/data/forbidden", map[string]interface{}{
		"data": map[string]interface{}{"key1": "findme"},
	}); err != nil {
		t.Fatalf("Failed to write forbidden secret: %v", err)
	}
	if _, err := client.Logical().Write(mountPath+"/data/restricted-dir/secret1", map[string]interface{}{
		"data": map[string]interface{}{"key1": "findme"},
	}); err != nil {
		t.Fatalf("Failed to write restricted-dir secret: %v", err)
	}
	if _, err := client.Logical().Write(mountPath+"/data/deleted-only", map[string]interface{}{
		"data": map[string]interface{}{"key1": "findme"},
	}); err != nil {
		t.Fatalf("Failed to write deleted-only secret: %v", err)
	}
	if _, err := client.Logical().Delete(mountPath + "/data/deleted-only"); err != nil {
		t.Fatalf("Failed to soft-delete secret: %v", err)
	}

	policy := fmt.Sprintf(`
path "%s/metadata/*" {
  capabilities = ["list"]
}
path "%s/metadata/restricted-dir/*" {
  capabilities = ["deny"]
}
path "%s/data/allowed" {
  capabilities = ["read"]
}
path "%s/data/deleted-only" {
  capabilities = ["read"]
}
path "%s/data/forbidden" {
  capabilities = ["deny"]
}
`, mountPath, mountPath, mountPath, mountPath, mountPath)

	if err := client.Sys().PutPolicy("partial-read", policy); err != nil {
		t.Fatalf("Failed to create policy: %v", err)
	}

	tokenSecret, err := client.Auth().Token().Create(&api.TokenCreateRequest{
		Policies:        []string{"partial-read"},
		NoDefaultPolicy: true,
	})
	if err != nil {
		t.Fatalf("Failed to create restricted token: %v", err)
	}

	t.Setenv("VAULT_TOKEN", tokenSecret.Auth.ClientToken)
	t.Setenv("VAULT_ADDR", client.Address())
	t.Setenv("VAULT_SKIP_VERIFY", "true")

	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	r, w, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = wErr

	err = VaultKvSearchWithOptions(SearchOptions{
		Args:           []string{mountPath + "/", "findme"},
		SearchObjects:  []string{"value"},
		Concurrency:    2,
		KvVersion:      2,
		JSONOutput:     false,
		TimeoutSeconds: 30,
	})
	_ = w.Close()
	_ = wErr.Close()
	if err != nil {
		t.Fatalf("Expected search to skip 403 paths and succeed, got: %v", err)
	}

	var buf, errBuf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_, _ = errBuf.ReadFrom(rErr)
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "test-perm-kv2/allowed") {
		t.Errorf("Expected allowed secret in output, got: %s", out)
	}
	if strings.Contains(out, "forbidden") {
		t.Errorf("Did not expect forbidden secret in output, got: %s", out)
	}
	if !strings.Contains(errBuf.String(), "path(s) skipped") {
		t.Errorf("Expected skipped paths count in stderr summary, got: %s", errBuf.String())
	}

	rDel, wDel, _ := os.Pipe()
	rDelErr, wDelErr, _ := os.Pipe()
	os.Stdout = wDel
	os.Stderr = wDelErr
	err = VaultKvSearchWithOptions(SearchOptions{
		Args:           []string{mountPath + "/", "deleted-only"},
		SearchObjects:  []string{"path"},
		Concurrency:    2,
		KvVersion:      2,
		JSONOutput:     false,
		TimeoutSeconds: 30,
		ExitCode:       true,
	})
	_ = wDel.Close()
	_ = wDelErr.Close()
	_, _ = io.Copy(io.Discard, rDel)
	var delErrBuf bytes.Buffer
	_, _ = delErrBuf.ReadFrom(rDelErr)
	if !errors.Is(err, ErrNoMatches) {
		t.Errorf("Expected ErrNoMatches when only soft-deleted KV v2 secret matches path, got: %v", err)
	}
	if !strings.Contains(delErrBuf.String(), "path(s) skipped") {
		t.Errorf("Expected soft-deleted secret to be counted as skipped, got stderr: %s", delErrBuf.String())
	}
}

func TestColorHighlighting(t *testing.T) {
	highlighted := highlightSubstring("foo-Bar-baz-BAR", "bar", true)
	expected := "foo-" + ansiBoldRed + "Bar" + ansiReset + "-baz-" + ansiBoldRed + "BAR" + ansiReset
	if highlighted != expected {
		t.Errorf("Expected %q, got %q", expected, highlighted)
	}

	for _, tc := range []struct {
		text   string
		substr string
		want   string
	}{
		{text: "Kx", substr: "x", want: "K" + ansiBoldRed + "x" + ansiReset},
		{text: "İx", substr: "x", want: "İ" + ansiBoldRed + "x" + ansiReset},
		{text: "τελικός", substr: "σ", want: "τελικό" + ansiBoldRed + "ς" + ansiReset},
	} {
		got := highlightSubstring(tc.text, tc.substr, true)
		if !utf8.ValidString(got) {
			t.Errorf("highlightSubstring(%q, %q, true) produced invalid UTF-8: %q", tc.text, tc.substr, got)
		}
		if got != tc.want {
			t.Errorf("highlightSubstring(%q, %q, true) = %q, want %q", tc.text, tc.substr, got, tc.want)
		}
	}

	if !containsCaseFold("τελικός", "σ") || !containsCaseFold("ſecret", "s") {
		t.Error("Expected Unicode case folding to match equivalent characters")
	}

	if got := highlightSubstring("\xffBar", "bar", true); got != "\xff"+ansiBoldRed+"Bar"+ansiReset {
		t.Errorf("Unexpected highlight with invalid UTF-8 prefix: %q", got)
	}

	if got := highlightSubstring("hello", "", false); got != "hello" {
		t.Errorf("Expected unchanged string on empty substr, got %q", got)
	}
	if got := highlightSubstring("hello", "xyz", false); got != "hello" {
		t.Errorf("Expected unchanged string on non-matching substr, got %q", got)
	}

	vcRegex := &vaultClient{
		useColor:    true,
		searchRegex: regexp.MustCompile(`(?i)bar[0-9]+`),
	}
	if got := vcRegex.highlightMatch("prefix-Bar42-suffix"); got != "prefix-"+ansiBoldRed+"Bar42"+ansiReset+"-suffix" {
		t.Errorf("Unexpected regex highlight: %q", got)
	}

	vcPathBasename := &vaultClient{
		useColor:    true,
		searchRegex: regexp.MustCompile(`^app$`),
	}
	if got := vcPathBasename.highlightPathMatch("secret/app"); got != "secret/"+ansiBoldRed+"app"+ansiReset {
		t.Errorf("Expected basename path highlight, got: %q", got)
	}

	vcSubstr := &vaultClient{
		useColor:     true,
		ignoreCase:   true,
		searchString: "bar",
	}
	if got := vcSubstr.highlightMatch("foo-Bar-baz"); got != "foo-"+ansiBoldRed+"Bar"+ansiReset+"-baz" {
		t.Errorf("Unexpected plain substring highlight: %q", got)
	}

	vcNoColor := &vaultClient{
		useColor:     false,
		searchString: "Bar",
	}
	if got := vcNoColor.highlightMatch("foo-Bar-baz"); got != "foo-Bar-baz" {
		t.Errorf("Expected uncolored output when useColor is false, got %q", got)
	}

	if shouldUseColor(SearchOptions{JSONOutput: true, ForceColor: true}) {
		t.Error("Expected shouldUseColor to be false when JSONOutput is true")
	}
	if shouldUseColor(SearchOptions{NoColor: true, ForceColor: true}) {
		t.Error("Expected shouldUseColor to be false when NoColor is true")
	}
	if !shouldUseColor(SearchOptions{ForceColor: true}) {
		t.Error("Expected shouldUseColor to be true when ForceColor is true")
	}
	t.Setenv("NO_COLOR", "1")
	if shouldUseColor(SearchOptions{}) {
		t.Error("Expected shouldUseColor to be false when NO_COLOR is set")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if shouldUseColor(SearchOptions{}) {
		t.Error("Expected shouldUseColor to be false when TERM=dumb")
	}
}

func TestInteractivePrompt(t *testing.T) {
	mounts := []startPathInfo{
		{path: "kv1/", kvVersion: 0},
		{path: "secret/", kvVersion: 2},
	}

	t.Run("select numbered mount and enable flags", func(t *testing.T) {
		input := strings.NewReader("2\ndb-password\nkey,value\ny\nn\ny\n")
		var out bytes.Buffer
		opts := SearchOptions{SearchObjects: []string{"value"}}

		if err := runInteractivePrompt(input, &out, &opts, mounts); err != nil {
			t.Fatalf("runInteractivePrompt failed: %v", err)
		}
		if !strings.Contains(out.String(), "[1] kv1/ (KV v1)") {
			t.Errorf("Expected legacy mount with version 0 to display as KV v1, got: %s", out.String())
		}
		if !slices.Equal(opts.Args, []string{"secret/", "db-password"}) {
			t.Errorf("Unexpected Args: %v", opts.Args)
		}
		if opts.KvVersion != 2 {
			t.Errorf("Expected KvVersion 2, got %d", opts.KvVersion)
		}
		if !slices.Equal(opts.SearchObjects, []string{"key", "value"}) {
			t.Errorf("Unexpected SearchObjects: %v", opts.SearchObjects)
		}
		if !opts.IgnoreCase || opts.UseRegex || !opts.ShowSecrets {
			t.Errorf("Unexpected boolean flags: %+v", opts)
		}
	})

	t.Run("select default all stores when mounts empty", func(t *testing.T) {
		input := strings.NewReader("\nmy-token\n\nn\ny\nn\n")
		var out bytes.Buffer
		opts := SearchOptions{SearchObjects: []string{"path"}}

		if err := runInteractivePrompt(input, &out, &opts, nil); err != nil {
			t.Fatalf("runInteractivePrompt failed: %v", err)
		}
		if !strings.Contains(out.String(), "[path]:") {
			t.Errorf("Expected prompt to display current target [path], got: %s", out.String())
		}
		if !slices.Equal(opts.Args, []string{"my-token"}) {
			t.Errorf("Expected single arg for all KV stores, got: %v", opts.Args)
		}
		if !opts.UseRegex {
			t.Error("Expected UseRegex to be true")
		}
	})

	t.Run("custom path when mounts listed", func(t *testing.T) {
		input := strings.NewReader("custom/path/\nmy-secret\npath\nn\nn\nn\n")
		var out bytes.Buffer
		opts := SearchOptions{SearchObjects: []string{"value"}}

		if err := runInteractivePrompt(input, &out, &opts, mounts); err != nil {
			t.Fatalf("runInteractivePrompt failed: %v", err)
		}
		if !slices.Equal(opts.Args, []string{"custom/path/", "my-secret"}) {
			t.Errorf("Unexpected Args: %v", opts.Args)
		}
		if !slices.Equal(opts.SearchObjects, []string{"path"}) {
			t.Errorf("Unexpected SearchObjects: %v", opts.SearchObjects)
		}
	})

	t.Run("empty query returns error", func(t *testing.T) {
		input := strings.NewReader("0\n\n")
		var out bytes.Buffer
		opts := SearchOptions{SearchObjects: []string{"value"}}

		if err := runInteractivePrompt(input, &out, &opts, mounts); err == nil {
			t.Error("Expected error for empty query, got nil")
		}
	})

	t.Run("preserve whitespace in search queries", func(t *testing.T) {
		for _, q := range []string{"  leading", "trailing  ", " "} {
			input := strings.NewReader("0\n" + q + "\n\nn\nn\nn\n")
			var out bytes.Buffer
			opts := SearchOptions{SearchObjects: []string{"value"}}
			if err := runInteractivePrompt(input, &out, &opts, mounts); err != nil {
				t.Fatalf("runInteractivePrompt(%q) failed: %v", q, err)
			}
			if !slices.Equal(opts.Args, []string{q}) {
				t.Errorf("Expected query %q to be preserved, got Args: %q", q, opts.Args)
			}
		}
	})

	t.Run("invalid search target returns error", func(t *testing.T) {
		input := strings.NewReader("0\nfoo\ninvalid_target\n")
		var out bytes.Buffer
		opts := SearchOptions{SearchObjects: []string{"value"}}

		if err := runInteractivePrompt(input, &out, &opts, mounts); err == nil {
			t.Error("Expected error for invalid search target, got nil")
		}
	})
}

func TestInputValidationAndHelpers(t *testing.T) {
	if err := validateSearchObjects([]string{"key", "value", "path"}); err != nil {
		t.Errorf("Expected valid search objects, got error: %v", err)
	}
	if err := validateSearchObjects([]string{"bogus"}); err == nil {
		t.Error("Expected error for invalid search object, got nil")
	}

	if isPermissionDenied(nil) {
		t.Error("Expected false for nil error")
	}
	if !isPermissionDenied(&api.ResponseError{StatusCode: 403}) {
		t.Error("Expected true for 403 ResponseError")
	}
	if isPermissionDenied(&api.ResponseError{StatusCode: 500, Errors: []string{"permission denied"}}) {
		t.Error("Expected false for 500 ResponseError even when body mentions permission denied")
	}
	if !isPermissionDenied(errors.New("Code: 403. permission denied")) {
		t.Error("Expected true for Code: 403 message")
	}
	if isPermissionDenied(errors.New("dial tcp: connect: permission denied")) {
		t.Error("Expected false for transport permission denied error")
	}
	if isPermissionDenied(errors.New("network timeout")) {
		t.Error("Expected false for network timeout")
	}

	ctx, cancel := context.WithCancel(context.Background())
	vc := &vaultClient{
		ctx:         ctx,
		cancel:      cancel,
		rateLimiter: rate.NewLimiter(rate.Every(10*time.Second), 1),
		sem:         make(chan struct{}, 2),
	}
	vc.queueCond = sync.NewCond(&vc.queueMu)
	_ = vc.rateLimiter.Wait(ctx)
	go func() {
		time.Sleep(20 * time.Millisecond)
		vc.setErr(errors.New("fatal failure"))
	}()
	start := time.Now()
	if err := vc.withWorker(func() error { return nil }); err == nil || err.Error() != "fatal failure" {
		t.Errorf("Expected withWorker to abort with fatal failure, got: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("Expected limiter wait to cancel promptly on setErr, took %s", time.Since(start))
	}

	vcQueue := &vaultClient{}
	vcQueue.queueCond = sync.NewCond(&vcQueue.queueMu)
	vcQueue.enqueueTasks(
		[]searchTask{{isDir: true, fullPath: "secret/sub1/"}, {isDir: true, fullPath: "secret/sub2/"}},
		[]searchTask{{isDir: false, fullPath: "secret/leaf1"}, {isDir: false, fullPath: "secret/leaf2"}},
	)
	if len(vcQueue.queue) != 4 || vcQueue.queue[len(vcQueue.queue)-1].fullPath != "secret/leaf1" {
		t.Errorf("Expected leaf1 at top of LIFO stack, got queue: %+v", vcQueue.queue)
	}

	if isTerminal(nil) {
		t.Error("Expected isTerminal(nil) to be false")
	}
	r, w, _ := os.Pipe()
	_ = r.Close()
	_ = w.Close()
	if isTerminal(r) {
		t.Error("Expected isTerminal on closed pipe to be false")
	}

	origSearchObjects := searchObjects
	origConcurrency := concurrency
	origInteractive := interactive
	defer func() {
		searchObjects = origSearchObjects
		concurrency = origConcurrency
		interactive = origInteractive
	}()

	cmd := &cobra.Command{}
	var cmdOut bytes.Buffer
	cmd.SetOut(&cmdOut)

	searchObjects = []string{"value"}
	concurrency = 0
	if err := checkInputs(cmd, []string{"foo"}); err == nil {
		t.Error("Expected error when concurrency < 1")
	}

	concurrency = 4
	interactive = false
	if err := checkInputs(cmd, []string{}); err == nil {
		t.Error("Expected error when 0 args and non-interactive")
	}

	if err := checkInputs(cmd, []string{"foo"}); err != nil {
		t.Errorf("Expected 1 arg to pass checkInputs, got: %v", err)
	}
	if !strings.Contains(cmdOut.String(), "searching all KV stores") {
		t.Errorf("Expected warning for 1 positional arg, got: %s", cmdOut.String())
	}

	if err := VaultKvSearchWithOptions(SearchOptions{}); err == nil {
		t.Error("Expected error when Args is empty")
	}
	t.Setenv("VAULT_TOKEN", "test-token")
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:8200")
	if err := VaultKvSearchWithOptions(SearchOptions{
		Args:     []string{"secret/", "[invalid-regex"},
		UseRegex: true,
	}); err == nil {
		t.Error("Expected error for invalid regex")
	}

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var failMode atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		for {
			prev := maxInFlight.Load()
			if cur <= prev || maxInFlight.CompareAndSwap(prev, cur) {
				break
			}
		}
		defer inFlight.Add(-1)

		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/metadata/") || strings.HasSuffix(r.URL.Path, "/metadata") {
			_, _ = w.Write([]byte(`{"data":{"keys":["s1","s2","s3","s4","s5","s6"]}}`))
			return
		}
		if failMode.Load() {
			if strings.HasSuffix(r.URL.Path, "/s1") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":["fatal backend failure"]}`))
				return
			}
			<-r.Context().Done()
			return
		}
		time.Sleep(25 * time.Millisecond)
		_, _ = w.Write([]byte(`{"data":{"data":{"k":"v"}}}`))
	}))
	defer srv.Close()

	t.Setenv("VAULT_ADDR", srv.URL)
	if err := VaultKvSearchWithOptions(SearchOptions{
		Args:           []string{"secret/", "nomatch"},
		SearchObjects:  []string{"value"},
		Concurrency:    2,
		CrawlingDelay:  0,
		KvVersion:      2,
		JSONOutput:     true,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("Unexpected error during concurrency test: %v", err)
	}
	if got := maxInFlight.Load(); got < 1 || got > 2 {
		t.Errorf("Expected max in-flight requests between 1 and 2, got %d", got)
	}

	failMode.Store(true)
	fatalStart := time.Now()
	err := VaultKvSearchWithOptions(SearchOptions{
		Args:           []string{"secret/", "nomatch"},
		SearchObjects:  []string{"value"},
		Concurrency:    4,
		CrawlingDelay:  0,
		KvVersion:      2,
		JSONOutput:     true,
		TimeoutSeconds: 10,
	})
	if err == nil || !strings.Contains(err.Error(), "fatal backend failure") {
		t.Errorf("Expected original fatal backend failure, got: %v", err)
	}
	if elapsed := time.Since(fatalStart); elapsed > 2*time.Second {
		t.Errorf("Expected fatal shutdown to cancel in-flight requests promptly, took %s", elapsed)
	}
}
