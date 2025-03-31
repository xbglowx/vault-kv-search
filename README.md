# ⚠️ Looking for a maintainer ⚠️
Looking for someone to take this project from me. https://github.com/xbglowx/vault-kv-search/issues/121

# vault-kv-search [![Build and Test](https://github.com/xbglowx/vault-kv-search/actions/workflows/build-test.yaml/badge.svg)](https://github.com/xbglowx/vault-kv-search/actions/workflows/build-test.yaml) [![CodeQL](https://github.com/xbglowx/vault-kv-search/actions/workflows/codeql-analysis.yml/badge.svg)](https://github.com/xbglowx/vault-kv-search/actions/workflows/codeql-analysis.yml) [![golangci-lint](https://github.com/xbglowx/vault-kv-search/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/xbglowx/vault-kv-search/actions/workflows/golangci-lint.yml)

This tool is compatible with secrets kv v1 and v2.

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
