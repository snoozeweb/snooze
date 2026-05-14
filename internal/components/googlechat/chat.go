package googlechat

import (
	"context"
	"fmt"
	"strings"

	chat "google.golang.org/api/chat/v1"
	"google.golang.org/api/option"
)

// ChatSender posts replies to Google Chat threads via the Chat REST v1 API.
type ChatSender interface {
	Reply(ctx context.Context, thread, text string) error
}

// chatSender is the production ChatSender backed by chat/v1.Service.
type chatSender struct {
	svc *chat.Service
}

// newChatSender constructs a Chat REST client. Credentials are sourced from
// the service-account key in cfg (or Application Default Credentials when no
// key is set). The required OAuth scope is chat.bot.
func newChatSender(ctx context.Context, cfg Config) (*chatSender, error) {
	opts := []option.ClientOption{
		option.WithScopes(chat.ChatBotScope),
	}
	if cfg.ServiceAccountKey != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.ServiceAccountKey))
	}
	svc, err := chat.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("googlechat: chat service: %w", err)
	}
	return &chatSender{svc: svc}, nil
}

// Reply posts text to thread (a "spaces/{space}/threads/{thread}" resource
// name). The reply falls back to a new thread if the original one has been
// archived (REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD).
func (s *chatSender) Reply(ctx context.Context, thread, text string) error {
	if s == nil || s.svc == nil {
		return fmt.Errorf("googlechat: chat sender not initialised")
	}
	if strings.TrimSpace(thread) == "" {
		return fmt.Errorf("googlechat: empty thread name")
	}
	space := parentSpaceFromThread(thread)
	if space == "" {
		return fmt.Errorf("googlechat: cannot derive space from thread %q", thread)
	}
	msg := &chat.Message{
		Text:   text,
		Thread: &chat.Thread{Name: thread},
	}
	call := s.svc.Spaces.Messages.Create(space, msg).
		MessageReplyOption("REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD").
		Context(ctx)
	_, err := call.Do()
	if err != nil {
		return fmt.Errorf("googlechat: post reply: %w", err)
	}
	return nil
}

// parentSpaceFromThread extracts the "spaces/{space}" prefix from a thread
// resource name of the form "spaces/{space}/threads/{thread}".
func parentSpaceFromThread(thread string) string {
	parts := strings.Split(thread, "/")
	if len(parts) < 2 || parts[0] != "spaces" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}
