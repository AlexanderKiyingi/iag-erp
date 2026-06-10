package notify

import (
	"context"
	"log/slog"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"
)

const (
	Source                    = "iag-erp"
	TypeNotificationRequested = "notification.requested"
	TopicNotifications        = "iag.notifications"

	TemplateBirthdayEmployee = "erp.birthday.employee"
	TemplateBirthdayManager  = "erp.birthday.manager"
	TemplateBirthdayHR       = "erp.birthday.hr"
)

type Publisher struct {
	producer *platformevents.Producer
	topic    string
	enabled  bool
}

type Config struct {
	Brokers  []string
	ClientID string
	Topic    string
}

func New(cfg Config) *Publisher {
	if len(cfg.Brokers) == 0 {
		return &Publisher{}
	}
	topic := cfg.Topic
	if topic == "" {
		topic = TopicNotifications
	}
	clientID := cfg.ClientID
	if clientID == "" {
		clientID = Source
	}
	return &Publisher{
		producer: platformevents.NewProducer(platformevents.ProducerConfig{
			Brokers:  cfg.Brokers,
			ClientID: clientID,
		}),
		topic:   topic,
		enabled: true,
	}
}

func (p *Publisher) Enabled() bool {
	return p != nil && p.enabled && p.producer != nil
}

func (p *Publisher) Close() error {
	if p == nil || p.producer == nil {
		return nil
	}
	return p.producer.Close()
}

func (p *Publisher) PublishEmail(ctx context.Context, eventID, recipient, templateID string, variables map[string]string) error {
	if !p.Enabled() || recipient == "" || templateID == "" {
		return nil
	}
	vars := map[string]any{}
	for k, v := range variables {
		vars[k] = v
	}
	env := platformevents.NewEnvelope(Source, TypeNotificationRequested, map[string]any{
		"channel":    "email",
		"recipient":  recipient,
		"templateId": templateID,
		"variables":  vars,
	})
	if eventID != "" {
		env.ID = eventID
	}
	if err := p.producer.Publish(ctx, p.topic, recipient, env); err != nil {
		slog.Warn("erp notification publish failed", "template", templateID, "recipient", recipient, "err", err)
		return err
	}
	return nil
}
