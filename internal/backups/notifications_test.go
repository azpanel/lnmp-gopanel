package backups

import "testing"

func TestNotificationValidation(t *testing.T) {
	token := "123:abcdefghijklmnopqrstuv_0123456789"
	for _, uid := range []string{"1", "123456789", "9223372036854775807"} {
		if err := ValidateNotification(&Notification{Enabled: true, Channel: "telegram", UID: uid}, "replace", token); err != nil {
			t.Fatal(uid, err)
		}
	}
	for _, uid := range []string{"", "0", "-123", "@name", "12 34", "01", "9223372036854775808", "1.2", "１２３"} {
		if err := ValidateNotification(&Notification{Enabled: true, Channel: "telegram", UID: uid}, "replace", token); err == nil {
			t.Fatal("invalid UID accepted", uid)
		}
	}
	n := &Notification{Channel: "telegram"}
	if err := ValidateNotification(n, "keep", ""); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "secret", "123:abcdefghijklmnopqrst\n", "123:../../abcdefghijklmnopqrst", "123:abcdefghijklmnopqrst?query"} {
		if err := ValidateNotification(n, "replace", token); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	if ValidateNotification(nil, "", "") != nil || ValidateNotification(nil, "replace", token) == nil {
		t.Fatal("legacy omission handling")
	}
	if ValidateNotification(n, "keep", token) == nil || ValidateNotification(n, "unknown", "") == nil {
		t.Fatal("token action ignored")
	}
	n.Enabled = true
	n.UID = "123"
	if ValidateNotification(n, "clear", "") == nil {
		t.Fatal("cleared active token")
	}
	n.Channel = "custom"
	if ValidateNotification(n, "keep", "") == nil {
		t.Fatal("custom endpoint/channel accepted")
	}
}
