package cmd

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestVaultKvSearchInvalidRegex(t *testing.T) {
	t.Setenv("VAULT_TOKEN", "test-token")
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:8200")

	tests := []struct {
		name     string
		args     []string
		useRegex bool
	}{
		{
			name:     "invalid regex unclosed bracket single arg",
			args:     []string{"[a-z"},
			useRegex: true,
		},
		{
			name:     "invalid regex unclosed paren with path arg",
			args:     []string{"secret/", "(unclosed"},
			useRegex: true,
		},
		{
			name:     "invalid regex dangling quantifier",
			args:     []string{"secret/", "+invalid"},
			useRegex: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VaultKvSearch(tt.args, []string{"value"}, false, tt.useRegex, 0, 1, true, 1)
			if err == nil {
				t.Fatalf("expected regex compilation error for args %v with useRegex=%v, got nil", tt.args, tt.useRegex)
			}
		})
	}
}

func TestSecretMatchRegexAndPlain(t *testing.T) {
	tests := []struct {
		name          string
		searchString  string
		regexPattern  string
		searchObject  string
		dirEntry      string
		fullPath      string
		key           string
		value         string
		showSecrets   bool
		jsonOutput    bool
		expectMatched bool
	}{
		{
			name:          "regex match on value",
			regexPattern:  `^prod-db-\d+$`,
			searchObject:  "value",
			dirEntry:      "app",
			fullPath:      "secret/app",
			key:           "database_host",
			value:         "prod-db-42",
			jsonOutput:    true,
			expectMatched: true,
		},
		{
			name:          "regex no match on value",
			regexPattern:  `^prod-db-\d+$`,
			searchObject:  "value",
			dirEntry:      "app",
			fullPath:      "secret/app",
			key:           "database_host",
			value:         "stage-db-42",
			jsonOutput:    true,
			expectMatched: false,
		},
		{
			name:          "regex match on key",
			regexPattern:  `(?i)password`,
			searchObject:  "key",
			dirEntry:      "creds",
			fullPath:      "secret/creds",
			key:           "DB_PASSWORD",
			value:         "s3cr3t",
			jsonOutput:    true,
			expectMatched: true,
		},
		{
			name:          "regex match on fullPath",
			regexPattern:  `^secret/production/.*`,
			searchObject:  "path",
			dirEntry:      "config",
			fullPath:      "secret/production/app/config",
			key:           "env",
			value:         "prod",
			jsonOutput:    true,
			expectMatched: true,
		},
		{
			name:          "plain substring match on value",
			searchString:  "database.internal",
			searchObject:  "value",
			dirEntry:      "db",
			fullPath:      "secret/db",
			key:           "host",
			value:         "postgres.database.internal",
			jsonOutput:    true,
			expectMatched: true,
		},
		{
			name:          "plain substring no match on value",
			searchString:  "database.internal",
			searchObject:  "value",
			dirEntry:      "db",
			fullPath:      "secret/db",
			key:           "host",
			value:         "postgres.other.net",
			jsonOutput:    true,
			expectMatched: false,
		},
		{
			name:          "plain substring match on fullPath",
			searchString:  "staging",
			searchObject:  "path",
			dirEntry:      "api",
			fullPath:      "secret/staging/api",
			key:           "port",
			value:         "8080",
			jsonOutput:    true,
			expectMatched: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var re *regexp.Regexp
			if tt.regexPattern != "" {
				var err error
				re, err = regexp.Compile(tt.regexPattern)
				if err != nil {
					t.Fatalf("failed to compile test regex: %v", err)
				}
			}

			vc := vaultClient{
				jsonOutput:   tt.jsonOutput,
				searchString: tt.searchString,
				searchRegex:  re,
				showSecrets:  tt.showSecrets,
			}

			// Capture stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("failed to create pipe: %v", err)
			}
			origStdout := os.Stdout
			os.Stdout = w

			err = vc.secretMatch(tt.dirEntry, tt.fullPath, tt.searchObject, tt.key, tt.value)

			if closeErr := w.Close(); closeErr != nil {
				t.Fatalf("failed to close pipe writer: %v", closeErr)
			}
			os.Stdout = origStdout

			if err != nil {
				t.Fatalf("secretMatch returned unexpected error: %v", err)
			}

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)
			output := strings.TrimSpace(buf.String())

			if tt.expectMatched && output == "" {
				t.Errorf("expected match output but got none")
			}
			if !tt.expectMatched && output != "" {
				t.Errorf("expected no match output but got: %s", output)
			}
		})
	}
}
