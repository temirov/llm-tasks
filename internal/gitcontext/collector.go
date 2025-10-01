// Package gitcontext provides helpers to extract git history for changelog generation.
package gitcontext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

type Options struct {
	WorkingDir      string
	ExplicitVersion string
	ExplicitDate    string
}

// Result contains the synthesized git context fragments for downstream prompts.
type Result struct {
	RangeDescription string
	CommitSummary    string
	PatchSummary     string
	Context          string
	BaseRef          string
}

// Collector gathers commit summaries and patch data for a repository.
type Collector struct {
	runner CommandRunner
}

// CommandRunner executes git commands within a working directory.
type CommandRunner interface {
	Run(ctx context.Context, dir string, name string, args ...string) (string, error)
}

type commandExecutor struct{}

// ErrDateAndVersionProvided indicates both --date and --version were supplied.
var (
	ErrDateAndVersionProvided   = errors.New("--version and --date cannot be used together")
	ErrStartingPointUnavailable = errors.New("unable to determine git starting point; provide --version or --date")
	ErrNoCommitsInRange         = errors.New("no commits found in selected range")
)

// NewCollector constructs a collector that shells out to git.
func NewCollector() Collector {
	return Collector{runner: commandExecutor{}}
}

// NewCollectorWithRunner injects a custom command runner, used mainly for tests.
func NewCollectorWithRunner(runner CommandRunner) Collector {
	return Collector{runner: runner}
}

// Collect builds commit and patch summaries based on the provided options.
func (c Collector) Collect(ctx context.Context, opts Options) (Result, error) {
	if strings.TrimSpace(opts.ExplicitVersion) != "" && strings.TrimSpace(opts.ExplicitDate) != "" {
		return Result{}, ErrDateAndVersionProvided
	}

	workingDir := strings.TrimSpace(opts.WorkingDir)
	if workingDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Result{}, fmt.Errorf("determine working directory: %w", err)
		}
		workingDir = wd
	}
	if err := ensureRepository(ctx, c.runner, workingDir); err != nil {
		return Result{}, err
	}

	if strings.TrimSpace(opts.ExplicitDate) != "" {
		return c.collectSinceDate(ctx, workingDir, strings.TrimSpace(opts.ExplicitDate))
	}
	baseRef := strings.TrimSpace(opts.ExplicitVersion)
	if baseRef == "" {
		tag, err := c.findLatestVersionTag(ctx, workingDir)
		if err != nil {
			if errors.Is(err, ErrStartingPointUnavailable) {
				return Result{}, ErrStartingPointUnavailable
			}
			return Result{}, err
		}
		baseRef = tag
	}
	if err := c.ensureRefExists(ctx, workingDir, baseRef); err != nil {
		return Result{}, err
	}
	result, err := c.collectSinceRef(ctx, workingDir, baseRef)
	if err != nil {
		return Result{}, err
	}
	result.BaseRef = baseRef
	return result, nil
}

func (c Collector) collectSinceRef(ctx context.Context, dir, baseRef string) (Result, error) {
	rangeRef := fmt.Sprintf("%s..HEAD", baseRef)
	commits, err := c.runner.Run(ctx, dir, "git", "log", rangeRef, "--pretty=format:%h %s", "--no-merges")
	if err != nil {
		return Result{}, fmt.Errorf("git log %s: %w", rangeRef, err)
	}
	if strings.TrimSpace(commits) == "" {
		return Result{}, fmt.Errorf("%w: %s", ErrNoCommitsInRange, rangeRef)
	}
	patch, err := c.runner.Run(ctx, dir, "git", "log", rangeRef, "--patch", "--no-merges")
	if err != nil {
		return Result{}, fmt.Errorf("git log patch %s: %w", rangeRef, err)
	}
	return buildResult(rangeRef, commits, patch), nil
}

func (c Collector) collectSinceDate(ctx context.Context, dir, since string) (Result, error) {
	commits, err := c.runner.Run(ctx, dir, "git", "log", "--since="+since, "--pretty=format:%h %s", "--no-merges")
	if err != nil {
		return Result{}, fmt.Errorf("git log since %s: %w", since, err)
	}
	if strings.TrimSpace(commits) == "" {
		return Result{}, fmt.Errorf("%w: since %s", ErrNoCommitsInRange, since)
	}
	patch, err := c.runner.Run(ctx, dir, "git", "log", "--since="+since, "--patch", "--no-merges")
	if err != nil {
		return Result{}, fmt.Errorf("git log patch since %s: %w", since, err)
	}
	return buildResult("since "+since, commits, patch), nil
}

func buildResult(rangeDescriptor, commits, patch string) Result {
	formattedCommits := strings.TrimSpace(commits)
	if formattedCommits == "" {
		formattedCommits = "No commits found."
	}
	formattedPatch := strings.TrimSpace(patch)
	if formattedPatch == "" {
		formattedPatch = "No diff available."
	}
	var buffer bytes.Buffer
	buffer.WriteString("Commits ")
	buffer.WriteString(rangeDescriptor)
	buffer.WriteString(":\n")
	buffer.WriteString(formattedCommits)
	buffer.WriteString("\n\nDiff ")
	buffer.WriteString(rangeDescriptor)
	buffer.WriteString(":\n")
	buffer.WriteString(formattedPatch)
	buffer.WriteString("\n")
	return Result{
		RangeDescription: rangeDescriptor,
		CommitSummary:    formattedCommits,
		PatchSummary:     formattedPatch,
		Context:          buffer.String(),
	}
}

func ensureRepository(ctx context.Context, runner CommandRunner, dir string) error {
	_, err := runner.Run(ctx, dir, "git", "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return fmt.Errorf("verify git repository: %w", err)
	}
	return nil
}

func (c Collector) findLatestVersionTag(ctx context.Context, dir string) (string, error) {
	out, err := c.runner.Run(ctx, dir, "git", "tag", "--list", "v[0-9]*", "--sort=-creatordate")
	if err != nil {
		return "", fmt.Errorf("list version tags: %w", err)
	}
	tags := parseTags(out)
	if len(tags) == 0 {
		return "", ErrStartingPointUnavailable
	}
	return tags[0], nil
}

func parseTags(output string) []string {
	tagPattern := regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	lines := strings.Split(output, "\n")
	var tags []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if tagPattern.MatchString(trimmed) {
			tags = append(tags, trimmed)
		}
	}
	return tags
}

func (c Collector) ensureRefExists(ctx context.Context, dir, ref string) error {
	_, err := c.runner.Run(ctx, dir, "git", "rev-parse", ref)
	if err != nil {
		return fmt.Errorf("resolve reference %s: %w", ref, err)
	}
	return nil
}

func (commandExecutor) Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run %s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
