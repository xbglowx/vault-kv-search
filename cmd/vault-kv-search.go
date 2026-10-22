package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	vault "github.com/hashicorp/vault/api"
	"golang.org/x/term"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/time/rate"
)

const (
	defaultConcurrency = 10

	ansiReset    = "\x1b[0m"
	ansiBoldCyan = "\x1b[1;36m"
	ansiBoldRed  = "\x1b[1;31m"
)

// ErrNoMatches is returned when --exit-code is enabled and no secrets matched the query.
var ErrNoMatches = errors.New("no matches found")

type SearchOptions struct {
	Args           []string
	SearchObjects  []string
	ShowSecrets    bool
	UseRegex       bool
	IgnoreCase     bool
	CrawlingDelay  int
	Concurrency    int
	KvVersion      int
	JSONOutput     bool
	TimeoutSeconds int
	NoColor        bool
	ForceColor     bool
	ExitCode       bool
}

type searchTask struct {
	isDir         bool
	dirEntry      string
	fullPath      string
	searchObjects []string
	version       int
}

type vaultClient struct {
	cancel         context.CancelFunc
	ctx            context.Context
	err            error
	errMu          sync.Mutex
	ignoreCase     bool
	jsonOutput     bool
	logical        *vault.Logical
	matchesFound   atomic.Int64
	outputMu       sync.Mutex
	pathsSkipped   atomic.Int64
	pendingTasks   int
	queue          []searchTask
	queueCond      *sync.Cond
	queueMu        sync.Mutex
	rateLimiter    *rate.Limiter
	searchObjects  []string
	searchRegex    *regexp.Regexp
	searchString   string
	secretsScanned atomic.Int64
	sem            chan struct{}
	showSecrets    bool
	sys            *vault.Sys
	useColor       bool
	wg             sync.WaitGroup
	workerCount    int
}

func (vc *vaultClient) setErr(err error) {
	if err == nil {
		return
	}
	vc.errMu.Lock()
	if vc.err == nil {
		vc.err = err
	}
	cancel := vc.cancel
	vc.errMu.Unlock()

	if cancel != nil {
		cancel()
	}

	if vc.queueCond != nil {
		vc.queueMu.Lock()
		vc.queueCond.Broadcast()
		vc.queueMu.Unlock()
	}
}

func (vc *vaultClient) getErr() error {
	vc.errMu.Lock()
	defer vc.errMu.Unlock()
	return vc.err
}

func (vc *vaultClient) warnf(format string, a ...interface{}) {
	vc.outputMu.Lock()
	defer vc.outputMu.Unlock()
	_, _ = fmt.Fprintf(os.Stderr, format, a...)
}

func (vc *vaultClient) searchContext() context.Context {
	if vc.ctx != nil {
		return vc.ctx
	}
	return context.Background()
}

func (vc *vaultClient) withWorker(fn func() error) error {
	if err := vc.getErr(); err != nil {
		return err
	}

	ctx := vc.searchContext()

	if vc.sem != nil {
		select {
		case vc.sem <- struct{}{}:
			defer func() { <-vc.sem }()
		case <-ctx.Done():
			if err := vc.getErr(); err != nil {
				return err
			}
			return ctx.Err()
		}
	}

	if err := vc.getErr(); err != nil {
		return err
	}

	if vc.rateLimiter != nil {
		if err := vc.rateLimiter.Wait(ctx); err != nil {
			if fatalErr := vc.getErr(); fatalErr != nil {
				return fatalErr
			}
			return err
		}
	}

	if err := vc.getErr(); err != nil {
		return err
	}

	return fn()
}

