package googlechat

import (
	"context"
)

// ChatSender posts replies to Google Chat threads via the Chat REST v1 API.
type ChatSender interface {
	Reply(ctx context.Context, thread, text string) error
}
