//go:build linux

package control

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

type storedBackupPolicy struct {
	backups.Policy
	Ciphertext string `json:"password_ciphertext,omitempty"`
}
type backupPolicyStore struct{ state string }

func (s backupPolicyStore) lock() (func(), error) { return (backupInstaller{state: s.state}).lock() }
func readPrivateBackupFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || st.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("政策文件须为 root 私有普通文件")
	}
	return readTrustedFile(path, limit)
}
func (s backupPolicyStore) load() ([]storedBackupPolicy, error) {
	path := filepath.Join(s.state, "policies.json")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return []storedBackupPolicy{}, nil
	}
	data, err := readPrivateBackupFile(path, 8*1024*1024)
	if err != nil {
		return nil, errors.New("政策文件不可读取或权限不安全")
	}
	var policies []storedBackupPolicy
	if json.Unmarshal(data, &policies) != nil || policies == nil {
		return nil, errors.New("政策文件损坏，请恢复备份")
	}
	return policies, nil
}
func (s backupPolicyStore) persist(policies []storedBackupPolicy) error {
	data, err := json.Marshal(policies)
	if err != nil {
		return err
	}
	if len(data) > 8*1024*1024 {
		return fmt.Errorf("%w：政策存储已达到大小上限", backups.ErrInvalid)
	}
	return atomicInstallFile(filepath.Join(s.state, "policies.json"), data, 0600)
}
func (s backupPolicyStore) aead() (cipher.AEAD, error) {
	path := filepath.Join(s.state, "policies.key")
	key, err := readPrivateBackupFile(path, 32)
	if os.IsNotExist(err) {
		// Never invent a new key while encrypted records exist.
		policies, loadErr := s.load()
		if loadErr != nil {
			return nil, loadErr
		}
		for _, p := range policies {
			if p.Ciphertext != "" {
				return nil, errors.New("政策密钥缺失，请恢复原密钥")
			}
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if createErr != nil {
			return nil, createErr
		}
		_, err = f.Write(key)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil || len(key) != 32 {
		return nil, errors.New("政策密钥无效或权限不安全")
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (s backupPolicyStore) list() ([]backups.PolicySummary, error) {
	release, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer release()
	policies, err := s.load()
	if err != nil {
		return nil, err
	}
	summaries := make([]backups.PolicySummary, 0, len(policies))
	for _, p := range policies {
		summaries = append(summaries, p.Summary())
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt) })
	return summaries, nil
}
func (s backupPolicyStore) get(id string) (backups.Policy, error) {
	if !backups.ValidPolicyID(id) {
		return backups.Policy{}, backups.ErrInvalid
	}
	release, err := s.lock()
	if err != nil {
		return backups.Policy{}, err
	}
	defer release()
	policies, err := s.load()
	if err != nil {
		return backups.Policy{}, err
	}
	for _, p := range policies {
		if p.ID == id {
			return p.Policy, nil
		}
	}
	return backups.Policy{}, backups.ErrNotFound
}
func (s backupPolicyStore) save(r backups.SavePolicyRequest) (backups.Policy, error) {
	if err := backups.ValidatePolicy(r); err != nil {
		return backups.Policy{}, err
	}
	// Check filesystem metadata only; saving never starts a backup.
	for _, source := range r.Files {
		if _, err := backupSourceInfo(source); err != nil {
			return backups.Policy{}, err
		}
	}
	release, err := s.lock()
	if err != nil {
		return backups.Policy{}, err
	}
	defer release()
	policies, err := s.load()
	if err != nil {
		return backups.Policy{}, err
	}
	index := -1
	var old storedBackupPolicy
	if r.ID != "" {
		for i, p := range policies {
			if p.ID == r.ID {
				index = i
				old = p
				break
			}
		}
		if index < 0 {
			return backups.Policy{}, backups.ErrNotFound
		}
		if old.Revision != r.Revision {
			return backups.Policy{}, backups.ErrConflict
		}
	} else {
		if len(policies) >= 1000 {
			return backups.Policy{}, fmt.Errorf("%w：最多保存 1000 个政策", backups.ErrInvalid)
		}
		id := make([]byte, 16)
		if _, err = rand.Read(id); err != nil {
			return backups.Policy{}, err
		}
		r.ID = hex.EncodeToString(id)
	}
	p := storedBackupPolicy{Policy: r.Policy, Ciphertext: old.Ciphertext}
	switch r.PasswordAction {
	case "clear":
		p.Ciphertext = ""
	case "replace":
		aead, err := s.aead()
		if err != nil {
			return backups.Policy{}, err
		}
		nonce := make([]byte, aead.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return backups.Policy{}, err
		}
		p.Ciphertext = base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(r.Password), []byte(p.ID)))
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Schedule = strings.Join(strings.Fields(p.Schedule), " ")
	if p.Files == nil {
		p.Files = []backups.FileSource{}
	}
	if p.Databases.Excluded == nil {
		p.Databases.Excluded = []string{}
	}
	if p.Databases.Selected == nil {
		p.Databases.Selected = []string{}
	}
	if p.Databases.Rules == nil {
		p.Databases.Rules = []backups.TableRule{}
	}
	for i := range p.Databases.Rules {
		if p.Databases.Rules[i].Tables == nil {
			p.Databases.Rules[i].Tables = []string{}
		}
	}
	for i := range p.Files {
		if p.Files[i].Excludes == nil {
			p.Files[i].Excludes = []string{}
		}
	}
	p.Compression.HasPassword = p.Ciphertext != ""
	p.Revision = old.Revision + 1
	p.CreatedAt = old.CreatedAt
	if index < 0 {
		p.CreatedAt = time.Now().UTC()
	}
	p.UpdatedAt = time.Now().UTC()
	if index < 0 {
		policies = append(policies, p)
	} else {
		policies[index] = p
	}
	if err = s.persist(policies); err != nil {
		return backups.Policy{}, err
	}
	return p.Policy, nil
}
func (s backupPolicyStore) delete(r backups.PolicyIdentity) error {
	if !backups.ValidPolicyID(r.ID) {
		return backups.ErrInvalid
	}
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()
	policies, err := s.load()
	if err != nil {
		return err
	}
	for i, p := range policies {
		if p.ID == r.ID {
			if r.Revision != p.Revision {
				return backups.ErrConflict
			}
			return s.persist(append(policies[:i], policies[i+1:]...))
		}
	}
	return backups.ErrNotFound
}
func decodeBackupPolicy(in io.Reader, out any) error {
	d := json.NewDecoder(io.LimitReader(in, backups.MaxPolicyBytes+1))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(out) != nil || d.Decode(&extra) != io.EOF {
		return backups.ErrInvalid
	}
	return nil
}
func HandleBackupPolicy(ctx context.Context, action string, in io.Reader, out io.Writer) error {
	s := backupPolicyStore{state: defaultBackupInstaller().state}
	var result any
	var err error
	switch action {
	case "list":
		result, err = s.list()
	case "get", "delete":
		var r backups.PolicyIdentity
		err = decodeBackupPolicy(in, &r)
		if err == nil {
			if action == "get" {
				result, err = s.get(r.ID)
			} else {
				err = s.delete(r)
				result = struct{}{}
			}
		}
	case "save":
		var r backups.SavePolicyRequest
		err = decodeBackupPolicy(in, &r)
		if err == nil {
			result, err = s.save(r)
		}
	case "databases", "tables":
		var r backups.DatabaseInspectRequest
		err = decodeBackupPolicy(in, &r)
		if err == nil {
			result, err = inspectBackupDatabase(ctx, action, r)
		}
	case "files":
		var r struct {
			backups.FileSource
			TimeoutSeconds int `json:"timeout_seconds"`
		}
		err = decodeBackupPolicy(in, &r)
		if err == nil {
			seconds := r.TimeoutSeconds
			if seconds <= 0 || seconds > 60 {
				seconds = 60
			}
			scanCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
			defer cancel()
			result, err = inspectBackupFile(scanCtx, r.FileSource)
		}
	default:
		err = backups.ErrInvalid
	}
	if err != nil {
		kind := "failed"
		switch {
		case errors.Is(err, backups.ErrInvalid):
			kind = "invalid"
		case errors.Is(err, backups.ErrNotFound):
			kind = "not_found"
		case errors.Is(err, backups.ErrConflict):
			kind = "conflict"
		}
		result = map[string]string{"error": err.Error(), "kind": kind}
	}
	data, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		return encodeErr
	}
	if len(data) > 1024*1024 {
		data = []byte(`{"error":"查询结果超过大小限制，请缩小查询范围","kind":"failed"}`)
	}
	_, encodeErr = out.Write(data)
	return encodeErr
}
