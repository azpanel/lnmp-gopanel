package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

const oldPassword = "legacy-password-123"
const strongPassword = "N7!vQ2_zK9@pL5#sT8$xR4"

func legacyHash(t *testing.T) string {
	t.Helper()
	b, err := bcrypt.GenerateFromPassword([]byte(oldPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func testAccount(t *testing.T) (*Account, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	a, err := OpenAccount(dir, "admin", legacyHash(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a, dir
}
func enableTOTP(t *testing.T, a *Account) (Setup, []string) {
	t.Helper()
	setup, err := a.StartSetup("session", oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(setup.Secret, a.now())
	if err != nil {
		t.Fatal(err)
	}
	codes, err := a.ConfirmSetup("session", setup.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	return setup, codes
}
func TestPasswordPolicy(t *testing.T) {
	data, err := os.ReadFile("../../web/src/lib/password-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Password string `json:"password"`
		Valid    bool   `json:"valid"`
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i, item := range cases {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			if got := ValidatePassword(item.Password, "admin") == nil; got != item.Valid {
				t.Errorf("case %d accepted=%v want=%v", i, got, item.Valid)
			}
		})
	}
}
func TestPasswordPersistsWithoutBootstrapFallback(t *testing.T) {
	a, dir := testAccount(t)
	hash := legacyHash(t)
	if err := a.ChangePassword(oldPassword, strongPassword, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.StartLogin(oldPassword); !errors.Is(err, ErrProof) {
		t.Fatal("old password accepted", err)
	}
	a.Close()
	restored, err := OpenAccount(dir, "admin", hash)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if challenge, err := restored.StartLogin(strongPassword); err != nil || challenge != "" {
		t.Fatal("new password not restored", err)
	}
	if _, err := restored.StartLogin(oldPassword); !errors.Is(err, ErrProof) {
		t.Fatal("bootstrap password accepted", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "auth-state.json"))
	if bytes.Contains(data, []byte(strongPassword)) {
		t.Fatal("plaintext password persisted")
	}
}
func TestTOTPReplayAndRecoverySurviveRestart(t *testing.T) {
	a, dir := testAccount(t)
	clock := time.Unix(1800000000, 0)
	a.now = func() time.Time { return clock }
	setup, codes := enableTOTP(t, a)
	code, _ := totp.GenerateCode(setup.Secret, clock)
	login, _ := a.StartLogin(oldPassword)
	if err := a.FinishLogin(login, code, ""); !errors.Is(err, ErrChallenge) {
		t.Fatal("setup code reused", err)
	}
	clock = clock.Add(30 * time.Second)
	code, _ = totp.GenerateCode(setup.Secret, clock)
	if err := a.FinishLogin(login, code, ""); err != nil {
		t.Fatal(err)
	}
	if err := a.FinishLogin(login, code, ""); !errors.Is(err, ErrChallenge) {
		t.Fatal("challenge reused", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "auth-state.json"))
	if bytes.Contains(data, []byte(setup.Secret)) || bytes.Contains(data, []byte(codes[0])) {
		t.Fatal("TOTP or recovery code stored in plaintext")
	}
	a.Close()
	a2, err := OpenAccount(dir, "admin", legacyHash(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	a2.now = func() time.Time { return clock }
	login, _ = a2.StartLogin(oldPassword)
	if err = a2.FinishLogin(login, code, ""); !errors.Is(err, ErrChallenge) {
		t.Fatal("OTP replay accepted after restart", err)
	}
	if err = a2.FinishLogin(login, "", strings.ToUpper(codes[0])); err != nil {
		t.Fatal(err)
	}
	login, _ = a2.StartLogin(oldPassword)
	if err = a2.FinishLogin(login, "", codes[0]); !errors.Is(err, ErrChallenge) {
		t.Fatal("recovery code replay accepted", err)
	}
	if a2.Security().RecoveryRemaining != 9 {
		t.Fatal("recovery code was not consumed")
	}
	a2.Close()
	a3, err := OpenAccount(dir, "admin", legacyHash(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a3.Close()
	login, _ = a3.StartLogin(oldPassword)
	if err = a3.FinishLogin(login, "", codes[0]); !errors.Is(err, ErrChallenge) {
		t.Fatal("recovery replay accepted after restart", err)
	}
}
func TestRecoveryCodeConsumptionIsAtomic(t *testing.T) {
	a, _ := testAccount(t)
	_, codes := enableTOTP(t, a)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		login, err := a.StartLogin(oldPassword)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if a.FinishLogin(login, "", codes[0]) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || a.Security().RecoveryRemaining != 9 {
		t.Fatal("recovery code consumed concurrently more than once")
	}
}
func TestDraftAndChallengeExpiryAndAttemptLimit(t *testing.T) {
	a, _ := testAccount(t)
	clock := time.Unix(1800000000, 0)
	a.now = func() time.Time { return clock }
	draft, err := a.StartSetup("first", oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(draft.Secret, clock)
	if _, err = a.ConfirmSetup("another", draft.ID, code); !errors.Is(err, ErrConflict) {
		t.Fatal("draft not bound to session")
	}
	a.DiscardSetup("first")
	if _, err = a.ConfirmSetup("first", draft.ID, code); !errors.Is(err, ErrConflict) {
		t.Fatal("cancelled draft accepted")
	}
	draft, err = a.StartSetup("first", oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(PendingTTL)
	code, _ = totp.GenerateCode(draft.Secret, clock)
	if _, err = a.ConfirmSetup("first", draft.ID, code); !errors.Is(err, ErrConflict) {
		t.Fatal("expired draft accepted")
	}
	_, codes := enableTOTP(t, a)
	login, _ := a.StartLogin(oldPassword)
	for range 5 {
		if err = a.FinishLogin(login, "wrong", ""); !errors.Is(err, ErrChallenge) {
			t.Fatal(err)
		}
	}
	if err = a.FinishLogin(login, "", codes[0]); !errors.Is(err, ErrChallenge) {
		t.Fatal("exhausted challenge accepted")
	}
	login, _ = a.StartLogin(oldPassword)
	clock = clock.Add(PendingTTL)
	if err = a.FinishLogin(login, "", codes[0]); !errors.Is(err, ErrChallenge) {
		t.Fatal("expired challenge accepted")
	}
}
func TestTOTPWindowAndSensitiveChanges(t *testing.T) {
	a, _ := testAccount(t)
	clock := time.Unix(1800000000, 0)
	a.now = func() time.Time { return clock }
	setup, codes := enableTOTP(t, a)
	clock = clock.Add(120 * time.Second)
	login, _ := a.StartLogin(oldPassword)
	old, _ := totp.GenerateCode(setup.Secret, clock.Add(-60*time.Second))
	if err := a.FinishLogin(login, old, ""); !errors.Is(err, ErrChallenge) {
		t.Fatal("out of window OTP accepted")
	}
	previous, _ := totp.GenerateCode(setup.Secret, clock.Add(-30*time.Second))
	if err := a.FinishLogin(login, previous, ""); err != nil {
		t.Fatal("previous window rejected", err)
	}
	login, _ = a.StartLogin(oldPassword)
	future, _ := totp.GenerateCode(setup.Secret, clock.Add(30*time.Second))
	if err := a.FinishLogin(login, future, ""); err != nil {
		t.Fatal("next window rejected", err)
	}
	if err := a.ChangePassword(oldPassword, strongPassword, "", ""); !errors.Is(err, ErrProof) {
		t.Fatal("missing second factor accepted")
	}
	if err := a.ChangePassword(oldPassword, strongPassword, "", codes[0]); err != nil {
		t.Fatal(err)
	}
	if a.Security().RecoveryRemaining != 9 {
		t.Fatal("password change did not consume recovery code")
	}
	if err := a.DisableTOTP(strongPassword, "", codes[1]); err != nil {
		t.Fatal(err)
	}
	if a.Security().TOTPEnabled || a.Security().RecoveryRemaining != 0 {
		t.Fatal("disable did not clear TOTP state")
	}
	if challenge, err := a.StartLogin(strongPassword); challenge != "" || err != nil {
		t.Fatal("password login after disable failed")
	}
}
func TestRegenerateInvalidatesCodesAndPendingLogins(t *testing.T) {
	a, _ := testAccount(t)
	_, codes := enableTOTP(t, a)
	oldLogin, _ := a.StartLogin(oldPassword)
	fresh, err := a.RegenerateCodes(oldPassword, "", codes[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = a.FinishLogin(oldLogin, "", fresh[0]); !errors.Is(err, ErrChallenge) {
		t.Fatal("old login challenge survived credential change")
	}
	login, _ := a.StartLogin(oldPassword)
	if err = a.FinishLogin(login, "", codes[1]); !errors.Is(err, ErrChallenge) {
		t.Fatal("old recovery code survived regeneration")
	}
	if err = a.FinishLogin(login, "", fresh[0]); err != nil {
		t.Fatal(err)
	}
}
func TestFailedPersistenceNeverGrantsAuthentication(t *testing.T) {
	a, _ := testAccount(t)
	_, codes := enableTOTP(t, a)
	login, _ := a.StartLogin(oldPassword)
	a.persist = func(accountState) error { return errors.New("disk unavailable") }
	if err := a.FinishLogin(login, "", codes[0]); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
	if a.Security().RecoveryRemaining != 10 {
		t.Fatal("failed save changed in-memory state")
	}
	if _, err := a.StartLogin(oldPassword); !errors.Is(err, ErrStorage) {
		t.Fatal("account did not fail closed")
	}
}
func TestPrivateStorageAndExclusiveLock(t *testing.T) {
	a, dir := testAccount(t)
	if _, err := OpenAccount(dir, "admin", legacyHash(t)); err == nil {
		t.Fatal("concurrent account store allowed")
	}
	a.Close()
	if err := os.Remove(filepath.Join(dir, "auth.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAccount(dir, "admin", legacyHash(t)); err == nil {
		t.Fatal("missing key silently reset account")
	}
	for _, name := range []string{"auth-state.json", "auth.key", "auth.lock"} {
		t.Run(name, func(t *testing.T) {
			b, d := testAccount(t)
			b.Close()
			path := filepath.Join(d, name)
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenAccount(d, "admin", legacyHash(t)); err == nil {
				t.Fatal("public auth file accepted")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		b, d := testAccount(t)
		b.Close()
		path := filepath.Join(d, "auth-state.json")
		if err := os.Rename(path, path+".real"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path+".real", path); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenAccount(d, "admin", legacyHash(t)); err == nil {
			t.Fatal("auth symlink accepted")
		}
	})
	t.Run("missing state", func(t *testing.T) {
		b, d := testAccount(t)
		b.Close()
		os.Remove(filepath.Join(d, "auth-state.json"))
		if _, err := OpenAccount(d, "admin", legacyHash(t)); err == nil {
			t.Fatal("missing state fell back to bootstrap")
		}
	})
	t.Run("corrupt state", func(t *testing.T) {
		b, d := testAccount(t)
		b.Close()
		os.WriteFile(filepath.Join(d, "auth-state.json"), []byte("invalid"), 0600)
		if _, err := OpenAccount(d, "admin", legacyHash(t)); err == nil {
			t.Fatal("corrupt state accepted")
		}
	})
}
