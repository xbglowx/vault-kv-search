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

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func checkInputs(cmd *cobra.Command, args []string) error {
	searchObjectChoices := map[string]struct{}{
		"key":   {},
		"value": {},
		"path":  {},
	}

	keys := []string{}
	for key := range searchObjectChoices {
		keys = append(keys, key)
	}

	for _, s := range searchObjects {
		if _, ok := searchObjectChoices[s]; !ok {
			errorMsg := fmt.Sprintf("%s is not a valid flag choice. Choices are %v", s, keys)
			return errors.New(errorMsg)
		}
	}

	if len(args) == 1 {
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
available KV stores and the argument specified is the substring you want to search for`,

	PreRunE: func(cmd *cobra.Command, args []string) error {
		return checkInputs(cmd, args)
	},
	Run: func(cmd *cobra.Command, args []string) {
		VaultKvSearch(args, searchObjects, showSecrets, useRegex, crawlingDelay, kvVersion, jsonOutput, timeout)
	},
	Args:    cobra.RangeArgs(1, 2),
	Example: "vault-kv-search kv/ foo",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(RootCmd.Execute())
}

var (
	crawlingDelay int
	jsonOutput    bool
	kvVersion     int
	searchObjects []string
	showSecrets   bool
	timeout       int
	useRegex      bool
)

func init() {
	RootCmd.Flags().IntVarP(&crawlingDelay, "delay", "d", 15, "Crawling delay in millisconds")
	RootCmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output as JSON")
	RootCmd.Flags().IntVarP(&kvVersion, "kv-version", "k", 0, "KV version (1,2). Autodetect if not defined")
	RootCmd.Flags().StringSliceVar(&searchObjects, "search", []string{"value"}, "Which Vault objects to "+
		"search against. Choices are any and all of the following 'key,value,path'. Can be specified multiple times or "+
		"once using format CSV. Defaults to 'value'")
	RootCmd.Flags().BoolVarP(&showSecrets, "showsecrets", "s", false, "Show secrets values")
	RootCmd.Flags().BoolVarP(&useRegex, "regex", "r", false, "Enable searching regex substring")
	RootCmd.Flags().IntVarP(&timeout, "timeout", "t", 30, "Vault client timeout in seconds")
}
