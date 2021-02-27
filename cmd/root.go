/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

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
package cmd

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func checkRequiredFlags(cmd *cobra.Command) error {
	searchObjectChoices := map[string]struct{}{
		"key" : {},
		"value": {},
	}

	keys := []string{}
	for key, _ := range searchObjectChoices {
		keys = append(keys, key)
	}

	for _, s := range searchObject {
		if _, ok := searchObjectChoices[s]; ! ok {
			errorMsg := fmt.Sprintf("%s is not a valid flag choice. Choices are %v", s, keys)
			return errors.New(errorMsg)
		}
	}
	return nil
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "vault-kv-search [flags] search-path substring",
	Short: "Search Hashicorp Vault",
	Long: `Search for a substring in Hashicorp Vault

Recursively search Hashicorp Vault for substring`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return checkRequiredFlags(cmd)
	},
	Run: func(cmd *cobra.Command, args []string) {
		vaultKvSearch(args, searchObject)
	},
	Args:    cobra.ExactArgs(2),
	Example: "vault-kv-search secret/ foo",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

var searchObject []string

func init() {
	//cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	//rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.vault-kv-search.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	rootCmd.Flags().StringSliceVar(&searchObject, "search", []string{"value"}, "Which Vault objects to "+
		"search against. Choices are any and all of the following 'key,value'. Can be specified multiple times or "+
		"once using format CSV. Defaults to 'value'")

}

// initConfig reads in config file and ENV variables if set.
//func initConfig() {
//	if cfgFile != "" {
//		Use config file from the flag.
//viper.SetConfigFile(cfgFile)
//} else {
//	Find home directory.
//home, err := homedir.Dir()
//cobra.CheckErr(err)
//
//Search config in home directory with name ".vault-kv-search" (without extension).
//viper.AddConfigPath(home)
//viper.SetConfigName(".vault-kv-search")
//}
//
//viper.AutomaticEnv() // read in environment variables that match
//
//If a config file is found, read it in.
//if err := viper.ReadInConfig(); err == nil {
//	fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
//}
//}
