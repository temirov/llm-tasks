package llm

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sampleFile struct {
	Path string
	Kind string
}

func TestCreateChatCompletionPerFileTypeIntegration(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		t.Fatal("OPENAI_API_KEY must be set for integration tests")
	}

	model := strings.TrimSpace(os.Getenv("LLM_TASKS_INTEGRATION_MODEL"))
	if model == "" {
		model = "gpt-4o-mini"
	}

	client := Client{
		HTTPBaseURL: "https://api.openai.com/v1",
		APIKey:      apiKey,
	}

	tempRoot := t.TempDir()
	files := []sampleFile{
		createJPEGSample(t, tempRoot),
		createHEICSample(t, tempRoot),
		createCSVSample(t, tempRoot),
		createZipSample(t, tempRoot, 100),
	}

	zero := 0.0

	for _, file := range files {
		file := file
		t.Run(file.Kind, func(t *testing.T) {
			info, err := os.Stat(file.Path)
			if err != nil {
				t.Fatalf("stat sample: %v", err)
			}
			prompt := buildMetadataPrompt(file, info.Size())
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			response, err := client.CreateChatCompletion(ctx, ChatCompletionRequest{
				Model: model,
				Messages: []ChatMessage{
					{Role: "system", Content: "You are a compliance tester. Reply with the single word PASS."},
					{Role: "user", Content: prompt},
				},
				MaxCompletionTokens: 8,
				Temperature:         &zero,
			})
			if err != nil {
				t.Fatalf("CreateChatCompletion failed for %s: %v", file.Kind, err)
			}
			if strings.TrimSpace(response) != "PASS" {
				t.Fatalf("expected PASS, got %q", response)
			}
		})
	}
}

func buildMetadataPrompt(file sampleFile, size int64) string {
	return fmt.Sprintf("File: %s\nExtension: %s\nSizeBytes: %d\nRespond with PASS only if you successfully processed this metadata.", filepath.Base(file.Path), filepath.Ext(file.Path), size)
}

func createJPEGSample(t *testing.T, root string) sampleFile {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 0x40, A: 0xFF})
		}
	}
	path := filepath.Join(root, "sample.jpeg")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jpeg: %v", err)
	}
	defer file.Close()
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return sampleFile{Path: path, Kind: "jpeg"}
}

func createHEICSample(t *testing.T, root string) sampleFile {
	path := filepath.Join(root, "sample.heic")
	if err := os.WriteFile(path, randomBytes(2048), 0o644); err != nil {
		t.Fatalf("write heic placeholder: %v", err)
	}
	return sampleFile{Path: path, Kind: "heic"}
}

func createCSVSample(t *testing.T, root string) sampleFile {
	path := filepath.Join(root, "sample.csv")
	content := "id,name\n1,test\n2,example\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	return sampleFile{Path: path, Kind: "csv"}
}

func createZipSample(t *testing.T, root string, files int) sampleFile {
	path := filepath.Join(root, "sample.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	for i := 0; i < files; i++ {
		name := fmt.Sprintf("file_%03d.txt", i)
		writer, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create entry: %v", err)
		}
		if _, err := writer.Write([]byte("content")); err != nil {
			t.Fatalf("zip write entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return sampleFile{Path: path, Kind: "zip"}
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
