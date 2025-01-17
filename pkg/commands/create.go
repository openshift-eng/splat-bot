package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"text/template"

	log "github.com/sirupsen/logrus"

	"github.com/openshift-splat-team/jira-bot/cmd/issue"
	"github.com/openshift-splat-team/splat-bot/data"
	"github.com/openshift-splat-team/splat-bot/pkg/util"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

const (
	issueTemplateSource = `
*User Story:*
As an {{.Principal}} I want {{.Goal}} so {{.Outcome}}.

*Description:*
< Record any background information >

*Acceptance Criteria:*
< Record how we'll know we're done >

*Other Information:*
< Record anything else that may be helpful to someone else picking up the card >

issue created by splat-bot
`
	issueTemplateDialogSource = `
*User Story:*
{{.UserStory}}

*Description:*
{{if .Description}} {{.Description}} {{else}} < Record any background information > {{end}}

*Acceptance Criteria:*
{{if .AcceptanceCriteria}} {{.AcceptanceCriteria}} {{else}} < Record how we'll know we're done > {{end}}

*Other Information:*
{{if .OtherInfo}} {{.OtherInfo}} {{else}} < Record anything else that may be helpful to someone else picking up the card > {{end}}

issue created by splat-bot
`

	CreateJiraDialogCommand = "Jira_Create_Dialog_Input"
)

// This variable is used to prevent sending JIRA requests when developing
var JIRA_TEST_MODE_ENABLED, _ = strconv.ParseBool(os.Getenv("JIRA_TEST_MODE_ENABLED"))

var issueTemplate *template.Template
var issueTemplateDialog *template.Template

func init() {
	var err error

	issueTemplate, err = template.New("issue").Parse(issueTemplateSource)
	if err != nil {
		panic(err)
	}

	issueTemplateDialog, err = template.New("issueDialog").Parse(issueTemplateDialogSource)
	if err != nil {
		panic(err)
	}
}

type assistantContext struct {
	Principal string
	Goal      string
	Outcome   string
}

type jiraDescriptionInput struct {
	UserStory          string
	Description        string
	AcceptanceCriteria string
	OtherInfo          string
}

