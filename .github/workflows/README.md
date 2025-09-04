# GitHub Workflows

## Overview

This repository uses GitHub Actions for CI/CD with the following workflows:

### 1. CI (`ci.yml`)
- **Triggers**: Every push and pull request
- **Purpose**: Run tests, linting, and build verification
- **Jobs**:
  - `test`: Run unit tests with coverage
  - `lint`: Check code formatting and run static analysis
  - `build`: Build binaries for multiple platforms

### 2. Internal Docker Build (`docker-internal.yml`)
- **Triggers**: Every push to any branch
- **Purpose**: Build and push Docker images to GitHub Container Registry (internal use)
- **Registry**: `ghcr.io/fastlane-labs/fastlane-sidecar`
- **Tags**: 
  - `latest-{branch}`: Latest build from each branch
  - `sha-{commit}`: Tagged with commit SHA
  - `dev`: Latest from main branch

### 3. Release (`release.yml`)
- **Triggers**: Git tags starting with `v*` or GitHub releases
- **Purpose**: Publish production artifacts
- **Jobs**:
  - `docker-hub`: Build and push to Docker Hub (public)
  - `debian-package`: Build and publish Debian package to APT repository

## Required Secrets

### For Docker Hub (Release Workflow)
- `DOCKERHUB_USERNAME`: Docker Hub username
- `DOCKERHUB_TOKEN`: Docker Hub access token

### For Debian Package Repository (Release Workflow)
- `AWS_ACCESS_KEY_ID`: AWS access key for S3 bucket
- `AWS_SECRET_ACCESS_KEY`: AWS secret key for S3 bucket
- `PGP_PRIVATE_KEY`: PGP private key for signing APT repository
- `PGP_KEY_ID`: PGP key ID for signing

## Workflow Triggers Summary

| Workflow | Push to Branch | Pull Request | Git Tag | Release |
|----------|---------------|--------------|---------|---------|
| CI | ✅ | ✅ | ❌ | ❌ |
| Docker Internal | ✅ | ❌ | ❌ | ❌ |
| Release | ❌ | ❌ | ✅ | ✅ |

## Notes

- Internal Docker images are stored in GitHub Container Registry (GHCR) and are private to the organization
- Public Docker images are published to Docker Hub under `fastlanelabs/fastlane-sidecar`
- Debian packages are published to `https://pkg.fastlane.xyz/` APT repository