func (vc *vaultClient) enqueueTasks(subDirs, leaves []searchTask) {
	total := len(subDirs) + len(leaves)
	if total == 0 {
		return
	}
	vc.queueMu.Lock()
	if vc.getErr() != nil {
		vc.queueMu.Unlock()
		return
	}
	// Enqueue subdirectories first and leaves last under one lock so LIFO workers always pop leaves before subdirectories.
	for i := len(subDirs) - 1; i >= 0; i-- {
		vc.queue = append(vc.queue, subDirs[i])
	}
	for i := len(leaves) - 1; i >= 0; i-- {
		vc.queue = append(vc.queue, leaves[i])
	}
	vc.pendingTasks += total
	if vc.queueCond != nil {
		if total == 1 {
			vc.queueCond.Signal()
		} else {
			vc.queueCond.Broadcast()
		}
	}
	vc.queueMu.Unlock()
}

func (vc *vaultClient) crawlPath(startPath string, searchObjects []string, version int) {
	vc.queueMu.Lock()
	if vc.queueCond == nil {
		vc.queueCond = sync.NewCond(&vc.queueMu)
	}
	vc.queue = []searchTask{{
		isDir:         true,
		fullPath:      startPath,
		searchObjects: searchObjects,
		version:       version,
	}}
	vc.pendingTasks = 1
	vc.queueMu.Unlock()

	workers := vc.workerCount
	if workers <= 0 {
		workers = defaultConcurrency
	}

	for range workers {
		vc.wg.Add(1)
		go vc.worker()
	}
	vc.wg.Wait()
}

func (vc *vaultClient) worker() {
	defer vc.wg.Done()
	for {
		vc.queueMu.Lock()
		for len(vc.queue) == 0 && vc.pendingTasks > 0 && vc.getErr() == nil {
			vc.queueCond.Wait()
		}
		if vc.getErr() != nil || (len(vc.queue) == 0 && vc.pendingTasks == 0) {
			vc.queueMu.Unlock()
			return
		}
		last := len(vc.queue) - 1
		task := vc.queue[last]
		vc.queue[last] = searchTask{}
		vc.queue = vc.queue[:last]
		vc.queueMu.Unlock()

		var err error
		if task.isDir {
			err = vc.readLeafs(task.fullPath, task.searchObjects, task.version)
		} else {
			err = vc.readSecret(task.dirEntry, task.fullPath, task.searchObjects, task.version)
		}
		if err != nil {
			vc.setErr(err)
		}

		vc.queueMu.Lock()
		vc.pendingTasks--
		if vc.pendingTasks == 0 || vc.getErr() != nil {
			vc.queueCond.Broadcast()
		}
		vc.queueMu.Unlock()
	}
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

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func shouldUseColor(opts SearchOptions) bool {
	if opts.JSONOutput || opts.NoColor {
		return false
	}
	if opts.ForceColor {
		return true
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTerminal(os.Stdout)
}

func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	var respErr *vault.ResponseError
	if errors.As(err, &respErr) {
		return respErr.StatusCode == http.StatusForbidden
	}
	return strings.Contains(strings.ToLower(err.Error()), "code: 403")
}

// configureToken tries to configure the Vault token on the client.
//
// Order:
//  1. If a token is already set on the client, keep it
//  2. Otherwise, use VAULT_TOKEN if present
//  3. Otherwise, try to read ~/.vault-token (token helper style)
func configureToken(client *vault.Client) error {
	if t := client.Token(); t != "" {
		return nil
	}

	if t := os.Getenv("VAULT_TOKEN"); t != "" {
		client.SetToken(t)
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory to read token helper: %w", err)
	}

	tokenFile := filepath.Join(home, ".vault-token")

	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return fmt.Errorf("no Vault token configured (VAULT_TOKEN env or %s): %w", tokenFile, err)
	}

	token := strings.TrimSpace(string(data))
	if token == "" {
		return fmt.Errorf("token helper file %s exists but is empty", tokenFile)
	}

	client.SetToken(token)
	return nil
}

