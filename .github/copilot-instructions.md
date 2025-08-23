# vault-kv-search

vault-kv-search is a Go-based CLI tool for recursively searching HashiCorp Vault KV stores (v1 and v2). The tool uses the Cobra CLI framework and provides JSON and text output formats with support for regex searching.

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Working Effectively

### Bootstrap and Build
- Install Go 1.24.0 or later: `go version` should show 1.24+
- Install Docker: `docker --version` (required for testing)
- Clone and build:
  - `cd /path/to/vault-kv-search`
  - `make vault-kv-search` -- takes ~20 seconds first time, ~3 seconds subsequent builds. NEVER CANCEL. Set timeout to 60+ seconds.
- Verify build: `./vault-kv-search --help`

### Testing
- Run all tests: `make test-docker` -- takes ~20 seconds including Docker lifecycle. NEVER CANCEL. Set timeout to 60+ seconds.
- Manual test setup (if needed):
  - `docker compose -f docker-compose.test.yml up -d`
  - `export VAULT_ADDR=http://localhost:8200`
  - `export VAULT_TOKEN=test-token`
  - `export VAULT_SKIP_VERIFY=true`
  - `make test` -- takes ~5 seconds. NEVER CANCEL. Set timeout to 30+ seconds.
  - `docker compose -f docker-compose.test.yml down`

### Code Quality
- Format code: `go fmt ./...`
- Static analysis: `go vet ./...`
- CI uses golangci-lint (not available locally) - ensure CI passes
- Always run formatting and vetting before committing or CI will fail

## Validation

### Always manually validate changes with these scenarios:
1. **Build validation**: `make vault-kv-search && ./vault-kv-search --help`
2. **Version check**: `./vault-kv-search version`
3. **Basic functionality test** (with Vault running):
   - Start test Vault: `docker compose -f docker-compose.test.yml up -d`
   - Wait for ready: `timeout 30 bash -c 'until curl -f http://localhost:8200/v1/sys/health; do sleep 2; done'`
   - Set environment: `export VAULT_ADDR=http://localhost:8200 VAULT_TOKEN=test-token VAULT_SKIP_VERIFY=true`
   - Test search: `./vault-kv-search secret/ test` (should show no results but not error)
   - Cleanup: `docker compose -f docker-compose.test.yml down`
4. **Test suite**: `make test-docker`

### After making changes:
- ALWAYS run the build validation scenario
- ALWAYS run `go fmt ./...` and `go vet ./...`
- ALWAYS run `make test-docker` to ensure tests pass
- Test any new CLI flags or functionality manually with a running Vault instance

## Project Structure

### Key directories and files:
```
/
├── cmd/                          # Command implementations
│   ├── root.go                   # CLI root command and flags
│   ├── vault-kv-search.go        # Main search logic
│   ├── vault-kv-search_test.go   # Test suite
│   └── version.go                # Version command
├── main.go                       # Entry point
├── Makefile                      # Build targets
├── go.mod                        # Go module definition
├── docker-compose.test.yml       # Test environment
├── test-with-docker.sh           # Automated test script
└── .github/workflows/            # CI/CD pipelines
    ├── build-test.yaml           # Build and test workflow
    ├── golangci-lint.yml         # Linting workflow
    └── codeql-analysis.yml       # Security analysis
```

### Important command patterns:
- `make vault-kv-search` or `make all` - Build binary
- `make test` - Run tests (requires running Vault)
- `make test-docker` - Run tests with managed Docker Vault
- `make clean` - Remove built binaries

## Common Development Tasks

### Adding new CLI flags:
1. Edit `cmd/root.go` - add flag definition in `init()` function
2. Add flag processing in command logic in `cmd/vault-kv-search.go`
3. Update help text in the command definition
4. Add tests in `cmd/vault-kv-search_test.go`
5. Validate with build and test scenarios

### Modifying search logic:
1. Edit `cmd/vault-kv-search.go` - main logic in `VaultKvSearch()` function
2. Test changes against both KV v1 and v2 engines
3. Ensure JSON and text output formats work correctly
4. Add test cases in `cmd/vault-kv-search_test.go`

### Testing patterns:
- Tests use Docker-based Vault with token `test-token`
- Helper function `testVaultServer()` sets up test client
- Tests create temporary KV mounts for isolation
- Always clean up test resources in defer functions

## Build Information

The build process injects version information via ldflags:
- Version from git tags
- Build date, user, branch, revision
- Go version and platform

## Environment Variables

For runtime:
- `VAULT_ADDR` - Vault server URL (required)
- `VAULT_TOKEN` - Authentication token (required)
- `VAULT_SKIP_VERIFY` - Skip TLS verification (optional, useful for dev)

For testing:
- Same as runtime, automatically set by test scripts
- Default test values: `http://localhost:8200`, `test-token`, `true`

## Common Issues and Solutions

1. **Build fails with "go: cannot find main module"**
   - Ensure you're in the repository root directory
   - Run `go mod tidy` to ensure dependencies are correct

2. **Tests fail with connection refused**
   - Ensure Docker is running: `docker --version`
   - Check if port 8200 is available: `ss -ln | grep 8200`
   - Use `make test-docker` instead of `make test`

3. **CI linting failures**
   - Run `go fmt ./...` and `go vet ./...` locally
   - golangci-lint runs only in CI - ensure CI passes before merging

4. **Binary not found after build**
   - Check that `make vault-kv-search` completed successfully
   - Binary is created in repository root as `vault-kv-search`
   - Add execute permissions if needed: `chmod +x vault-kv-search`