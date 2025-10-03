# LLM Tasks

Run structured tasks with LLMs from the command line.
Tasks are declared in `config.yaml` and executed through a simple CLI.

## Installation

```bash
git clone https://github.com/temirov/llm-tasks.git
cd llm-tasks
go build -o llm-tasks ./cmd/llm-tasks
```

## Configuration

All configuration lives in a single root-level file `config.yaml`.

```yaml
common:
  log_level: info
  log_format: structured
  api:
    endpoint: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
  defaults:
    attempts: 3
    timeout_seconds: 45

models:
  - name: gpt-5-mini
    provider: openai
    model_id: gpt-5-mini
    default: true
    supports_temperature: false
    default_temperature: 1
    max_completion_tokens: 1500

recipes:
  - name: sort
    enabled: true
    model: gpt-5-mini
    # task-specific keys omitted for brevity
  - name: changelog
    enabled: true
    model: gpt-5-mini
    # task-specific keys omitted for brevity
```

### Sections

* **common**
  Global settings: logging, API endpoint, API key environment variable, and default retry/timeout values.

* **models**
  Declares available models. Each entry specifies provider, model ID, whether it is the default, token limits, and
  temperature support.

* **recipes**
  Array of enabled tasks. The recipe `name` selects the task implementation (e.g., `sort`, `changelog`) and binds it to a
  model. Disabled recipes are ignored unless explicitly listed with `--all`.

### Embedded defaults

When no user configuration file is found, the embedded fallback remains active. Operators must provide the following
environment variables so the sort recipe can resolve directories safely:

* `SORT_DOWNLOADS_DIR` — absolute path to the directory containing inbound files that require sorting.
* `SORT_STAGING_DIR` — absolute path to the directory where categorized files are placed.

If either environment variable is missing, the sort recipe should be disabled in a custom configuration or the
variables should be exported before invoking the CLI.

## Usage

List registered tasks (enabled by default):

```bash
./llm-tasks list --config ./config.yaml
```

Example output:

```
sort        (enabled, model=gpt-5-mini)
changelog   (enabled, model=gpt-5-mini)
```

Show disabled recipes as well:

```bash
./llm-tasks list --config ./config.yaml --all
```

### Run a task

Run any recipe directly by name:

```bash
./llm-tasks run changelog --config ./config.yaml
./llm-tasks run sort --config ./config.yaml
```

Optional flags:

* `--model` override recipe’s model by name
* `--attempts` max refine attempts (default from config)
* `--timeout` per-attempt timeout
* `--version` changelog release label and starting reference (mutually exclusive with `--date`; exports to `CHANGELOG_VERSION`)
* `--date` changelog release date and git cutoff (mutually exclusive with `--version`; exports to `CHANGELOG_DATE`)
* `--dry` dry-run mode (for tasks that support it)

### Example: changelog

Summarize recent commits into release notes without piping logs. The command assembles commit messages and diffs automatically:

```bash
./llm-tasks run changelog --config ./config.yaml
```

If no semantic version tag exists (or you want to define the starting point explicitly), provide either a version tag or an ISO-8601 date:

```bash
./llm-tasks run changelog --config ./config.yaml --version v0.9.0
./llm-tasks run changelog --config ./config.yaml --date 2025-09-27T00:00:00Z
```

`--version` and `--date` are mutually exclusive. They also populate the `CHANGELOG_VERSION` and `CHANGELOG_DATE` environment variables so the changelog pipeline receives release metadata automatically. When you omit both flags the CLI bumps the most recent `vX.Y.Z` tag to the next patch version and uses today’s UTC date.

### Example: sort

Organize files into project-based subfolders:

```bash
./llm-tasks run sort --config ./config.yaml --dry
```

Dry mode shows actions without applying changes.

## Development

Format and run tests:

```bash
go fmt ./... && go vet ./... && go test ./...
```

---

## License

LLM Tasks is released under the [MIT License](MIT-LICENSE).
