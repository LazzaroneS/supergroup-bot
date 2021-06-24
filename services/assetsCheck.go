package services

import (
	"context"
	"github.com/MixinNetwork/supergroup/config"
	"github.com/MixinNetwork/supergroup/models"
	"github.com/MixinNetwork/supergroup/session"
	"time"
)

type AssetsCheckService struct{}

func (service *AssetsCheckService) Run(ctx context.Context) error {
	for {
		//handlePendingUsers(ctx)
		if err := startAssetCheck(ctx); err != nil {
			session.Logger(ctx).Println(err)
		}
		time.Sleep(config.AssetsCheckTime)
	}
}

func startAssetCheck(ctx context.Context) error {
	// 获取所有的用户
	allClientUser, err := models.GetAllClientUser(ctx)
	if err != nil {
		return err
	}
	// 检查所有的用户是否活跃
	//go models.CheckUserIsActive(ctx, allClientUser)
	var allUser []string
	_allUser := make(map[string]bool)
	for _, user := range allClientUser {
		_allUser[user.UserID] = true
	}
	for k := range _allUser {
		allUser = append(allUser, k)
	}
	foxUserAssetMap, _ := models.GetAllUserFoxShares(ctx, allUser)
	exinUserAssetMap, _ := models.GetAllUserExinShares(ctx, allUser)

	for _, user := range allClientUser {
		if curStatus, err := models.GetClientUserStatus(ctx, user, foxUserAssetMap[user.UserID], exinUserAssetMap[user.UserID]); err != nil {
			session.Logger(ctx).Println(err)
			if err := models.UpdateClientUserPriorityAndStatus(ctx, user.ClientID, user.UserID, models.ClientUserPriorityLow, models.ClientUserStatusAudience); err != nil {
				session.Logger(ctx).Println(err)
			}
		} else {
			// 如果之前是低状态，现在是高状态，那么先 pending 之前的消息
			if user.SpeakStatus == models.ClientSpeckStatusOpen && user.Priority == models.ClientUserPriorityLow && curStatus != models.ClientUserStatusAudience {
				if err := models.UpdateClientUserAndMessageToPending(ctx, user.ClientID, user.UserID); err != nil {
					session.Logger(ctx).Println(err)
				}
			}
			// 如果之前是高状态，现在是低状态
			if user.Priority == models.ClientUserPriorityHigh && curStatus == models.ClientUserStatusAudience {
				if err := models.UpdateClientUserPriority(ctx, user.ClientID, user.UserID, models.ClientUserPriorityLow); err != nil {
					return err
				}
			}
			// 更新用户的身份
			if err := models.UpdateClientUserStatus(ctx, user.ClientID, user.UserID, curStatus); err != nil {
				session.Logger(ctx).Println(err)
			}
		}
	}
	return nil
}
