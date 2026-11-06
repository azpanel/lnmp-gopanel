package backups

import (
	"regexp"
	"strconv"
	"time"
)

type Notification struct {
	Enabled  bool   `json:"enabled"`
	Channel  string `json:"channel"`
	UID      string `json:"uid"`
	HasToken bool   `json:"has_token"`
}

type NotificationTestRequest struct {
	PolicyIdentity
	Notification Notification `json:"notification"`
	TokenAction  string       `json:"token_action"`
	Token        string       `json:"token"`
}

var telegramToken = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]{20,200}$`)
var telegramUID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

func ValidateNotification(n *Notification, action, token string) error {
	if n == nil {
		if action != "" || token != "" {
			return invalidPolicy("通知配置缺失")
		}
		return nil
	}
	if n.Channel != "telegram" {
		return invalidPolicy("通知渠道仅支持 Telegram 机器人")
	}
	if n.Enabled || n.UID != "" {
		if _, err := strconv.ParseInt(n.UID, 10, 64); err != nil || !telegramUID.MatchString(n.UID) {
			return invalidPolicy("通知用户 UID 须为正整数")
		}
	}
	if action != "" && action != "keep" && action != "replace" && action != "clear" {
		return invalidPolicy("通知 Token 操作无效")
	}
	if action == "replace" {
		if len(token) > 256 || !telegramToken.MatchString(token) {
			return invalidPolicy("通知机器人 Token 格式无效")
		}
	} else if token != "" {
		return invalidPolicy("请明确选择更换通知 Token")
	}
	if n.Enabled && action == "clear" {
		return invalidPolicy("启用通知时不能清除 Token")
	}
	return nil
}

func ValidateNotificationTest(r NotificationTestRequest) error {
	if (r.ID == "") != (r.Revision == 0) || (r.ID != "" && !ValidPolicyID(r.ID)) {
		return invalidPolicy("通知测试的政策标识或版本无效")
	}
	n := r.Notification
	n.Enabled = true
	return ValidateNotification(&n, r.TokenAction, r.Token)
}

type NotificationResult struct {
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
}

func PendingNotification(n *NotificationResult) bool {
	return n != nil && (n.Status == "pending" || n.Status == "sending")
}
