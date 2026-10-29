package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/trustelem/zxcvbn"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrProof     = errors.New("当前密码或验证码错误")
	ErrChallenge = errors.New("验证码错误或登录验证已过期")
	ErrInput     = errors.New("认证参数无效")
	ErrConflict  = errors.New("认证状态已变化，请重新打开账户安全设置")
	ErrStorage   = errors.New("认证状态保存失败，请检查状态目录后重试")
)

const PendingTTL = 5 * time.Minute

type accountState struct {
	Version        int      `json:"version"`
	Username       string   `json:"username"`
	PasswordHash   string   `json:"password_hash"`
	SecretCipher   string   `json:"totp_secret,omitempty"`
	RecoveryHashes []string `json:"recovery_hashes,omitempty"`
	LastStep       int64    `json:"last_step"`
}

type proof struct {
	password string
	code     string
	recovery string
}

type pendingLogin struct {
	expires  time.Time
	attempts int
}
type pendingSetup struct {
	ID, Secret string
	Expires    time.Time
}

type Setup struct {
	ID      string    `json:"setup_id"`
	Secret  string    `json:"secret"`
	URL     string    `json:"otpauth_url"`
	Expires time.Time `json:"expires_at"`
}
type Security struct {
	TOTPEnabled       bool `json:"totp_enabled"`
	RecoveryRemaining int  `json:"recovery_remaining"`
	MinCharacters     int  `json:"min_characters"`
	MaxBytes          int  `json:"max_bytes"`
	MinScore          int  `json:"min_score"`
}

// Account serializes credential checks and durable one-time proof consumption.
// The HTTP layer also serializes session creation/revocation with these operations.
type Account struct {
	mu            sync.Mutex
	state         accountState
	secret        string
	aead          cipher.AEAD
	dir           string
	lock          *os.File
	now           func() time.Time
	logins        map[string]pendingLogin
	setups        map[string]pendingSetup
	persist       func(accountState) error
	storageFailed bool
}

// NewAccount is an in-memory account for embedded callers and isolated tests.
// The production entrypoint always installs OpenAccount before serving requests.
func NewAccount(username, hash string) *Account {
	return &Account{state: accountState{Version: 1, Username: username, PasswordHash: hash, LastStep: -1}, now: time.Now, logins: map[string]pendingLogin{}, setups: map[string]pendingSetup{}}
}

