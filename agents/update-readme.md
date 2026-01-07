# Adding screenshots with termshot

## Prerequisites
- termshot installed and available in PATH
- This repository cloned locally

## Steps

```bash
set -euo pipefail
ROOT="$HOME/git/asynkron/QuickDup"
IMGDIR="$ROOT/assets/images"
mkdir -p "$IMGDIR"
cd "$ROOT"

# Take a screenshot of a command invocation
termshot -- <your command here>

# Save the resulting image with a descriptive name
cp -f out.png "$IMGDIR/<your-image-name>.png"

# Append to the README (example snippet)
cat >> README.md <<EOT

## Screenshots ($(date -u +%Y-%m-%dT%H:%M:%S.%3NZ))

Command: <your command here>

![<description>](assets/images/<your-image-name>.png)
EOT

# Commit and push
git add README.md assets/images/*
git commit -m "Add screenshot <your-image-name>.png"
git push
```

## Notes
- Repeat the termshot/copy/append steps for additional variations (e.g., different flags).
- Keep images under assets/images and reference them with relative paths in README.
