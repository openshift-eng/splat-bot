package util

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

var (
	channelIDMaptest = map[string]string{
		"test":   "test",
		"random": "random",
		"vmware": "vmware",
	}
)

type SlackClientInterface interface {
	PostEphemeral(channelID string, userID string, options ...slack.MsgOption) (string, error)
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	OpenConversation(params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)
	GetConversationReplies(params *slack.GetConversationRepliesParameters) (msgs []slack.Message, hasMore bool, nextCursor string, err error)
	GetConversationInfo(input *slack.GetConversationInfoInput) (*slack.Channel, error)
	OpenViewContext(ctx context.Context, triggerID string, view slack.ModalViewRequest) (*slack.ViewResponse, error)
}

type StubInterface struct {
}

func (s *StubInterface) PostEphemeral(channelID string, userID string, options ...slack.MsgOption) (string, error) {
	return "", fmt.Errorf("PostEphemeral")
}

func (s *StubInterface) PostMessage(channelID string, options ...slack.MsgOption) (string, string, error) {
	return "", "", fmt.Errorf("PostMessage")
}

func (s *StubInterface) OpenConversation(params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
	return nil, false, false, fmt.Errorf("OpenConversation")
}

func (s *StubInterface) GetConversationReplies(params *slack.GetConversationRepliesParameters) (msgs []slack.Message, hasMore bool, nextCursor string, err error) {
	return nil, false, "", fmt.Errorf("GetConversationReplies")
}

func (s *StubInterface) GetConversationInfo(input *slack.GetConversationInfoInput) (*slack.Channel, error) {
	if channelID, ok := channelIDMaptest[input.ChannelID]; ok {
		return &slack.Channel{
			GroupConversation: slack.GroupConversation{
				Name: channelID,
			},
		}, nil
	}
	return nil, fmt.Errorf("GetConversationInfo")
}

func (s *StubInterface) OpenViewContext(ctx context.Context, triggerID string, view slack.ModalViewRequest) (*slack.ViewResponse, error) {
	return nil, fmt.Errorf("OpenViewContext")
}
