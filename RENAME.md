# Renaming this project

The project name appears in three places. Changing it is a mechanical process;
nothing is dynamically derived beyond `internal/app.Name`.

## What's affected

| Concern | Location |
|---|---|
| Go module path | `go.mod` line 1 + every `import` statement |
| Binary name | `cmd/<name>/` directory name |
| Display name (logs, default DB file) | `internal/app/name.go` — single constant |

## One-shot rename (safe, scripted)

Replace `NEWNAME` with the target name before running:

```bash
OLD=pulse
NEW=NEWNAME

# 1. Rewrite module path in every .go file and in go.mod
find . -type f \( -name '*.go' -o -name 'go.mod' \) \
  -not -path './.git/*' \
  -exec sed -i "s|github.com/rbryce90/${OLD}|github.com/rbryce90/${NEW}|g" {} +

# 2. Rename the binary directory
git mv cmd/${OLD} cmd/${NEW}

# 3. Update the display-name constant
sed -i "s|const Name = \"${OLD}\"|const Name = \"${NEW}\"|" internal/app/name.go

# 4. Update .gitignore anchors
sed -i "s|/${OLD}|/${NEW}|g" .gitignore

# 5. Update README heading
sed -i "s|# ${OLD}|# ${NEW}|" README.md

# 6. Verify
go mod tidy
go build ./...
go vet ./...
go run ./cmd/${NEW}  # Ctrl-C to exit
```

## Manual touch-ups after the script

- `README.md` — any prose references to the old name beyond the `#` heading
- `RENAME.md` — this file, if you care about historical accuracy
- Git remote (`git remote set-url origin ...`) once the GitHub repo is renamed

## Why this works cleanly

- `go.mod`'s module declaration + all imports must match exactly — `sed` handles both in one pass
- `internal/app.Name` is the only place where the name appears as a *runtime string*, so log prefixes and the default DB filename update automatically
- Everything else is directory/file naming, handled by `git mv` (which preserves history)
