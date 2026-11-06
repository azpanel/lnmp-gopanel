//go:build linux

package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

type backupNotificationSender func(context.Context, string, string, string) error

type backupNotificationDelivery struct {
	UID        string `json:"uid"`
	Ciphertext string `json:"ciphertext"`
}

func (s backupPolicyStore) encryptNotification(id string, token []byte) (string, error) {
	aead, err := s.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, token, []byte("telegram:"+id))), nil
}

func (s backupPolicyStore) decryptNotification(id, ciphertext string) ([]byte, error) {
	aead, err := s.policyCipher(false)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil || len(data) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("通知 Token 无法解密，请重新配置")
	}
	token, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte("telegram:"+id))
	if err != nil {
		return nil, errors.New("通知 Token 无法解密，请重新配置")
	}
	return token, nil
}

func (s backupPolicyStore) configureNotification(p *storedBackupPolicy, old, source storedBackupPolicy, r backups.SavePolicyRequest) error {
	base := old
	if r.CopyFrom != nil {
		base = source
	}
	n := r.Notification
	if n == nil {
		n = base.Notification
	}
	if n == nil {
		n = &backups.Notification{Channel: "telegram"}
	}
	value := *n
	p.Notification = &value
	if r.CopyFrom != nil {
		p.Notification.Enabled = false
	}
	p.NotificationCiphertext = base.NotificationCiphertext
	switch r.NotificationTokenAction {
	case "replace":
		encrypted, err := s.encryptNotification(p.ID, []byte(r.NotificationToken))
		if err != nil {
			return err
		}
		p.NotificationCiphertext = encrypted
	case "clear":
		p.NotificationCiphertext = ""
	default:
		if r.CopyFrom != nil && base.NotificationCiphertext != "" {
			token, err := s.decryptNotification(base.ID, base.NotificationCiphertext)
			if err != nil {
				return err
			}
			defer clear(token)
			p.NotificationCiphertext, err = s.encryptNotification(p.ID, token)
			if err != nil {
				return err
			}
		}
	}
	p.Notification.HasToken = p.NotificationCiphertext != ""
	if p.Notification.Enabled && !p.Notification.HasToken {
		return fmt.Errorf("%w：启用通知需要配置机器人 Token", backups.ErrInvalid)
	}
	return nil
}

func (s backupPolicyStore) testNotification(ctx context.Context, r backups.NotificationTestRequest, send backupNotificationSender) error {
	if err := backups.ValidateNotificationTest(r); err != nil {
		return err
	}
	var token []byte
	if err := func() error {
		release, err := s.lock()
		if err != nil {
			return err
		}
		defer release()
		var policy storedBackupPolicy
		if r.ID != "" {
			policies, err := s.load()
			if err != nil {
				return err
			}
			for _, p := range policies {
				if p.ID == r.ID {
					policy = p
					break
				}
			}
			if policy.ID == "" {
				return backups.ErrNotFound
			}
			if policy.Revision != r.Revision {
				return backups.ErrConflict
			}
		}
		if r.TokenAction == "replace" {
			token = []byte(r.Token)
			return nil
		}
		if policy.NotificationCiphertext == "" {
			return fmt.Errorf("%w：请填写通知机器人 Token", backups.ErrInvalid)
		}
		token, err = s.decryptNotification(policy.ID, policy.NotificationCiphertext)
		return err
	}(); err != nil {
		return err
	}
	defer clear(token)
	return send(ctx, string(token), r.Notification.UID, "LNMP GoPanel 通知测试\n机器人和接收用户配置有效。备份完成后将通过此机器人发送结果。")
}

func sendTelegram(ctx context.Context, token, uid, text string) error {
	return sendTelegramClient(ctx, &http.Client{}, token, uid, text)
}

