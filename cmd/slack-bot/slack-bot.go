package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	events "github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/openshift-splat-team/splat-bot/pkg/commands"
	_ "github.com/openshift-splat-team/splat-bot/pkg/controllers"
	_ "github.com/openshift-splat-team/splat-bot/pkg/knowledge"
	slackutil "github.com/openshift-splat-team/splat-bot/pkg/util"
)

type CustomFormatter struct{}

func (f *CustomFormatter) Format(entry *log.Entry) ([]byte, error) {
	var callerInfo string
	if entry.HasCaller() {
		// Extract only the file name
		callerInfo = fmt.Sprintf("[%s:%d]", filepath.Base(entry.Caller.File), entry.Caller.Line)
	}
	logMessage := fmt.Sprintf("%s [%s]\t%s %s\n", entry.Time.Format("2006-01-02 15:04:05"), strings.ToUpper(entry.Level.String()), callerInfo, entry.Message)
	return []byte(logMessage), nil
}

func main() {
	ctx := context.TODO()

	// Define a flag for log level
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error, fatal, panic)")
	flag.Parse()

	// Parse and set the log level
	level, err := log.ParseLevel(*logLevel)
	if err != nil {
		log.Fatalf("Invalid log level: %s", *logLevel)
	}
	log.SetLevel(level)
	log.SetReportCaller(true)
	log.SetFormatter(&CustomFormatter{})
	log.SetOutput(os.Stdout)

	client, err := slackutil.GetClient()
	if err != nil {
		log.Debugf("unable to get slack client: %v", err)
		os.Exit(1)
	}

	err = commands.Initialize()
	if err != nil {
		log.Debugf("unable to get users in group")
		os.Exit(1)
	}

	go func() {
		for evt := range client.Events {
			switch evt.Type {
			case socketmode.EventTypeConnecting:
				log.Infof("Connecting to Slack with Socket Mode...")
			case socketmode.EventTypeConnectionError:
				log.Infof("Connection failed. Retrying later...")
			case socketmode.EventTypeConnected:
				log.Infof("Connected to Slack with Socket Mode.")
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(events.EventsAPIEvent)
				if !ok {
					log.Warnf("ignored %+v\n", evt)
					continue
				}

				log.Debugf("event received: %+v\n", eventsAPIEvent)

				client.Ack(*evt.Request)
				err = commands.Handler(ctx, client, eventsAPIEvent)
				if err != nil {
					log.Warnf("error encountered while processing event: %v", err)
				}
			case socketmode.EventTypeInteractive:
				// NOTE: we can get one of these when user is responding to slash command dialogs well as when we have
				// interactive responses such as the pull-request command.
				log.Debugf("GOT INTERACTIVE EVENT: %v\n", evt)
				client.Ack(*evt.Request)
				processInteractiveEvent(ctx, client, evt)
			case socketmode.EventTypeSlashCommand:
				log.Debug("GOT SLASH COMMAND")
				client.Ack(*evt.Request)

				command := evt.Data.(slack.SlashCommand)

				buffer := bytes.NewBuffer([]byte{})
				if err := json.NewEncoder(buffer).Encode(command); err != nil {
					log.Warnf("Error: %v", err)
				}
				log.Debugf("Slash command: %v", buffer)

				err = commands.SlashHandler(ctx, client, command)

			default:
				log.Warnf("Unexpected event type received: %s\n", evt.Type)
			}
		}
	}()

	err = client.Run()
	if err != nil {
		log.Fatalf("error encountered while running client: %v", err)
	}
}

func processInteractiveEvent(ctx context.Context, client *socketmode.Client, evt socketmode.Event) {
	client.Ack(*evt.Request)

	data := evt.Data.(slack.InteractionCallback)

	// This outputs the event data for debugging
	buffer := bytes.NewBuffer([]byte{})
	if err := json.NewEncoder(buffer).Encode(data); err != nil {
		log.Warnf("Error: %v", err)
	} else {
		log.Debugf("EVENT DATA: %v", buffer.String())
	}

	// block_actions payloads are received when a user clicks a Block Kit interactive component.
	//shortcut and message_actions payloads are received when global and message shortcuts are used.
	//view_submission payloads are received when a modal is submitted.
	//view_closed payloads are received when a modal is cancelled.
	log.Printf("Handling event type %v", data.Type)
	switch data.Type {
	case slack.InteractionTypeDialogCancellation:
	case slack.InteractionTypeDialogSubmission:
	case slack.InteractionTypeDialogSuggestion:
	case slack.InteractionTypeInteractionMessage:
	case slack.InteractionTypeMessageAction:
	case slack.InteractionTypeBlockActions:
		// Ok, so we are going have to pass the evt to the attribute / slash command that this belongs to.
		// Currently, we are only closing the dialog.
		if data.ActionCallback.BlockActions[0].Text.Text == "Close" {
			_, _, err := client.PostMessage(data.Channel.ID, slack.MsgOptionDeleteOriginal(data.ResponseURL))
			if err != nil {
				log.Warnf("Error occurred handling interative event: %v", err)
			}
		}
	case slack.InteractionTypeBlockSuggestion:
	case slack.InteractionTypeViewSubmission:
		err := commands.ViewSubmissionHandler(ctx, client, data)
		if err != nil {
			log.Warnf("Error occurred handling interative event: %v", err)
		}
	case slack.InteractionTypeViewClosed:
	case slack.InteractionTypeShortcut:
	case slack.InteractionTypeWorkflowStepEdit:
	default:
		log.Warnf("Unexpected event type received: %s\n", evt.Type)
	}

}
