# GitHub Workflows

## Overview

This repository uses GitHub Actions for CI/CD with the following workflows:

### 1. CI (`ci.yml`)
- **Triggers**: Every push to any branch
- **Purpose**: Run tests, linting, and build verification
- **Jobs**:
  - `test`: Run unit tests with coverage
  - `lint`: Check code formatting and run static analysis
  - `build`: Build binaries for multiple platforms

### 2. Build and Publish (`build-and-publish.yml`)
- **Triggers**:
  - Git tags starting with `v*` (e.g., `v1.0.0`)
  - Manual workflow dispatch
- **Purpose**: Build and publish production artifacts
- **Jobs**:
  - `build-and-push-docker`: Build and push Docker images to GHCR
  - `build-and-publish-debian`: Build and publish Debian packages to APT repository

#### Version Determination
- **Tag push** (e.g., `v1.0.0`):
  - Package version: `1.0.0`
  - Docker tags: `1.0.0`, `1.0`, `1`, `latest`
- **Manual trigger**:
  - Package version: `0~dev.<commit-sha>`
  - Docker tags: `dev`, `sha-<commit-sha>`

## Required Secrets

### For Docker Images (GHCR)
- `GITHUB_TOKEN`: Automatically provided by GitHub Actions

### For Debian Package Repository
- `AWS_ACCESS_KEY_ID`: AWS access key for S3 bucket
- `AWS_SECRET_ACCESS_KEY`: AWS secret key for S3 bucket
- `APT_SIGNING_KEY`: Base64-encoded GPG private key for signing APT repository

### Optional Variables
- `AWS_REGION`: AWS region for S3 (default: `eu-central-1`)
- `S3_APT_BUCKET`: S3 bucket name for APT repository (default: `fastlane-apt-repo`)

## Workflow Triggers Summary

| Workflow | Push to Branch | Git Tag | Manual Trigger |
|----------|---------------|---------|----------------|
| CI | ✅ | ❌ | ❌ |
| Build and Publish | ❌ | ✅ | ✅ |

## Publishing Artifacts

### Docker Images
- **Registry**: `ghcr.io/fastlane-labs/fastlane-sidecar`
- **Release tags**: Semver tags + `latest`
- **Dev tags**: `dev`, `sha-<commit-sha>`

### Debian Packages
- **APT Repository**: S3-based APT repository
- **Suite**: `stable` (works across all Ubuntu/Debian versions)
- **Architecture**: `amd64`
- **GitHub Releases**: Packages are also attached to GitHub releases (on tag push)