func (vc *vaultClient) getKvVersion(path string) (int, error) {
	mounts, err := vc.sys.ListMounts()
	if err != nil {
		return 0, fmt.Errorf("error while listing mounts: %w", err)
	}

	secret := strings.Split(strings.TrimLeft(path, "/"), "/")[0]
	if secret == "" {
		return 0, errors.New("can't find secret store version")
	}
	for _, exact := range []bool{true, false} {
		for mount, mountOpts := range mounts {
			matched := strings.TrimSuffix(mount, "/") == secret
			if !exact {
				matched = strings.Contains(mount, secret)
			}
			if matched {
				version, _ := strconv.Atoi(mountOpts.Options["version"])
				if !vc.jsonOutput {
					vc.outputMu.Lock()
					fmt.Printf("Store path %q, version: %v\n", secret, version)
					vc.outputMu.Unlock()
				}
				return version, nil
			}
		}
	}

	return 0, errors.New("can't find secret store version")
}

// VaultKvSearch is kept for backward compatibility with callers using positional parameters.
func VaultKvSearch(args []string, searchObjects []string, showSecrets bool, useRegex bool, crawlingDelay int, kvVersion int, jsonOutput bool, timeoutSeconds int) error {
	return VaultKvSearchWithOptions(SearchOptions{
		Args:           args,
		SearchObjects:  searchObjects,
		ShowSecrets:    showSecrets,
		UseRegex:       useRegex,
		CrawlingDelay:  crawlingDelay,
		Concurrency:    defaultConcurrency,
		KvVersion:      kvVersion,
		JSONOutput:     jsonOutput,
		TimeoutSeconds: timeoutSeconds,
	})
}

func VaultKvSearchWithOptions(opts SearchOptions) error {
	startTime := time.Now()

	if len(opts.Args) == 0 {
		return errors.New("at least one positional argument is required")
	}

	if len(opts.SearchObjects) == 0 {
		opts.SearchObjects = []string{"value"}
	}
	if err := validateSearchObjects(opts.SearchObjects); err != nil {
		return err
	}

	timeoutSeconds := opts.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}

	config := vault.DefaultConfig()
	config.Timeout = time.Duration(timeoutSeconds) * time.Second

	client, err := vault.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create vault client: %w", err)
	}

	if err := configureToken(client); err != nil {
		return err
	}

	// Single positional arg means no search-path was given; search all KV mounts.
	var searchString string
	var searchAllKvStores bool
	if len(opts.Args) == 1 {
		searchAllKvStores = true
		searchString = opts.Args[0]
	} else {
		searchAllKvStores = false
		searchString = opts.Args[1]
	}

	var re *regexp.Regexp
	if opts.UseRegex {
		pattern := searchString
		if opts.IgnoreCase && !strings.HasPrefix(pattern, "(?i)") {
			pattern = "(?i)" + pattern
		}
		if re, err = regexp.Compile(pattern); err != nil {
			return err
		}
	} else if opts.IgnoreCase && searchString != "" {
		if re, err = regexp.Compile("(?i)" + regexp.QuoteMeta(searchString)); err != nil {
			return err
		}
	}

	workerCount := opts.Concurrency
	if workerCount <= 0 {
		workerCount = defaultConcurrency
	}

	var limiter *rate.Limiter
	if opts.CrawlingDelay > 0 {
		limiter = rate.NewLimiter(rate.Every(time.Duration(opts.CrawlingDelay)*time.Millisecond), 1)
	}

	searchCtx, cancelSearch := context.WithCancel(context.Background())
	defer cancelSearch()

	vc := vaultClient{
		cancel:        cancelSearch,
		ctx:           searchCtx,
		ignoreCase:    opts.IgnoreCase,
		jsonOutput:    opts.JSONOutput,
		logical:       client.Logical(),
		rateLimiter:   limiter,
		searchObjects: opts.SearchObjects,
		searchString:  searchString,
		searchRegex:   re,
		sem:           make(chan struct{}, workerCount),
		showSecrets:   opts.ShowSecrets, // pragma: allowlist secret
		sys:           client.Sys(),
		useColor:      shouldUseColor(opts),
		wg:            sync.WaitGroup{},
		workerCount:   workerCount,
	}
	vc.queueCond = sync.NewCond(&vc.queueMu)

	var startPathsInfo []startPathInfo
	kvVersion := opts.KvVersion
	if searchAllKvStores {
		startPathsInfo, err = vc.getAllKvStores()
		if err != nil {
			return err
		}
	} else {
		if kvVersion == 0 {
			kvVersion, err = vc.getKvVersion(opts.Args[0])
			if err != nil {
				return err
			}
		}
		startPathsInfo = append(startPathsInfo, startPathInfo{path: opts.Args[0], kvVersion: kvVersion})
	}

	for _, startPathInfo := range startPathsInfo {
		startPathInfo.path = strings.TrimLeft(startPathInfo.path, "/")
		if ok := strings.HasSuffix(startPathInfo.path, "/"); !ok {
			startPathInfo.path += "/"
		}

		if !vc.jsonOutput {
			vc.outputMu.Lock()
			fmt.Printf("Searching for substring '%s' against: %v\n", searchString, opts.SearchObjects)
			fmt.Printf("Start path: %s\n", startPathInfo.path)
			vc.outputMu.Unlock()
		}

		if startPathInfo.kvVersion > 1 {
			startPathInfo.path = strings.Replace(startPathInfo.path, "/", "/metadata/", 1)
		}

		vc.crawlPath(startPathInfo.path, opts.SearchObjects, startPathInfo.kvVersion)
		if err := vc.getErr(); err != nil {
			return err
		}
	}

	elapsed := time.Since(startTime).Round(time.Millisecond)
	matches := vc.matchesFound.Load()
	scanned := vc.secretsScanned.Load()
	skipped := vc.pathsSkipped.Load()

	if !vc.jsonOutput {
		if skipped > 0 {
			vc.warnf("Summary: %d match(es) across %d secret(s) in %s (%d path(s) skipped)\n", matches, scanned, elapsed, skipped)
		} else {
			vc.warnf("Summary: %d match(es) across %d secret(s) in %s\n", matches, scanned, elapsed)
		}
	}

	if opts.ExitCode && matches == 0 {
		return ErrNoMatches
	}

	return nil
}

