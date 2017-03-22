package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"os/signal"
	"syscall"

	"sync"

	"github.com/streadway/amqp"
)

const (
	pubURIEnvName              = "OUT_AMQP_URI"
	subURIEnvName              = "IN_AMQP_URI"
	pubExchangeEnvName         = "OUT_EXCHANGE"
	subExchangeEnvName         = "IN_EXCHANGE"
	subBindingKeyEnvName       = "IN_BINDINGKEY"
	consumerTagOverrideEnvName = "CONSUMERTAG_OVERRIDE"
)

var (
	EnvPubAmqpURI          = os.Getenv(pubURIEnvName)
	EnvSubAmqpURI          = os.Getenv(subURIEnvName)
	EnvPubExchange         = os.Getenv(pubExchangeEnvName)
	EnvSubExchange         = os.Getenv(subExchangeEnvName)
	EnvSubBindingKey       = os.Getenv(subBindingKeyEnvName)
	EnvConsumerTagOverride = os.Getenv(consumerTagOverrideEnvName)
)

var (
	PubAmqpConnection *PublishingAmqpConnection
	SubAmqpConnection *AmqpConnection
)

var envOpts = map[string]string{
	pubURIEnvName:              EnvPubAmqpURI,
	subURIEnvName:              EnvSubAmqpURI,
	pubExchangeEnvName:         EnvPubExchange,
	subExchangeEnvName:         EnvSubExchange,
	subBindingKeyEnvName:       EnvSubBindingKey,
	consumerTagOverrideEnvName: EnvConsumerTagOverride,
}

var ShutdownSignal = sync.NewCond(&sync.Mutex{})
var ShutdownWaitGroup = &sync.WaitGroup{}

func init() {
	Log("Notification init()")
	var err error

	envOptsJSON, err := json.Marshal(envOpts)
	if err != nil {
		LogError(err)
		shutdown()
	}
	Log("Environment options: %s", string(envOptsJSON))
	if EnvPubExchange == "" ||
		EnvSubExchange == "" ||
		EnvPubAmqpURI == "" ||
		EnvSubAmqpURI == "" {
		requiredParams := []string{
			pubURIEnvName,
			subURIEnvName,
			pubExchangeEnvName,
			subExchangeEnvName,
		}
		LogErrFormat("Must specify the following environment variables: %s", strings.Join(requiredParams, ", "))
		os.Exit(1)
	}

	PubAmqpConnection, err = NewPublishingAmqpConnection("Pub", EnvPubAmqpURI, true, ShutdownWaitGroup)
	if err != nil {
		LogErrFormat("Publisher connection: %v", err)
		LogErrFormat("Exiting")
		os.Exit(1)
	}
	go listenForUnexpectedAmqpShutdown(PubAmqpConnection.AmqpConnection)

	SubAmqpConnection, err = NewAmqpConnection("Sub", EnvSubAmqpURI, false, ShutdownWaitGroup)
	if err != nil {
		LogErrFormat("Subscriber connection: %v", err)
		if err := PubAmqpConnection.Shutdown(); err != nil {
			LogError(AddMyInfoToErr(fmt.Errorf("Publisher shutdown: %v", err)))
		}
		LogErrFormat("Exiting")
		os.Exit(1)
	}
	go listenForUnexpectedAmqpShutdown(SubAmqpConnection)

	// Define a channel that will be called when the OS wants the program to exit
	// This will be used to gracefully shutdown the consumer
	osChan := make(chan os.Signal, 3)
	signal.Notify(osChan, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func() {
		LogErrFormat("OS signal received: %v", <-osChan)
		shutdown()
	}()
}

func main() {
	Log("Notification startup")

	Log("Setting up queue listener")
	newNotificationChan := declareExchangesAndQueues()
	ShutdownWaitGroup.Add(1)
	go HandleNotificationDeliveries(newNotificationChan, ShutdownWaitGroup)

	Log("Listening...")
	ShutdownWaitGroup.Wait()
}

// Declares default queues and exchanges
// Returns a delivery channel for new Invoices
func declareExchangesAndQueues() <-chan amqp.Delivery {
	var err error

	// Declare "outgoing notification" exchange
	if _, err := PubAmqpConnection.ExchangeDeclare(
		EnvPubExchange,     // exchange name
		amqp.ExchangeTopic, // exchange type
		true,               // durable
		false,              // autoDelete
		false,              // internal
	); err != nil {
		LogErrFormat("Declaring \"%s\" outgoing exchange: %v", EnvPubExchange, err)
		shutdown()
	}

	// Declare "incoming notification" exchange
	var newInvoiceDeadLetterExchange string
	if newInvoiceDeadLetterExchange, err = SubAmqpConnection.ExchangeDeclare(
		EnvSubExchange,      // exchange name
		amqp.ExchangeDirect, // exchange type
		true,
		false,
		false,
	); err != nil {
		LogErrFormat("Declaring \"%s\" incoming exchange: %v", EnvSubExchange, err)
		shutdown()
	}

	// Declare "incoming notification" queue
	var newInvoiceQueue amqp.Queue
	if newInvoiceQueue, err = SubAmqpConnection.MakeQueue(
		EnvSubExchange,   // exchange name
		EnvSubExchange,   // queue name
		EnvSubBindingKey, // binding key
		true,             // durable
		false,            // autoDelete
		false,            // exclusive
		amqp.Table{
			DeadLetterExchangeSetting: newInvoiceDeadLetterExchange,
		},
	); err != nil {
		LogErrFormat("Making incoming notification queue: %v", err)
		shutdown()
	}

	// Listen for "new invoice" messages
	var newInvoiceChan <-chan amqp.Delivery
	if newInvoiceChan, err = SubAmqpConnection.GetDeliveryChannel(
		newInvoiceQueue, // queue to listen on
		false,           // exclusive
	); err != nil {
		LogErrFormat("Getting incoming notification delivery channel: %v", err)
		shutdown()
	}

	return newInvoiceChan
}

var unexpectedShutdownOnce sync.Once

func listenForUnexpectedAmqpShutdown(conn *AmqpConnection) {
	shutdownChan := make(chan struct{}, 1)
	go func() {
		ShutdownSignal.L.Lock()
		ShutdownSignal.Wait()
		ShutdownSignal.L.Unlock()
		shutdownChan <- struct{}{}
	}()
	connectionCloseChan := PubAmqpConnection.NotifyClose()
	select {
	case <-connectionCloseChan:
		LogErrFormat("'%s' connection shut down unexpectedly!", conn.Name)
		unexpectedShutdownOnce.Do(shutdown)
	case <-shutdownChan:
		// Do nothing, we're shutting down normally
		Log("'%s' graceful shutdown", conn.Name)
	}
}

func shutdown() {
	Log("Shutting down!")
	ShutdownSignal.Broadcast()

	if err := PubAmqpConnection.Shutdown(); err != nil {
		LogErrFormat("Publisher shutdown: %v", err)
	}
	if err := SubAmqpConnection.Shutdown(); err != nil {
		LogErrFormat("Subscriber shutdown: %v", err)
	}

	Log("Waiting for handlers to exit")
	ShutdownWaitGroup.Wait()
	Log("All handlers done. Shutting down...")

	os.Exit(1)
}
