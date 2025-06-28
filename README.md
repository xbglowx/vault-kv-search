# ⚠️ Looking for a maintainer ⚠️
Looking for someone to take this project from me. https://github.com/xbglowx/vault-kv-search/issues/121

# vault-kv-search [![Build and Test](https://github.com/xbglowx/vault-kv-search/actions/workflows/build-test.yaml/badge.svg)](https://github.com/xbglowx/vault-kv-search/actions/workflows/build-test.yaml) [![CodeQL](https://github.com/xbglowx/vault-kv-search/actions/workflows/codeql-analysis.yml/badge.svg)](https://github.com/xbglowx/vault-kv-search/actions/workflows/codeql-analysis.yml) [![golangci-lint](https://github.com/xbglowx/vault-kv-search/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/xbglowx/vault-kv-search/actions/workflows/golangci-lint.yml)

This tool is compatible with secrets kv v1 and v2.

> **Note**: Testing has been migrated to use Docker-based Vault containers for more realistic testing environments.

## Example Usage

- Export or prepend command with VAULT_ADDR and your VAULT_TOKEN

  ```
  > export VAULT_ADDR=https://vaultserver:8200
  > export VAULT_TOKEN=$(cat ~/.vault-token)
  ```

- Search values for the substring 'example.com':

  `> vault-kv-search secret/ example.com`

- Search keys for substring 'example.com':

  `> vault-kv-search --search=key secret/ example.com`

- Search keys and values for substring 'example.com':

  `> vault-kv-search --search=value --search=key secret/ example.com`

- Search keys and values for substring starting with 'example.com':

  `> vault-kv-search --search=value --search=key --regex secret/ '^example.com'`

- Search secret name containing substring 'sshkeys':

  `> vault-kv-search --search=path secret/ sshkeys`

- Search all mounted KV secret engines. Since this requires listing all mounts, the operator must have proper permissions to do so.

  `> vault-kv-search example.com`

- To display the secrets, and not only the vault path, use the `--showsecrets` parameter.

## Development

### Running Tests

Tests require a running Vault instance. You can run tests in two ways:

#### Using Docker (Recommended)

```bash
# Run tests with automatically managed Vault container
make test-docker
```

This will:
1. Start a Vault dev server in a Docker container
2. Run all tests against the containerized Vault
3. Clean up the container when done

#### Manual Setup

```bash
# Start Vault container manually
docker compose -f docker-compose.test.yml up -d

# Set environment variables
export VAULT_ADDR=http://localhost:8200
export VAULT_TOKEN=test-token
export VAULT_SKIP_VERIFY=true

# Run tests
make test

# Clean up
docker compose -f docker-compose.test.yml down
```
