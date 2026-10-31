package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/components"
	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/azpanel/lnmp-gopanel/internal/docker"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"github.com/azpanel/lnmp-gopanel/internal/metrics"
	"github.com/azpanel/lnmp-gopanel/internal/paneltls"
	"github.com/azpanel/lnmp-gopanel/internal/releases"
	"github.com/azpanel/lnmp-gopanel/internal/screenshots"
	"github.com/azpanel/lnmp-gopanel/internal/server"
	"github.com/azpanel/lnmp-gopanel/internal/storage"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"github.com/azpanel/lnmp-gopanel/web"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(); err != nil {
		slog.Error("panel failed", "error", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "metrics" {
		return configureMetrics(os.Args[2:])
	}
	if len(os.Args) > 1 && os.Args[1] == "auth" {
		return configureAuth(os.Args[2:])
	}
	if len(os.Args) > 1 && os.Args[1] == "init" {
		return initialize(os.Args[2:])
	}
	if len(os.Args) > 1 && os.Args[1] == "check-screenshots" {
		return checkScreenshots(os.Args[2:])
	}
	if len(os.Args) > 1 && os.Args[1] == "https" {
		return configureHTTPS(os.Args[2:])
	}
	flags := flag.NewFlagSet("lnmp-gopanel", flag.ContinueOnError)
	path := flags.String("config", "/etc/lnmp-gopanel/config.yaml", "panel configuration")
	servicesPath := flags.String("services", "/etc/lnmp-gopanel/services.yaml", "service configuration")
	rootDev := flags.Bool("allow-root-for-development", false, "allow root only for local development")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if os.Geteuid() == 0 && !*rootDev {
		return errors.New("run as a dedicated unprivileged user; for isolated local development only use --allow-root-for-development")
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	account, err := auth.OpenAccount(c.Auth.StateDir, c.Username, c.PasswordHash)
	if err != nil {
		return fmt.Errorf("账户认证状态：%w", err)
	}
	defer account.Close()
	certificate, err := paneltls.Load(c.TLS.CertFile, c.TLS.KeyFile)
	if err != nil {
		return fmt.Errorf("HTTPS certificate: %w", err)
	}
	services, err := config.LoadServices(*servicesPath)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	collector := metrics.New()
	collector.Configure(c.Metrics)
	widgets, widgetsErr := metrics.OpenWidgetStore(c.Metrics.StateDir)
	if widgetsErr != nil {
		slog.Warn("状态指标配置初始化失败，系统指标继续可用", "error", widgetsErr)
	} else {
		collector.SetEnabled(widgets.Get().Enabled)
	}
	manager := control.New(services, control.Helper{Path: c.Helper})
	go collector.Run(ctx)
	go manager.Run(ctx)
	app := server.New(c, collector, manager, web.Assets())
	app.Widgets = widgets
	app.ServicesFile = *servicesPath
	app.Account = account
	app.LoginLog, err = logs.OpenLoginLog(c.Logs.StateDir)
	if err != nil {
		slog.Warn("登录日志初始化失败", "error", err)
	}
	app.CustomLogs, err = logs.OpenRegistry(c.Logs.StateDir)
	if err != nil {
		app.CustomLogsError = "自定义日志列表不可用，请检查状态目录及文件"
		slog.Warn("自定义日志列表初始化失败，内置日志继续可用", "error", err)
	}
	app.Storage = storage.NewManager(ctx, control.Helper{Path: c.Helper})
	app.Vhosts = vhosts.NewManager(ctx, control.Helper{Path: c.Helper})
	app.VhostJobs = vhosts.NewJobs(ctx, control.Helper{Path: c.Helper}, func() { app.Vhosts.Invalidate() })
	app.DNS = &vhosts.DNSChecker{PublicIPs: c.Vhosts.PublicIPs}
	app.Components = components.NewManager(ctx, control.Helper{Path: c.Helper})
	app.Releases = releases.NewManager(ctx, releases.NewFetcher())
	if err := app.Releases.ConfigureRanges(c.Metrics.StateDir); err != nil {
		slog.Warn("版本范围配置初始化失败，Nginx、PHP、MariaDB 版本查询暂停", "error", err)
	}
	app.Docker = docker.NewManager(ctx, control.Helper{Path: c.Helper})
	app.Screenshots, err = screenshots.NewManager(ctx, c.Screenshots.StateDir, &screenshots.Chromium{Browser: c.Screenshots.Browser, StateDir: c.Screenshots.StateDir})
	if err != nil {
		slog.Warn("截图状态存储初始化失败，其他功能继续运行", "error", err)
	}
	httpServer := &http.Server{Addr: c.Listen, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	httpServer.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	httpServer.BaseContext = func(net.Listener) context.Context { return ctx }
	slog.Info("LNMP GoPanel HTTPS listening", "address", c.Listen, "services", len(services))
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.ListenAndServeTLS("", "") }()
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, done := context.WithTimeout(context.Background(), 12*time.Second)
		defer done()
		if err := httpServer.Shutdown(shutdown); err != nil {
			_ = httpServer.Close()
			return err
		}
		return nil
	}
}
func checkScreenshots(args []string) error {
	flags := flag.NewFlagSet("check-screenshots", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/lnmp-gopanel/config.yaml", "panel configuration")
	if err := flags.Parse(args); err != nil {
		return err
	}
	c, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><html><body>截图组件检查</body></html>")
	}), ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(listener) }()
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), screenshots.Timeout)
	defer cancel()
	renderer := &screenshots.Chromium{Browser: c.Screenshots.Browser, StateDir: c.Screenshots.StateDir}
	if _, err := screenshots.NewManager(ctx, c.Screenshots.StateDir, renderer); err != nil {
		return err
	}
	if _, err := renderer.Capture(ctx, screenshots.Target{URL: "http://" + listener.Addr().String()}); err != nil {
		return err
	}
	fmt.Println("截图组件检查通过")
	return nil
}
func initialize(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	dir := flags.String("dir", "/etc/lnmp-gopanel", "configuration directory (existing files are never overwritten)")
	username := flags.String("username", "admin", "administrator name")
	stdin := flags.Bool("password-stdin", false, "read a password from stdin instead of terminal")
	listen := flags.String("listen", "127.0.0.1:8090", "HTTPS listen address; port 0 selects an unused port")
	tlsDir := flags.String("tls-dir", "", "installed TLS directory (defaults to DIR/tls)")
	certificateDir := flags.String("certificate-dir", "", "import an existing certificate/key pair instead of generating a self-signed certificate")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *username == "" || len(*username) > 128 {
		return errors.New("invalid username")
	}
	for _, name := range []string{"config.yaml", "services.yaml"} {
		if _, err := os.Lstat(filepath.Join(*dir, name)); err == nil {
			return fmt.Errorf("%s already exists; refusing to overwrite credentials or service configuration", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	var password []byte
	if *stdin {
		r := bufio.NewReader(io.LimitReader(os.Stdin, 256))
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		password = []byte(strings.TrimRight(line, "\r\n"))
	} else {
		fmt.Fprint(os.Stderr, "管理员密码（至少 12 字节）：")
		var err error
		password, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, "再次输入密码：")
		confirmation, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		if string(password) != string(confirmation) {
			return errors.New("passwords do not match")
		}
	}
	if len(password) < 12 || len(password) > 72 {
		return errors.New("password must be 12–72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword(password, 12)
	clear(password)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*dir, 0750); err != nil {
		return err
	}
	c := config.Config{Listen: *listen, Username: *username, PasswordHash: string(hash), Helper: "/usr/local/libexec/lnmp-gopanel-control"}
	if err := prepareHTTPS(&c, *dir, *tlsDir, *certificateDir); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	services := control.Detect(ctx)
	files := []struct {
		name  string
		value any
		mode  os.FileMode
	}{{"config.yaml", c, 0640}, {"services.yaml", config.Services{Services: services}, 0644}}
	for _, file := range files {
		data, err := yaml.Marshal(file.value)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(filepath.Join(*dir, file.name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.mode)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	fmt.Fprintf(os.Stdout, "已创建配置：%s；检测到 %d 个服务。请检查 services.yaml 后部署辅助程序和 sudoers。\n", *dir, len(services))
	return nil
}
