package commands

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/openshift-splat-team/splat-bot/data"
	"github.com/openshift-splat-team/splat-bot/pkg/chat"
	"github.com/openshift-splat-team/splat-bot/pkg/util"
)

var (
	attributeMu        sync.Mutex
	attributes         = []data.Attributes{}
	allowedUsers       = map[string]bool{}
	enableChatResponse = false
	slashMu            sync.Mutex
	slashCommands      = []data.SlashCommand{}
)

// AddCommand adds a handler to the list of handlers. Matching of the message can be overriden
// by providing a MessageOfInterest function.
func AddCommand(attribute data.Attributes, handler ...data.MessageOfInterest) {
	attributeMu.Lock()
	defer attributeMu.Unlock()
	log.Printf("adding command: %v", attribute.Commands)
	if len(handler) > 0 {
		attribute.MessageOfInterest = handler[0]
	} else {
		attribute.MessageOfInterest = checkForCommand
	}
	attributes = append(attributes, attribute)
}

// AddSlashCommand adds a handler to the list of handlers.
func AddSlashCommand(slashCmd data.SlashCommand) {
	slashMu.Lock()
	defer slashMu.Unlock()
	log.Printf("adding slash command: %v", slashCmd.Commands)
	slashCommands = append(slashCommands, slashCmd)
}

func getAttributes() []data.Attributes {
	attributeMu.Lock()
	defer attributeMu.Unlock()

	newAttributes := make([]data.Attributes, len(attributes))

	copy(newAttributes, attributes)
	return newAttributes
}

func getSlashCommands() []data.SlashCommand {
	slashMu.Lock()
	defer slashMu.Unlock()

	newCommands := make([]data.SlashCommand, len(slashCommands))

	copy(newCommands, slashCommands)
	return newCommands
}

func init() {
	_enableChatResponse := os.Getenv("ENABLE_CHAT_RESPONSE")
	if _enableChatResponse != "" {
		enableChatResponse = strings.ToLower(_enableChatResponse) == "true"
	}
	AddCommand(CreateAttributes)
	AddCommand(HelpAttributes)
	AddCommand(ProwAttributes)
	AddCommand(ProwGraphAttributes)
	AddCommand(CreateJiraWithThreadAttributes)

	AddSlashCommand(JiraCreateSlashCommand)
}

func Initialize() error {
	// TODO:  Global allowed users means we cannot make some actions available to some users while others not.  This could
	//        be beefed up in the future to be allowed users per command from config provided by a yaml file or something of
	//        that nature.
	allowed := os.Getenv("SLACK_ALLOWED_USERS")
	if len(allowed) == 0 {
		log.Warnf("Disabling user enforcement.  Please configure SLACK_ALLOWED_USERS if you wish to enforce allowed users on certain commands.")
	} else {
		allowedUsersIDs := strings.Split(allowed, ",")
		for _, user := range allowedUsersIDs {
			allowedUsers[user] = true
			log.Infof("user id %s is allowed", user)
		}
	}
	return nil
}

func isAllowedUser(evt *slackevents.MessageEvent) error {
	log.Debugf("User size: %d\n", len(allowedUsers))
	if _, found := allowedUsers[evt.User]; !found && len(allowedUsers) > 0 {
		return fmt.Errorf("user %s with id %s is not allowed", evt.Username, evt.User)
	}
	return nil
}
func tokenize(msgText string, glob bool) []string {
	msgText = strings.ReplaceAll(msgText, "\n", " ")
	var tokens []string
	if glob {
		re := regexp.MustCompile(`"([^"]*?)"|(\S+)`)
		matches := re.FindAllStringSubmatch(msgText, -1)

		for _, match := range matches {
			if match[1] != "" {
				// Remove leading and trailing quotation marks
				tokens = append(tokens, strings.Trim(match[1], "\""))
			} else {
				tokens = append(tokens, match[2])
			}
		}
		return tokens
	} else {
		return strings.Split(msgText, " ")
	}
}

func getDMChannelID(client util.SlackClientInterface, evt *slackevents.MessageEvent) (string, error) {
	return getDMChannelIDByUser(client, evt.User)
}

