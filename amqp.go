package main

import (
	"fmt"

	"sync"

	"time"

	uuid "github.com/nu7hatch/gouuid"
	"github.com/streadway/amqp"
)

type AmqpConnection struct {
	Name    string
	conn    *amqp.Connection
	channel *amqp.Channel
}

type PublishingAmqpConnection struct {
	*AmqpConnection
}

const (
	DeadLetterExchangeSetting = "x-dead-letter-exchange"
	MessageTTLSetting         = "x-message-ttl"
	AlternateExchangeSetting  = "alternate-exchange"
	DeadLetterAppend          = "DeadLetter"
	MessageTTL                = int64(5000) // milliseconds
)

var ConsumerTag string

func init() {
	if EnvConsumerTagOverride != "" {
		ConsumerTag = EnvConsumerTagOverride
	} else {
		tag, err := uuid.NewV4()
		if err != nil {
			LogError(err)
			shutdown()
		}
		ConsumerTag = tag.String()
	}
}

// ExchangeDeclare returns the dead-letter exchange name, and an error
func (conn *AmqpConnection) ExchangeDeclare(name, exchangeType string, durable, autoDelete, internal bool) (deadLetterExchangeName string, theError error) {
	deadLetterExchangeName = fmt.Sprintf("%s_%s", conn.Name, DeadLetterAppend)

	conn.log("declaring dead-letter Exchange (%q)", deadLetterExchangeName)
	if err := conn.channel.ExchangeDeclare(
		deadLetterExchangeName, // name of the exchange
		amqp.ExchangeFanout,    // type
		true,                   // durable
		false,                  // delete when complete
		true,                   // internal
		false,                  // noWait
		nil,                    // arguments
	); err != nil {
		theError = fmt.Errorf("Dead-letter Exchange Declare: %s", err)
		return
	}
	conn.log("Dead-letter Exchange declared (%q)", deadLetterExchangeName)

	conn.log("declaring real Exchange (%q)", name)
	if err := conn.channel.ExchangeDeclare(
		name,         // name of the exchange
		exchangeType, // type
		durable,      // durable
		autoDelete,   // delete when complete
		internal,     // internal
		false,        // noWait
		amqp.Table{
			AlternateExchangeSetting: deadLetterExchangeName,
		}, // arguments
	); err != nil {
		theError = fmt.Errorf("Exchange Declare: %s", err)
		return
	}
	conn.log("Exchange declared (%q)", name)

	conn.log("declaring dead-letter queue for exchange (%q)", name)
	if _, err := conn.MakeQueue(
		deadLetterExchangeName, // exchange name
		deadLetterExchangeName, // queue name
		"",    // binding key
		true,  // durable
		false, // autoDelete
		false, // exclusive
		amqp.Table{
			DeadLetterExchangeSetting: name,
			MessageTTLSetting:         MessageTTL,
		}, // args
	); err != nil {
		theError = fmt.Errorf("Dead-letter queue declare: %s", err)
		return
	}
	conn.log("Dead-letter queue declared for exchange (%q)", name)

	return
}

func (conn *AmqpConnection) MakeQueue(exchangeName, queueName, bindingKey string, durable, autoDelete, exclusive bool, args amqp.Table) (amqp.Queue, error) {
	var q amqp.Queue
	var err error

	conn.log("declaring Queue %q", queueName)
	q, err = conn.channel.QueueDeclare(
		queueName,  // name of the queue
		durable,    // durable
		autoDelete, // delete when unused
		exclusive,  // exclusive
		false,      // noWait
		args,       // arguments
	)
	if err != nil {
		return q, fmt.Errorf("Queue Declare: %s", err)
	}

	conn.log("declared Queue (%q %d messages, %d consumers), binding to Exchange (key %q)", q.Name, q.Messages, q.Consumers, bindingKey)

	if err = conn.channel.QueueBind(
		q.Name,       // name of the queue
		bindingKey,   // bindingKey
		exchangeName, // sourceExchange
		false,        // noWait
		nil,          // arguments
	); err != nil {
		return q, fmt.Errorf("Queue Bind: %s", err)
	}

	conn.log("Queue bound to Exchange")
	return q, nil
}

func (conn *AmqpConnection) GetDeliveryChannel(q amqp.Queue, exclusive bool) (<-chan amqp.Delivery, error) {
	conn.log("starting Consume (consumer tag %q)", ConsumerTag)
	deliveries, err := conn.channel.Consume(
		q.Name,      // name
		ConsumerTag, // consumerTag,
		false,       // noAck
		false,       // exclusive
		false,       // noLocal
		false,       // noWait
		nil,         // arguments
	)
	if err != nil {
		return nil, fmt.Errorf("Queue Consume: %s", err)
	}

	return deliveries, nil
}

