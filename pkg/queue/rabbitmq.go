package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// MessageType represents the type of message being processed
type MessageType string

const (
	OCRTask         MessageType = "ocr_task"
	TranslationTask MessageType = "translation_task"
	PDFTask         MessageType = "pdf_task"
)

// ProcessingTask represents a task to be processed
type ProcessingTask struct {
	Type      MessageType `json:"type"`
	ImagePath string      `json:"image_path,omitempty"`
	Text      string      `json:"text,omitempty"`
	ResultID  string      `json:"result_id"`
}

// RabbitMQ represents a RabbitMQ connection and channel
type RabbitMQ struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queues  map[string]amqp.Queue
}

// NewRabbitMQ creates a new RabbitMQ connection
func NewRabbitMQ(url string) (*RabbitMQ, error) {
	// Connect to RabbitMQ
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Create a channel
	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	// Enable publish confirmations
	if err := channel.Confirm(false); err != nil {
		channel.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to enable publish confirmations: %w", err)
	}

	return &RabbitMQ{
		conn:    conn,
		channel: channel,
		queues:  make(map[string]amqp.Queue),
	}, nil
}

// DeclareQueue declares a queue
func (r *RabbitMQ) DeclareQueue(name string) error {
	queue, err := r.channel.QueueDeclare(
		name,  // name
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	r.queues[name] = queue
	return nil
}

// PublishMessage publishes a message to a queue
func (r *RabbitMQ) PublishMessage(queueName string, task ProcessingTask) error {
	body, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// Get confirms channel
	confirms := r.channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Publish the message
	err = r.channel.PublishWithContext(
		ctx,
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent, // Make message persistent
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	// Wait for confirmation
	if confirmed := <-confirms; !confirmed.Ack {
		return fmt.Errorf("failed to receive publish confirmation")
	}

	return nil
}

// ConsumeMessages consumes messages from a queue
func (r *RabbitMQ) ConsumeMessages(queueName string, handler func(ProcessingTask) error) error {
	// Set prefetch count
	err := r.channel.Qos(
		1,     // prefetch count
		0,     // prefetch size
		false, // global
	)
	if err != nil {
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	// Register consumer
	msgs, err := r.channel.Consume(
		queueName, // queue
		"",        // consumer
		false,     // auto-ack
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	go func() {
		for msg := range msgs {
			var task ProcessingTask
			if err := json.Unmarshal(msg.Body, &task); err != nil {
				log.Printf("Error unmarshaling message: %v", err)
				msg.Reject(false) // Don't requeue
				continue
			}

			// Process the message
			if err := handler(task); err != nil {
				log.Printf("Error processing message: %v", err)
				// Nack and requeue the message
				msg.Nack(false, true)
				continue
			}

			// Acknowledge the message
			msg.Ack(false)
		}
	}()

	return nil
}

// Close closes the RabbitMQ connection and channel
func (r *RabbitMQ) Close() {
	if r.channel != nil {
		r.channel.Close()
	}
	if r.conn != nil {
		r.conn.Close()
	}
}