func (vc *vaultClient) getAllKvStores() ([]startPathInfo, error) {
	var info []startPathInfo

	mountPoints, err := vc.sys.ListMounts()
	if err != nil {
		return nil, fmt.Errorf("could not get a list of mounts: %w", err)
	}

	// Only include kv or generic (legacy KV v1) mounts.
	for mountPath, mountOptions := range mountPoints {
		if mountOptions.Type == "kv" || mountOptions.Type == "generic" {
			version, _ := strconv.Atoi(mountOptions.Options["version"])
			if version <= 0 {
				version = 1
			}
			info = append(info, startPathInfo{path: mountPath, kvVersion: version})
		}
	}

	return info, nil
}

func containsCaseFold(text, substr string) bool {
	if substr == "" {
		return true
	}
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(substr))
	if err != nil {
		return strings.Contains(strings.ToLower(text), strings.ToLower(substr))
	}
	return re.MatchString(text)
}

func (vc *vaultClient) secretMatch(dirEntry string, fullPath string, searchObject string, key string, value string) error {
	search := map[string]string{"path": dirEntry, "key": key, "value": value}
	term := search[searchObject]
	found := false

	if vc.searchRegex != nil {
		found = vc.searchRegex.MatchString(term)
		if !found && searchObject == "path" {
			found = vc.searchRegex.MatchString(fullPath)
		}
	} else if vc.ignoreCase {
		found = containsCaseFold(term, vc.searchString)
		if !found && searchObject == "path" {
			found = containsCaseFold(fullPath, vc.searchString)
		}
	} else {
		found = strings.Contains(term, vc.searchString)
		if !found && searchObject == "path" {
			found = strings.Contains(fullPath, vc.searchString)
		}
	}

	if found {
		vc.matchesFound.Add(1)
		match := secretMatched{searchObject, fullPath, key, value}
		vc.showMatch(match)
	}
	return nil
}