func getDMChannelIDByUser(client util.SlackClientInterface, user string) (string, error) {
	channel, _, _, err := client.OpenConversation(&slack.OpenConversationParameters{
		Users: []string{user},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open conversation: %v", err)
	}

	return channel.Latest.Channel, nil
}

func SlashHandler(ctx context.Context, client util.SlackClientInterface, cmd slack.SlashCommand) error {
	log.Debugf("Looking for command to handle %v", cmd.Command)
	for _, command := range getSlashCommands() {
		args := tokenize(cmd.Text, !command.DontGlobQuotes)
		if checkForSlashCommand(cmd.Command, command) {
			log.Debugf("Found command: %v", command.Commands)

			_, err := command.ProcessCommand(ctx, client, cmd, args)
			if err != nil {
				log.Warnf("failed processing message: %v", err)
			}
		}
	}
	return nil
}

func ViewSubmissionHandler(ctx context.Context, client util.SlackClientInterface, evt slack.InteractionCallback) error {
	// For now, only SlashCommands are handling view submissions
	for _, command := range getSlashCommands() {
		log.Debugf("Checking command: %v", command.Commands)
		check := command.ViewSubmissionCheck
		if check != nil && check(evt.View.CallbackID) {
			log.Debugf("Found command: %v", command.Commands)
			response, err := command.HandleViewSubmission(ctx, client, evt)
			if err != nil {
				log.Warnf("failed processing message: %v", err)
			}

			// This block should be moved into a function and shared w/ other workflows
			if len(response) > 0 {
				log.Debugf("responding to message: %v", response)

				// channelID is set in private metadata in the view
				channelID := evt.View.PrivateMetadata
				userID := evt.User.ID

				if command.RespondInDM {
					channelID, err = getDMChannelIDByUser(client, evt.User.ID)
					if err != nil {
						log.Warnf("failed getting channel ID: %v", err)
					}
				}

				log.Printf("responding to message in channel: %s", channelID)
				if command.ResponseIsEphemeral {
					_, err = client.PostEphemeral(channelID, userID, response...)
				} else {
					_, _, err = client.PostMessage(channelID, response...)
				}
				if err != nil {
					return fmt.Errorf("failed responding to message: %v", err)
				}
				return nil
			}

			break
		}
	}
	return nil
}

func Handler(ctx context.Context, client util.SlackClientInterface, evt slackevents.EventsAPIEvent) error {
	isAppMentionEvent := false

	switch evt.Type {
	case "message":
	case "event_callback":
	default:
		log.Warnf("event type: %s discarded", evt.Type)
		return nil
	}

	msg := &slackevents.MessageEvent{}
	switch ev := evt.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		isAppMentionEvent = true
		appMentionEvent := evt.InnerEvent.Data.(*slackevents.AppMentionEvent)
		msg = &slackevents.MessageEvent{
			Channel:         appMentionEvent.Channel,
			User:            appMentionEvent.User,
			Text:            appMentionEvent.Text,
			TimeStamp:       appMentionEvent.TimeStamp,
			ThreadTimeStamp: appMentionEvent.ThreadTimeStamp,
			Type:            evt.Type,
		}
		log.Debugf("AppMentionEvent: %s; %s;\n%s", appMentionEvent.User, appMentionEvent.Channel, appMentionEvent.Text)
	case *slackevents.MessageEvent:
		msg = evt.InnerEvent.Data.(*slackevents.MessageEvent)
		log.Debugf("MessageEvent: %s; %s;\n%s", msg.User, msg.Channel, msg.Text)
	default:
		return fmt.Errorf("received an unknown event type: %T", ev)
	}

	if len(msg.BotID) > 0 && util.IsSPLATBotID(msg.BotID) {
		log.Warnf("throwing away message from bot: %s", msg.BotID)
		return nil
	}

	var args []string
	var response []slack.MsgOption
	for _, attribute := range getAttributes() {
		log.Debugf("checking command: %v", attribute.Commands)

		// For app mention logic, there are two scenarios: 1.) Channel Msg.  2.) Direct Message
		// For Channel messages, we want the event to be an AppMention if attribute.RequireMention.
		// For Direct messages, we will want event to be Message, Channel = "im", and ContainsBotMention
		// Note, for AppMessage, InnerEvent is AppMessageEvent, for Message, its MessageEvent.
		if attribute.RequireMention {
			if isAppMentionEvent && !util.ContainsBotMention(msg.Text) {
				log.Warnf("command requires a mention: %s", msg.Text)
				continue
			} else if !isAppMentionEvent {
				ieData := evt.InnerEvent.Data.(*slackevents.MessageEvent)
				channelType := ieData.ChannelType

				if !util.ContainsBotMention(msg.Text) && channelType == slack.TYPE_CHANNEL {
					log.Warnf("message is targeting a %s and doesnt contain a bot mention: %s", channelType, msg.Text)
					continue
				}
				if channelType == slack.TYPE_IM && !util.ContainsBotMention(msg.Text) {
					log.Warnf("message is targeting %s and doesnt contain a bot mention: %s", channelType, msg.Text)
					continue
				}
			}
		}

		if len(attribute.RequireInChannel) > 0 {
			allowedInChannel := false
			for _, channel := range attribute.RequireInChannel {
				if allowedInChannel = channel == msg.Channel; allowedInChannel {
					break
				}
			}
			if !allowedInChannel {
				log.Warnf("message must be in a DM: %s", msg.Text)
				continue
			}
		}

		args = tokenize(msg.Text, !attribute.DontGlobQuotes)
		if util.ContainsBotMention(msg.Text) {
			args = args[1:]
		}
		log.Warnf("msg: %v\n", msg)
		if len(msg.Type) > 0 && checkForCommand(args, attribute, msg.Channel) {
			log.Debugf("found command: %v", attribute.Commands)
			// Now that we found command, make sure it can be used by current user.
			if !attribute.AllowNonSplatUsers {
				err := isAllowedUser(msg)
				if err != nil {
					return fmt.Errorf("user not allowed: %v", err)
				}
			}

			var err error
			inThread := len(util.GetThreadUrl(msg)) > 0
			if attribute.MustBeInThread && !inThread {
				log.Warnf("message must be in a thread, but isnt: %s", msg.Text)
				continue
			}

			maxExceeded := false
			if attribute.MaxArgs > 0 && len(args) > attribute.MaxArgs {
				maxExceeded = true
			}
			minRequired := attribute.RequiredArgs > 0 && len(args) < attribute.RequiredArgs

			if len(args) < attribute.RequiredArgs {
				response = []slack.MsgOption{
					slack.MsgOptionText(fmt.Sprintf("command requires %d arguments.\n%s\n", attribute.RequiredArgs, attribute.HelpMarkdown), true),
				}
			} else if minRequired || maxExceeded {
				response = []slack.MsgOption{
					slack.MsgOptionText(fmt.Sprintf("command requires %d arguments. if an argument is greater than one word, be sure to wrap that argument in quotes.\n%s\n", attribute.RequiredArgs, attribute.HelpMarkdown), true),
				}
			} else {
				response, err = attribute.Callback(ctx, client, msg, args)
				if err != nil {
					log.Warnf("failed processing message: %v, %v", err, response)
				}
			}
			if len(response) > 0 {
				log.Debugf("responding to message: %v", response)
				if attribute.RespondInDM {
					channelID, err := getDMChannelID(client, msg)
					if err != nil {
						log.Warnf("failed getting channel ID: %v", err)
					}
					msg.Channel = channelID
				} else if !attribute.RespondInChannel {
					response = append(response, slack.MsgOptionTS(msg.TimeStamp))
				} else if len(util.GetThreadUrl(msg)) > 0 {
					response = append(response, slack.MsgOptionTS(msg.ThreadTimeStamp))
				}

				log.Debugf("responding to message in channel: %s", msg.Channel)
				if attribute.ResponseIsEphemeral {
					_, err = client.PostEphemeral(msg.Channel, msg.User, response...)
				} else {
					_, _, err = client.PostMessage(msg.Channel, response...)
				}
				if err != nil {
					return fmt.Errorf("failed responding to message: %v", err)
				}
				return nil
			}
			log.Debugf("finished processing command")
		}
	}

	// if the message isn't handled, check to see if this is an IM message
	// and the user is allowed.
	if len(response) == 0 && enableChatResponse {
		ieData := msg
		channelType := ieData.ChannelType
		if channelType == slack.TYPE_IM && !util.ContainsBotMention(msg.Text) && (len(msg.BotID) == 0 || util.IsSPLATBotID(msg.BotID)) {
			response, err := chat.HandleChatInteraction(ctx, client, msg)
			if err != nil {
				log.Warnf("failed processing message: %v", err)
			}
			if len(response) > 0 {
				_, _, err = client.PostMessage(msg.Channel, response...)
				if err != nil {
					log.Warnf("failed posting message: %v", err)
				}
			}
		}
	}
	return nil
}

func checkForCommand(args []string, attribute data.Attributes, channel string) bool {
	match := true
	for index, command := range attribute.Commands {
		if command != args[index] {
			match = false
			break
		}
	}
	return match
}

func checkForSlashCommand(cmdVal string, command data.SlashCommand) bool {
	match := true
	for _, curCommand := range command.Commands {
		if curCommand != cmdVal {
			match = false
			break
		}
	}
	return match
}
