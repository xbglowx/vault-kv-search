package cmd

import (
	"encoding/json"
	"fmt"
	vault "github.com/hashicorp/vault/api"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type vaultClient struct {
	logical       *vault.Logical
	searchString  string
	searchObjects []string
	wg            sync.WaitGroup
}

func vaultKvSearch(args []string, searchObjects []string) {
	config := vault.DefaultConfig()
	config.Timeout = time.Second * 5

	client, err := vault.NewClient(config)
	if err != nil {
		fmt.Printf("Failed to create vault client: %s\n", err)
	}

	vc := vaultClient{
		logical:      client.Logical(),
		searchString: args[1],
		searchObjects: searchObjects,
		wg:           sync.WaitGroup{},
	}

	fmt.Printf("Searching for substring '%s' against: %v\n", args[1], searchObjects)
	startPath := args[0]
	vc.readLeafs(startPath)
	vc.wg.Wait()
}

func (vc *vaultClient) readLeafs(path string) {
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
		dirEntry := x.(string)
		fullPath := fmt.Sprintf("%s%s", path, dirEntry)
		if strings.HasSuffix(dirEntry, "/") {
			vc.wg.Add(1)
			go func() {
				defer vc.wg.Done()
				vc.readLeafs(fullPath)
			}()

		} else {
			secretInfo, err := vc.logical.Read(fullPath)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			for _, searchObject := range searchObjects {
				// Convert types to strings
				var valueStringType string
				for key, value := range secretInfo.Data {
					switch v := value.(type) {
					case string:
						valueStringType = value.(string)
					case json.Number:
						valueStringType = v.String()
					case bool:
						valueStringType = strconv.FormatBool(v)
					default:
						fmt.Printf("I don't know what %T is\n", v)
						os.Exit(1)
					}

					if strings.Contains(valueStringType, vc.searchString) && searchObject == "value" {
						fmt.Printf("Value match:\n\tSecret: %v\n\tKey: %v\n\tValue: %v\n", fullPath, key, value)
					}

					if strings.Contains(key, vc.searchString) && searchObject == "key" {
						fmt.Printf("Key match:\n\tSecret: %v\n\tKey: %v\n\tValue: %v\n", fullPath, key, value)
					}
				}
			}
		}
	}
}