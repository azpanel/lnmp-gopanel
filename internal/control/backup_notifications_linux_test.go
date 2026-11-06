//go:build linux

package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

const notificationTestToken = "123456:" + "abcdefghijklmnopqrstuv_xyz-0123456789"

func TestBackupNotificationTextBoundsUnicodeErrors(t *testing.T) {
	category := backups.CategoryResult{Category: "files", Status: "failed"}
	for range 20 {
		category.Deliveries = append(category.Deliveries, backups.Delivery{
			Status: "failed", Error: strings.Repeat("📁", 200),
		})
	}
	message := backupNotificationText(backups.Run{
		PolicyName: "通知测试", Status: "failed", Categories: []backups.CategoryResult{category},
	})
	if len(utf16.Encode([]rune(message))) > 4096 || !strings.HasSuffix(message, "更多详情请查看执行历史。") {
		t.Fatal("long Unicode notification exceeds Telegram limit or lacks truncation notice")
	}
}

func saveNotificationPolicy(t *testing.T, e backupExecutor) backups.Policy {
	t.Helper()
	r := sampleBackupPolicy()
	r.Notification = &backups.Notification{Enabled: true, Channel: "telegram", UID: "123456789"}
	r.NotificationTokenAction = "replace"
	r.NotificationToken = notificationTestToken
	p, err := e.store.save(r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBackupNotificationSecretsCompatibilityAndCopy(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	p := saveNotificationPolicy(t, e)
	records, err := e.store.load()
	if err != nil {
		t.Fatal(err)
	}
	original := records[0].NotificationCiphertext
	raw, _ := os.ReadFile(filepath.Join(e.store.state, "policies.json"))
	public, _ := json.Marshal(p)
	if strings.Contains(string(raw), notificationTestToken) || strings.Contains(string(public), notificationTestToken) || strings.Contains(string(public), "ciphertext") || !p.Notification.HasToken {
		t.Fatal("notification secret leaked or flag missing")
	}
	token, err := e.store.decryptNotification(p.ID, original)
	if err != nil || string(token) != notificationTestToken {
		t.Fatal(err)
	}
	clear(token)
	if _, err := e.store.decryptNotification(strings.Repeat("f", 32), original); err == nil {
		t.Fatal("ciphertext not bound to policy")
	}
	legacy := backups.SavePolicyRequest{Policy: p, PasswordAction: "keep"}
	legacy.Notification = nil
	p, err = e.store.save(legacy)
	if err != nil || p.Notification == nil || !p.Notification.Enabled || !p.Notification.HasToken {
		t.Fatal("legacy save lost configuration", err)
	}
	records, _ = e.store.load()
	if records[0].NotificationCiphertext != original {
		t.Fatal("legacy save changed token")
	}
	copyRequest := backups.SavePolicyRequest{Policy: p, PasswordAction: "keep", CopyFrom: &backups.PolicyIdentity{ID: p.ID, Revision: p.Revision}}
	copyRequest.ID = ""
	copyRequest.Revision = 0
	copied, err := e.store.save(copyRequest)
	if err != nil || copied.Notification.Enabled || !copied.Notification.HasToken {
		t.Fatal("copy defaults", err)
	}
	records, _ = e.store.load()
	for _, record := range records {
		if record.ID == copied.ID {
			if record.NotificationCiphertext == original {
				t.Fatal("copy reused ciphertext")
			}
			value, err := e.store.decryptNotification(copied.ID, record.NotificationCiphertext)
			if err != nil || string(value) != notificationTestToken {
				t.Fatal("copied token", err)
			}
			clear(value)
		}
	}
	replacement := backups.SavePolicyRequest{Policy: p, PasswordAction: "keep", NotificationTokenAction: "replace", NotificationToken: "987654:abcdefghijklmnopqrstuv_0123456789"}
	p, err = e.store.save(replacement)
	if err != nil {
		t.Fatal(err)
	}
	records, _ = e.store.load()
	for _, record := range records {
		if record.ID == p.ID {
			token, err := e.store.decryptNotification(p.ID, record.NotificationCiphertext)
			if err != nil || string(token) != replacement.NotificationToken {
				t.Fatal("replacement token", err)
			}
			clear(token)
		}
	}
	clearRequest := backups.SavePolicyRequest{Policy: p, PasswordAction: "keep", NotificationTokenAction: "clear"}
	if _, err := e.store.save(clearRequest); !errors.Is(err, backups.ErrInvalid) {
		t.Fatal("enabled token cleared", err)
	}
	clearRequest.Notification = &backups.Notification{Channel: "telegram", UID: p.Notification.UID}
	p, err = e.store.save(clearRequest)
	if err != nil || p.Notification.HasToken {
		t.Fatal("token not cleared", err)
	}
	r := sampleBackupPolicy()
	r.Notification = &backups.Notification{Enabled: true, Channel: "telegram", UID: "123", HasToken: true}
	if _, err := e.store.save(r); !errors.Is(err, backups.ErrInvalid) {
		t.Fatal("client forged has_token", err)
	}
}

func TestBackupNotificationSnapshotOnceAndIndependentResult(t *testing.T) {
	for _, status := range []string{"succeeded", "partial", "failed", "skipped", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			e, _, _ := backupExecutionFixture(t)
			p := saveNotificationPolicy(t, e)
			run, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
			if err != nil {
				t.Fatal(err)
			}
			edit := backups.SavePolicyRequest{Policy: p, PasswordAction: "keep"}
			edit.Notification = &backups.Notification{Enabled: true, Channel: "telegram", UID: "987654321"}
			if _, err := e.store.save(edit); err != nil {
				t.Fatal(err)
			}
			j, err := e.loadRun(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			j.Status = status
			finish := e.now().Add(time.Minute)
			j.FinishedAt = &finish
			if err := e.update(&j); err != nil {
				t.Fatal(err)
			}
			eligible := status != "skipped" && status != "cancelled"
			if (j.Notification != nil) != eligible || j.Snapshot != nil {
				t.Fatal("terminal notification", j.Notification)
			}
			before, _ := os.ReadFile(filepath.Join(e.store.state, "run-index.json"))
			updated := j.UpdatedAt
			var calls atomic.Int32
			entered := make(chan struct{})
			release := make(chan struct{})
			e.notify = func(ctx context.Context, token, uid, message string) error {
				calls.Add(1)
				if token != notificationTestToken || uid != "123456789" || !strings.Contains(message, p.Name) || !strings.Contains(message, "耗时：1m0s") {
					t.Error("incorrect notification snapshot", uid, message)
				}
				// The policy lock must be free during network calls.
				if unlock, err := e.store.lock(); err != nil {
					t.Error(err)
				} else {
					unlock()
				}
				close(entered)
				<-release
				return errors.New("通知发送失败：测试故障")
			}
			if eligible {
				done := make(chan error, 1)
				go func() { done <- e.deliverNotification(context.Background(), j.ID) }()
				<-entered
				if err := e.deliverNotification(context.Background(), j.ID); err != nil {
					t.Fatal(err)
				}
				close(release)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			if err := e.deliverNotification(context.Background(), j.ID); err != nil {
				t.Fatal(err)
			}
			disk, _ := e.loadRun(j.ID)
			if disk.Status != status || !disk.UpdatedAt.Equal(updated) || !disk.FinishedAt.Equal(finish) || disk.NotificationDelivery != nil {
				t.Fatal("notification altered execution result")
			}
			after, _ := os.ReadFile(filepath.Join(e.store.state, "run-index.json"))
			var oldIndex, newIndex backupRunIndex
			if json.Unmarshal(before, &oldIndex) != nil || json.Unmarshal(after, &newIndex) != nil {
				t.Fatal("index invalid")
			}
			for n := range oldIndex.Runs {
				oldIndex.Runs[n].NotificationPending = false
			}
			if !reflect.DeepEqual(oldIndex, newIndex) {
				t.Fatal("notification changed execution history")
			}
			if eligible && (calls.Load() != 1 || disk.Notification.Status != "failed") {
				t.Fatal("notification retried or unrecorded")
			}
			if !eligible && calls.Load() != 0 {
				t.Fatal("skipped/cancelled notification sent")
			}
			public, _ := json.Marshal(disk.Run)
			if strings.Contains(string(public), "ciphertext") || strings.Contains(string(public), notificationTestToken) {
				t.Fatal("run leaked credentials")
			}
		})
	}
}

func TestBackupNotificationRecoveryAndMissingKey(t *testing.T) {
	e, now, _ := backupExecutionFixture(t)
	p := saveNotificationPolicy(t, e)
	run, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	finishExecution(t, e, run.ID)
	// The terminal pending delivery must still protect its encryption key even
	// after the live policy clears its token and the execution snapshot is gone.
	edit := backups.SavePolicyRequest{Policy: p, PasswordAction: "keep", NotificationTokenAction: "clear"}
	edit.Notification = &backups.Notification{Channel: "telegram"}
	if _, err := e.store.save(edit); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(e.store.state, "policies.key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.aead(); err == nil {
		t.Fatal("invented replacement key for pending notification")
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	e.notify = func(context.Context, string, string, string) error { calls++; return nil }
	if err := e.dispatch(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	disk, _ := e.loadRun(run.ID)
	if calls != 1 || disk.Notification.Status != "sent" || disk.NotificationDelivery != nil {
		t.Fatal("pending notification not recovered")
	}
	// A crashed sender must never cause a repeat, even if its response was lost.
	disk.Notification.Status = "sending"
	disk.Notification.UpdatedAt = *now
	if err := writeBackupJSON(e.runPath(disk.ID), disk); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	if err := e.deliverNotification(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	disk, _ = e.loadRun(run.ID)
	if calls != 1 || disk.Notification.Status != "failed" || disk.Notification.Error != "发送中断，结果未知" {
		t.Fatal("interrupted send retried")
	}
}

func TestBackupNotificationTestUsesDraftOrSavedTokenAndRevision(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	p := saveNotificationPolicy(t, e)
	before, _ := os.ReadFile(filepath.Join(e.store.state, "policies.json"))
	calls := 0
	send := func(ctx context.Context, token, uid, message string) error {
		calls++
		if token != notificationTestToken || uid != "555" || !strings.Contains(message, "通知测试") {
			t.Error("incorrect test payload")
		}
		return nil
	}
	r := backups.NotificationTestRequest{PolicyIdentity: backups.PolicyIdentity{ID: p.ID, Revision: p.Revision}, Notification: backups.Notification{Channel: "telegram", UID: "555"}, TokenAction: "keep"}
	if err := e.store.testNotification(context.Background(), r, send); err != nil {
		t.Fatal(err)
	}
	r.Revision++
	if err := e.store.testNotification(context.Background(), r, send); !errors.Is(err, backups.ErrConflict) {
		t.Fatal("stale test accepted", err)
	}
	r.PolicyIdentity = backups.PolicyIdentity{}
	r.TokenAction = "replace"
	r.Token = notificationTestToken
	if err := e.store.testNotification(context.Background(), r, send); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(e.store.state, "policies.json"))
	if calls != 2 || string(before) != string(after) {
		t.Fatal("test persisted draft or sent on conflict")
	}
}

type notificationTransport func(*http.Request) (*http.Response, error)

func (f notificationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTelegramTransportSafety(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       string
	}{
		{"success", `{"ok":true}`, 200, ""}, {"token", `{"ok":false,"error_code":401,"description":"secret"}`, 401, "Token 无效"},
		{"uid", `{"ok":false,"error_code":400}`, 400, "/start"}, {"blocked", `{"ok":false,"error_code":403}`, 403, "屏蔽"},
		{"limit", `{"ok":false,"error_code":429}`, 429, "频率"}, {"server", `{"ok":false}`, 503, "暂不可用"},
		{"malformed", `broken`, 200, "响应异常"}, {"large", strings.Repeat("x", 65537), 200, "响应异常"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: notificationTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Scheme != "https" || r.URL.Host != "api.telegram.org" || r.Method != "POST" {
					t.Fatal("unexpected destination")
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("unbounded request")
				}
				var payload map[string]any
				if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["chat_id"] != "123" || payload["text"] != "测试 <&>" {
					t.Fatal("invalid payload")
				}
				if _, exists := payload["parse_mode"]; exists {
					t.Fatal("unescaped message formatting")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
			})}
			err := sendTelegramClient(context.Background(), client, notificationTestToken, "123", "测试 <&>")
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
	client := &http.Client{Transport: notificationTransport(func(r *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Post", URL: r.URL.String(), Err: errors.New(notificationTestToken)}
	})}
	if err := sendTelegramClient(context.Background(), client, notificationTestToken, "123", "test"); err == nil || strings.Contains(err.Error(), notificationTestToken) || strings.Contains(err.Error(), "https:") {
		t.Fatal("transport leaked URL", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.Transport = notificationTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	if err := sendTelegramClient(ctx, client, notificationTestToken, "123", "test"); err == nil {
		t.Fatal("cancel ignored")
	}
	// Exercise the HTTP client's redirect handling without contacting Telegram.
	var redirected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1); fmt.Fprint(w, `{"ok":true}`) }))
	defer server.Close()
	client.Transport = notificationTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{server.URL}}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})
	if err := sendTelegramClient(context.Background(), client, notificationTestToken, "123", "test"); err == nil || redirected.Load() != 0 {
		t.Fatal("followed redirect")
	}
}