func invokeTemplate(t *template.Template, data any) (string, error) {
	// Create a bytes.Buffer to hold the output
	var buf bytes.Buffer

	// Execute the template and write the result into the buffer
	err := t.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

var CreateAttributes = data.Attributes{
	Commands:       []string{"jira", "create"},
	RequireMention: true,
	Callback: func(ctx context.Context, client util.SlackClientInterface, evt *slackevents.MessageEvent, args []string) ([]slack.MsgOption, error) {
		// If only two args are passed in, we'll generate a dialog to collect the data we need, else we will use the
		// provided info to generate the Jira card.
		return createJira(ctx, evt, args)
	},
	RequiredArgs: 2,
	MaxArgs:      4,
	HelpMarkdown: "create a Jira issue: `jira create \"[description]\"`",
	ShouldMatch: []string{
		"jira create description",
		"jira create description",
	},
	ShouldntMatch: []string{
		"jira create-with-summary PROJECT bug",
		"jira create-with-summary PROJECT Todo",
	},
}

var JiraCreateSlashCommand = data.SlashCommand{
	Commands: []string{"/jira-create"},
	ProcessCommand: func(ctx context.Context, client util.SlackClientInterface, command slack.SlashCommand, args []string) ([]slack.MsgOption, error) {
		return createJiraDialog(ctx, client, command)

	},
	RequiredArgs:         2,
	MaxArgs:              4,
	HelpMarkdown:         "create a Jira issue: `jira create \"[description]\"`",
	ViewSubmissionCheck:  CanHandleViewSubmission,
	HandleViewSubmission: HandleViewSubmission,
}

func createJira(ctx context.Context, evt *slackevents.MessageEvent, args []string) ([]slack.MsgOption, error) {
	var description, issueKey, issueURL string
	var err error

	log.Debug("processing Jira Create")
	log.Debugf("evt: %+v", evt)
	assistantCtx := assistantContext{
		Principal: "OpenShift Engineer",
		Goal:      "___",
		Outcome:   "___",
	}

	url := util.GetThreadUrl(evt)
	log.Debugf("%v", args)
	summary := args[2]

	if len(args) >= 4 {
		assistantCtx.Goal = args[2]
		assistantCtx.Outcome = args[3]
	}

	// Execute the template and write the result into the buffer
	description, err = invokeTemplate(issueTemplate, assistantCtx)
	if err != nil {
		return util.StringToBlock(fmt.Sprintf("unable to to process template. error: %v", err), false), nil
	}

	if len(url) > 0 {
		description = fmt.Sprintf("%s\n\ncreated from thread: %s", description, url)
	}

	if JIRA_TEST_MODE_ENABLED {
		issueKey = "Key"
		issueURL = "www.example.com"
	} else {
		issue, err := issue.CreateIssue("SPLAT", summary, description, "Task")
		if err != nil {
			return util.WrapErrorToBlock(err, "error creating issue"), nil
		}
		issueKey = issue.Key
		issueURL = fmt.Sprintf("%s/browse/%s", JIRA_BASE_URL, issueKey)

	}
	return util.StringToBlock(fmt.Sprintf("issue <%s|%s> created", issueURL, issueKey), false), nil
}

// generateJira creates a jira ticket
// summary - The title of the jira task
// description - The text to enter into the
// aiGeneratedFields - Boolean to trigger using AI to generate fields that are not populated.
func generateJira(ctx context.Context, data slack.InteractionCallback, summary, description string, aiGenerateFields bool) ([]slack.MsgOption, error) {
	var issueKey, issueURL string

	if JIRA_TEST_MODE_ENABLED {
		issueKey = "Key"
		issueURL = "http://www.example.com"
	} else {
		jiraIssue, err := issue.CreateIssue("SPLAT", summary, description, "Task")
		if err != nil {
			return util.WrapErrorToBlock(err, "error creating issue"), nil
		}
		issueKey = jiraIssue.Key
		issueURL = fmt.Sprintf("%s/browse/%s", JIRA_BASE_URL, issueKey)

		// TODO - for now, stubbing in AI usage.  Ideally, I think I'd have a button in the dialog that can be used to trigger this
		//        separately so that the fields are populated.  Each field has a limit of 3000 characters currently.
		if aiGenerateFields {
			log.Println("Processing AI generate fields")
		}
	}

	return util.StringToBlock(fmt.Sprintf("<@%v>: issue <%s|%s> created", data.User.ID, issueURL, issueKey), false), nil
}

func createJiraDialog(ctx context.Context, client util.SlackClientInterface, command slack.SlashCommand) ([]slack.MsgOption, error) {
	log.Printf("Processing slash command: %v", command.Command)
	var messageBlocks []slack.Block

	view := slack.ModalViewRequest{}
	//response := util.StringToBlock(fmt.Sprintf("Manta's brain is hard at work generating the dialog"), false)

	log.Printf("Attempting to creating dialog for jira create.")
	triggerId := command.TriggerID

	view.Type = slack.VTModal
	view.CallbackID = CreateJiraDialogCommand
	view.ClearOnClose = true
	view.Title = slack.NewTextBlockObject("plain_text", "Create JIRA", false, false)

	view.Submit = slack.NewTextBlockObject("plain_text", "Create", false, false)
	view.Close = slack.NewTextBlockObject("plain_text", "Close", false, false)

	// Generate Header (title)
	messageBlocks = append(messageBlocks, createTextInputField("title", "Title", "", "", 0, false, false))

	// Generate User Story
	messageBlocks = append(messageBlocks, createTextInputField("userStory", "User Story", "As a ____ I want to ____ so that ____", "", 3000, true, false))

	// Generate Description
	messageBlocks = append(messageBlocks, createTextInputField("description", "Description", "A single sentence describing what this story needs to accomplish", "", 3000, true, true))

	// Generate Acceptance Criteria
	messageBlocks = append(messageBlocks, createTextInputField("acceptanceCriteria", "AcceptanceCriteria", "Describe in detail what all needs to be done including acceptance criteria and definition of done, done, done.", "", 3000, true, true))

	// Generate Other Information
	messageBlocks = append(messageBlocks, createTextInputField("otherInfo", "Other Information", "Record anything else that may be helpful to someone else picking up the card.", "", 3000, true, true))

	view.Blocks = slack.Blocks{
		BlockSet: messageBlocks,
	}

	// We set channel id of where request came from to log the new jira that was created.
	view.PrivateMetadata = command.ChannelID

	// Create request and set the view
	log.Println("Attempting to open dialog")
	resp, err := client.OpenViewContext(ctx, triggerId, view)
	if err != nil {
		log.Printf("Unable to create view: %v", err)
		log.Printf("failure response: %v", resp)
		return nil, err
	} else {
		log.Println("Dialog was opened without err.")
		return nil, nil
	}
}

func createTextInputField(fieldId, fieldLabel, fieldPlaceholderText, fieldHintText string, maxLength int, multiline, isOptional bool) slack.Block {
	var channelNameText, channelNameHint, channelPlaceholder *slack.TextBlockObject

	channelNameText = slack.NewTextBlockObject(slack.PlainTextType, fmt.Sprintf("%v:", fieldLabel), false, false)

	if len(fieldHintText) > 0 {
		channelNameHint = slack.NewTextBlockObject(slack.PlainTextType, fieldHintText, false, false)
	}
	if len(fieldPlaceholderText) > 0 {
		channelPlaceholder = slack.NewTextBlockObject(slack.PlainTextType, fieldPlaceholderText, false, false)
	}
	channelNameElement := slack.NewPlainTextInputBlockElement(channelPlaceholder, fieldId)

	// Set max length if requested.  Must be greater than 0.
	if maxLength > 0 {
		channelNameElement.MaxLength = maxLength
	} else {
		channelNameElement.MaxLength = 80
	}

	channelNameElement.Multiline = multiline
	channelNameBlock := slack.NewInputBlock(fieldId, channelNameText, channelNameHint, channelNameElement)
	channelNameBlock.Optional = isOptional

	return channelNameBlock
}

func CanHandleViewSubmission(callbackID string) bool {
	return callbackID == CreateJiraDialogCommand
}

func HandleViewSubmission(ctx context.Context, client util.SlackClientInterface, data slack.InteractionCallback) ([]slack.MsgOption, error) {
	log.Println("Handling ViewSubmission")
	var err error
	var title, description string

	inputValues := data.View.State.Values
	title = inputValues["title"]["title"].Value

	// Create text to enter into Jira description field (Big block with user story, description, etc)
	jiraInput := jiraDescriptionInput{
		UserStory:          inputValues["userStory"]["userStory"].Value,
		Description:        inputValues["description"]["description"].Value,
		AcceptanceCriteria: inputValues["acceptanceCriteria"]["acceptanceCriteria"].Value,
		OtherInfo:          inputValues["otherInfo"]["otherInfo"].Value,
	}

	description, err = invokeTemplate(issueTemplateDialog, jiraInput)
	if err != nil {
		return util.StringToBlock(fmt.Sprintf("unable to to process template. error: %v", err), false), nil
	}

	return generateJira(ctx, data, title, description, false)
}
