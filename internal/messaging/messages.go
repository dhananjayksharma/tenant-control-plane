package messaging

import (
	"context"
	amqp "github.com/rabbitmq/amqp091-go"
)

const TasksQueue = "tenant.tasks.v1"
const ProgressQueue = "tenant.progress.v1"

type TaskEvent struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
}
type ProgressEvent struct {
	EventID string `json:"event_id"`
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
}

func Declare(ch *amqp.Channel) error {
	for _, name := range []string{TasksQueue, ProgressQueue} {
		if _, err := ch.QueueDeclare(name, true, false, false, false, nil); err != nil {
			return err
		}
	}
	return nil
}
func Publish(ch *amqp.Channel, queue string, body []byte) error {
	return ch.PublishWithContext(context.Background(), "", queue, false, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body})
}