func TestBackupNotificationWorkerFailureAndConcurrentDispatch(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	p := saveNotificationPolicy(t, e)
	run, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	e.notify = func(context.Context, string, string, string) error { calls.Add(1); return nil }
	e.execute = func(context.Context, *storedBackupRun) error { return errors.New("fixture execution failure") }
	if err := e.worker(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			if err := e.dispatch(context.Background(), false); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	disk, _ := e.loadRun(run.ID)
	if calls.Load() != 1 || disk.Status != "failed" || disk.Notification.Status != "sent" {
		t.Fatal("worker notification", calls.Load(), disk.Notification)
	}
}

func TestBackupNotificationLaunchFailureAndInterruptedWorker(t *testing.T) {
	for _, mode := range []string{"launch", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			e, now, _ := backupExecutionFixture(t)
			p := saveNotificationPolicy(t, e)
			calls := 0
			e.notify = func(context.Context, string, string, string) error { calls++; return nil }
			failRunner := func(ctx context.Context, program string, args ...string) (string, error) {
				if program == "/usr/bin/systemd-run" {
					return "", errors.New("fixture launch error")
				}
				return "ActiveState=inactive\nLoadState=not-found", nil
			}
			if mode == "launch" {
				e.run = failRunner
			}
			run, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "interrupted" {
				*now = now.Add(2 * time.Minute)
				e.run = failRunner
			}
			if err := e.dispatch(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			j, err := e.loadRun(run.ID)
			if err != nil || j.Status != "failed" || j.Notification == nil || j.Notification.Status != "sent" || calls != 1 {
				t.Fatal("failure notification missing", mode, err, calls, j.Notification)
			}
		})
	}
}

func TestBackupPendingNotificationSurvivesHistoryPruning(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	p := saveNotificationPolicy(t, e)
	run, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	finishExecution(t, e, run.ID)
	release, err := e.store.lock()
	if err != nil {
		t.Fatal(err)
	}
	index, err := e.index()
	if err != nil {
		release()
		t.Fatal(err)
	}
	for n := 0; n < 101; n++ {
		id, err := newBackupRunID()
		if err != nil {
			release()
			t.Fatal(err)
		}
		j := storedBackupRun{Run: backups.Run{ID: id, PolicyID: p.ID, Status: "skipped", CreatedAt: e.now()}}
		if err := e.saveRun(&j); err != nil {
			release()
			t.Fatal(err)
		}
		index.Runs = append(index.Runs, backupRunRef{ID: id, PolicyID: p.ID, Status: j.Status, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt})
	}
	err = e.saveIndex(index)
	release()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	e.notify = func(context.Context, string, string, string) error { calls++; return nil }
	if err := e.dispatch(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	j, err := e.loadRun(run.ID)
	if err != nil || calls != 1 || j.Notification.Status != "sent" {
		t.Fatal("pending notification was pruned", err, calls)
	}
}
