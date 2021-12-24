package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	vault "github.com/hashicorp/vault/api"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type vaultClient struct {
	logical       *vault.Logical
	sys           *vault.Sys
	searchString  string
	showSecrets   bool
	useRegex      bool
	crawlingDelay int
	searchObjects []string
	wg            sync.WaitGroup
}

func (vc *vaultClient) getKvVersion(path string) (int, error) {
	mounts, err := vc.sys.ListMounts()
	if err != nil {
		err = fmt.Errorf("error while getting mounts: %w", err)
		fmt.Println(err)
		os.Exit(1)
	}

	secret := strings.Split(path, "/")[0]
	for mount := range mounts {
		if strings.Contains(mount, secret) {
			version, _ := strconv.Atoi(mounts[mount].Options["version"])
			fmt.Printf("Store path %q, version: %v\n", secret, version)
			return version, nil
		}
	}

	return 0, errors.New("can't find secret store version")
}

// VaultKvSearch is the main function
func VaultKvSearch(args []string, searchObjects []string, showSecrets bool, useRegex bool, crawlingDelay int) {
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
		searchString:  args[1],
		searchObjects: searchObjects,
		showSecrets:   showSecrets, //pragma: allowlist secret
		crawlingDelay: crawlingDelay,
		useRegex:      useRegex,
		wg:            sync.WaitGroup{},
	}

	startPath := args[0]
	version, err := vc.getKvVersion(startPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Printf("Searching for substring '%s' against: %v\n", args[1], searchObjects)
	fmt.Printf("Start path: %s\n", startPath)

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
		if vc.showSecrets {
			fmt.Printf("%s match:\n\tSecret: %s\n\tKey: %s\n\tValue: %s\n\n", strings.Title(searchObject), fullPath, key, value)
		} else {
			fmt.Printf("%s match:\n\tSecret: %s\n\tKey: %s\n\n", strings.Title(searchObject), fullPath, key)
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
		fmt.Printf("Failed to list: %s\n%s", vc.searchString, err)
		os.Exit(1)
	}

	if pathList == nil {
		fmt.Printf("%s is not a valid path\n", path)
		os.Exit(1)
	}

	if len(pathList.Warnings) > 0 {
		fmt.Println(pathList.Warnings[0])
		os.Exit(1)
	}

	for _, x := range pathList.Data["keys"].([]interface{}) {

		// Slow down a little the crawling
		time.Sleep(time.Duration(crawlingDelay) * time.Millisecond)

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
