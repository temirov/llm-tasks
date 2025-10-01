package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type LLMClient interface {
	Chat(ctx context.Context, request LLMRequest) (LLMResponse, error)
}

type RunOptions struct {
	MaxAttempts int
	DryRun      bool
	Timeout     time.Duration
}

type Runner struct {
	Client  LLMClient
	Options RunOptions
}

func (r Runner) Run(ctx context.Context, p Pipeline) (ApplyReport, error) {
	gathered, gatherErr := p.Gather(ctx)
	if gatherErr != nil {
		return ApplyReport{}, fmt.Errorf("gather: %w", gatherErr)
	}

	var (
		attemptLogs   []attemptRecord
		lastResponse  LLMResponse
		verified      VerifiedOutput
		accepted      bool
		pendingRefine string
	)
	for attempt := 1; attempt <= max(1, r.Options.MaxAttempts); attempt++ {
		req, reqErr := p.Prompt(ctx, gathered)
		if reqErr != nil {
			return ApplyReport{}, fmt.Errorf("prompt: %w", reqErr)
		}
		if strings.TrimSpace(pendingRefine) != "" {
			req.UserPrompt = appendRefine(req.UserPrompt, pendingRefine)
		}
		attemptCtx, cancel := context.WithTimeout(ctx, r.Options.Timeout)
		resp, chatErr := r.Client.Chat(attemptCtx, req)
		cancel()
		if chatErr != nil {
			return ApplyReport{}, fmt.Errorf("llm chat: %w", chatErr)
		}
		lastResponse = resp
		record := attemptRecord{Request: req, Response: resp}

		ok, out, refine, verErr := p.Verify(ctx, gathered, resp)
		if verErr != nil {
			return ApplyReport{}, fmt.Errorf("verify: %w", verErr)
		}
		if ok {
			record.Accepted = true
			attemptLogs = append(attemptLogs, record)
			accepted = true
			verified = out
			break
		}
		if refine == nil {
			attemptLogs = append(attemptLogs, record)
			return ApplyReport{}, errors.New("verify rejected result and no refine request provided")
		}
		record.Refine = refine
		attemptLogs = append(attemptLogs, record)
		pendingRefine = formatRefine(refine.UserPromptDelta)
	}

	if !accepted {
		return ApplyReport{}, fmt.Errorf("exhausted attempts without acceptance\n%s", renderAttemptDebug(attemptLogs, lastResponse))
	}

	report, applyErr := p.Apply(ctx, verified)
	return report, applyErr
}

type attemptRecord struct {
	Request  LLMRequest
	Response LLMResponse
	Refine   *RefineRequest
	Accepted bool
}

func renderAttemptDebug(attempts []attemptRecord, lastResponse LLMResponse) string {
	if len(attempts) == 0 {
		return fmt.Sprintf("last response: %s", truncate(lastResponse.RawText, 280))
	}
	var sb strings.Builder
	for idx, attempt := range attempts {
		sb.WriteString(fmt.Sprintf("Attempt %d:\n", idx+1))
		sb.WriteString(fmt.Sprintf("  Model: %s\n", attempt.Request.Model))
		sb.WriteString(fmt.Sprintf("  MaxTokens: %d Temp: %.2f\n", attempt.Request.MaxTokens, attempt.Request.Temperature))
		sb.WriteString("  System Prompt:\n")
		sb.WriteString(indentBlock(truncate(attempt.Request.SystemPrompt, 1000)))
		sb.WriteString("\n  User Prompt:\n")
		sb.WriteString(indentBlock(truncate(attempt.Request.UserPrompt, 1200)))
		sb.WriteString("\n  Response:\n")
		sb.WriteString(indentBlock(truncate(attempt.Response.RawText, 1200)))
		sb.WriteString("\n")
		if attempt.Refine != nil {
			sb.WriteString("  Refine Suggestion:\n")
			sb.WriteString(indentBlock(truncate(attempt.Refine.UserPromptDelta, 600)))
			sb.WriteString("\n  Refine Reason: ")
			sb.WriteString(attempt.Refine.Reason)
			sb.WriteString("\n")
		}
		if attempt.Accepted {
			sb.WriteString("  Status: accepted\n")
		} else {
			sb.WriteString("  Status: rejected\n")
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func indentBlock(block string) string {
	if block == "" {
		return "    <empty>"
	}
	lines := strings.Split(block, "\n")
	for idx, line := range lines {
		lines[idx] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func appendRefine(original, refine string) string {
	trimmedOriginal := strings.TrimRight(original, "\n")
	if trimmedOriginal == "" {
		return refine
	}
	return trimmedOriginal + "\n\n" + refine
}

func formatRefine(delta string) string {
	trimmed := strings.TrimSpace(delta)
	if trimmed == "" {
		return "REFINE:\n<empty>"
	}
	return "REFINE:\n" + trimmed
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Optional helper if tasks want strict JSON responses.
func DecodeStrictJSON[T any](raw string) (T, error) {
	var zero T
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var out T
	if err := dec.Decode(&out); err != nil {
		return zero, err
	}
	return out, nil
}
