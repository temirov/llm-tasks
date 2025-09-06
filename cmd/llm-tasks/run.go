package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/temirov/llm-tasks/internal/llm"
	"github.com/temirov/llm-tasks/internal/recipes"
)

func init() {
	runCmd.Flags().String("recipe", "", "Path to XML recipe")
	runCmd.Flags().String("version", "", "Release version string")
	runCmd.Flags().String("date", "", "Release date in YYYY-MM-DD")
	runCmd.Flags().String("model", "gpt-5-mini", "Model identifier")
	runCmd.Flags().String("endpoint", "https://api.openai.com/v1", "OpenAI-compatible endpoint")
	runCmd.Flags().String("api-key", "", "API key")
	runCmd.Flags().Duration("timeout", 60*time.Second, "Request timeout")
	runCmd.Flags().Int("max-tokens", 1200, "Max tokens for completion")
	runCmd.Flags().Float64("temperature", 0.2, "Sampling temperature")

	_ = viper.BindPFlag("recipe", runCmd.Flags().Lookup("recipe"))
	_ = viper.BindPFlag("version", runCmd.Flags().Lookup("version"))
	_ = viper.BindPFlag("date", runCmd.Flags().Lookup("date"))
	_ = viper.BindPFlag("model", runCmd.Flags().Lookup("model"))
	_ = viper.BindPFlag("endpoint", runCmd.Flags().Lookup("endpoint"))
	_ = viper.BindPFlag("api_key", runCmd.Flags().Lookup("api-key"))
	_ = viper.BindPFlag("timeout", runCmd.Flags().Lookup("timeout"))
	_ = viper.BindPFlag("max_tokens", runCmd.Flags().Lookup("max-tokens"))
	_ = viper.BindPFlag("temperature", runCmd.Flags().Lookup("temperature"))

	rootCmd.AddCommand(runCmd)
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run an LLM task using a semantic XML recipe and stdin input",
	RunE: func(cmd *cobra.Command, args []string) error {
		recipePath := viper.GetString("recipe")
		releaseVersion := viper.GetString("version")
		releaseDate := viper.GetString("date")
		if recipePath == "" {
			return fmt.Errorf("recipe is required")
		}
		if releaseVersion == "" || releaseDate == "" {
			return fmt.Errorf("version and date are required")
		}

		stdinBuffer := &bytes.Buffer{}
		if err := readAllToBuffer(os.Stdin, stdinBuffer); err != nil {
			return err
		}
		if stdinBuffer.Len() == 0 {
			return fmt.Errorf("no input received on stdin")
		}

		recipe, err := recipes.LoadFromFile(recipePath)
		if err != nil {
			return err
		}

		variables := map[string]string{
			"version": releaseVersion,
			"date":    releaseDate,
			"git_log": stdinBuffer.String(),
		}

		userPrompt, err := buildUserPrompt(recipe, variables)
		if err != nil {
			return err
		}

		client := llm.Client{
			HTTPBaseURL:       viper.GetString("endpoint"),
			APIKey:            chooseNonEmpty(viper.GetString("api_key"), os.Getenv("OPENAI_API_KEY")),
			ModelIdentifier:   viper.GetString("model"),
			MaxTokensResponse: viper.GetInt("max_tokens"),
			Temperature:       viper.GetFloat64("temperature"),
		}
		if client.APIKey == "" {
			return fmt.Errorf("api key is required")
		}

		request := llm.ChatCompletionRequest{
			Model: client.ModelIdentifier,
			Messages: []llm.ChatMessage{
				{Role: "system", Content: strings.TrimSpace(buildSystemPrompt(recipe))},
				{Role: "user", Content: strings.TrimSpace(userPrompt)},
			},
			MaxTokens:   client.MaxTokensResponse,
			Temperature: client.Temperature,
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), viper.GetDuration("timeout"))
		defer cancel()

		output, err := client.CreateChatCompletion(ctx, request)
		if err != nil {
			return err
		}
		fmt.Println(output)
		return nil
	},
}

func buildSystemPrompt(recipe recipes.Recipe) string {
	var builder strings.Builder
	builder.WriteString("You are a release notes editor.\n")
	for _, rule := range recipe.Rules.Rule {
		builder.WriteString("- ")
		builder.WriteString(rule)
		builder.WriteString("\n")
	}
	return builder.String()
}

func buildUserPrompt(recipe recipes.Recipe, vars map[string]string) (string, error) {
	var b strings.Builder
	b.WriteString("Summarize the git log into a Markdown Changelog section.\n\n")
	b.WriteString("Inputs:\n")
	for _, p := range recipe.Inputs.Params {
		if p.Required && vars[p.Name] == "" {
			return "", fmt.Errorf("missing required input: %s", p.Name)
		}
		b.WriteString("- ")
		b.WriteString(p.Name)
		b.WriteString("\n")
	}
	b.WriteString("\nFormat:\n")
	heading, err := recipes.ExpandInline(recipe.Format.Heading.Nodes, vars)
	if err != nil {
		return "", err
	}
	b.WriteString(heading)
	b.WriteString("\n\n")
	for _, s := range recipe.Format.Sections {
		b.WriteString("### ")
		b.WriteString(s.Title)
		b.WriteString("\n\n")
	}
	footer, err := recipes.ExpandInline(recipe.Format.Footer.Nodes, vars)
	if err != nil {
		return "", err
	}
	b.WriteString(footer)
	b.WriteString("\n\n")
	b.WriteString("Git log:\n")
	b.WriteString(vars["git_log"])
	return b.String(), nil
}

func readAllToBuffer(reader io.Reader, destination *bytes.Buffer) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		_, _ = destination.WriteString(scanner.Text())
		_, _ = destination.WriteString("\n")
	}
	return scanner.Err()
}

func chooseNonEmpty(primary string, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}
