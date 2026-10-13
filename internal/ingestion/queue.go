package ingestion

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const ExtractionQueue = "horse.extraction"

// Queueは通知の配送を担い、PostgreSQLのジョブを実行状態の正本とする。
type Queue struct {
	connection *amqp.Connection
	channel    *amqp.Channel
}

func NewQueue(endpoint string) (*Queue, error) {
	conn, err := amqp.DialConfig(endpoint, amqp.Config{Heartbeat: 10 * time.Second, Dial: amqp.DefaultDial(10 * time.Second)})
	if err != nil {
		return nil, fmt.Errorf("connect extraction queue failed: %w", safeQueueDialError(err))
	}
	channel, err := conn.Channel()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open extraction queue channel: %w", err), conn.Close())
	}
	if _, err := channel.QueueDeclare(ExtractionQueue, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		return nil, errors.Join(fmt.Errorf("declare extraction queue: %w", err), channel.Close(), conn.Close())
	}
	return &Queue{connection: conn, channel: channel}, nil
}

func safeQueueDialError(err error) error {
	urlErr, ok := errors.AsType[*url.Error](err)
	if !ok {
		return err
	}
	return fmt.Errorf("parse AMQP endpoint: %w", urlErr.Err)
}

func (q *Queue) Publish(ctx context.Context, jobID string) error {
	return q.channel.PublishWithContext(ctx, "", ExtractionQueue, false, false, amqp.Publishing{ContentType: mimeTypePlainText, DeliveryMode: amqp.Persistent, Body: []byte(jobID)})
}

func (q *Queue) Consume() (<-chan amqp.Delivery, error) {
	if err := q.channel.Qos(1, 0, false); err != nil {
		return nil, err
	}
	return q.channel.Consume(ExtractionQueue, "", false, false, false, false, nil)
}

func (q *Queue) Close() error { return errors.Join(q.channel.Close(), q.connection.Close()) }
