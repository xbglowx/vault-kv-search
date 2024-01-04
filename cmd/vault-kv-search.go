package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	vault "github.com/hashicorp/vault/api"
)

type vaultClient struct {
	logical       *vault.Logical
	sys           *vault.Sys
	crawlingDelay int
	jsonOutput    bool
	showSecrets   bool
	useRegex      bool
	searchObjects []string
	searchString  string
	wg            sync.WaitGroup
}

type secretMatched struct {
	Search   string `json:"search"`
	FullPath string `json:"path"`
	Key      string `json:"key"`
	Value    string `json:"value"`
}

func (vc *vaultClient) getKvVersion(path string) (int, error) {
	mounts, err := vc.sys.ListMounts()
	if err != nil {
		fmt.Println(fmt.Errorf("error while getting mounts: %w", err))
		os.Exit(1)
	}

	secret := strings.Split(path, "/")[0]
	for mount := range mounts {
		if strings.Contains(mount, secret) {
			version, _ := strconv.Atoi(mounts[mount].Options["version"])
			if !vc.jsonOutput {
				fmt.Printf("Store path %q, version: %v\n", secret, version)
			}
			return version, nil
		}
	}

	return 0, errors.New("can't find secret store version")
}

// VaultKvSearch is the main function
func VaultKvSearch(args []string, searchObjects []string, showSecrets bool, useRegex bool, crawlingDelay int, version int, jsonOutput bool) {
	config := vault.DefaultConfig()
	config.Timeout = time.Second * 5

	client, err := vault.NewClient(config)
	if err != nil {
		err = fmt.Errorf("failed to create vault client: %w", err)
		fmt.Println(err)
		os.Exit(1)
	}

	vc := vaultClient{
		logical:       client.Logical(),
		sys:           client.Sys(),
		crawlingDelay: crawlingDelay,
		jsonOutput:    jsonOutput,
		showSecrets:   showSecrets, // pragma: allowlist secret
		useRegex:      useRegex,
		searchObjects: searchObjects,
		searchString:  args[1],
		wg:            sync.WaitGroup{},
	}

	startPath := args[0]

	if version == 0 {
		version, err = vc.getKvVersion(startPath)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	if !vc.jsonOutput {
		fmt.Printf("Searching for substring '%s' against: %v\n", args[1], searchObjects)
		fmt.Printf("Start path: %s\n", startPath)
	}

	if version > 1 {
		startPath = strings.Replace(startPath, "/", "/metadata/", 1)
	}

	if ok := strings.HasSuffix(startPath, "/"); !ok {
		startPath += "/"
	}

	vc.readLeafs(startPath, searchObjects, version)
	vc.wg.Wait()
}

func (vc *vaultClient) secretMatch(dirEntry string, fullPath string, searchObject string, key string, value string) {
	search := map[string]string{"path": dirEntry, "key": key, "value": value}
	term := search[searchObject]
	found := false

	if vc.useRegex {
		found, _ = regexp.MatchString(vc.searchString, term)
	} else {
		found = strings.Contains(term, vc.searchString)
	}

	if found {
		match := secretMatched{searchObject, fullPath, key, value}
		vc.showMatch(match)
	}
}

func (vc *vaultClient) showMatch(secret secretMatched) {
	if vc.jsonOutput {
		if !vc.showSecrets {
			secret.Value = "obfuscated"
		}
		secretJSON, err := json.Marshal(secret)
		if err != nil {
			fmt.Fprintf(os.Stderr, "can't marshal JSON: %s\n", err)
		}
		fmt.Println(string(secretJSON))
	} else {
		if vc.showSecrets {
			fmt.Printf("%s match:\n\tSecret: %s\n\tKey: %s\n\tValue: %s\n\n", strings.Title(secret.Search), secret.FullPath, secret.Key, secret.Value)
		} else {
			fmt.Printf("%s match:\n\tSecret: %s\n\tKey: %s\n\n", strings.Title(secret.Search), secret.FullPath, secret.Key)
		}
	}
}

func (vc *vaultClient) digDeeper(version int, data map[string]interface{}, dirEntry string, fullPath string, searchObject string) (key string, value string) {
	var valueStringType string

	for key, value := range data {
		if version > 1 && key == "metadata" {
			continue
		}
		switch v := value.(type) {
		// Convert types to strings
		case string:
			valueStringType = value.(string)
		case json.Number:
			valueStringType = v.String()
		case bool:
			valueStringType = strconv.FormatBool(v)
		case map[string]interface{}:
			// Recurse
			return vc.digDeeper(version, v, dirEntry, fullPath, searchObject)
		// Needed when start from root of the store
		case []interface{}:
		case nil:
		default:
			fmt.Printf("I don't know what %T is\n", v)
			os.Exit(1)
		}
		// Search matches
		vc.secretMatch(dirEntry, fullPath, searchObject, key, valueStringType)
	}

	return key, valueStringType
}

func (vc *vaultClient) readLeafs(path string, searchObjects []string, version int) {
	pathList, err := vc.logical.List(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list: %s\n%s", vc.searchString, err)
		os.Exit(1)
	}

	if pathList == nil {
		fmt.Fprintf(os.Stderr, "%s is not a valid path\n", path)
		os.Exit(1)
	}

	if len(pathList.Warnings) > 0 {
		fmt.Fprintf(os.Stderr, pathList.Warnings[0])
		os.Exit(1)
	}

	for _, x := range pathList.Data["keys"].([]interface{}) {

		// Slow down a little the crawling
		time.Sleep(time.Duration(vc.crawlingDelay) * time.Millisecond)

		dirEntry := x.(string)
		fullPath := fmt.Sprintf("%s%s", path, dirEntry)
		if strings.HasSuffix(dirEntry, "/") {
			vc.wg.Add(1)
			go func() {
				defer vc.wg.Done()
				vc.readLeafs(fullPath, searchObjects, version)
			}()

		} else {
			if version > 1 {
				fullPath = strings.Replace(fullPath, "/metadata/", "/data/", 1)
			}

			secretInfo, err := vc.logical.Read(fullPath)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			for _, searchObject := range searchObjects {
				if version > 1 {
					fullPath = strings.Replace(fullPath, "/data", "", 1)
				}
				vc.digDeeper(version, secretInfo.Data, dirEntry, fullPath, searchObject)
			}
		}
	}
}
