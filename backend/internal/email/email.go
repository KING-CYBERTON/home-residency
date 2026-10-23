// Package email sends transactional email (password reset links). Amazon SES in production;
// the log sender is for development and is refused when APP_ENV=prod.
package email

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

type Message struct {
	To, Subject, Text string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// New returns the sender for provider: "log" (or empty) writes messages to the log, "ses" sends
// them through Amazon SES from the verified address from.
func New(ctx context.Context, provider, from string) (Sender, error) {
	switch provider {
	case "log", "":
		return LogSender{}, nil
	case "ses":
		if from == "" {
			return nil, fmt.Errorf("EMAIL_FROM is required with EMAIL_PROVIDER=ses")
		}
		cfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, err
		}
		return &SES{client: sesv2.NewFromConfig(cfg), from: from}, nil
	default:
		return nil, fmt.Errorf("email provider %q is not supported (use log or ses)", provider)
	}
}

type SES struct {
	client *sesv2.Client
	from   string
}

func (s *SES) Send(ctx context.Context, m Message) error {
	_, err := s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: &s.from,
		Destination:      &types.Destination{ToAddresses: []string{m.To}},
		Content: &types.EmailContent{Simple: &types.Message{
			Subject: &types.Content{Data: &m.Subject},
			Body:    &types.Body{Text: &types.Content{Data: &m.Text}},
		}},
	})
	if err != nil {
		return fmt.Errorf("ses send: %w", err)
	}
	return nil
}

// LogSender prints messages to the log. Dev only: config refuses it when APP_ENV=prod.
type LogSender struct{}

func (LogSender) Send(ctx context.Context, m Message) error {
	slog.InfoContext(ctx, "email (dev log sender)", "to", m.To, "subject", m.Subject, "body", m.Text)
	return nil
}

// Capture records messages for tests.
type Capture struct {
	mu   sync.Mutex
	Msgs []Message
}

func (c *Capture) Send(_ context.Context, m Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Msgs = append(c.Msgs, m)
	return nil
}

// LastTo returns the latest message sent to addr.
func (c *Capture) LastTo(addr string) (Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.Msgs) - 1; i >= 0; i-- {
		if c.Msgs[i].To == addr {
			return c.Msgs[i], true
		}
	}
	return Message{}, false
}
