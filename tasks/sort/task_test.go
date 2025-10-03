package sort_test

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temirov/llm-tasks/internal/pipeline"
	sorttask "github.com/temirov/llm-tasks/tasks/sort"
)

// --- helpers ---

func writeTempFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func makeTempConfig(t *testing.T, downloads, staging string, dryRun bool) string {
	t.Helper()
	cfg := `grant:
  base_directories:
    downloads: "` + downloads + `"
    staging: "` + staging + `"
  safety:
    dry_run: ` + map[bool]string{true: "true", false: "false"}[dryRun] + `
projects:
  - name: "Data_CSV"
    target: "Data_CSV"
    keywords: ["csv"]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "task.sort.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func marshalResults(t *testing.T, results []sorttask.LLMResult) string {
	t.Helper()
	envelope := map[string][]sorttask.LLMResult{
		pipeline.SortedFilesSchemaName: results,
	}
	b, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeZip(t *testing.T, path string, files map[string]string) {
	writer, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	zipWriter := zip.NewWriter(writer)
	for name, body := range files {
		entry, err := zipWriter.Create(name)
		if err != nil {
			zipWriter.Close()
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			zipWriter.Close()
			t.Fatal(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
}

// --- tests ---

func TestSort_VerifyAndApply_DryRun(t *testing.T) {
	base := t.TempDir()
	downloads := filepath.Join(base, "001")
	staging := filepath.Join(base, "001", "_sorted")
	_ = os.MkdirAll(downloads, 0o755)

	img := writeTempFile(t, downloads, "image.png", "png-bytes")
	csv := writeTempFile(t, downloads, "report.csv", "a,b,c\n1,2,3\n")

	cfgPath := makeTempConfig(t, downloads, staging, true)
	task := sorttask.NewWithDeps(sorttask.DefaultFS(), sorttask.FileSortConfigProvider{Path: cfgPath}).(*sorttask.Task)
	_, err := task.Gather(context.Background())
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	results := []sorttask.LLMResult{
		{FileName: filepath.Base(img), ProjectName: "Data_CSV", TargetSubdir: "Resources/Datasets"},
		{FileName: filepath.Base(csv), ProjectName: "Repo", TargetSubdir: "Projects/Codebases"},
	}

	// Verify
	ok, verified, refine, err := task.Verify(
		context.Background(),
		task.Inventory,
		pipeline.LLMResponse{RawText: marshalResults(t, results)},
	)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if refine != nil {
		t.Fatalf("unexpected refine: %+v", refine)
	}
	if !ok {
		t.Fatalf("verify not accepted")
	}

	// Apply (dry run)
	report, err := task.Apply(context.Background(), verified)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !report.DryRun {
		t.Fatalf("expected dry-run")
	}
	if _, err := os.Stat(img); err != nil {
		t.Fatalf("expected image to still exist: %v", err)
	}
	if _, err := os.Stat(csv); err != nil {
		t.Fatalf("expected csv to still exist: %v", err)
	}
	plan := verified.(sorttask.MovePlan)
	if len(plan.Actions) != 2 {
		t.Fatalf("expected two actions, got %d", len(plan.Actions))
	}
	secondDestination := plan.Actions[1].ToPath
	expectedSecond := filepath.Join(staging, "Projects", "Codebases", filepath.Base(csv))
	if secondDestination != expectedSecond {
		t.Fatalf("expected nested destination %s, got %s", expectedSecond, secondDestination)
	}
}

func TestSort_Verify_RejectsCountMismatch(t *testing.T) {
	base := t.TempDir()
	downloads := filepath.Join(base, "001")
	staging := filepath.Join(base, "001", "_sorted")
	_ = os.MkdirAll(downloads, 0o755)

	_ = writeTempFile(t, downloads, "a.txt", "x")
	_ = writeTempFile(t, downloads, "b.txt", "y")

	cfgPath := makeTempConfig(t, downloads, staging, true)
	task := sorttask.NewWithDeps(sorttask.DefaultFS(), sorttask.FileSortConfigProvider{Path: cfgPath}).(*sorttask.Task)
	if _, err := task.Gather(context.Background()); err != nil {
		t.Fatalf("gather: %v", err)
	}

	// LLM returns only 1 item for 2 files -> should request refine
	resp := marshalResults(t, []sorttask.LLMResult{
		{FileName: "a.txt", ProjectName: "Repo", TargetSubdir: "Projects/Codebases"},
	})
	ok, _, refine, err := task.Verify(
		context.Background(),
		task.Inventory,
		pipeline.LLMResponse{RawText: resp},
	)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ok {
		t.Fatalf("expected not accepted due to count mismatch")
	}
	if refine == nil || refine.Reason != "count-mismatch" {
		t.Fatalf("expected refine: count-mismatch, got %+v", refine)
	}
}

func TestSort_VerifyRequestsRefineOnEmptyResponse(t *testing.T) {
	base := t.TempDir()
	downloads := filepath.Join(base, "001")
	staging := filepath.Join(base, "001", "_sorted")
	_ = os.MkdirAll(downloads, 0o755)

	_ = writeTempFile(t, downloads, "a.txt", "x")
	_ = writeTempFile(t, downloads, "b.txt", "y")

	cfgPath := makeTempConfig(t, downloads, staging, true)
	task := sorttask.NewWithDeps(sorttask.DefaultFS(), sorttask.FileSortConfigProvider{Path: cfgPath}).(*sorttask.Task)
	if _, err := task.Gather(context.Background()); err != nil {
		t.Fatalf("gather: %v", err)
	}

	accepted, _, refine, err := task.Verify(
		context.Background(),
		task.Inventory,
		pipeline.LLMResponse{RawText: ""},
	)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if accepted {
		t.Fatalf("expected verification to reject empty response")
	}
	if refine == nil {
		t.Fatalf("expected refine request for empty response")
	}
	if refine.Reason != "empty-response" {
		t.Fatalf("expected refine reason empty-response, got %q", refine.Reason)
	}
}

func TestSort_GatherIncludesArchiveEntries(t *testing.T) {
	base := t.TempDir()
	downloads := filepath.Join(base, "downloads")
	staging := filepath.Join(base, "downloads", "_sorted")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatalf("mkdir downloads: %v", err)
	}
	zipPath := filepath.Join(downloads, "bundle.zip")
	writeZip(t, zipPath, map[string]string{
		"docs/readme.md":    "content",
		"images/sample.png": "binarydata",
	})

	cfgPath := makeTempConfig(t, downloads, staging, true)
	task := sorttask.NewWithDeps(sorttask.DefaultFS(), sorttask.FileSortConfigProvider{Path: cfgPath}).(*sorttask.Task)
	_, err := task.Gather(context.Background())
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var archiveMeta *sorttask.FileMeta
	for _, meta := range task.Inventory {
		if strings.HasSuffix(meta.AbsolutePath, "bundle.zip") {
			m := meta
			archiveMeta = &m
			break
		}
	}
	if archiveMeta == nil {
		t.Fatalf("expected archive metadata for bundle.zip")
	}
	if archiveMeta.RelativePath != "bundle.zip" {
		t.Fatalf("expected relative path bundle.zip, got %s", archiveMeta.RelativePath)
	}
	if len(archiveMeta.ArchiveEntries) == 0 {
		t.Fatalf("expected archive entries to be populated")
	}
	var sawReadme bool
	for _, entry := range archiveMeta.ArchiveEntries {
		if entry.Path == filepath.Clean("docs/readme.md") {
			sawReadme = true
		}
	}
	if !sawReadme {
		t.Fatalf("expected to see docs/readme.md entry")
	}
}

func TestChunkFileMetas(t *testing.T) {
	files := []sorttask.FileMeta{{BaseName: "a"}, {BaseName: "b"}, {BaseName: "c"}}
	batches := sorttask.ChunkFileMetasForTest(files, 2)
	if len(batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(batches))
	}
	if len(batches[0]) != 2 || len(batches[1]) != 1 {
		t.Fatalf("unexpected batch sizes: %+v", batches)
	}
}
