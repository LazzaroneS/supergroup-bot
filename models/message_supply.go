// 消息补发模块
package models

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MixinNetwork/supergroup/config"
	"github.com/MixinNetwork/supergroup/session"
	"github.com/MixinNetwork/supergroup/tools"
	"github.com/fox-one/mixin-sdk-go"
	"github.com/jackc/pgx/v4"
)

// 新人入群发送的消息
func SendWelcomeAndLatestMsg(clientID, userID string) {
	client, r, err := GetReplayAndMixinClientByClientID(clientID)
	if err != nil {
		return
	}
	if err := SendTextMsg(_ctx, clientID, userID, r.Welcome); err != nil {
		session.Logger(_ctx).Println(err)
	}
	btns := mixin.AppButtonGroupMessage{
		{Label: config.Text.Home, Action: client.Host, Color: "#5979F0"},
	}
	if client.AssetID != "" {
		btns = append(btns, mixin.AppButtonMessage{Label: config.Text.Transfer, Action: fmt.Sprintf("%s/trade/%s", client.Host, client.AssetID), Color: "#8A64D0"})
	}
	if err := SendBtnMsg(_ctx, clientID, userID, btns); err != nil {
		session.Logger(_ctx).Println(err)
	}
	conversationStatus := getClientConversationStatus(_ctx, clientID)
	if conversationStatus == "" ||
		conversationStatus == ClientConversationStatusNormal ||
		conversationStatus == ClientConversationStatusMute {
		go sendLatestMsgAndPINMsg(client, userID, 20)
	} else if conversationStatus == ClientConversationStatusAudioLive {
		go sendLatestLiveMsg(client, userID)
	}
}

func sendLatestMsgAndPINMsg(client *MixinClient, userID string, msgCount int) {
	c, err := GetClientUserByClientIDAndUserID(_ctx, client.ClientID, userID)
	if err != nil {
		session.Logger(_ctx).Println(err)
		return
	}
	_ = UpdateClientUserPriority(_ctx, client.ClientID, userID, ClientUserPriorityPending)
	sendPendingMsgByCount(_ctx, client.ClientID, userID, msgCount)
	_ = UpdateClientUserPriority(_ctx, client.ClientID, userID, c.Priority)
	SendAssetsNotPassMsg(client.ClientID, userID, "", true)
}

func sendLatestLiveMsg(client *MixinClient, userID string) {
	c, err := GetClientUserByClientIDAndUserID(_ctx, client.ClientID, userID)
	if err != nil {
		session.Logger(_ctx).Println(err)
		return
	}
	_ = UpdateClientUserPriority(_ctx, client.ClientID, userID, ClientUserPriorityPending)
	// 1. 获取直播的开始时间
	var startAt time.Time
	err = session.Database(_ctx).QueryRow(_ctx, `
SELECT ld.start_at FROM live_data ld
LEFT JOIN lives l ON ld.live_id=l.live_id
WHERE l.status=1
`).Scan(&startAt)
	if err != nil {
		session.Logger(_ctx).Println(err)
		return
	}
	sendPendingLiveMsg(_ctx, client.ClientID, userID, startAt)
	_ = UpdateClientUserPriority(_ctx, client.ClientID, userID, c.Priority)
}

func sendPendingMsgByCount(ctx context.Context, clientID, userID string, count int) {
	lastCreatedAt, err := sendMsgWithSQL(ctx, clientID, userID, `
	(SELECT user_id,message_id,category,data,status,created_at
		FROM messages
		WHERE client_id=$1
		AND status IN (4,6)
		AND category!='MESSAGE_RECALL'
			AND category!='MESSAGE_PIN'
		AND created_at > CURRENT_DATE-1
		ORDER BY created_at DESC
		LIMIT $2) UNION (SELECT user_id,message_id,category,data,status,created_at
		FROM messages
		WHERE client_id=$1
		AND status=10
		AND category!='MESSAGE_PIN'
		ORDER BY created_at DESC) order by created_at desc
	`, clientID, count)
	if err != nil {
		session.Logger(ctx).Println(err)
		return
	}

	for {
		if lastCreatedAt.IsZero() {
			break
		}
		lastCreatedAt, err = sendLeftMsg(ctx, clientID, userID, lastCreatedAt)
		if err != nil {
			session.Logger(ctx).Println(err)
			return
		}
	}
}

func sendPendingLiveMsg(ctx context.Context, clientID, userID string, startTime time.Time) {
	lastCreatedAt := startTime
	var err error
	for {
		if lastCreatedAt.IsZero() {
			break
		}
		lastCreatedAt, err = sendLeftMsg(ctx, clientID, userID, lastCreatedAt)
		if err != nil {
			session.Logger(ctx).Println(err)
			return
		}
	}
}

