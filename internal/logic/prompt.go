package logic

import "context"

// PromptEnvelope keeps trusted instructions separate from task data when
// rendering provider messages. Tool results remain separate conversation turns.
type PromptEnvelope struct {
	System string
	User   string
}

type promptEnvelopeKey struct{}

func withPromptEnvelope(ctx context.Context, prompt PromptEnvelope) context.Context {
	return context.WithValue(ctx, promptEnvelopeKey{}, prompt)
}

func promptEnvelopeFrom(ctx context.Context) (PromptEnvelope, bool) {
	prompt, ok := ctx.Value(promptEnvelopeKey{}).(PromptEnvelope)
	return prompt, ok
}

func withoutPromptEnvelope(ctx context.Context) context.Context {
	return context.WithValue(ctx, promptEnvelopeKey{}, nil)
}