// Credentials are present in Telegram's URL. Never return transport errors or
// Telegram response descriptions, both of which may contain that URL or input.
func sendTelegramClient(ctx context.Context, client *http.Client, token, uid, text string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	n := backups.Notification{Enabled: true, Channel: "telegram", UID: uid}
	if err := backups.ValidateNotification(&n, "replace", token); err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{"chat_id": uid, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(data))
	if err != nil {
		return errors.New("通知请求无法创建")
	}
	request.Header.Set("Content-Type", "application/json")
	bounded := *client
	bounded.Timeout = 10 * time.Second
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := bounded.Do(request)
	if err != nil {
		return errors.New("通知发送失败：无法连接 Telegram 或请求超时")
	}
	defer response.Body.Close()
	data, err = io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return errors.New("通知发送失败：Telegram 响应异常")
	}
	var result struct {
		OK        bool `json:"ok"`
		ErrorCode int  `json:"error_code"`
	}
	if json.Unmarshal(data, &result) != nil {
		return errors.New("通知发送失败：Telegram 响应异常")
	}
	if response.StatusCode == http.StatusOK && result.OK {
		return nil
	}
	code := response.StatusCode
	if result.ErrorCode != 0 {
		code = result.ErrorCode
	}
	switch code {
	case 401, 404:
		return errors.New("通知发送失败：机器人 Token 无效")
	case 400:
		return errors.New("通知发送失败：请检查用户 UID，并先向机器人发送 /start")
	case 403:
		return errors.New("通知发送失败：用户未授权或已屏蔽机器人")
	case 429:
		return errors.New("通知发送失败：Telegram 请求频率受限，请稍后测试")
	default:
		return errors.New("通知发送失败：Telegram 服务暂不可用")
	}
}

func prepareBackupNotification(j *storedBackupRun, now time.Time) {
	if j.Notification != nil || j.Snapshot == nil || j.Snapshot.Notification == nil || !j.Snapshot.Notification.Enabled {
		return
	}
	if j.Status != "succeeded" && j.Status != "partial" && j.Status != "failed" {
		return
	}
	j.Notification = &backups.NotificationResult{Status: "pending", UpdatedAt: now}
	j.NotificationDelivery = &backupNotificationDelivery{UID: j.Snapshot.Notification.UID, Ciphertext: j.Snapshot.NotificationCiphertext}
}

func backupNotificationText(j backups.Run) string {
	host, _ := os.Hostname()
	labels := map[string]string{"succeeded": "成功", "partial": "部分成功", "failed": "失败"}
	trigger := "手动"
	if j.Trigger == "schedule" {
		trigger = "计划"
	}
	stamp := func(t *time.Time) string {
		if t == nil {
			return "—"
		}
		return t.Local().Format("2006-01-02 15:04:05 -07:00")
	}
	duration := "—"
	if j.StartedAt != nil && j.FinishedAt != nil {
		duration = max(time.Duration(0), j.FinishedAt.Sub(*j.StartedAt)).Truncate(time.Second).String()
	}
	short := func(s string) string {
		chars := []rune(s)
		if len(chars) > 200 {
			return string(chars[:200]) + "…"
		}
		return s
	}
	lines := []string{"LNMP GoPanel 备份结果", "主机：" + short(host), "政策：" + j.PolicyName, "触发：" + trigger, "结果：" + labels[j.Status], "开始：" + stamp(j.StartedAt), "结束：" + stamp(j.FinishedAt), "耗时：" + duration}
	for _, category := range j.Categories {
		name := "文件"
		if category.Category == "databases" {
			name = "数据库"
		}
		delivered := 0
		for _, d := range category.Deliveries {
			if d.Status == "succeeded" {
				delivered++
			}
		}
		size := float64(category.Bytes) / (1024 * 1024)
		unit := "MB"
		if category.Bytes > 1024*1024*1024 {
			size /= 1024
			unit = "GB"
		}
		lines = append(lines, fmt.Sprintf("%s：%s，%.2f %s，交付 %d/%d", name, labels[category.Status], size, unit, delivered, len(category.Deliveries)))
		if category.Error != "" {
			lines = append(lines, "原因："+short(backupDiagnostic(category.Error)))
		}
		for _, d := range category.Deliveries {
			if d.Error != "" {
				lines = append(lines, "交付失败："+short(backupDiagnostic(d.Error)))
			}
			if d.CleanupError != "" {
				lines = append(lines, "清理失败："+short(backupDiagnostic(d.CleanupError)))
			}
		}
	}
	if j.Error != "" {
		lines = append(lines, "原因："+short(backupDiagnostic(j.Error)))
	}
	text := []rune(strings.Join(lines, "\n"))
	// Keep supplementary characters (for example emoji in paths) within the
	// message limit even when Telegram counts them as two UTF-16 code units.
	units := 0
	for i, r := range text {
		units++
		if r > 0xffff {
			units++
		}
		if units > 3500 {
			text = append(text[:i], []rune("\n更多详情请查看执行历史。")...)
			break
		}
	}
	return string(text)
}

