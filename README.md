# LLM tasks

A tiny framework for running **LLM-driven pipelines** from the CLI.  
Each pipeline follows the same lifecycle:

1) **Gather** data
2) **Prompt** the LLM
3) **Verify** & optionally **Refine** until acceptable
4) **Apply** the result (e.g., change files, write output)

## Why

- Keep LLM “tasks” composable and testable
- Separate prompting, verification, and side effects
- Reuse one Runner for many pipelines

## Install

```bash
go build -o llm-tasks ./cmd/llm-tasks
````

## Configure (global)

Edit `configs/app.yaml`:

```yaml
api:
  endpoint: https://api.openai.com/v1
  api_key_env: OPENAI_API_KEY
defaults:
  model: gpt-5-mini
  temperature: 0.2
  max_tokens: 1500
  attempts: 3
  timeout_seconds: 45
```

## Tasks

### 1) sort (Downloads → project folders)

Per-task config: `configs/task.sort.yaml`

```yaml
grant:
  base_directories:
    downloads: "/path/to/Downloads"
    staging: "/path/to/Downloads/_sorted"
  safety:
    dry_run: true         # keep true while testing!
projects:
  - name: "3D_Printing"
    target: "3D_Printing"
    keywords: [ "3mf","stl","obj","mtl","bambu","plate","nozzle" ]
  - name: "Data_CSV"
    target: "Data_CSV"
    keywords: [ "csv","ghcnd","lcd","sales_tax","zip_locale" ]
thresholds:
  min_confidence: 0.6
```

Run:

```bash
export OPENAI_API_KEY=sk-...
./llm-tasks task run \
  --name sort \
  --dry \
  --attempts 3 \
  --timeout 45s \
  --model gpt-5-mini \
  --sort-config ./configs/task.sort.yaml
```

* With `--dry` (or `dry_run: true`), it **prints planned moves** instead of changing files.
* Once output looks good, set `dry_run: false` in `configs/task.sort.yaml` or omit `--dry`.

### 2) changelog (YAML-only)

Per-task config: `configs/task.changelog.yaml` (already in this repo).
Inputs:

* `version` and `date` (pass by flags or env)
* `git_log` from **stdin** (pipe a `git log`)

**Example:**

```bash
# prepare the log (if you have no tags yet, just dump everything)
git log --oneline --no-merges > /tmp/git.log

export OPENAI_API_KEY=sk-...

# run YAML-only changelog task (single canonical way)
./llm-tasks task run \
  --name changelog \
  --changelog-config ./configs/task.changelog.yaml \
  --version v0.1.0 \
  --date 2025-09-27 \
  < /tmp/git.log
```

This prepends the generated section to `CHANGELOG.md`.
You can also set env instead of flags:

```bash
export CHANGELOG_VERSION=v0.1.0
export CHANGELOG_DATE=2025-09-27
./llm-tasks task run --name changelog < /tmp/git.log
```

## How it works

**sort**

* **Gather**: build a file inventory (name, ext, mime, size) under `downloads`, skipping hidden and `_sorted`.
* **Prompt**: send inventory + known projects (names/keywords) to the LLM; ask for JSON only.
* **Verify**: ensure valid JSON, 1:1 items, confidence thresholds, safe folder names; request a **refine** if invalid.
* **Apply**: move files into `<staging>/<TargetSubdir>/...` (or print moves in dry-run).

**changelog**

* **Gather**: read `version`, `date`, and `git_log` (stdin).
* **Prompt**: build Markdown heading and section structure from YAML.
* **Verify**: check heading, require all sections, ensure highlight bullet minimum; refine on violations.
* **Apply**: prepend the section to `CHANGELOG.md` (or print, per YAML).

## Add your own task

Create `tasks/<yourtask>/task.go` implementing:

```go
type Pipeline interface {
    Name() string
    Gather(ctx) (GatherOutput, error)
    Prompt(ctx, gathered) (LLMRequest, error)
    Verify(ctx, gathered, response) (accepted bool, verified VerifiedOutput, refine *RefineRequest, err error)
    Apply(ctx, verified) (ApplyReport, error)
}
```

Register it in `cmd/llm-tasks/task_run.go` and `task_list.go` with a factory (see how `sort` and `changelog` are wired).

Then:

```bash
./llm-tasks task run --name mytask
```

## Testing

```bash
go test ./...
```
