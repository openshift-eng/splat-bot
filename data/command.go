package data

import (
	"context"

	"github.com/openshift-splat-team/splat-bot/pkg/util"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

type Callback func(ctx context.Context, client util.SlackClientInterface, evt *slackevents.MessageEvent, args []string) ([]slack.MsgOption, error)

type SlashCallback func(ctx context.Context, client util.SlackClientInterface, cmd slack.SlashCommand, args []string) ([]slack.MsgOption, error)

type MessageOfInterest func(args []string, attribute Attributes, channel string) bool

type CanHandleViewSubmission func(callbackID string) bool

type HandleViewSubmission func(ctx context.Context, client util.SlackClientInterface, data slack.InteractionCallback) ([]slack.MsgOption, error)

// Attributes define when and how to handle a message
type Attributes struct {
	// Commands when matched, the Callback is invoked.
	Commands []string
	// MessageOfInterest supercedes Commands. If MessageOfInterest is set, Commands is ignored.  This is useful for more complex matching.
	MessageOfInterest MessageOfInterest
	// The number of arguments a command must have. var args are not supported.
	RequiredArgs int
	// MaxArgs The maximum number of allowed arguments
	MaxArgs int
	// Callback function called when the attributes are met
	Callback Callback
	// Rank: Future - in a situation where multiple regexes match, this allows a priority to be assigned.
	Rank int64
	// RequireMention when true, @splat-bot must be used to invoke the command.
	RequireMention bool
	// HelpMarkdown is markdown that is contributed with the bot shows help.
	HelpMarkdown string
	// RespondInDM responds in a DM to the user.
	RespondInDM bool
	// RequireInChannel the attribute will only be recognized in a given channel(s).
	RequireInChannel []string
	// MustBeInThread the attribute will only be recognized in a thread.
	MustBeInThread bool
	// AllowNonSplatUsers by default, only members of @splat-team can interact with the bot
	AllowNonSplatUsers bool
	// This command will not be included in the help message.
	ExcludeFromHelp bool
	// DontGlobQuotes when true, quotes are not globbed.  This is useful for knowledge commands that need discrete tokens.
	DontGlobQuotes bool
	// RespondInChannel responds in the channel to the user. If false, responds in a thread.
	RespondInChannel bool
	// ResponseIsEphemeral specifies if the response should be ephemeral.
	ResponseIsEphemeral bool
	// ShouldMatch is a list of strings that should match
	ShouldMatch []string `yaml:"should_match"`
	// ShouldntMatch is a list of strings that shouldnt match
	ShouldntMatch []string `yaml:"shouldnt_match"`
}

type SlashCommand struct {
	// Commands when matched, the Callback is invoked.
	Commands []string
	// The number of arguments a command must have. var args are not supported.
	RequiredArgs int
	// MaxArgs The maximum number of allowed arguments
	MaxArgs int
	// Callback function called when the attributes are met
	ProcessCommand SlashCallback
	// HelpMarkdown is markdown that is contributed with the bot shows help.
	HelpMarkdown string
	// RespondInDM responds in a DM to the user.
	RespondInDM bool
	// RequireInChannel the attribute will only be recognized in a given channel(s).
	RequireInChannel []string
	// MustBeInThread the attribute will only be recognized in a thread.
	MustBeInThread bool
	// AllowNonSplatUsers by default, only members of @splat-team can interact with the bot
	AllowNonSplatUsers bool
	// This command will not be included in the help message.
	ExcludeFromHelp bool
	// DontGlobQuotes when true, quotes are not globbed.  This is useful for knowledge commands that need discrete tokens.
	DontGlobQuotes bool
	// RespondInChannel responds in the channel to the user. If false, responds in a thread.
	RespondInChannel bool
	// ResponseIsEphemeral specifies if the response should be ephemeral.
	ResponseIsEphemeral bool
	// ViewSubmissionCheck func to test if this command can handle the ViewSubmission event for the specified callback ID
	ViewSubmissionCheck CanHandleViewSubmission
	// HandleViewSubmission func to call to perform actions on the ViewSubmission event
	HandleViewSubmission HandleViewSubmission
}
