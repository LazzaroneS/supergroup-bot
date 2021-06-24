package models

import (
	"context"
	"github.com/MixinNetwork/supergroup/durable"
	"github.com/MixinNetwork/supergroup/session"
	"github.com/jackc/pgx/v4"
	"github.com/shopspring/decimal"
	"strconv"
	"time"
)

const client_block_user_DDL = `
CREATE TABLE IF NOT EXISTS client_block_user (
  client_id           VARCHAR(36) NOT NULL,
  user_id             VARCHAR(36) NOT NULL,
  created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  PRIMARY KEY (client_id,user_id)
);
CREATE INDEX client_block_user_idx ON client_block_user(client_id);
`

const block_user_DDL = `
CREATE TABLE IF NOT EXISTS block_user (
  user_id             VARCHAR(36) NOT NULL PRIMARY KEY,
  created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
`

type ClientBlockUser struct {
	ClientID  string
	UserID    string
	CreatedAt time.Time
}

type BlockUser struct {
	UserID    string
	CreatedAt time.Time
}

var cacheBlockClientUserIDMap = make(map[string]map[string]bool)

func blockAll(ctx context.Context, u string) error {
	query := durable.InsertQueryOrUpdate("block_user", "user_id", "")
	_, err := session.Database(ctx).Exec(ctx, query, u)
	return err
}

// 检查是否是block的用户
func checkIsBlockUser(ctx context.Context, clientID, userID string) bool {
	if cacheBlockClientUserIDMap[clientID] == nil {
		blockUsers := make(map[string]bool)
		if err := session.Database(ctx).ConnQuery(ctx, `SELECT user_id FROM block_user`, func(rows pgx.Rows) error {
			for rows.Next() {
				var u string
				if err := rows.Scan(&u); err != nil {
					return err
				}
				blockUsers[u] = true
			}
			return nil
		}); err != nil {
			return false
		}
		if err := session.Database(ctx).ConnQuery(ctx, `SELECT user_id FROM client_block_user WHERE client_id=$1`, func(rows pgx.Rows) error {
			for rows.Next() {
				var u string
				if err := rows.Scan(&u); err != nil {
					return err
				}
				blockUsers[u] = true
			}
			return nil
		}, clientID); err != nil {
			return false
		}
		cacheBlockClientUserIDMap[clientID] = blockUsers
	}

	return cacheBlockClientUserIDMap[clientID][userID]
}

// 禁言 一个用户 mutedTime=0 则为取消禁言
func muteClientUser(ctx context.Context, clientID, userID, mutedTime string) error {
	var mutedAt time.Time
	mute, _ := strconv.Atoi(mutedTime)
	mutedAt = time.Now().Add(time.Duration(int64(mute)) * time.Hour)
	_, err := session.Database(ctx).Exec(ctx, `UPDATE client_users SET (muted_time,muted_at)=($3,$4) WHERE client_id=$1 AND user_id=$2`, clientID, userID, mutedTime, mutedAt)
	return err
}

// 拉黑一个用户
func blockClientUser(ctx context.Context, clientID, userID string, isCancel bool) error {
	var query string
	if isCancel {
		query = "DELETE FROM client_block_user WHERE client_id=$1 AND user_id=$2"
	} else {
		query = durable.InsertQueryOrUpdate("client_block_user", "client_id,user_id", "")
		go recallLatestMsg(clientID, userID)
	}
	cacheBlockClientUserIDMap[clientID] = nil
	_, err := session.Database(ctx).Exec(ctx, query, clientID, userID)
	return err
}

// 撤回用户最近 1 小时的消息
func recallLatestMsg(clientID, uid string) {
	// 1. 找到该用户最近发的消息列表的ID
	msgIDList := make([]string, 0)
	err := session.Database(_ctx).ConnQuery(_ctx, `
SELECT message_id FROM messages WHERE user_id=$1 AND status=$2 AND category=ANY($3) AND now()-created_at<interval '1 hours'
`, func(rows pgx.Rows) error {
		var msgID string
		for rows.Next() {
			if err := rows.Scan(&msgID); err != nil {
				return err
			}
			msgIDList = append(msgIDList, msgID)
		}
		return nil
	}, uid, MessageStatusFinished, recallMsgCategorySupportList)
	if err != nil {
		session.Logger(_ctx).Println(err)
		return
	}
	for _, msgID := range msgIDList {
		if err := CreatedManagerRecallMsg(_ctx, clientID, msgID, uid); err != nil {
			session.Logger(_ctx).Println(err)
			return
		}
	}
}

func checkIsMutedUser(user *ClientUser) bool {
	now := time.Now()
	if user.MutedAt.After(now) {
		duration := decimal.NewFromFloat(user.MutedAt.Sub(now).Hours())
		hour := duration.IntPart()
		minute := duration.Sub(decimal.NewFromInt(hour)).Mul(decimal.NewFromInt(60)).IntPart()
		go SendMutedMsg(user.ClientID, user.UserID, user.MutedTime, int(hour), int(minute))
		return true
	}
	return false
}

func AddBlockUser(ctx context.Context, userID string) error {
	u, err := searchUser(ctx, userID)
	if err != nil {
		return err
	}
	query := durable.InsertQueryOrUpdate("block_user", "user_id", "")
	_, err = session.Database(ctx).Exec(ctx, query, u.UserID)
	return err
}
