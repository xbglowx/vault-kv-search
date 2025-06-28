#!/bin/bash
set -e

echo "Starting Vault container for testing..."
docker compose -f docker-compose.test.yml up -d

echo "Waiting for Vault to be ready..."
timeout 30 bash -c 'until curl -f http://localhost:8200/v1/sys/health; do sleep 2; done'

echo "Running tests..."
export VAULT_ADDR=http://localhost:8200
export VAULT_TOKEN=test-token
export VAULT_SKIP_VERIFY=true

go test -v ./...

echo "Stopping Vault container..."
docker compose -f docker-compose.test.yml down

echo "Tests completed successfully!"