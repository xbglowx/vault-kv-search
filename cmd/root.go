package cmd

/*
Copyright © 2024 Brian Glogower <xbglowx@gmail.com>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func validateSearchObjects(objects []string) error {
	if len(objects) == 0 {
		return errors.New("at least one search target must be specified")
	}
	searchObjectChoices := map[string]struct{}{
		"key":   {},
		"value": {},
		"path":  {},
	}

	keys := make([]string, 0, len(searchObjectChoices))
	for key := range searchObjectChoices {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, s := range objects {
		if _, ok := searchObjectChoices[s]; !ok {
			errorMsg := fmt.Sprintf("%s is not a valid flag choice. Choices are %v", s, keys)
			return errors.New(errorMsg)
		}
	}
	return nil
}

func checkInputs(cmd *cobra.Command, args []string) error {
	if err := validateSearchObjects(searchObjects); err != nil {
		return err
	}

	if concurrency < 1 {
		return errors.New("concurrency must be at least 1")
	}

	if cmd.Flags().Changed("vault-addr") && strings.TrimSpace(vaultAddr) == "" {
		return errors.New("vault-addr cannot be empty")
	}

	if cmd.Flags().Changed("vault-token") && strings.TrimSpace(vaultToken) == "" {
		return errors.New("vault-token cannot be empty")
	}

	if len(args) == 0 && !interactive {
		return errors.New("requires at least 1 arg(s), only received 0")
	}

	if len(args) == 1 && !interactive {
		cmd.Printf("!!Warning!! searching all KV stores, since only one positional argument was specified\n")
	}

	return nil
}

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "vault-kv-search [flags] [search-path] substring",
	Short: "Search Hashicorp Vault",
	Long: `Recursively search Hashicorp Vault for substring

If only one positional argument is given, it is assumed you want to search all 
available KV stores and the argument specified is the substring you want to search for.
Use -I / --interactive to launch a guided search prompt.

Authentication and Connection:
  Vault address: --vault-addr (-a) flag or VAULT_ADDR environment variable
  Vault token:   --vault-token flag (literal token, @file, or file path), VAULT_TOKEN environment variable, or ~/.vault-token
  TLS verify:    --skip-verify flag or VAULT_SKIP_VERIFY environment variable`,

	PreRunE: func(cmd *cobra.Command, args []string) error {
		return checkInputs(cmd, args)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceErrors = false
		cmd.SilenceUsage = false

		opts := SearchOptions{
			Args:           args,
			SearchObjects:  searchObjects,
			ShowSecrets:    showSecrets,
			UseRegex:       useRegex,
			IgnoreCase:     ignoreCase,
			CrawlingDelay:  crawlingDelay,
			Concurrency:    concurrency,
			KvVersion:      kvVersion,
			JSONOutput:     jsonOutput,
			TimeoutSeconds: timeout,
			NoColor:        noColor,
			ExitCode:       exitCode,
			VaultAddr:      vaultAddr,
			VaultToken:     vaultToken,
			SkipVerify:     skipVerify,
			SkipVerifySet:  cmd.Flags().Changed("skip-verify"),
		}

		if interactive {
			mounts := discoverMountsForInteractive(opts)
			if err := runInteractivePrompt(os.Stdin, os.Stderr, &opts, mounts); err != nil {
				return err
			}
			if len(opts.Args) == 1 {
				cmd.Printf("!!Warning!! searching all KV stores, since only one positional argument was specified\n")
			}
		}

		err := VaultKvSearchWithOptions(opts)
		if errors.Is(err, ErrNoMatches) {
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
		}
		return err
	},
	Args:    cobra.RangeArgs(0, 2),
	Example: "vault-kv-search kv/ foo",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := RootCmd.Execute()
	if errors.Is(err, ErrNoMatches) {
		os.Exit(1)
	}
	cobra.CheckErr(err)
}

var (
	concurrency   int
	crawlingDelay int
	exitCode      bool
	ignoreCase    bool
	interactive   bool
	jsonOutput    bool
	kvVersion     int
	noColor       bool
	searchObjects []string
	showSecrets   bool
	skipVerify    bool
	timeout       int
	useRegex      bool
	vaultAddr     string
	vaultToken    string
)

func init() {
	RootCmd.Flags().IntVarP(&concurrency, "concurrency", "c", defaultConcurrency, "Number of concurrent Vault workers")
	RootCmd.Flags().IntVarP(&crawlingDelay, "delay", "d", 15, "Crawling delay in milliseconds")
	RootCmd.Flags().BoolVarP(&exitCode, "exit-code", "e", false, "Return exit code 1 when no matches are found")
	RootCmd.Flags().BoolVarP(&ignoreCase, "ignore-case", "i", false, "Perform case-insensitive matching")
	RootCmd.Flags().BoolVarP(&interactive, "interactive", "I", false, "Launch interactive guided search prompt")
	RootCmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output as JSON")
	RootCmd.Flags().IntVarP(&kvVersion, "kv-version", "k", 0, "KV version (1,2). Autodetect if not defined")
	RootCmd.Flags().BoolVar(&noColor, "no-color", false, "Disable colored output and match highlighting")
	RootCmd.Flags().StringSliceVar(&searchObjects, "search", []string{"value"}, "Which Vault objects to "+
		"search against. Choices are any and all of the following 'key,value,path'. Can be specified multiple times or "+
		"once using format CSV. Defaults to 'value'")
	RootCmd.Flags().BoolVarP(&showSecrets, "showsecrets", "s", false, "Show secrets values")
	RootCmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip TLS certificate verification (overrides VAULT_SKIP_VERIFY)")
	RootCmd.Flags().BoolVarP(&useRegex, "regex", "r", false, "Enable searching regex substring")
	RootCmd.Flags().IntVarP(&timeout, "timeout", "t", 30, "Vault client timeout in seconds")
	RootCmd.Flags().StringVarP(&vaultAddr, "vault-addr", "a", "", "Vault server address (overrides VAULT_ADDR)")
	RootCmd.Flags().StringVar(&vaultToken, "vault-token", "", "Vault authentication token, @file, or file path (overrides VAULT_TOKEN and ~/.vault-token)")
}
