# Archive Formats

Supported archive formats for documentation uploads.

## Supported Formats

| Format | Extensions | Notes |
|--------|------------|-------|
| ZIP | `.zip` | Most common, widely supported |
| Gzip tarball | `.tar.gz`, `.tgz` | Unix standard |
| Bzip2 tarball | `.tar.bz2`, `.tbz2` | Better compression |
| XZ tarball | `.tar.xz`, `.txz` | Best compression |
| 7-Zip | `.7z` | Cross-platform |

## Archive Structure

### Recommended Structure

Place an `index.html` at the root of your archive:

```
docs.zip
├── index.html
├── getting-started.html
├── api/
│   ├── index.html
│   └── endpoints.html
├── css/
│   └── style.css
└── js/
    └── main.js
```

### Single Directory

If your archive contains a single root directory, Asiakirjat extracts from that directory:

```
docs.zip
└── html/           # This directory is "flattened"
    ├── index.html
    ├── api/
    └── css/
```

This supports common documentation tools that output to a `html/` or `public/` directory.

### Entry Points

Asiakirjat looks for entry points in this order:
1. `index.html`
2. `index.htm`

If no index file is found, directory listing is shown.

## Creating Archives

### ZIP

```bash
# From build output
cd dist/docs
zip -r ../../docs.zip .

# Or specify files
zip -r docs.zip html/
```

### tar.gz

```bash
# Create from directory
tar -czf docs.tar.gz -C dist/docs .

# Or with directory prefix
tar -czf docs.tar.gz dist/docs
```

### tar.bz2

```bash
tar -cjf docs.tar.bz2 -C dist/docs .
```

### tar.xz

```bash
tar -cJf docs.tar.xz -C dist/docs .
```

### 7z

```bash
7z a docs.7z dist/docs/*
```

## Documentation Tool Outputs

### Sphinx

```bash
cd docs
make html
tar -czf docs.tar.gz -C _build/html .
```

### MkDocs

```bash
mkdocs build
zip -r docs.zip site
```

### Docusaurus

```bash
npm run build
tar -czf docs.tar.gz -C build .
```

### Hugo

```bash
hugo
zip -r docs.zip public
```

### Jekyll

```bash
bundle exec jekyll build
tar -czf docs.tar.gz -C _site .
```

### VuePress

```bash
npm run build
zip -r docs.zip src/.vuepress/dist
```

## Size Limits

There is no hard-coded size limit, but consider:
- Upload timeout (typically 2 minutes)
- Available disk space
- Search indexing time increases with size

For very large documentation sets (>500MB), consider:
- Splitting into multiple projects
- Excluding unnecessary assets
- Compressing images

## Compression Comparison

| Format | Compression | Speed | Compatibility |
|--------|-------------|-------|---------------|
| ZIP | Good | Fast | Best |
| tar.gz | Good | Fast | Unix |
| tar.bz2 | Better | Slower | Unix |
| tar.xz | Best | Slowest | Unix |
| 7z | Best | Slow | Cross-platform |

For most use cases, ZIP or tar.gz provides the best balance of compression and speed.

## Troubleshooting

**"Unsupported archive format"**
- Check file extension matches content
- Ensure file isn't corrupted
- Verify format is in supported list

**"No index.html found"**
- Check archive structure
- Ensure index.html is at root (or in single subdirectory)

**"Archive extraction failed"**
- Check archive isn't password protected
- Verify archive isn't corrupted (`zip -T` or `tar -tzf`)
- Check for unsupported compression in archive
