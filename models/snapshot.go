package models

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MixinNetwork/supergroup/config"
	"github.com/MixinNetwork/supergroup/durable"
	"github.com/MixinNetwork/supergroup/session"
	"github.com/MixinNetwork/supergroup/tools"
	"github.com/fox-one/mixin-sdk-go"
	"github.com/jackc/pgx/v4"
	"github.com/shopspring/decimal"
)

const snapshots_DDL = `
CREATE TABLE IF NOT EXISTS snapshots (
    snapshot_id  				VARCHAR(36) NOT NULL PRIMARY KEY,
    client_id    				VARCHAR(36) NOT NULL,
    trace_id     				VARCHAR(36) NOT NULL,
    user_id      				VARCHAR(36) NOT NULL,
    asset_id     				VARCHAR(36) NOT NULL,
    amount       				VARCHAR NOT NULL,
    memo         				VARCHAR DEFAULT '',
    created_at   				timestamp with time zone NOT NULL
);
`
const transfer_pendding_DDL = `
CREATE TABLE IF NOT EXISTS transfer_pendding (
	trace_id 				VARCHAR(36) NOT NULL PRIMARY KEY,
	client_id 			VARCHAR(36) NOT NULL,
	asset_id 				VARCHAR(36) NOT NULL,
	opponent_id 		VARCHAR(36) NOT NULL,
	amount 					VARCHAR NOT NULL,
	memo 						VARCHAR DEFAULT '',
	status 					SMALLINT NOT NULL DEFAULT 1, -- 1 pending, 2 success
	created_at 			timestamp with time zone NOT NULL
);
`

type Snapshot struct {
	ClientID   string          `json:"client_id"`
	SnapshotID string          `json:"snapshot_id"`
	TraceID    string          `json:"trace_id"`
	UserID     string          `json:"user_id"`
	AssetID    string          `json:"asset_id"`
	Amount     decimal.Decimal `json:"amount"`
	Memo       string          `json:"memo"`
	CreatedAt  time.Time       `json:"created_at"`
}

type Transfer struct {
	*mixin.TransferInput
	ClientID string `json:"client_id"`
}

const (
	TransferStatusPending = 1
	TransferStatusSucceed = 2
)

func handelRewardSnapshot(ctx context.Context, clientID string, s *mixin.Snapshot) (bool, error) {
	var isReward bool
	var r reward
	if err := json.Unmarshal([]byte(s.Memo), &r); err != nil {
		return isReward, nil
	}
	if r.Reward == "" {
		session.Logger(ctx).Println("reward is empty")
		tools.PrintJson(s)
		return isReward, nil
	}
	isReward = true
	msg := config.Config.Text.Reward
	from, err := getUserByID(ctx, s.OpponentID)
	if err != nil {
		return isReward, err
	}
	to, err := getUserByID(ctx, r.Reward)
	if err != nil {
		return isReward, err
	}
	asset, err := GetAssetByID(ctx, nil, s.AssetID)
	if err != nil {
		return isReward, err
	}
	client := GetMixinClientByID(ctx, clientID)

	msg = strings.ReplaceAll(msg, "{send_name}", from.FullName)
	msg = strings.ReplaceAll(msg, "{reward_name}", to.FullName)
	msg = strings.ReplaceAll(msg, "{amount}", s.Amount.String())
	msg = strings.ReplaceAll(msg, "{symbol}", asset.Symbol)

	byteMsg, err := json.Marshal([]mixin.AppButtonMessage{
		{Label: msg, Action: fmt.Sprintf("%s/reward?uid=%s", client.Host, to.IdentityNumber), Color: tools.RandomColor()},
	})
	if err != nil {
		return isReward, err
	}

	go SendClientMsg(clientID, mixin.MessageCategoryAppButtonGroup, tools.Base64Encode(byteMsg))
	go handleReward(clientID, s, from, to)
	return isReward, nil
}

// 处理 reward 的转账添加
func handleReward(clientID string, s *mixin.Snapshot, from, to *mixin.User) error {
	// 1. 保存转账记录
	if err := addSnapshot(_ctx, clientID, s); err != nil {
		session.Logger(_ctx).Println("add snapshot error", err)
		return err
	}
	// 2. 添加transfer_pendding
	traceID := mixin.UniqueConversationID(s.SnapshotID, s.TraceID)
	msg := strings.ReplaceAll(config.Config.Text.From, "{identity_number}", from.IdentityNumber)
	if err := createTransferPending(_ctx, clientID, traceID, s.AssetID, to.UserID, msg, s.Amount); err != nil {
		session.Logger(_ctx).Println("create transfer_pendding error", err)
		return err
	}
	return nil
}

func HandleTransfer() {
	for {
		handleTransfer(_ctx)
		time.Sleep(5 * time.Second)
	}
}

func handleTransfer(ctx context.Context) {
	ts := make([]*Transfer, 0)
	if err := session.Database(ctx).ConnQuery(ctx, `
SELECT client_id,trace_id,asset_id,opponent_id,amount,memo 
FROM transfer_pendding 
WHERE status=1`, func(rows pgx.Rows) error {
		for rows.Next() {
			t := Transfer{new(mixin.TransferInput), ""}
			if err := rows.Scan(&t.ClientID, &t.TraceID, &t.AssetID, &t.OpponentID, &t.Amount, &t.Memo); err != nil {
				return err
			}
			ts = append(ts, &t)
		}
		return nil
	}); err != nil {
		session.Logger(ctx).Println("select transfer_pendding error", err)
		return
	}
	for _, t := range ts {
		client := GetMixinClientByID(_ctx, t.ClientID)
		pin, err := getMixinPinByID(_ctx, t.ClientID)
		if err != nil {
			session.Logger(ctx).Println("get pin error", err)
			continue
		}
		s, err := client.Transfer(_ctx, t.TransferInput, pin)
		if err != nil {
			session.Logger(ctx).Println("transfer error", err)
			continue
		}
		if err := addSnapshot(ctx, t.ClientID, s); err != nil {
			session.Logger(ctx).Println("add snapshot error", err)
			continue
		}
		if err := updateTransferToSuccess(_ctx, t.TraceID); err != nil {
			session.Logger(ctx).Println("update transfer_pendding error", err)
			continue
		}
	}
}

func addSnapshot(ctx context.Context, clientID string, s *mixin.Snapshot) error {
	query := durable.InsertQueryOrUpdate("snapshots", "snapshot_id", "client_id,trace_id,user_id,asset_id,amount,memo,created_at")
	_, err := session.Database(ctx).Exec(ctx, query, s.SnapshotID, clientID, s.TraceID, s.UserID, s.AssetID, s.Amount.String(), s.Memo, s.CreatedAt)
	return err
}

func createTransferPending(ctx context.Context, client_id, traceID, assetID, opponentID, memo string, amount decimal.Decimal) error {
	query := durable.InsertQuery("transfer_pendding", "client_id,trace_id,asset_id,opponent_id,amount,memo,status,created_at")
	_, err := session.Database(ctx).Exec(ctx, query, client_id, traceID, assetID, opponentID, amount.String(), memo, TransferStatusPending, time.Now())
	return err
}

func updateTransferToSuccess(ctx context.Context, traceID string) error {
	_, err := session.Database(ctx).Exec(ctx, `UPDATE transfer_pendding SET status = 2 WHERE trace_id = $1`, traceID)
	return err
}