func sendLeftMsg(ctx context.Context, clientID, userID string, leftTime time.Time) (time.Time, error) {
	return sendMsgWithSQL(ctx, clientID, userID, `
SELECT user_id,message_id,category,data,status,created_at
FROM messages
WHERE client_id=$1
AND created_at>$2
AND status IN (4,6)
AND category!='MESSAGE_RECALL'
AND category!='MESSAGE_PIN'
ORDER BY created_at DESC`, clientID, leftTime)
}

func sendMsgWithSQL(ctx context.Context, clientID, userID, sql string, params ...interface{}) (time.Time, error) {
	msgs, err := getMsgWithSQL(ctx, clientID, userID, sql, params...)
	if err != nil {
		return time.Time{}, err
	}
	return distributeMsg(ctx, msgs, clientID, userID)
}

func getMsgWithSQL(ctx context.Context, clientID, userID, sql string, params ...interface{}) ([]*Message, error) {
	msgs := make([]*Message, 0)
	if err := session.Database(ctx).ConnQuery(ctx, sql, func(rows pgx.Rows) error {
		for rows.Next() {
			var msg Message
			if err := rows.Scan(&msg.UserID, &msg.MessageID, &msg.Category, &msg.Data, &msg.Status, &msg.CreatedAt); err != nil {
				return err
			}
			msgs = append(msgs, &msg)
		}
		return nil
	}, params...); err != nil {
		return nil, err
	}
	return msgs, nil
}

func distributeMsg(ctx context.Context, msgList []*Message, clientID, userID string) (time.Time, error) {
	if len(msgList) == 0 {
		return time.Time{}, nil
	}
	msgs := make([]*mixin.MessageRequest, 0)
	conversationID := mixin.UniqueConversationID(clientID, userID)
	for _, message := range msgList {
		if userID == message.UserID {
			continue
		}
		msgID := tools.GetUUID()
		msgs = append([]*mixin.MessageRequest{{
			ConversationID:   conversationID,
			RecipientID:      userID,
			MessageID:        msgID,
			Category:         message.Category,
			Data:             message.Data,
			RepresentativeID: message.UserID,
		}}, msgs...)
		if message.Status == MessageStatusPINMsg {
			dataByte, _ := json.Marshal(map[string]interface{}{"message_ids": []string{msgID}, "action": "PIN"})
			dataStr := tools.Base64Encode(dataByte)
			msgs = append(msgs, &mixin.MessageRequest{
				ConversationID: conversationID,
				RecipientID:    userID,
				MessageID:      mixin.UniqueConversationID(message.UserID, message.MessageID),
				Category:       "MESSAGE_PIN",
				Data:           dataStr,
			})
		}
	}
	client := GetMixinClientByID(ctx, clientID)
	// 存入成功之后再发送
	for _, m := range msgs {
		if err := createFinishedDistributeMsg(ctx, clientID, userID, m.MessageID, m.ConversationID, "0", m.MessageID, "", time.Now()); err != nil {
			session.Logger(ctx).Println(err)
			continue
		}
		_ = SendMessage(ctx, client.Client, m, true)
	}
	return msgList[0].CreatedAt, nil
}

func getLeftDistributeMsgAndDistribute(ctx context.Context, clientID, userID string) (time.Time, error) {
	msgs := make([]*mixin.MessageRequest, 0)
	var originMsgID string
	if err := session.Database(ctx).ConnQuery(ctx, `
SELECT conversation_id,user_id,message_id,category,data,representative_id,quote_message_id,origin_message_id
FROM distribute_messages
WHERE client_id=$1 AND user_id=$2 AND status=$3
ORDER BY created_at
`,
		func(rows pgx.Rows) error {
			for rows.Next() {
				var dm mixin.MessageRequest
				if err := rows.Scan(&dm.ConversationID, &dm.RecipientID, &dm.MessageID, &dm.Category, &dm.Data, &dm.RepresentativeID, &dm.QuoteMessageID, &originMsgID); err != nil {
					return err
				}
				msgs = append(msgs, &dm)
			}
			return nil
		}, clientID, userID, DistributeMessageStatusAloneList); err != nil {
		return time.Time{}, err
	}
	if len(msgs) == 0 {
		return time.Time{}, nil
	}
	client := GetMixinClientByID(ctx, clientID)
	for _, msg := range msgs {
		if err := SendMessage(ctx, client.Client, msg, true); err == nil {
			if err := UpdateDistributeMessagesStatusToFinished(ctx, []string{msg.MessageID}); err != nil {
				session.Logger(ctx).Println(err)
			}
		}
	}

	msg, err := getMsgByClientIDAndMessageID(ctx, clientID, originMsgID)
	if err != nil {
		session.Logger(ctx).Println(err)
		return time.Time{}, err
	}
	return msg.CreatedAt, nil
}