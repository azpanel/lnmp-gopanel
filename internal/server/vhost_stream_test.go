package server

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

type streamProvisioner struct {
	fakeProvisioner
	lines  chan string
	finish chan struct{}
}

func (p *streamProvisioner) CreateVhost(ctx context.Context, r vhosts.CreateRequest, emit func(vhosts.CreateEvent)) (vhosts.CreateResult, error) {
	for {
		select {
		case line := <-p.lines:
			emit(vhosts.CreateEvent{Phase: "ssl", Line: line})
		case <-p.finish:
			return vhosts.CreateResult{Name: r.Name, SiteCreated: true}, nil
		case <-ctx.Done():
			return vhosts.CreateResult{}, ctx.Err()
		}
	}
}

func nextStreamEvent(t *testing.T, events <-chan string, contains string) string {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("stream ended before %q", contains)
			}
			if strings.Contains(event, contains) {
				return event
			}
		case <-timer.C:
			t.Fatalf("stream did not send %q", contains)
		}
	}
}

func readStreamEvents(r io.Reader) <-chan string {
	out := make(chan string, 32)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 4096), 8*1024*1024)
		var event strings.Builder
		for scanner.Scan() {
			if scanner.Text() == "" {
				out <- event.String()
				event.Reset()
			} else {
				event.WriteString(scanner.Text())
				event.WriteByte('\n')
			}
		}
	}()
	return out
}

func TestVhostStreamNotificationsDisconnectAndTerminal(t *testing.T) {
	s, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &streamProvisioner{lines: make(chan string), finish: make(chan struct{})}
	s.VhostJobs = vhosts.NewJobs(ctx, p, nil)
	r := vhosts.CreateRequest{RequestID: "stream-test-123", Domain: "example.com", PHP: "none", SSL: vhosts.SSLInput{Mode: "none"}}
	j, err := s.VhostJobs.Start(r)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cookie, _ := storedAuthSession(s)
	connect := func() (*http.Response, context.CancelFunc) {
		t.Helper()
		requestCtx, stop := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(requestCtx, "GET", srv.URL+"/api/v1/vhosts/jobs/"+j.ID+"/events", nil)
		req.AddCookie(cookie)
		response, err := srv.Client().Do(req)
		if err != nil {
			stop()
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			stop()
			response.Body.Close()
			t.Fatal(response.StatusCode)
		}
		return response, stop
	}
	first, stopFirst := connect()
	defer first.Body.Close()
	defer stopFirst()
	second, stopSecond := connect()
	defer second.Body.Close()
	defer stopSecond()
	events := readStreamEvents(second.Body)
	nextStreamEvent(t, events, "event:status")
	stopFirst()
	p.lines <- "incremental output"
	nextStreamEvent(t, events, "incremental output")
	close(p.finish)
	nextStreamEvent(t, events, `"status":"succeeded"`)
	nextStreamEvent(t, events, "event:complete")
	deadline := time.Now().Add(time.Second)
	for len(s.streams) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(s.streams) != 0 {
		t.Fatal("stream slots leaked")
	}
}

func TestVhostIdleStreamRevocation(t *testing.T) {
	s, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &streamProvisioner{lines: make(chan string), finish: make(chan struct{})}
	s.VhostJobs = vhosts.NewJobs(ctx, p, nil)
	j, err := s.VhostJobs.Start(vhosts.CreateRequest{RequestID: "idle-stream-test", Domain: "example.com", PHP: "none", SSL: vhosts.SSLInput{Mode: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	requestCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	cookie, _ := storedAuthSession(s)
	req, _ := http.NewRequestWithContext(requestCtx, "GET", srv.URL+"/api/v1/vhosts/jobs/"+j.ID+"/events", nil)
	req.AddCookie(cookie)
	response, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	events := readStreamEvents(response.Body)
	nextStreamEvent(t, events, "event:status")
	s.Sessions.Delete(cookie.Value)
	nextStreamEvent(t, events, "event:auth_expired")
}
