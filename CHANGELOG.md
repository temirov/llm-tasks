## v0.0.4 - 2025-10-3

### Highlights
- Standardised the changelog task schema with typed inputs, mutual-exclusion rules, and a `--root` flag so release notes always target the intended repository.
- Introduced a resilient configuration loader with embedded defaults, making new installs work out of the box.

### Features ✨
- Enabled the sort pipeline to honour CLI source/destination overrides while keeping configuration defaults.
- Exposed changelog metadata flags (version/date) and wired them through the CLI, runner, and task.
- Added an embedded configuration loader capable of falling back to bundled defaults when no user config is present.

### Improvements ⚙️
- Normalised sort grant directories by sanitising embedded defaults and loading overrides from the environment.
- Validated blank directory entries and surfaced permission errors for unreadable configuration candidates.
- Truncated changelog prompts intelligently, excluding changelog-related edits so summaries stay concise and relevant.

### Bug Fixes
- Restored the changelog command to a functional state and enforced the version flag as required input.
- Fixed sort task validation for blank grant directories.
- Ensured configuration loader failure modes propagate clear errors instead of silently continuing.

### CI & Maintenance
- Excluded transient `bin/` artefacts from version control to keep the repository clean.

## v0.0.3 - 2025-09-28

### Highlights
- Published the module for direct installation with `go install`, making distribution straightforward.

### Improvements ⚙️
- Tidied release metadata by removing an erroneous package description.


## v0.0.2 - 2025-09-28

### Highlights
- Consolidated task configuration into a single unified `config.yaml`, simplifying project setup.

### Improvements ⚙️
- Refreshed documentation and internal wiring to match the unified configuration model.


## v0.0.1 - 2025-09-28

### Highlights
- Initial release providing `llm-tasks` CLI commands (`run`, `list`) and foundational sort/changelog tasks.

### Improvements ⚙️
- Added core configuration loader, OpenAI adapter, and task scaffolding that power the initial workflows.

