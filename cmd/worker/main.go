package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/dhananjayksharma/tenant-control-plane/internal/messaging"
	amqp "github.com/rabbitmq/amqp091-go"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
func main() {
	log.SetPrefix("service=worker ")
	log.SetFlags(log.LstdFlags | log.LUTC | log.Lmicroseconds)
	failRate := flag.Float64("fail-rate", 0, "probability of failure [0,1]")
	minDelay := flag.Int("min-delay-ms", 300, "minimum simulated work time")
	maxDelay := flag.Int("max-delay-ms", 900, "maximum simulated work time")
	flag.Parse()
	if *failRate < 0 || *failRate > 1 || *minDelay < 0 || *maxDelay < *minDelay {
		log.Fatal("invalid worker args")
	}
	url := getenv("AMQP_URL", "amqp://guest:guest@localhost:5672/")
	log.Printf("starting worker fail_rate=%g min_delay_ms=%d max_delay_ms=%d", *failRate, *minDelay, *maxDelay)
	for {
		if e := run(url, *failRate, *minDelay, *maxDelay); e != nil {
			log.Printf("worker reconnect after error: %v", e)
		}
		time.Sleep(2 * time.Second)
	}
}
func run(url string, rate float64, minDelay, maxDelay int) error {
	log.Print("connecting to RabbitMQ")
	conn, e := amqp.Dial(url)
	if e != nil {
		return e
	}
	defer conn.Close()
	log.Print("RabbitMQ connected")
	ch, e := conn.Channel()
	if e != nil {
		return e
	}
	defer ch.Close()
	log.Print("worker channel opened successfully")
	if e = messaging.Declare(ch); e != nil {
		return e
	}
	log.Print("worker queues declared successfully")
	if e = ch.Qos(1, 0, false); e != nil {
		return e
	}
	log.Print("worker prefetch configured successfully prefetch=1")
	// Publish confirm on same channel keeps task unacked until both progress updates are durable in broker.
	if e = ch.Confirm(false); e != nil {
		return e
	}
	log.Print("worker publisher confirmations enabled successfully")
	acks := ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	msgs, e := ch.Consume(messaging.TasksQueue, "", false, false, false, false, nil)
	if e != nil {
		return e
	}
	log.Printf("worker ready queue=%s prefetch=1", messaging.TasksQueue)
	for d := range msgs {
		started := time.Now()
		var task messaging.TaskEvent
		if e = json.Unmarshal(d.Body, &task); e != nil || task.ID == "" {
			log.Printf("invalid task discarded delivery_tag=%d body_bytes=%d error=%v missing_id=%t", d.DeliveryTag, len(d.Body), e, task.ID == "")
			if err := d.Ack(false); err != nil {
				return fmt.Errorf("ack invalid task: %w", err)
			}
			continue
		}
		log.Printf("task received task_id=%s type=%s redelivered=%t delivery_tag=%d", task.ID, task.Type, d.Redelivered, d.DeliveryTag)
		publish := func(status string) error {
			event := messaging.ProgressEvent{EventID: fmt.Sprintf("%s:%s", task.ID, status), TaskID: task.ID, Status: status}
			body, _ := json.Marshal(event)
			if e := messaging.Publish(ch, messaging.ProgressQueue, body); e != nil {
				return e
			}
			select {
			case confirmation := <-acks:
				if !confirmation.Ack {
					return fmt.Errorf("publish unconfirmed")
				}
				log.Printf("progress confirmed task_id=%s event_id=%s status=%s", task.ID, event.EventID, status)
				return nil
			case <-time.After(5 * time.Second):
				return fmt.Errorf("progress publish timeout")
			}
		}
		if e = publish("in_progress"); e != nil {
			log.Printf("progress publish failed task_id=%s status=in_progress error=%v", task.ID, e)
			if err := d.Nack(false, true); err != nil {
				log.Printf("task requeue failed task_id=%s error=%v", task.ID, err)
			}
			return e
		}
		delay := minDelay
		if maxDelay > minDelay {
			delay += rand.Intn(maxDelay - minDelay + 1)
		}
		time.Sleep(time.Duration(delay) * time.Millisecond)
		status := "done"
		if rand.Float64() < rate {
			status = "failed"
		}
		if status == "done" {
			log.Printf("task work completed successfully task_id=%s type=%s simulated_delay_ms=%d", task.ID, task.Type, delay)
		} else {
			log.Printf("task work failed task_id=%s type=%s reason=simulated_failure", task.ID, task.Type)
		}
		if e = publish(status); e != nil {
			log.Printf("progress publish failed task_id=%s status=%s error=%v", task.ID, status, e)
			if err := d.Nack(false, true); err != nil {
				log.Printf("task requeue failed task_id=%s error=%v", task.ID, err)
			}
			return e
		}
		if err := d.Ack(false); err != nil {
			return fmt.Errorf("ack task %s: %w", task.ID, err)
		}
		log.Printf("task acknowledged task_id=%s type=%s status=%s simulated_delay_ms=%d duration=%s", task.ID, task.Type, status, delay, time.Since(started))
	}
	return fmt.Errorf("delivery channel closed")
}
