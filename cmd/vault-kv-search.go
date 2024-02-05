package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	vault "github.com/hashicorp/vault/api"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type vaultClient struct {
	crawlingDelay int
	jsonOutput    bool
	logical       *vault.Logical
	searchObjects []string
	searchString  string
	showSecrets   bool
	sys           *vault.Sys
	useRegex      bool
	wg            sync.WaitGroup
}

type startPathInfo struct {
	path      string
	kvVersion int
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
		return 0, fmt.Errorf("error while listing mounts: %w", err)
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
func VaultKvSearch(args []string, searchObjects []string, showSecrets bool, useRegex bool, crawlingDelay int, kvVersion int, jsonOutput bool) {
	config := vault.DefaultConfig()
	config.Timeout = time.Second * 5

	client, err := vault.NewClient(config)
	if err != nil {
		err = fmt.Errorf("failed to create vault client: %w", err)
		fmt.Println(err)
		os.Exit(1)
	}

	// If length of postional args is 1, the users didn't specify a search-path and wants to search all available KV stores.
	var searchString string
	var searchAllKvStores bool
	if len(args) == 1 {
		searchAllKvStores = true
		searchString = args[0]
	} else {
		searchAllKvStores = false
		searchString = args[1]
	}

	vc := vaultClient{
		crawlingDelay: crawlingDelay,
		jsonOutput:    jsonOutput,
		logical:       client.Logical(),
		searchObjects: searchObjects,
		searchString:  searchString,
		showSecrets:   showSecrets, // pragma: allowlist secret
		sys:           client.Sys(),
		useRegex:      useRegex,
		wg:            sync.WaitGroup{},
	}

	var startPathsInfo []startPathInfo
	if searchAllKvStores {
		startPathsInfo = vc.getAllKvStores()
	} else {
		if kvVersion == 0 {
			kvVersion, err = vc.getKvVersion(args[0])
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		}
		startPathsInfo = append(startPathsInfo, startPathInfo{path: args[0], kvVersion: kvVersion})
	}

	for _, startPathInfo := range startPathsInfo {
		// In case the user leaves off the trailing /, let's add it for them
		if ok := strings.HasSuffix(startPathInfo.path, "/"); !ok {
			startPathInfo.path += "/"
		}

		if !vc.jsonOutput {
			fmt.Printf("Searching for substring '%s' against: %v\n", searchString, searchObjects)
			fmt.Printf("Start path: %s\n", startPathInfo.path)
		}

		if startPathInfo.kvVersion > 1 {
			startPathInfo.path = strings.Replace(startPathInfo.path, "/", "/metadata/", 1)
		}

		err := vc.readLeafs(startPathInfo.path, searchObjects, startPathInfo.kvVersion)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		vc.wg.Wait()
	}
}

func (vc *vaultClient) getAllKvStores() []startPathInfo {
	var info []startPathInfo

	mountPoints, err := vc.sys.ListMounts()
	if err != nil {
		log.Fatalf("Could not get a list of mounts: %v", err)
	}

	// Loop through all mountpoints and save only those that are of types kv or generic (old vault KVv1)
	for mountPath, mountOptions := range mountPoints {
		if mountOptions.Type == "kv" || mountOptions.Type == "generic" {
			version, _ := strconv.Atoi(mountOptions.Options["version"])
			info = append(info, startPathInfo{path: mountPath, kvVersion: version})
		}
	}

	return info
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
		title := cases.Title(language.English)
		if vc.showSecrets {
			fmt.Printf("%s match:\n\tSecret: %s\n\tKey: %s\n\tValue: %s\n\n", title.String(secret.Search), secret.FullPath, secret.Key, secret.Value)
		} else {
			fmt.Printf("%s match:\n\tSecret: %s\n\tKey: %s\n\n", title.String(secret.Search), secret.FullPath, secret.Key)
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

func (vc *vaultClient) readLeafs(path string, searchObjects []string, version int) error {
	// fmt.Println("oh no: ", path, version)
	pathList, err := vc.logical.List(path)
	if err != nil {
		return fmt.Errorf("failed to list: %s\n%s", vc.searchString, err)
	}

	if pathList == nil {
		fmt.Fprintf(os.Stderr, "!!Warning!! search-path %s doesn't have any contents. Skipping.\n", path)
		return nil
	}

	if len(pathList.Warnings) > 0 {
		return fmt.Errorf(pathList.Warnings[0])
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
	return nil
}
