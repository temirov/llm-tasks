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
		lastResponse LLMResponse
		verified     VerifiedOutput
		accepted     bool
	)
	for attempt := 1; attempt <= max(1, r.Options.MaxAttempts); attempt++ {
		req, reqErr := p.Prompt(ctx, gathered)
		if reqErr != nil {
			return ApplyReport{}, fmt.Errorf("prompt: %w", reqErr)
		}
		attemptCtx, cancel := context.WithTimeout(ctx, r.Options.Timeout)
		resp, chatErr := r.Client.Chat(attemptCtx, req)
		cancel()
		if chatErr != nil {
			return ApplyReport{}, fmt.Errorf("llm chat: %w", chatErr)
		}
		lastResponse = resp

		ok, out, refine, verErr := p.Verify(ctx, gathered, resp)
		if verErr != nil {
			return ApplyReport{}, fmt.Errorf("verify: %w", verErr)
		}
		if ok {
			accepted = true
			verified = out
			break
		}
		if refine == nil {
			return ApplyReport{}, errors.New("verify rejected result and no refine request provided")
		}
		// mutate request by appending delta; tasks may encode their own logic if needed
		req.UserPrompt = req.UserPrompt + "\n\nREFINE:\n" + refine.UserPromptDelta
	}

	if !accepted {
		return ApplyReport{}, fmt.Errorf("exhausted attempts without acceptance (last response: %s)", truncate(lastResponse.RawText, 280))
	}

	report, applyErr := p.Apply(ctx, verified)
	return report, applyErr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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