func (vc *vaultClient) highlightMatch(text string) string {
	if !vc.useColor || text == "" {
		return text
	}
	if vc.searchRegex != nil {
		return vc.searchRegex.ReplaceAllStringFunc(text, func(m string) string {
			if m == "" {
				return m
			}
			return ansiBoldRed + m + ansiReset
		})
	}
	return highlightSubstring(text, vc.searchString, vc.ignoreCase)
}

func highlightSubstring(text, substr string, ignoreCase bool) string {
	if substr == "" {
		return text
	}
	if !ignoreCase {
		var b strings.Builder
		start := 0
		for {
			idx := strings.Index(text[start:], substr)
			if idx < 0 {
				b.WriteString(text[start:])
				break
			}
			matchStart := start + idx
			matchEnd := matchStart + len(substr)
			b.WriteString(text[start:matchStart])
			b.WriteString(ansiBoldRed)
			b.WriteString(text[matchStart:matchEnd])
			b.WriteString(ansiReset)
			start = matchEnd
		}
		return b.String()
	}

	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(substr))
	if err != nil {
		return text
	}
	return re.ReplaceAllStringFunc(text, func(m string) string {
		if m == "" {
			return m
		}
		return ansiBoldRed + m + ansiReset
	})
}

func (vc *vaultClient) highlightPathMatch(fullPath string) string {
	highlighted := vc.highlightMatch(fullPath)
	if highlighted != fullPath {
		return highlighted
	}
	slashIdx := strings.LastIndex(fullPath, "/")
	if slashIdx < 0 || slashIdx == len(fullPath)-1 {
		return highlighted
	}
	return fullPath[:slashIdx+1] + vc.highlightMatch(fullPath[slashIdx+1:])
}

func (vc *vaultClient) showMatch(secret secretMatched) {
	vc.outputMu.Lock()
	defer vc.outputMu.Unlock()

	if vc.jsonOutput {
		if !vc.showSecrets {
			secret.Value = "obfuscated"
		}
		secretJSON, err := json.Marshal(secret)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "can't marshal JSON: %s\n", err)
			return
		}
		fmt.Println(string(secretJSON))
	} else {
		title := cases.Title(language.English)
		header := fmt.Sprintf("%s match:", title.String(secret.Search))
		pathText := secret.FullPath
		keyText := secret.Key
		valText := secret.Value

		if vc.useColor {
			header = ansiBoldCyan + header + ansiReset
			switch secret.Search {
			case "path":
				pathText = vc.highlightPathMatch(pathText)
			case "key":
				keyText = vc.highlightMatch(keyText)
			case "value":
				if vc.showSecrets {
					valText = vc.highlightMatch(valText)
				}
			}
		}

		if vc.showSecrets {
			fmt.Printf("%s\n\tSecret: %s\n\tKey: %s\n\tValue: %s\n\n", header, pathText, keyText, valText)
		} else {
			fmt.Printf("%s\n\tSecret: %s\n\tKey: %s\n\n", header, pathText, keyText)
		}
	}
}

func (vc *vaultClient) digDeeper(version int, data map[string]interface{}, dirEntry string, fullPath string, searchObject string) error {
	var valueStringType string
	var err error

	for key, value := range data {
		if version > 1 && key == "metadata" {
			continue
		}
		switch v := value.(type) {
		case string:
			valueStringType = v
		case json.Number:
			valueStringType = v.String()
		case bool:
			valueStringType = strconv.FormatBool(v)
		case map[string]interface{}:
			if err := vc.digDeeper(version, v, dirEntry, fullPath, searchObject); err != nil {
				return err
			}
			continue
		// Needed when starting from the root of a KV v2 store.
		case []interface{}:
		case nil:
		default:
			return fmt.Errorf("unknown data type %T for key %s", v, key)
		}
		err = vc.secretMatch(dirEntry, fullPath, searchObject, key, valueStringType)
		if err != nil {
			return err
		}
	}

	return nil
}

