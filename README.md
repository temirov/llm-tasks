# llm-tasks

Run structured tasks with LLMs from the command line.  
Tasks are defined in recipes and executed through a simple CLI.

## Installation

```bash
git clone https://github.com/temirov/llm-tasks.git
cd llm-tasks
go build -o llm-tasks ./cmd/llm-tasks
````

## Configuration

All configurations are centralized in a single root-level file:

```yaml
# config.yaml
common:
  log_level: info
  log_format: structured

models:
  - name: gpt-5-mini
    endpoint: https://api.openai.com/v1
    default: true

recipes:
  - name: sort
    type: sort
    config_path: ./configs/task.sort.yaml   # if extra metadata needed
  - name: changelog
    type: changelog
    config_path: ./configs/task.changelog.yaml
```

* **common**: logging and global settings
* **models**: available model definitions (one should have `default: true`)
* **recipes**: array of tasks to register (`sort`, `changelog`, …)

## Usage

List available tasks:

```bash
./llm-tasks task list --config ./config.yaml
```

### Sort task

Sorts a folder of files into project-based subfolders:

```bash
./llm-tasks task run \
  --name sort \
  --config ./config.yaml
```

Supports `--dry` mode to preview without applying:

```bash
./llm-tasks task run \
  --name sort \
  --config ./config.yaml \
  --dry
```

### Changelog task

Summarize a git log into Markdown release notes:

```bash
git log --oneline --no-merges HEAD~20..HEAD > /tmp/git.log

./llm-tasks task run \
  --name changelog \
  --config ./config.yaml \
  --version v0.1.0 \
  --date 2025-09-27 \
  < /tmp/git.log
```

## Development

Format and test:

```bash
go fmt ./... && go vet ./... && go test ./...
```

---

## License


openai_extract is released under the [MIT License](MIT-LICENSE).
