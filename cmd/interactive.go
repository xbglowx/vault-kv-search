package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

func discoverMountsForInteractive(opts SearchOptions) []startPathInfo {
	client, err := newVaultClient(opts)
	if err != nil {
		return nil
	}

	vc := &vaultClient{
		logical: client.Logical(),
		sys:     client.Sys(),
	}
	mounts, err := vc.getAllKvStores()
	if err != nil {
		return nil
	}
	slices.SortFunc(mounts, func(a, b startPathInfo) int {
		return strings.Compare(a.path, b.path)
	})
	return mounts
}

func runInteractivePrompt(in io.Reader, out io.Writer, opts *SearchOptions, mounts []startPathInfo) error {
	scanner := bufio.NewScanner(in)
	promptRaw := func(label string) (string, error) {
		if _, err := fmt.Fprint(out, label); err != nil {
			return "", err
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", io.EOF
		}
		return scanner.Text(), nil
	}
	prompt := func(label string) (string, error) {
		raw, err := promptRaw(label)
		return strings.TrimSpace(raw), err
	}

	_, _ = fmt.Fprintln(out, "Interactive Vault KV Search")
	_, _ = fmt.Fprintln(out, "---------------------------")

	var selectedPath string
	if len(mounts) > 0 {
		_, _ = fmt.Fprintln(out, "Available KV stores:")
		_, _ = fmt.Fprintln(out, "  [0] All KV stores (default)")
		for i, m := range mounts {
			ver := max(1, m.kvVersion)
			_, _ = fmt.Fprintf(out, "  [%d] %s (KV v%d)\n", i+1, m.path, ver)
		}
		ans, err := prompt("Select store number or enter a custom path [0]: ")
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if ans != "" && ans != "0" {
			if idx, convErr := strconv.Atoi(ans); convErr == nil && idx >= 1 && idx <= len(mounts) {
				selectedPath = mounts[idx-1].path
				if opts.KvVersion == 0 {
					opts.KvVersion = max(1, mounts[idx-1].kvVersion)
				}
			} else {
				selectedPath = ans
			}
		}
	} else {
		ans, err := prompt("Search path (leave blank for all KV stores): ")
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		selectedPath = ans
	}

	query, err := promptRaw("Search query (substring or regex): ")
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if query == "" {
		return errors.New("search query cannot be empty")
	}

	if selectedPath == "" {
		opts.Args = []string{query}
	} else {
		opts.Args = []string{selectedPath, query}
	}

	defaultTargets := "value"
	if len(opts.SearchObjects) > 0 {
		defaultTargets = strings.Join(opts.SearchObjects, ",")
	} else {
		opts.SearchObjects = []string{"value"}
	}
	targetsAns, err := prompt(fmt.Sprintf("Search targets (comma-separated: value,key,path) [%s]: ", defaultTargets))
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if targetsAns != "" {
		parts := strings.Split(targetsAns, ",")
		cleaned := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				cleaned = append(cleaned, t)
			}
		}
		if err := validateSearchObjects(cleaned); err != nil {
			return err
		}
		opts.SearchObjects = cleaned
	}

	if !opts.IgnoreCase {
		icAns, err := prompt("Case-insensitive search? [y/N]: ")
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		opts.IgnoreCase = isYes(icAns)
	}

	if !opts.UseRegex {
		reAns, err := prompt("Use regular expression? [y/N]: ")
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		opts.UseRegex = isYes(reAns)
	}

	if !opts.ShowSecrets {
		secAns, err := prompt("Show secret values in output? [y/N]: ")
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		opts.ShowSecrets = isYes(secAns)
	}

	_, _ = fmt.Fprintln(out)
	return nil
}

func isYes(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes"
}