func (vc *vaultClient) readSecret(dirEntry string, fullPath string, searchObjects []string, version int) error {
	if err := vc.getErr(); err != nil {
		return err
	}

	readPath := fullPath
	if version > 1 {
		readPath = strings.Replace(fullPath, "/metadata/", "/data/", 1)
	}

	var secretInfo *vault.Secret
	err := vc.withWorker(func() error {
		var readErr error
		secretInfo, readErr = vc.logical.ReadWithContext(vc.searchContext(), readPath)
		return readErr
	})
	if err != nil {
		if fatalErr := vc.getErr(); fatalErr != nil && errors.Is(err, context.Canceled) {
			return fatalErr
		}
		if isPermissionDenied(err) {
			vc.pathsSkipped.Add(1)
			vc.warnf("!!Warning!! permission denied reading %s. Skipping.\n", readPath)
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", readPath, err)
	}

	if secretInfo == nil || secretInfo.Data == nil {
		vc.pathsSkipped.Add(1)
		return nil
	}
	if version > 1 {
		innerData, ok := secretInfo.Data["data"].(map[string]interface{})
		if !ok || innerData == nil {
			vc.pathsSkipped.Add(1)
			return nil
		}
	}

	vc.secretsScanned.Add(1)

	displayPath := readPath
	if version > 1 {
		displayPath = strings.Replace(readPath, "/data/", "/", 1)
	}

	for _, searchObject := range searchObjects {
		if err := vc.digDeeper(version, secretInfo.Data, dirEntry, displayPath, searchObject); err != nil {
			return err
		}
	}
	return nil
}

func (vc *vaultClient) readLeafs(path string, searchObjects []string, version int) error {
	if err := vc.getErr(); err != nil {
		return err
	}

	var pathList *vault.Secret
	err := vc.withWorker(func() error {
		var listErr error
		pathList, listErr = vc.logical.ListWithContext(vc.searchContext(), path)
		return listErr
	})
	if err != nil {
		if fatalErr := vc.getErr(); fatalErr != nil && errors.Is(err, context.Canceled) {
			return fatalErr
		}
		if isPermissionDenied(err) {
			vc.pathsSkipped.Add(1)
			vc.warnf("!!Warning!! permission denied listing %s. Skipping.\n", path)
			return nil
		}
		return fmt.Errorf("failed to list: %s\n%s", vc.searchString, err)
	}

	if pathList == nil {
		vc.pathsSkipped.Add(1)
		vc.warnf("!!Warning!! search-path %s doesn't have any contents. Skipping.\n", path)
		return nil
	}

	if len(pathList.Warnings) > 0 {
		return errors.New(pathList.Warnings[0])
	}

	rawKeys, ok := pathList.Data["keys"].([]interface{})
	if !ok {
		return nil
	}

	var subDirs []searchTask
	var leaves []searchTask
	for _, x := range rawKeys {
		if err := vc.getErr(); err != nil {
			return err
		}

		dirEntry, ok := x.(string)
		if !ok {
			continue
		}
		fullPath := fmt.Sprintf("%s%s", path, dirEntry)
		task := searchTask{
			isDir:         strings.HasSuffix(dirEntry, "/"),
			dirEntry:      dirEntry,
			fullPath:      fullPath,
			searchObjects: searchObjects,
			version:       version,
		}
		if task.isDir {
			subDirs = append(subDirs, task)
		} else {
			leaves = append(leaves, task)
		}
	}

	vc.enqueueTasks(subDirs, leaves)
	return nil
}
