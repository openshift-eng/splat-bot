package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift-splat-team/jira-bot/pkg/util"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

var UnsizedAttributes = Attributes{
	Commands:       []string{"jira", "unsized"},
	RequireMention: true,
	Callback: func(ctx context.Context, client *socketmode.Client, evt *slackevents.MessageEvent, args []string) ([]slack.MsgOption, error) {
		issues, err := util.GetUnsizedStories()
		if err != nil {
			return WrapErrorToBlock(err, "error querying issues"), nil
		}

		var builder strings.Builder
		for _, issue := range issues {
			builder.WriteString(fmt.Sprintf("%s - %s\n", issue.Key, issue.Fields.Summary))
		}
		if len(issues) == 0 {
			builder.WriteString("no issues found")
		}

		return StringToBlock(builder.String(), false), nil
	},
	RequiredArgs: 3,
	HelpMarkdown: "outputs a list of unsized stories for import in to PlanIt Poker: `jira unsized [project]`",
}
