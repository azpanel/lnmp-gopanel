package screenshots

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"image/jpeg"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

const Timeout = 30 * time.Second
const DefaultLanguage = "zh-CN"
const MinLoadTimeoutSeconds = 5
const MaxLoadTimeoutSeconds = 300
const QueueLimit = 64
const MaxImageSize = 5 * 1024 * 1024
const DefaultViewportWidth = 1440
const DefaultViewportHeight = 900
const MinViewportSize = 320
const MaxViewportSize = 3840
const MaxJavaScriptSize = 16 * 1024
const DefaultZoomPercent = 100
const MinZoomPercent = 25
const MaxZoomPercent = 200

var ErrBusy = errors.New("截图队列已满，请稍后重试")
var ErrMissing = errors.New("请先保存截图设置")
var languagePattern = regexp.MustCompile(`^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$`)

func validateZoomPercent(percent int) error {
	if percent < MinZoomPercent || percent > MaxZoomPercent {
		return errors.New("网页缩放必须为 25–200 的整数百分比")
	}
	return nil
}

func normalizeCaptureOptions(width, height int, javascript string) (int, int, error) {
	if width == 0 && height == 0 {
		width, height = DefaultViewportWidth, DefaultViewportHeight
	}
	if width < MinViewportSize || width > MaxViewportSize || height < MinViewportSize || height > MaxViewportSize {
		return 0, 0, errors.New("截图宽度和高度必须均为 320–3840 的整数")
	}
	if len(javascript) > MaxJavaScriptSize {
		return 0, 0, errors.New("截图前 JavaScript 不能超过 16 KiB")
	}
	return width, height, nil
}

func normalizeSettings(language string, seconds int) (string, int, error) {
	language = strings.TrimSpace(language)
	if language == "" {
		language = DefaultLanguage
	}
	if len(language) > 64 || !languagePattern.MatchString(language) {
		return "", 0, errors.New("浏览器语言格式无效，请使用 zh-CN、en-US 等语言标签")
	}
	if seconds == 0 {
		seconds = int(Timeout / time.Second)
	}
	if seconds < MinLoadTimeoutSeconds || seconds > MaxLoadTimeoutSeconds {
		return "", 0, errors.New("最长加载时间必须为 5–300 秒")
	}
	return language, seconds, nil
}

type Input struct {
	URL                string  `json:"url"`
	ViewportWidth      int     `json:"viewport_width"`
	ViewportHeight     int     `json:"viewport_height"`
	ZoomPercent        *int    `json:"zoom_percent,omitempty"`
	JavaScript         string  `json:"javascript"`
	Language           string  `json:"language"`
	LoadTimeoutSeconds int     `json:"load_timeout_seconds"`
	AuthEnabled        bool    `json:"auth_enabled"`
	Username           string  `json:"username"`
	Password           *string `json:"password,omitempty"`
}
type Target struct {
	URL, Username, Password string
	ViewportWidth           int
	ViewportHeight          int
	ZoomPercent             int
	JavaScript              string
	AuthEnabled             bool
	Language                string
	LoadTimeoutSeconds      int
}
type View struct {
	Name               string     `json:"name"`
	URL                string     `json:"url"`
	ViewportWidth      int        `json:"viewport_width"`
	ViewportHeight     int        `json:"viewport_height"`
	ZoomPercent        int        `json:"zoom_percent"`
	JavaScript         string     `json:"javascript"`
	Language           string     `json:"language"`
	LoadTimeoutSeconds int        `json:"load_timeout_seconds"`
	AuthEnabled        bool       `json:"auth_enabled"`
	Username           string     `json:"username"`
	PasswordSaved      bool       `json:"password_saved"`
	Status             string     `json:"status"`
	Error              string     `json:"error,omitempty"`
	CapturedAt         *time.Time `json:"captured_at"`
	CapturedURL        string     `json:"captured_url,omitempty"`
	ImageVersion       string     `json:"image_version,omitempty"`
}
type record struct {
	View
	PasswordCipher string `json:"password_cipher,omitempty"`
}
type Snapshot struct {
	Items            map[string]View `json:"items"`
	BrowserAvailable bool            `json:"browser_available"`
	BrowserError     string          `json:"browser_error,omitempty"`
}
type Renderer interface {
	Available() error
	Capture(context.Context, Target) ([]byte, error)
}
type job struct {
	name     string
	revision uint64
}
type Manager struct {
	mu            sync.Mutex
	ctx           context.Context
	store         *store
	renderer      Renderer
	items         map[string]record
	versions      map[string]uint64
	queue         []job
	notify        chan struct{}
	runningName   string
	runningCancel context.CancelFunc
}

