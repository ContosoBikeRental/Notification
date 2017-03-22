package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	uuid "github.com/nu7hatch/gouuid"
	"github.com/streadway/amqp"
)

type Notification struct {
	RequestID       string
	RecipientUserID string
	Subject         string
	Message         string
}

func (n Notification) Serialize() (string, error) {
	nBytes, err := json.Marshal(n)
	if err != nil {
		return "", AddMyInfoToErr(err)
	}

	return string(nBytes), nil
}

func (n Notification) Validate() error {
	var errorSlice []string
	var zeroString string

	if n.RequestID == zeroString {
		errorSlice = append(errorSlice, "Must specify RequestID")
	} else if _, err := uuid.Parse([]byte(n.RequestID)); err != nil {
		errorSlice = append(errorSlice, "RequestID must be a GUID")
	}
	if n.Message == zeroString && n.Subject == zeroString {
		errorSlice = append(errorSlice, "Must provide a non-empty Subject or Message")
	}

	// TODO validate that Recipient is a valid userID

	if len(errorSlice) > 0 {
		errorBytes, err := json.Marshal(errorSlice)
		if err != nil {
			return AddMyInfoToErr(err)
		}

		return errors.New(string(errorBytes))
	}

	return nil
}

func ProcessNotification(n Notification) error {
	if err := n.Validate(); err != nil {
		return AddMyInfoToErr(err)
	}

	Log("Processing notification")

	var serializedNotification string
	var err error
	if serializedNotification, err = n.Serialize(); err != nil {
		return AddMyInfoToErr(err)
	}

	Log("Notification is: %s", serializedNotification)

	if n.RecipientUserID != "" {
		Log("Notifying user (%s)", n.RecipientUserID)
		/*
		 * Send email/text to user
		 */
		Log("User notified")
	}

	Log("Publishing notification to outbound queue")

	// Try to publish completion
	maxTries := 3
	complete := false
	for i := 1; i <= maxTries; i++ {
		if err = PubAmqpConnection.Publish(
			EnvPubExchange,
			n.RequestID,
			amqp.Publishing{
				ContentType: "application/json",
				Body:        []byte(serializedNotification),
			},
		); err != nil {
			LogErrFormat("%d/%d Publish failed: %v", i, maxTries, err)
			time.Sleep(1 * time.Second)
			continue
		}

		complete = true
		break
	}

	if !complete {
		return fmt.Errorf("Failed to publish notification to outbound queue! (%s)", n.RequestID)
	}

	Log("Notification complete")
	return nil
}
