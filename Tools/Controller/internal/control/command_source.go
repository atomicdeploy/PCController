package control

import "context"

type CommandSource string

const CommandSourceBackground CommandSource = "background"

type commandSourceContextKey struct{}

// WithBackgroundCommand identifies automatic maintenance/presentation traffic.
// It changes only capture provenance, never whether a command reaches the board.
// Explicit user output commands must retain their original context.
func WithBackgroundCommand(ctx context.Context) context.Context {
	return context.WithValue(ctx, commandSourceContextKey{}, CommandSourceBackground)
}

func CommandSourceFromContext(ctx context.Context) CommandSource {
	source, _ := ctx.Value(commandSourceContextKey{}).(CommandSource)
	return source
}