var AmqpPublishingDefaults = amqp.Publishing{
	Headers:         amqp.Table{},
	ContentType:     "text/plain",
	ContentEncoding: "",
	Body:            nil,
	DeliveryMode:    amqp.Persistent, // 1=non-persistent, 2=persistent
	Priority:        1,               // 0-9
	// a bunch of application/implementation-specific fields
}

func (conn *PublishingAmqpConnection) Publish(exchangeName, routingKey string, msg amqp.Publishing) error {
	conn.log("publishing %dB body (%q)", len(msg.Body), msg.Body)
	if err := conn.channel.Publish(
		exchangeName, // publish to an exchange
		routingKey,   // routing to 0 or more queues
		false,        // mandatory
		false,        // immediate
		msg,          // message
	); err != nil {
		return fmt.Errorf("Exchange Publish: %s", err)
	}

	return nil
}

func (conn *AmqpConnection) Shutdown() (theError error) {
	// will close() any delivery channels
	if err := conn.channel.Cancel(ConsumerTag, true); err != nil {
		theError = fmt.Errorf("'%s' channel cancel failed: %v", conn.Name, err)
	}

	if err := conn.channel.Close(); err != nil {
		theError = fmt.Errorf("'%s' channel close failed: %v: %v", conn.Name, err, theError)
	}

	if err := conn.conn.Close(); err != nil {
		theError = fmt.Errorf("'%s' AMQP connection close error: %v: %v", conn.Name, err, theError)
	}

	if theError == nil {
		Log("'%s' AMQP shutdown OK", conn.Name)
	}
	return
}

func (conn *AmqpConnection) NotifyClose() <-chan *amqp.Error {
	return conn.conn.NotifyClose(make(chan *amqp.Error))
}

func (conn *AmqpConnection) log(format string, args ...interface{}) {
	logMessageTo(stdLogger, fmt.Sprintf("AMQP '%s': %s", conn.Name, format), args...)
}

func (conn *AmqpConnection) logerr(format string, args ...interface{}) {
	logMessageTo(errLogger, fmt.Sprintf("AMQP '%s': %s", conn.Name, format), args...)
}

func NewPublishingAmqpConnection(connectionName, amqpURI string, reliable bool, shutdownWg *sync.WaitGroup) (c *PublishingAmqpConnection, theError error) {
	base, err := NewAmqpConnection(connectionName, amqpURI, reliable, shutdownWg)
	return &PublishingAmqpConnection{base}, err
}

func NewAmqpConnection(connectionName, amqpURI string, reliable bool, shutdownWg *sync.WaitGroup) (c *AmqpConnection, theError error) {
	c = &AmqpConnection{
		conn:    nil,
		channel: nil,
		Name:    connectionName,
	}

	var err error

	c.log("dialing %q", amqpURI)
	maxTries := 10
	for i := 1; i <= maxTries; i++ {
		c.conn, err = amqp.Dial(amqpURI)
		if err == nil {
			break
		}

		if i < maxTries {
			c.logerr("%d/%d - Couldn't connect, sleeping and trying again", i, maxTries)
			time.Sleep(1 * time.Second)
		} else {
			c.logerr("%d/%d - Couldn't connect.", i, maxTries)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("Dial: %s", err)
	}
	defer func() {
		if theError != nil {
			c.conn.Close()
		}
	}()

	c.log("got Connection, getting Channel")
	c.channel, err = c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("Channel: %s", err)
	}
	c.log("Channel opened")
	defer func() {
		if theError != nil {
			c.channel.Close()
		}
	}()

	// Reliable publisher confirms require confirm.select support from the
	// connection.
	if reliable {
		c.log("enabling publishing confirms.")
		if err := c.channel.Confirm(false); err != nil {
			return nil, fmt.Errorf("Channel could not be put into confirm mode: %s", err)
		}

		confirms := c.channel.NotifyPublish(make(chan amqp.Confirmation, 1))

		go confirm(c.Name, confirms)
	}

	// Register for shutdown waitgroup
	shutdownWg.Add(1)
	go func() {
		c.log("closing: %s", <-c.conn.NotifyClose(make(chan *amqp.Error)))
		shutdownWg.Done()
	}()

	return c, nil
}

// One would typically keep a channel of publishings, a sequence number, and a
// set of unacknowledged sequence numbers and loop until the publishing channel
// is closed.
func confirm(connectionName string, confirms <-chan amqp.Confirmation) {
	for confirmed := range confirms {
		if !confirmed.Ack {
			LogErrFormat("AMQP '%s': failed delivery of delivery tag: %d", connectionName, confirmed.DeliveryTag)
		}
	}
}
