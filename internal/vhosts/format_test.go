package vhosts

import (
	"strings"
	"testing"
)

func TestFormatNginxPreservesLiteralsCommentsAndStructure(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"dense", "server{listen 80;location /{return 301 https://$server_name$request_uri;}}", "server {\n    listen 80;\n    location / {\n        return 301 https://$server_name$request_uri;\n    }\n}\n"},
		{"literals", "server { set $v \"literal; { } # value\"; set $path /a\\ b; return 200 '${v};#{}'; }", "server {\n    set $v \"literal; { } # value\";\n    set $path /a\\ b;\n    return 200 '${v};#{}';\n}\n"},
		{"comments", "# before\nserver { # server\nlisten 80; # port\n# route\nlocation / {return 200 ok;} # end\n}\n", "# before\nserver { # server\n    listen 80; # port\n    # route\n    location / {\n        return 200 ok;\n    } # end\n}\n"},
		{"variables", "server { return 301 https://${host}$request_uri; }", "server {\n    return 301 https://${host}$request_uri;\n}\n"},
		{"brace lines", "server\n    {\n\tlisten 80;\n    }\n", "server\n{\n    listen 80;\n}\n"},
		{"crlf", "server {\r\n\tlisten 80;\r\n}\r\n", "server {\r\n    listen 80;\r\n}\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FormatNginx(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("format: %v\nwant %q\ngot  %q", err, tc.want, got)
			}
			again, err := FormatNginx(got)
			if err != nil || again != got {
				t.Fatalf("format is not stable: %v\n%s", err, again)
			}
		})
	}
	for _, input := range []string{"server {", "server { content_by_lua_block { ngx.say('x') } }", "server {\x00}", strings.Repeat(" ", MaxConfig+1)} {
		if _, err := FormatNginx(input); err == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
}
