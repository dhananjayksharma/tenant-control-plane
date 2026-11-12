package broker

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/dhananjayksharma/tenant-control-plane/internal/messaging"
	"github.com/dhananjayksharma/tenant-control-plane/internal/store"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Repository contains persistence operations needed by the message processor.
type Repository interface {
	ApplyProgress(context.Context, messaging.ProgressEvent) error
	Pending(context.Context) ([]store.Pending, error)
	MarkPublished(context.Context, string) error
}

// Run processes progress events and publishes pending outbox messages until ctx is canceled.
func Run(ctx context.Context, db Repository, url string) {
	for ctx.Err() == nil {
		if e := mqSession(ctx, db, url); e != nil {
			log.Printf("broker reconnect: %v", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}
func mqSession(ctx context.Context, db Repository, url string) error {
	log.Print("broker connecting to RabbitMQ")
	conn, e := amqp.Dial(url)
	if e != nil {
		return e
	}
	defer conn.Close()
	ch, e := conn.Channel()
	if e != nil {
		return e
	}
	defer ch.Close()
	if e = messaging.Declare(ch); e != nil {
		return e
	}
	if e = ch.Qos(10, 0, false); e != nil {
		return e
	}
	msgs, e := ch.Consume(messaging.ProgressQueue, "", false, false, false, false, nil)
	if e != nil {
		return e
	}
	// Publisher confirmation ensures broker accepted the event before recording publication.
	pub, e := conn.Channel()
	if e != nil {
		return e
	}
	defer pub.Close()
	if e = pub.Confirm(false); e != nil {
		return e
	}
	confirms := pub.NotifyPublish(make(chan amqp.Confirmation, 1))
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	log.Printf("broker ready progress_queue=%s tasks_queue=%s", messaging.ProgressQueue, messaging.TasksQueue)
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-msgs:
			if !ok {
				return errors.New("consumer disconnected")
			}
			var p messaging.ProgressEvent
			if e = json.Unmarshal(msg.Body, &p); e != nil {
				log.Printf("poison message JSON: %v", e)
				_ = msg.Ack(false)
				continue
			}
			log.Printf("progress received task_id=%s event_id=%s status=%s redelivered=%t", p.TaskID, p.EventID, p.Status, msg.Redelivered)
			if e = db.ApplyProgress(ctx, p); e != nil {
				if strings.Contains(e.Error(), "invalid progress") || strings.Contains(e.Error(), "missing event") || strings.Contains(e.Error(), "unknown task id") {
					log.Printf("poison message: %v", e)
					_ = msg.Ack(false)
				} else {
					log.Printf("transient consumer error: %v", e)
					time.Sleep(time.Second)
					_ = msg.Nack(false, true)
				}
				continue
			}
			if err := msg.Ack(false); err != nil {
				log.Printf("progress acknowledgement failed task_id=%s event_id=%s error=%v", p.TaskID, p.EventID, err)
			} else {
				log.Printf("progress applied and acknowledged task_id=%s event_id=%s status=%s", p.TaskID, p.EventID, p.Status)
			}
		case <-ticker.C:
			items, e := db.Pending(ctx)
			if e != nil {
				log.Printf("outbox fetch: %v", e)
				continue
			}
			for _, item := range items {
				log.Printf("outbox publishing task_id=%s queue=%s", item.TaskID, messaging.TasksQueue)
				if e = messaging.Publish(pub, messaging.TasksQueue, item.Payload); e != nil {
					return e
				}
				select {
				case c, ok := <-confirms:
					if !ok || !c.Ack {
						return errors.New("broker did not confirm publish")
					}
				case <-time.After(5 * time.Second):
					return errors.New("publisher confirm timeout")
				}
				if e = db.MarkPublished(ctx, item.TaskID); e != nil {
					log.Printf("outbox mark failed task_id=%s error=%v", item.TaskID, e)
				} else {
					log.Printf("outbox published task_id=%s", item.TaskID)
				}
			}
		}
	}
}
