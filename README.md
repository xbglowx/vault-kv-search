# ⚠️ Looking for a maintainer ⚠️
Looking for someone to take this project from me. https://github.com/xbglowx/vault-kv-search/issues/121

# vault-kv-search
[![Build and Test](https://github.com/xbglowx/vault-kv-search/actions/workflows/build-test.yaml/badge.svg)](https://github.com/xbglowx/vault-kv-search/actions/workflows/build-test.yaml) [![CodeQL](https://github.com/xbglowx/vault-kv-search/actions/workflows/codeql-analysis.yml/badge.svg)](https://github.com/xbglowx/vault-kv-search/actions/workflows/codeql-analysis.yml) [![golangci-lint](https://github.com/xbglowx/vault-kv-search/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/xbglowx/vault-kv-search/actions/workflows/golangci-lint.yml)

`vault-kv-search` is a command-line tool for recursively searching for secrets within HashiCorp Vault's Key-Value (KV) stores (versions 1 and 2). It helps you quickly find where a specific value, key, or path is located across many secrets, making it an essential utility for auditing and managing your Vault environment.

## Table of Contents
- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
  - [Prerequisites](#prerequisites)
  - [Command Flags](#command-flags)
  - [Examples](#examples)
- [Development](#development)
  - [Building from Source](#building-from-source)
  - [Running Tests](#running-tests)
- [License](#license)

## Features
- **Recursive Search:** Traverses nested paths in Vault to find secrets.
- **Multi-Target Search:** Search within secret values, keys, or paths.
- **Regex Support:** Use regular expressions for powerful and flexible search patterns.
- **KV v1 and v2 Support:** Works seamlessly with both versions of the KV secrets engine.
- **Multiple Output Formats:** Choose between human-readable text and structured `json` output.
- **Cross-Platform:** Builds for Linux, macOS, and Windows.
- **Search All Stores:** Can automatically discover and search all mounted KV stores.

## Installation
You can download the latest pre-compiled binaries for your operating system from the [**GitHub Releases**](https://github.com/xbglowx/vault-kv-search/releases) page.

1.  Download the appropriate binary for your system (e.g., `vault-kv-search-linux-amd64`).
2.  Make the binary executable: `chmod +x vault-kv-search-*`
3.  (Optional) Move it to a directory in your `PATH` for easy access: `sudo mv vault-kv-search-* /usr/local/bin/vault-kv-search`

## Usage

### Prerequisites
The tool requires the following environment variables to be set to authenticate with your Vault server:
```sh
export VAULT_ADDR="https://your-vault-server:8200"
export VAULT_TOKEN="s.YourVaultToken"
```
You may also need `VAULT_SKIP_VERIFY=true` if your Vault instance uses a self-signed certificate.

### Command Flags
```
Usage:
  vault-kv-search [search-path] <search-string> [flags]

Flags:
  -c, --crawling-delay int   Crawling delay in milliseconds (default 15)
  -h, --help                 help for vault-kv-search
  -j, --json                 Enable JSON output
  -k, --kv-version int       KV store version
      --regex                Enable regex search
  -s, --search stringArray   What to search for: path, key, or value (default [value])
      --show-secrets         Show secret values in output
  -t, --timeout int          Vault client timeout in seconds (default 30)
      --version              version for vault-kv-search
```

### Examples

1.  **Search values for a substring:**
    ```sh
    vault-kv-search secret/production/ "api.example.com"
    ```

2.  **Search keys for a substring:**
    ```sh
    vault-kv-search --search=key secret/ "username"
    ```

3.  **Search both keys and values:**
    ```sh
    vault-kv-search --search=key --search=value secret/ "database"
    ```

4.  **Search using a regular expression:**
    ```sh
    vault-kv-search --regex secret/ "^db-"
    ```

5.  **Search for a secret by its path (name):**
    ```sh
    vault-kv-search --search=path secret/ "ssh-keys"
    ```

6.  **Search all mounted KV stores at once:**
    *This requires permissions to list mounts.*
    ```sh
    vault-kv-search "sensitive-data"
    ```

7.  **Show the secret value in the output:**
    ```sh
    vault-kv-search --show-secrets secret/ "password123"
    ```

8.  **Output results in JSON format:**
    ```sh
    vault-kv-search --json secret/ "user@example.com"
    ```

## Development

### Building from Source
**Prerequisites:**
- Go 1.24+
- Make

To build the binary from the source code:
```sh
make vault-kv-search
```
The compiled binary will be available in the root of the project directory.

### Running Tests
Tests use [testcontainers-go](https://golang.testcontainers.org/) to automatically start and stop Vault containers, providing complete isolation and eliminating the need for manual container management.

**Prerequisites:**
- Docker (Docker Desktop, Colima, or similar)

```sh
make test
```

#### Using Colima
If you're using [Colima](https://github.com/abiosoft/colima) instead of Docker Desktop, you need to set the following environment variables:

```sh
export DOCKER_HOST="unix://${HOME}/.colima/default/docker.sock"
export TESTCONTAINERS_RYUK_DISABLED=true
make test
```

You can add these exports to your shell profile (`~/.zshrc` or `~/.bashrc`) to make them persistent.

## License
This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
