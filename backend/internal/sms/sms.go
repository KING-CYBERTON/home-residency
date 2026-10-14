// Package sms sends one-time codes and notifications. Only a log sender is built in;
// the Africa's Talking / WhatsApp senders implement Sender and are wired in New.
package sms

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

type Sender interface {
	Send(ctx context.Context, toE164, body string) error
}

func New(provider string) Sender {
	switch provider {
	case "log", "":
		return LogSender{}
	default:
		// No real gateway is wired yet. Failing is safer than falling back to the log sender, which
		// would write every login code to the logs in production.
		return unavailable{provider: provider}
	}
}

type unavailable struct{ provider string }

func (u unavailable) Send(context.Context, string, string) error {
	return fmt.Errorf("sms provider %q is not implemented", u.provider)
}

// LogSender prints messages to the log. Dev only: config refuses it when APP_ENV=prod.
type LogSender struct{}

func (LogSender) Send(ctx context.Context, to, body string) error {
	slog.InfoContext(ctx, "sms (dev log sender)", "to", to, "body", body)
	return nil
}

// Capture records messages for tests.
type Capture struct {
	mu   sync.Mutex
	Msgs []Message
}

type Message struct{ To, Body string }

func (c *Capture) Send(_ context.Context, to, body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Msgs = append(c.Msgs, Message{to, body})
	return nil
}

func (c *Capture) Last() Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.Msgs) == 0 {
		return Message{}
	}
	return c.Msgs[len(c.Msgs)-1]
}