func validName(name string) bool { return vhosts.ValidName(name) }
func validateURL(value string) error {
	u, e := url.Parse(value)
	if e != nil || len(value) > 2048 || u == nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("请输入有效的 HTTP/HTTPS 地址，认证信息请单独填写")
	}
	return nil
}
func origin(value string) string {
	u, _ := url.Parse(value)
	if u == nil {
		return ""
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return strings.ToLower(u.Scheme+"://"+u.Hostname()) + ":" + port
}
func NewManager(ctx context.Context, dir string, renderer Renderer) (*Manager, error) {
	s, items, e := openStore(dir)
	if e != nil {
		return nil, e
	}
	m := &Manager{ctx: ctx, store: s, renderer: renderer, items: items, versions: map[string]uint64{}, notify: make(chan struct{}, 1)}
	go m.work()
	return m, nil
}
func (m *Manager) Latest() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := Snapshot{Items: map[string]View{}, BrowserAvailable: true}
	if err := m.renderer.Available(); err != nil {
		v.BrowserAvailable = false
		v.BrowserError = err.Error()
	}
	for k, item := range m.items {
		v.Items[k] = item.View
	}
	return v
}
func (m *Manager) commit(name string, r *record) error {
	next := make(map[string]record, len(m.items)+1)
	for k, v := range m.items {
		next[k] = v
	}
	if r == nil {
		delete(next, name)
	} else {
		next[name] = *r
	}
	if err := m.store.save(next); err != nil {
		if errors.Is(err, ErrStateTooLarge) {
			return err
		}
		return errors.New("无法保存截图状态，请检查状态目录权限和磁盘空间")
	}
	m.items = next
	return nil
}
func (m *Manager) queued(name string) bool {
	for _, j := range m.queue {
		if j.name == name {
			return true
		}
	}
	return false
}
func (m *Manager) invalidate(name string) {
	m.versions[name]++
	next := m.queue[:0]
	for _, j := range m.queue {
		if j.name != name {
			next = append(next, j)
		}
	}
	m.queue = next
	if m.runningName == name && m.runningCancel != nil {
		m.runningCancel()
	}
}
func (m *Manager) enqueue(name string) {
	m.invalidate(name)
	m.queue = append(m.queue, job{name, m.versions[name]})
	select {
	case m.notify <- struct{}{}:
	default:
	}
}
func (m *Manager) Save(name string, input Input) (View, error) {
	if !validName(name) {
		return View{}, errors.New("站点参数无效")
	}
	input.URL = strings.TrimSpace(input.URL)
	if e := validateURL(input.URL); e != nil {
		return View{}, e
	}
	language, seconds, err := normalizeSettings(input.Language, input.LoadTimeoutSeconds)
	if err != nil {
		return View{}, err
	}
	width, height, err := normalizeCaptureOptions(input.ViewportWidth, input.ViewportHeight, input.JavaScript)
	if err != nil {
		return View{}, err
	}
	zoom := DefaultZoomPercent
	if input.ZoomPercent != nil {
		zoom = *input.ZoomPercent
	}
	if err := validateZoomPercent(zoom); err != nil {
		return View{}, err
	}
	if len(input.Username) > 256 || strings.ContainsAny(input.Username, ":\r\n\x00") || (input.Password != nil && len(*input.Password) > 1024) {
		return View{}, errors.New("认证信息无效或过长")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.queue) >= QueueLimit && !m.queued(name) {
		return View{}, ErrBusy
	}
	old := m.items[name]
	r := old
	r.Name, r.URL, r.AuthEnabled, r.Username = name, input.URL, input.AuthEnabled, input.Username
	r.Language, r.LoadTimeoutSeconds = language, seconds
	r.ViewportWidth, r.ViewportHeight, r.JavaScript = width, height, input.JavaScript
	r.ZoomPercent = zoom
	r.PasswordCipher = ""
	r.PasswordSaved = false
	if input.AuthEnabled {
		if input.Username == "" {
			return View{}, errors.New("请填写 Basic Auth 用户名")
		}
		if input.Password == nil || *input.Password == "" {
			if !old.AuthEnabled || old.Username != input.Username || origin(old.URL) != origin(input.URL) || old.PasswordCipher == "" {
				return View{}, errors.New("首次配置、更换用户名或目标来源时，请重新填写 Basic Auth 密码")
			}
			r.PasswordCipher = old.PasswordCipher
		} else {
			var e error
			r.PasswordCipher, e = m.store.encrypt(name, *input.Password)
			if e != nil {
				return View{}, errors.New("无法加密截图凭据")
			}
		}
		r.PasswordSaved = true
	} else {
		r.Username = ""
	}
	r.Status, r.Error = "queued", ""
	if e := m.commit(name, &r); e != nil {
		return View{}, e
	}
	m.enqueue(name)
	return r.View, nil
}
func (m *Manager) Refresh(name string) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.items[name]
	if !ok {
		return View{}, ErrMissing
	}
	if r.Status == "queued" || r.Status == "running" {
		return r.View, nil
	}
	if len(m.queue) >= QueueLimit {
		return View{}, ErrBusy
	}
	r.Status, r.Error = "queued", ""
	if e := m.commit(name, &r); e != nil {
		return View{}, e
	}
	m.enqueue(name)
	return r.View, nil
}
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.items[name]
	if err := m.commit(name, nil); err != nil {
		return err
	}
	m.invalidate(name)
	if r.ImageVersion != "" {
		_ = os.Remove(filepath.Join(m.store.dir, imageName(name, r.ImageVersion)))
	}
	return nil
}
func (m *Manager) Image(name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.items[name]
	if !ok || r.ImageVersion == "" {
		return nil, os.ErrNotExist
	}
	path := filepath.Join(m.store.dir, imageName(name, r.ImageVersion))
	if err := privatePath(path, false); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > MaxImageSize {
		return nil, errors.New("截图文件无效")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxImageSize+1))
	if len(data) > MaxImageSize {
		return nil, errors.New("截图文件过大")
	}
	return data, err
}
func (m *Manager) work() {
	for {
		m.mu.Lock()
		if m.ctx.Err() != nil {
			m.mu.Unlock()
			return
		}
		if len(m.queue) == 0 {
			m.mu.Unlock()
			select {
			case <-m.ctx.Done():
				return
			case <-m.notify:
			}
			continue
		}
		j := m.queue[0]
		m.queue = m.queue[1:]
		r := m.items[j.name]
		r.Status = "running"
		if err := m.commit(j.name, &r); err != nil {
			r.Status, r.Error = "failed", err.Error()
			m.items[j.name] = r
			m.mu.Unlock()
			continue
		}
		target := Target{URL: r.URL, Username: r.Username, AuthEnabled: r.AuthEnabled, Language: r.Language, LoadTimeoutSeconds: r.LoadTimeoutSeconds,
			ViewportWidth: r.ViewportWidth, ViewportHeight: r.ViewportHeight, ZoomPercent: r.ZoomPercent, JavaScript: r.JavaScript}
		var err error
		if r.AuthEnabled {
			target.Password, err = m.store.decrypt(j.name, r.PasswordCipher)
		}
		ctx, cancel := context.WithTimeout(m.ctx, time.Duration(r.LoadTimeoutSeconds)*time.Second)
		m.runningName, m.runningCancel = j.name, cancel
		m.mu.Unlock()
		var image []byte
		if err == nil {
			image, err = m.renderer.Capture(ctx, target)
		} else {
			err = errors.New("无法解密截图凭据")
		}
		target.Password = ""
		cancel()
		m.finish(j, r.URL, image, err)
	}
}
func (m *Manager) finish(j job, capturedURL string, image []byte, captureErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runningName, m.runningCancel = "", nil
	if m.versions[j.name] != j.revision {
		return
	}
	r, ok := m.items[j.name]
	if !ok {
		return
	}
	if captureErr == nil {
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(image))
		if err != nil || cfg.Width != r.ViewportWidth || cfg.Height != r.ViewportHeight || len(image) > MaxImageSize {
			captureErr = errors.New("截图图像无效或超过 5 MiB 限制")
		}
	}
	if captureErr != nil {
		r.Status, r.Error = "failed", captureErr.Error()
		if err := m.commit(j.name, &r); err != nil {
			m.items[j.name] = r
		}
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		r.Status, r.Error = "failed", "无法生成截图标识"
		if err := m.commit(j.name, &r); err != nil {
			m.items[j.name] = r
		}
		return
	}
	v := hex.EncodeToString(b)
	if err := m.store.atomic(imageName(j.name, v), image); err != nil {
		r.Status, r.Error = "failed", "无法保存截图，请检查磁盘空间"
		if err := m.commit(j.name, &r); err != nil {
			m.items[j.name] = r
		}
		return
	}
	old := r.ImageVersion
	now := time.Now()
	r.ImageVersion, r.CapturedAt, r.CapturedURL, r.Status, r.Error = v, &now, capturedURL, "ready", ""
	if err := m.commit(j.name, &r); err != nil {
		_ = os.Remove(filepath.Join(m.store.dir, imageName(j.name, v)))
		previous := m.items[j.name]
		previous.Status, previous.Error = "failed", err.Error()
		m.items[j.name] = previous
		return
	}
	if old != "" {
		_ = os.Remove(filepath.Join(m.store.dir, imageName(j.name, old)))
	}
}
