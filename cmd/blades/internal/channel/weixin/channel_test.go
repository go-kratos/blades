package weixin

import (
	"context"
	"errors"
	"testing"

	wx "github.com/daemon365/weixin-clawbot"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
)

func TestHandleMessagesContinuesAfterHandlerError(t *testing.T) {
	t.Parallel()

	ch := New(wx.Account{}, "")

	var handled []string
	handler := func(ctx context.Context, sessionID, text string, w channel.Writer) (string, error) {
		handled = append(handled, text)
		if len(handled) == 1 {
			return "", errors.New("edit: edits[0] target not found")
		}
		return "ok", nil
	}

	err := ch.handleMessages(context.Background(), []wx.WeixinMessage{
		textMessage("user-1", "first"),
		textMessage("user-1", "second"),
	}, handler)
	if err != nil {
		t.Fatalf("handleMessages: %v", err)
	}
	if got, want := len(handled), 2; got != want {
		t.Fatalf("handled messages = %d, want %d", got, want)
	}
	if got, want := handled[0], "first"; got != want {
		t.Fatalf("first handled text = %q, want %q", got, want)
	}
	if got, want := handled[1], "second"; got != want {
		t.Fatalf("second handled text = %q, want %q", got, want)
	}
}

func textMessage(fromUserID, text string) wx.WeixinMessage {
	return wx.WeixinMessage{
		FromUserID:  fromUserID,
		MessageType: wx.MessageTypeUser,
		ItemList: []wx.MessageItem{{
			Type:     wx.MessageItemTypeText,
			TextItem: &wx.TextItem{Text: text},
		}},
	}
}