// Claim durably before contacting Telegram. A claimed attempt is never retried,
// including when its response was lost or its process was interrupted.
func (e backupExecutor) deliverNotification(ctx context.Context, id string) error {
	release, err := e.store.lock()
	if err != nil {
		return err
	}
	j, err := e.loadRun(id)
	if err != nil {
		release()
		return err
	}
	if !backups.PendingNotification(j.Notification) {
		err = e.clearNotificationPending(j.ID)
		release()
		return err
	}
	if j.Notification.Status == "sending" {
		if e.now().Sub(j.Notification.UpdatedAt) > time.Minute {
			j.Notification.Status = "failed"
			j.Notification.Error = "发送中断，结果未知"
			j.Notification.UpdatedAt = e.now().UTC()
			j.NotificationDelivery = nil
			err = writeBackupJSON(e.runPath(id), j)
			if err == nil {
				err = e.clearNotificationPending(id)
			}
		}
		release()
		return err
	}
	if ctx.Err() != nil {
		release()
		return nil
	}
	j.Notification.Status = "sending"
	j.Notification.UpdatedAt = e.now().UTC()
	err = writeBackupJSON(e.runPath(id), j)
	release()
	if err != nil {
		return err
	}
	var sendErr error
	if j.NotificationDelivery == nil {
		sendErr = errors.New("通知凭据缺失，请重新配置")
	} else {
		token, decryptErr := e.store.decryptNotification(j.PolicyID, j.NotificationDelivery.Ciphertext)
		if decryptErr != nil {
			sendErr = errors.New("通知 Token 无法解密，请检查政策密钥或重新配置")
		} else {
			send := e.notify
			if send == nil {
				send = sendTelegram
			}
			message := strings.ReplaceAll(backupNotificationText(j.Run), string(token), "[已隐藏]")
			sendErr = send(ctx, string(token), j.NotificationDelivery.UID, message)
			clear(token)
		}
	}
	release, err = e.store.lock()
	if err != nil {
		return err
	}
	defer release()
	current, err := e.loadRun(id)
	if err != nil {
		return err
	}
	if current.Notification == nil || current.Notification.Status != "sending" {
		return nil
	}
	current.Notification.Status = "sent"
	if sendErr != nil {
		current.Notification.Status = "failed"
		current.Notification.Error = sendErr.Error()
	}
	current.Notification.UpdatedAt = e.now().UTC()
	current.NotificationDelivery = nil
	// Notification updates must not modify execution timestamps or history order.
	if err := writeBackupJSON(e.runPath(id), current); err != nil {
		return err
	}
	return e.clearNotificationPending(id)
}

func (e backupExecutor) deliverNotifications(ctx context.Context) error {
	release, err := e.store.lock()
	if err != nil {
		return err
	}
	index, err := e.index()
	release()
	if err != nil {
		return err
	}
	for _, ref := range index.Runs {
		if ctx.Err() != nil {
			break
		}
		if backups.ActiveRun(ref.Status) || !ref.NotificationPending {
			continue
		}
		if err := e.deliverNotification(ctx, ref.ID); err != nil && !errors.Is(err, backups.ErrNotFound) {
			return err
		}
	}
	return nil
}

// Called under the store lock. Clear only the delivery flag; execution times
// and history ordering remain untouched. A failed index save is safe to repeat.
func (e backupExecutor) clearNotificationPending(id string) error {
	index, err := e.index()
	if err != nil {
		return err
	}
	for n := range index.Runs {
		if index.Runs[n].ID == id && index.Runs[n].NotificationPending {
			index.Runs[n].NotificationPending = false
			return e.saveIndex(index)
		}
	}
	return nil
}