func privateFile(info os.FileInfo, directory bool) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	perm := os.FileMode(0600)
	if directory {
		perm = 0700
	}
	if !ok || int(st.Uid) != os.Geteuid() || info.Mode().Perm() != perm || info.IsDir() != directory || (!directory && (!info.Mode().IsRegular() || st.Nlink != 1)) {
		return errors.New("认证目录及文件必须由当前面板账号所有，权限为 0700 / 0600，且不能是链接")
	}
	return nil
}
func readPrivate(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if err = privateFile(info, false); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return nil, err
	}
	if len(data) > 65536 {
		return nil, errors.New("认证文件过大")
	}
	return data, nil
}
func OpenAccount(dir, username, bootstrapHash string) (*Account, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, errors.New("认证状态目录必须是规范的绝对路径")
	}
	for path := dir; path != "/"; path = filepath.Dir(path) {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("认证目录不能包含符号链接")
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if err = privateFile(info, true); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "auth.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			lock.Close()
		}
	}()
	info, err = lock.Stat()
	if err != nil {
		return nil, err
	}
	if err = privateFile(info, false); err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.New("认证状态正在使用，请先停止面板服务")
	}
	a := NewAccount(username, bootstrapHash)
	a.dir = dir
	a.lock = lock
	statePath, keyPath := filepath.Join(dir, "auth-state.json"), filepath.Join(dir, "auth.key")
	data, stateErr := readPrivate(statePath)
	key, keyErr := readPrivate(keyPath)
	fresh := os.IsNotExist(stateErr) && os.IsNotExist(keyErr)
	if fresh {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
		if e != nil {
			return nil, e
		}
		_, err = f.Write(key)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
	} else if stateErr != nil || keyErr != nil {
		return nil, errors.New("认证状态或密钥缺失、权限错误，请恢复匹配的备份；不会回退使用旧密码")
	}
	if len(key) != 32 {
		return nil, errors.New("认证密钥无效")
	}
	block, err := aes.NewCipher(key)
	clear(key)
	if err != nil {
		return nil, err
	}
	a.aead, err = cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	a.persist = a.writeState
	if !fresh {
		if json.Unmarshal(data, &a.state) != nil || a.state.Version != 1 || a.state.Username != username || a.state.LastStep < -1 || len(a.state.RecoveryHashes) > 10 {
			return nil, errors.New("认证状态无效或用户名与配置不一致")
		}
		seen := map[string]bool{}
		for _, hash := range a.state.RecoveryHashes {
			b, e := hex.DecodeString(hash)
			if e != nil || len(b) != 32 || seen[hash] {
				return nil, errors.New("恢复码状态无效")
			}
			seen[hash] = true
		}
		if a.state.SecretCipher != "" {
			a.secret, err = a.decrypt(a.state.SecretCipher)
			if err != nil {
				return nil, errors.New("无法解密 TOTP，请恢复匹配的认证状态和密钥")
			}
			decoded, decodeErr := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(a.secret)
			if decodeErr != nil || len(decoded) != 20 {
				return nil, errors.New("TOTP 密钥无效")
			}
			clear(decoded)
			if _, err = totp.GenerateCode(a.secret, time.Now()); err != nil {
				return nil, errors.New("TOTP 密钥无效")
			}
		} else if len(a.state.RecoveryHashes) != 0 || a.state.LastStep != -1 {
			return nil, errors.New("TOTP 状态无效")
		}
	}
	if _, err = bcrypt.Cost([]byte(a.state.PasswordHash)); err != nil {
		return nil, errors.New("认证密码哈希无效")
	}
	if fresh {
		if err = a.writeState(a.state); err != nil {
			return nil, err
		}
	}
	ok = true
	return a, nil
}
func (a *Account) Close() error {
	if a.lock != nil {
		return a.lock.Close()
	}
	return nil
}
func (a *Account) writeState(next accountState) error {
	info, err := os.Lstat(a.dir)
	if err != nil {
		return err
	}
	if err = privateFile(info, true); err != nil {
		return err
	}
	path := filepath.Join(a.dir, "auth-state.json")
	if info, err = os.Lstat(path); err == nil {
		if err = privateFile(info, false); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(a.dir, ".auth-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(a.dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (a *Account) commit(next accountState) error {
	if a.persist != nil {
		if err := a.persist(next); err != nil {
			slog.Error("认证状态写入失败，请检查存储并重启面板", "error", err)
			a.storageFailed = true
			return ErrStorage
		}
	}
	a.state = next
	return nil
}
func (a *Account) encrypt(secret string) (string, error) {
	if a.aead == nil {
		return secret, nil
	} // In-memory tests never persist this value.
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(a.aead.Seal(nonce, nonce, []byte(secret), []byte(a.state.Username))), nil
}
func (a *Account) decrypt(value string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(b) < a.aead.NonceSize() {
		return "", ErrInput
	}
	plain, err := a.aead.Open(nil, b[:a.aead.NonceSize()], b[a.aead.NonceSize():], []byte(a.state.Username))
	if err != nil {
		return "", err
	}
	defer clear(plain)
	return string(plain), nil
}
func (a *Account) Security() Security {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Security{a.secret != "", len(a.state.RecoveryHashes), 15, 72, 3}
}
func ValidatePassword(password, username string) error {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 15 || len(password) > 72 {
		return fmt.Errorf("%w：新密码至少 15 个字符，最多 72 个 UTF-8 字节", ErrInput)
	}
	for _, r := range password {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w：密码不能包含控制字符", ErrInput)
		}
	}
	if zxcvbn.PasswordStrength(password, []string{username, "lnmp", "gopanel", "lnmp-gopanel"}).Score < 3 {
		return fmt.Errorf("%w：新密码容易被猜到，请增加不相关的词语或使用随机密码", ErrInput)
	}
	return nil
}
func (a *Account) prune() {
	for id, p := range a.logins {
		if !a.now().Before(p.expires) {
			delete(a.logins, id)
		}
	}
	for id, p := range a.setups {
		if !a.now().Before(p.Expires) {
			delete(a.setups, id)
		}
	}
}
func (a *Account) invalidate() { clear(a.logins); clear(a.setups) }
func (a *Account) StartLogin(password string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storageFailed {
		return "", ErrStorage
	}
	a.prune()
	if len(password) > 72 || bcrypt.CompareHashAndPassword([]byte(a.state.PasswordHash), []byte(password)) != nil {
		return "", ErrProof
	}
	if a.secret == "" {
		return "", nil
	}
	if len(a.logins) >= 128 {
		return "", ErrConflict
	}
	id := token()
	a.logins[id] = pendingLogin{expires: a.now().Add(PendingTTL)}
	return id, nil
}
func recoveryHash(value string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))))
	return hex.EncodeToString(h[:])
}
func (a *Account) factor(next *accountState, code, recovery string) error {
	if a.secret == "" {
		if code != "" || recovery != "" {
			return ErrProof
		}
		return nil
	}
	if (code == "") == (recovery == "") {
		return ErrProof
	}
	if recovery != "" {
		hash := recoveryHash(recovery)
		for i, saved := range next.RecoveryHashes {
			if Equal(hash, saved) {
				next.RecoveryHashes = append(append([]string{}, next.RecoveryHashes[:i]...), next.RecoveryHashes[i+1:]...)
				return nil
			}
		}
		return ErrProof
	}
	step, ok := a.matchStep(a.secret, code, next.LastStep)
	if !ok {
		return ErrProof
	}
	next.LastStep = step
	return nil
}
func (a *Account) matchStep(secret, code string, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	current := a.now().Unix() / 30
	for _, delta := range []int64{0, -1, 1} {
		step := current + delta
		if step <= last || step < 0 {
			continue
		}
		expected, err := totp.GenerateCodeCustom(secret, time.Unix(step*30, 0), totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && Equal(code, expected) {
			return step, true
		}
	}
	return 0, false
}
func (a *Account) FinishLogin(id, code, recovery string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storageFailed {
		return ErrStorage
	}
	a.prune()
	p, ok := a.logins[id]
	if !ok {
		return ErrChallenge
	}
	p.attempts++
	a.logins[id] = p
	next := a.state
	if err := a.factor(&next, code, recovery); err != nil {
		if p.attempts >= 5 {
			delete(a.logins, id)
		}
		return ErrChallenge
	}
	if err := a.commit(next); err != nil {
		return err
	}
	delete(a.logins, id)
	return nil
}
func (a *Account) verify(next *accountState, p proof) error {
	if len(p.password) > 72 || bcrypt.CompareHashAndPassword([]byte(a.state.PasswordHash), []byte(p.password)) != nil {
		return ErrProof
	}
	return a.factor(next, p.code, p.recovery)
}
func (a *Account) ChangePassword(current, password, code, recovery string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storageFailed {
		return ErrStorage
	}
	next := a.state
	if err := a.verify(&next, proof{current, code, recovery}); err != nil {
		return err
	}
	if err := ValidatePassword(password, a.state.Username); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(a.state.PasswordHash), []byte(password)) == nil {
		return fmt.Errorf("%w：新密码不能与当前密码相同", ErrInput)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return ErrInput
	}
	next.PasswordHash = string(hash)
	if err = a.commit(next); err != nil {
		return err
	}
	a.invalidate()
	return nil
}
func (a *Account) StartSetup(session, password string) (Setup, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storageFailed {
		return Setup{}, ErrStorage
	}
	a.prune()
	if a.secret != "" {
		return Setup{}, ErrConflict
	}
	if len(password) > 72 || bcrypt.CompareHashAndPassword([]byte(a.state.PasswordHash), []byte(password)) != nil {
		return Setup{}, ErrProof
	}
	if len(a.setups) >= 128 {
		return Setup{}, ErrConflict
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "LNMP GoPanel", AccountName: a.state.Username, Period: 30, SecretSize: 20, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return Setup{}, err
	}
	p := pendingSetup{token(), key.Secret(), a.now().Add(PendingTTL)}
	a.setups[session] = p
	return Setup{p.ID, p.Secret, key.URL(), p.Expires}, nil
}
func recoveryCodes() ([]string, []string, error) {
	codes, hashes := make([]string, 10), make([]string, 10)
	for i := range codes {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		h := hex.EncodeToString(b)
		codes[i] = h[:8] + "-" + h[8:16] + "-" + h[16:24] + "-" + h[24:]
		hashes[i] = recoveryHash(codes[i])
	}
	return codes, hashes, nil
}
func (a *Account) ConfirmSetup(session, id, code string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storageFailed {
		return nil, ErrStorage
	}
	a.prune()
	p, ok := a.setups[session]
	if !ok || !Equal(p.ID, id) || a.secret != "" {
		return nil, ErrConflict
	}
	step, ok := a.matchStep(p.Secret, code, -1)
	if !ok {
		return nil, ErrProof
	}
	codes, hashes, err := recoveryCodes()
	if err != nil {
		return nil, err
	}
	next := a.state
	next.SecretCipher, err = a.encrypt(p.Secret)
	if err != nil {
		return nil, err
	}
	next.LastStep = step
	next.RecoveryHashes = hashes
	if err = a.commit(next); err != nil {
		return nil, err
	}
	a.secret = p.Secret
	a.invalidate()
	return codes, nil
}
func (a *Account) DisableTOTP(current, code, recovery string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storageFailed {
		return ErrStorage
	}
	next := a.state
	if err := a.verify(&next, proof{current, code, recovery}); err != nil {
		return err
	}
	return a.disable(next)
}
func (a *Account) disable(next accountState) error {
	next.SecretCipher = ""
	next.RecoveryHashes = nil
	next.LastStep = -1
	if err := a.commit(next); err != nil {
		return err
	}
	a.secret = ""
	a.invalidate()
	return nil
}

// ResetPasswordOffline holds the daemon's exclusive state lock. The password
// change and second-factor removal are committed together; bootstrap config is untouched.
func (a *Account) ResetPasswordOffline(password string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storageFailed {
		return ErrStorage
	}
	if err := ValidatePassword(password, a.state.Username); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	next := a.state
	next.PasswordHash = string(hash)
	return a.disable(next)
}

// DisableTOTPOffline requires the same exclusive file lock as the running daemon.
func (a *Account) DisableTOTPOffline() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storageFailed {
		return ErrStorage
	}
	return a.disable(a.state)
}
func (a *Account) RegenerateCodes(current, code, recovery string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storageFailed {
		return nil, ErrStorage
	}
	if a.secret == "" {
		return nil, ErrConflict
	}
	next := a.state
	if err := a.verify(&next, proof{current, code, recovery}); err != nil {
		return nil, err
	}
	codes, hashes, err := recoveryCodes()
	if err != nil {
		return nil, err
	}
	next.RecoveryHashes = hashes
	if err = a.commit(next); err != nil {
		return nil, err
	}
	a.invalidate()
	return codes, nil
}
func (a *Account) DiscardSetup(session string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.setups, session)
}
