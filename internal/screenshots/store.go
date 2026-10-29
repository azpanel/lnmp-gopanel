package screenshots

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type store struct {
	dir  string
	aead cipher.AEAD
}

func privatePath(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Geteuid() || info.Mode().Perm()&0077 != 0 || info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) {
		return errors.New("截图状态目录及文件必须由面板账号所有，目录权限 0700、文件权限 0600，且不能是符号链接")
	}
	return nil
}

func openStore(dir string) (*store, map[string]record, error) {
	if !filepath.IsAbs(dir) {
		return nil, nil, errors.New("截图状态目录必须是绝对路径")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, err
	}
	if err := privatePath(dir, true); err != nil {
		return nil, nil, err
	}
	s := &store{dir: dir}
	statePath, keyPath := filepath.Join(dir, "screenshot-settings.json"), filepath.Join(dir, "screenshots.key")
	_, stateErr := os.Lstat(statePath)
	if stateErr != nil && !os.IsNotExist(stateErr) {
		return nil, nil, stateErr
	}
	key, err := os.ReadFile(keyPath)
	if os.IsNotExist(err) && os.IsNotExist(stateErr) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, nil, err
		}
		f, e := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, nil, e
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
		return nil, nil, errors.New("截图加密密钥缺失或无效，请恢复原密钥，不能重新生成覆盖已有凭据")
	}
	if err := privatePath(keyPath, false); err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher(key)
	clear(key)
	if err != nil {
		return nil, nil, err
	}
	s.aead, err = cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	items := map[string]record{}
	if stateErr == nil {
		if err := privatePath(statePath, false); err != nil {
			return nil, nil, err
		}
		f, err := os.Open(statePath)
		if err != nil {
			return nil, nil, err
		}
		data, err := io.ReadAll(io.LimitReader(f, 16*1024*1024+1))
		_ = f.Close()
		if err != nil || len(data) > 16*1024*1024 || json.Unmarshal(data, &items) != nil || items == nil {
			return nil, nil, errors.New("截图设置文件无效")
		}
	}
	for name, item := range items {
		if item.ZoomPercent == 0 {
			item.ZoomPercent = DefaultZoomPercent
		}
		language, seconds, settingsErr := normalizeSettings(item.Language, item.LoadTimeoutSeconds)
		width, height, captureErr := normalizeCaptureOptions(item.ViewportWidth, item.ViewportHeight, item.JavaScript)
		if !validName(name) || item.Name != name || validateURL(item.URL) != nil || !validVersion(item.ImageVersion) || settingsErr != nil || captureErr != nil || validateZoomPercent(item.ZoomPercent) != nil {
			return nil, nil, errors.New("截图设置文件包含无效记录")
		}
		item.Language, item.LoadTimeoutSeconds = language, seconds
		item.ViewportWidth, item.ViewportHeight = width, height
		if item.AuthEnabled {
			if _, err := s.decrypt(name, item.PasswordCipher); err != nil {
				return nil, nil, errors.New("无法解密截图凭据，请恢复匹配的设置和密钥")
			}
		}
		if item.Status == "queued" || item.Status == "running" {
			item.Status, item.Error = "failed", "面板重启，中断了上次截图，请重新截图"
		}
		items[name] = item
	}
	return s, items, nil
}

func (s *store) encrypt(name, password string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(password), []byte(name))), nil
}
func (s *store) decrypt(name, value string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(b) < s.aead.NonceSize() {
		return "", errors.New("凭据无效")
	}
	p, err := s.aead.Open(nil, b[:s.aead.NonceSize()], b[s.aead.NonceSize():], []byte(name))
	if err != nil {
		return "", err
	}
	defer clear(p)
	return string(p), nil
}
func (s *store) atomic(name string, data []byte) error {
	f, err := os.CreateTemp(s.dir, ".screenshot-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.dir, name))
}
func (s *store) save(items map[string]record) error {
	b, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return s.atomic("screenshot-settings.json", b)
}
func imageName(name, version string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:]) + "-" + version + ".jpg"
}
func validVersion(v string) bool {
	if v == "" {
		return true
	}
	b, err := hex.DecodeString(v)
	return err == nil && len(b) == 16
}
