package screenshots

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type Chromium struct {
	Browser  string
	StateDir string
	// Only isolated integration tests may run Chromium as root without its sandbox.
	testNoSandbox bool
}

func BrowserPath(configured string) string {
	if configured != "" {
		if !filepath.IsAbs(configured) {
			return ""
		}
		if info, err := os.Stat(configured); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return configured
		}
		return ""
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}
func (c *Chromium) Available() error {
	if os.Geteuid() == 0 && !c.testNoSandbox {
		return errors.New("截图浏览器必须以普通面板账号运行")
	}
	if BrowserPath(c.Browser) == "" {
		return errors.New("未找到 Chromium，请使用安装参数 --with-screenshots 或配置 screenshots.browser")
	}
	return nil
}
func browserError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("截图超时，请检查网站是否可访问")
	}
	if ctx.Err() != nil {
		return errors.New("截图已取消")
	}
	text := strings.ToUpper(err.Error())
	if strings.Contains(text, "ERR_INVALID_AUTH_CREDENTIALS") || strings.Contains(text, "ERR_MISSING_AUTH_CREDENTIALS") {
		return errors.New("网站认证失败或拒绝访问，请检查 Basic Auth 信息和截图地址")
	}
	if strings.Contains(text, "ERR_CERT_") || strings.Contains(text, "ERR_SSL_") {
		return errors.New("网站 HTTPS 证书校验失败，请检查证书或截图地址")
	}
	if strings.Contains(text, "ERR_NAME_NOT_RESOLVED") {
		return errors.New("无法解析截图网站域名")
	}
	if strings.Contains(text, "ERR_CONNECTION") || strings.Contains(text, "ERR_ADDRESS") {
		return errors.New("无法连接截图网站，请检查地址和网络")
	}
	// Browser output and protocol errors may contain credentials or URL tokens.
	return errors.New("浏览器截图失败，请检查 Chromium 安装、沙箱及网站加载情况")
}
func (c *Chromium) Capture(parent context.Context, target Target) ([]byte, error) {
	if err := c.Available(); err != nil {
		return nil, err
	}
	if err := validateURL(target.URL); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, Timeout)
	defer cancel()
	profile, err := os.MkdirTemp(c.StateDir, ".browser-*")
	if err != nil {
		return nil, errors.New("无法创建浏览器临时目录")
	}
	defer os.RemoveAll(profile)
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.ExecPath(BrowserPath(c.Browser)), chromedp.UserDataDir(profile),
		chromedp.NoInheritEnv, chromedp.Env("PATH=/usr/bin:/bin", "HOME="+profile, "LANG=C.UTF-8", "XDG_CACHE_HOME="+profile),
		chromedp.Flag("disable-popup-blocking", false), chromedp.Flag("no-sandbox", c.testNoSandbox))
	allocator, closeAllocator := chromedp.NewExecAllocator(ctx, opts...)
	defer closeAllocator()
	ctx, closeBrowser := chromedp.NewContext(allocator, chromedp.WithLogf(func(string, ...any) {}), chromedp.WithErrorf(func(string, ...any) {}))
	defer closeBrowser()
	if err := chromedp.Do(ctx, chromedp.EmulateViewport(1440, 900)); err != nil {
		return nil, browserError(ctx, err)
	}
	session := chromedp.FromContext(ctx).Target
	tree, err := cdp.Call(ctx, session, page.GetFrameTree, cdp.Empty{})
	if err != nil {
		return nil, browserError(ctx, err)
	}
	var authRejected atomic.Bool
	requests := chromedp.Events(ctx, fetch.RequestPaused)
	auths := chromedp.Events(ctx, fetch.AuthRequired)
	go func() {
		for ev, err := range requests {
			if err != nil {
				return
			}
			u, _ := url.Parse(ev.Request.URL)
			if u == nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "data" && u.Scheme != "blob" && ev.Request.URL != "about:blank") {
				_, _ = cdp.Call(ctx, session, fetch.FailRequest, fetch.FailRequestParams{RequestID: ev.RequestID, ErrorReason: network.ErrorReasonBlockedByClient})
			} else {
				_, _ = cdp.Call(ctx, session, fetch.ContinueRequest, fetch.ContinueRequestParams{RequestID: ev.RequestID})
			}
		}
	}()
	go func() {
		attempted := map[fetch.RequestID]bool{}
		for ev, err := range auths {
			if err != nil {
				return
			}
			response := &fetch.AuthChallengeResponse{Response: fetch.AuthChallengeResponseResponseCancelAuth}
			if target.AuthEnabled && ev.AuthChallenge != nil && ev.AuthChallenge.Source == fetch.AuthChallengeSourceServer && strings.EqualFold(ev.AuthChallenge.Scheme, "basic") && origin(ev.AuthChallenge.Origin) == origin(target.URL) && !attempted[ev.RequestID] {
				attempted[ev.RequestID] = true
				response.Response = fetch.AuthChallengeResponseResponseProvideCredentials
				response.Username, response.Password = target.Username, target.Password
			}
			if response.Response == fetch.AuthChallengeResponseResponseCancelAuth && ev.FrameID == tree.FrameTree.Frame.ID {
				authRejected.Store(true)
			}
			_, _ = cdp.Call(ctx, session, fetch.ContinueWithAuth, fetch.ContinueWithAuthParams{RequestID: ev.RequestID, AuthChallengeResponse: response})
		}
	}()
	_, err = cdp.Call(ctx, chromedp.FromContext(ctx).Browser, browser.SetDownloadBehavior, browser.SetDownloadBehaviorParams{Behavior: browser.SetDownloadBehaviorBehaviorDeny})
	if err != nil {
		return nil, browserError(ctx, err)
	}
	_, err = cdp.Call(ctx, session, fetch.Enable, fetch.EnableParams{HandleAuthRequests: ptr(true)})
	if err != nil {
		return nil, browserError(ctx, err)
	}
	response, err := chromedp.Run(ctx, chromedp.NavigateResponse(target.URL))
	if err != nil {
		if authRejected.Load() {
			return nil, errors.New("网站认证失败或拒绝访问，请检查 Basic Auth 信息和截图地址")
		}
		return nil, browserError(ctx, err)
	}
	if response == nil {
		return nil, errors.New("网站未返回有效页面")
	}
	if response.Status == 401 || response.Status == 403 {
		return nil, errors.New("网站认证失败或拒绝访问，请检查 Basic Auth 信息和截图地址")
	}
	if response.Status >= 400 {
		return nil, fmt.Errorf("网站返回 HTTP %d，未更新截图", response.Status)
	}
	if err := chromedp.Do(ctx, chromedp.WaitReady(chromedp.CSS("body")), chromedp.Sleep(2*time.Second)); err != nil {
		return nil, browserError(ctx, err)
	}
	image, err := cdp.Call(ctx, session, page.CaptureScreenshot, page.CaptureScreenshotParams{Format: page.CaptureScreenshotFormatJpeg, Quality: ptr(int64(80)), CaptureBeyondViewport: ptr(false)})
	if err != nil {
		return nil, browserError(ctx, err)
	}
	return image.Data, nil
}
func ptr[T any](v T) *T { return &v }
