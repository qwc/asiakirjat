# CI/CD Integration

This guide shows you how to integrate Asiakirjat with your CI/CD pipeline for automated documentation deployment.

## Overview

Automate documentation uploads when:
- A new version is released
- Documentation source changes
- A scheduled build runs

## Prerequisites

1. An API token (see [API Tokens](api-tokens.md))
2. Documentation built as an archive (.zip, .tar.gz, etc.)
3. CI/CD system with HTTP request capability

## General Workflow

1. Build your documentation (Sphinx, MkDocs, Docusaurus, etc.)
2. Create an archive of the output
3. Upload to Asiakirjat using the API

## GitHub Actions

### Basic Upload

```yaml
name: Deploy Documentation

on:
  push:
    tags:
      - 'v*'

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Build docs
        run: |
          npm install
          npm run build:docs

      - name: Create archive
        run: zip -r docs.zip ./dist/docs

      - name: Upload to Asiakirjat
        env:
          ASIAKIRJAT_TOKEN: ${{ secrets.ASIAKIRJAT_TOKEN }}
          ASIAKIRJAT_URL: ${{ vars.ASIAKIRJAT_URL }}
        run: |
          curl -f -X POST \
            -H "Authorization: Bearer $ASIAKIRJAT_TOKEN" \
            -F "file=@docs.zip" \
            -F "tag=${{ github.ref_name }}" \
            "$ASIAKIRJAT_URL/api/project/my-project/upload"
```

### With Latest Tag

```yaml
- name: Upload versioned docs
  run: |
    curl -f -X POST \
      -H "Authorization: Bearer $ASIAKIRJAT_TOKEN" \
      -F "file=@docs.zip" \
      -F "tag=${{ github.ref_name }}" \
      "$ASIAKIRJAT_URL/api/project/my-project/upload"

- name: Upload as latest
  run: |
    curl -f -X POST \
      -H "Authorization: Bearer $ASIAKIRJAT_TOKEN" \
      -F "file=@docs.zip" \
      -F "tag=latest" \
      "$ASIAKIRJAT_URL/api/project/my-project/upload"
```

## GitLab CI

```yaml
stages:
  - build
  - deploy

build_docs:
  stage: build
  script:
    - npm install
    - npm run build:docs
    - tar -czf docs.tar.gz -C dist/docs .
  artifacts:
    paths:
      - docs.tar.gz

deploy_docs:
  stage: deploy
  script:
    - |
      curl -f -X POST \
        -H "Authorization: Bearer $ASIAKIRJAT_TOKEN" \
        -F "file=@docs.tar.gz" \
        -F "tag=$CI_COMMIT_TAG" \
        "$ASIAKIRJAT_URL/api/project/my-project/upload"
  only:
    - tags
  dependencies:
    - build_docs
```

## Jenkins Pipeline

```groovy
pipeline {
    agent any

    environment {
        ASIAKIRJAT_URL = 'https://docs.example.com'
    }

    stages {
        stage('Build Docs') {
            steps {
                sh 'npm install && npm run build:docs'
                sh 'zip -r docs.zip dist/docs'
            }
        }

        stage('Deploy Docs') {
            when {
                tag pattern: "v\\d+\\.\\d+\\.\\d+", comparator: "REGEXP"
            }
            steps {
                withCredentials([string(credentialsId: 'asiakirjat-token', variable: 'TOKEN')]) {
                    sh '''
                        curl -f -X POST \
                            -H "Authorization: Bearer $TOKEN" \
                            -F "file=@docs.zip" \
                            -F "tag=${TAG_NAME}" \
                            "${ASIAKIRJAT_URL}/api/project/my-project/upload"
                    '''
                }
            }
        }
    }
}
```

## Azure DevOps

```yaml
trigger:
  tags:
    include:
      - v*

pool:
  vmImage: 'ubuntu-latest'

steps:
  - task: NodeTool@0
    inputs:
      versionSpec: '18.x'

  - script: |
      npm install
      npm run build:docs
      zip -r docs.zip dist/docs
    displayName: 'Build Documentation'

  - script: |
      curl -f -X POST \
        -H "Authorization: Bearer $(ASIAKIRJAT_TOKEN)" \
        -F "file=@docs.zip" \
        -F "tag=$(Build.SourceBranchName)" \
        "$(ASIAKIRJAT_URL)/api/project/my-project/upload"
    displayName: 'Upload to Asiakirjat'
```

## Common Documentation Tools

### Sphinx

```bash
cd docs
make html
cd _build
zip -r docs.zip html
```

### MkDocs

```bash
mkdocs build
zip -r docs.zip site
```

### Docusaurus

```bash
npm run build
zip -r docs.zip build
```

### Hugo

```bash
hugo
zip -r docs.zip public
```

## Error Handling

Always use `-f` flag with curl to fail on HTTP errors:

```bash
curl -f -X POST ... || exit 1
```

Check response for success:

```bash
response=$(curl -s -w "\n%{http_code}" -X POST ...)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | head -n-1)

if [ "$http_code" != "200" ]; then
    echo "Upload failed: $body"
    exit 1
fi
```

## Best Practices

1. **Use version tags**: Upload with semantic version tags (`v1.2.3`)
2. **Update `latest`**: Also upload as `latest` for easy linking
3. **Fail fast**: Use `-f` flag to catch errors early
4. **Secure tokens**: Store tokens in CI/CD secrets, never in code
5. **Project-scoped tokens**: Use the minimum required permissions
