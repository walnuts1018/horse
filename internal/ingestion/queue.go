package ingestion

import (
	"context"
	"errors"
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
		return nil, errors.New("connect extraction queue failed")
	}
	channel, err := conn.Channel()
	if err != nil {
		return nil, errors.Join(errors.New("open extraction queue failed"), conn.Close())
	}
	if _, err := channel.QueueDeclare(ExtractionQueue, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		return nil, errors.Join(errors.New("declare extraction queue failed"), channel.Close(), conn.Close())
	}
	return &Queue{connection: conn, channel: channel}, nil
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
