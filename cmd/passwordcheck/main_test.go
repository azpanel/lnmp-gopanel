package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/passwordcheck"
	"github.com/trustelem/zxcvbn"
)

func TestScorerPreservesAlgorithmAndPolicyCases(t *testing.T) {
	data, err := os.ReadFile("../../web/src/lib/password-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Password string `json:"password"`
		Valid    bool   `json:"valid"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i, item := range cases {
		request := passwordcheck.Request{Password: item.Password, Username: "admin"}
		input, _ := json.Marshal(request)
		var output bytes.Buffer
		err := run(bytes.NewReader(input), &output)
		if !passwordcheck.ValidRequest(request) {
			if err == nil || output.Len() != 0 {
				t.Fatalf("invalid case %d returned data", i)
			}
			continue
		}
		var response passwordcheck.Response
		if err != nil || json.Unmarshal(output.Bytes(), &response) != nil || response.Score == nil {
			t.Fatalf("case %d did not return a score", i)
		}
		want := zxcvbn.PasswordStrength(item.Password, []string{"admin", "lnmp", "gopanel", "lnmp-gopanel"}).Score
		if *response.Score != want || (*response.Score >= 3) != item.Valid || response.Version != 1 {
			t.Fatalf("case %d changed score or policy", i)
		}
	}
	for _, username := range []string{"operator", "lnmp-gopanel", "自定义用户"} {
		password := username + "-password-123456"
		input, _ := json.Marshal(passwordcheck.Request{Password: password, Username: username})
		var output bytes.Buffer
		if err := run(bytes.NewReader(input), &output); err != nil {
			t.Fatal(err)
		}
		var response passwordcheck.Response
		_ = json.Unmarshal(output.Bytes(), &response)
		want := zxcvbn.PasswordStrength(password, []string{username, "lnmp", "gopanel", "lnmp-gopanel"}).Score
		if response.Score == nil || *response.Score != want {
			t.Fatal("username hints changed")
		}
	}
}

func TestScorerRejectsMalformedAndOversizedInput(t *testing.T) {
	valid := `{"password":"N7!vQ2_zK9@pL5#sT8$xR4","username":"admin"}`
	for _, input := range []string{
		"", "null", "[]", "{}", `{"password":42}`, valid + `{}`, valid + "garbage",
		valid + strings.Repeat(" ", passwordcheck.MaxRequestBytes),
		`{"password":"N7!vQ2_zK9@pL5#sT8$xR4","username":"admin","extra":true}`,
		`{"password":"` + strings.Repeat("a", 73) + `"}`, `{"password":"short"}`,
		`{"password":"long-password-with-\n-control"}`,
	} {
		var output bytes.Buffer
		if run(strings.NewReader(input), &output) == nil || output.Len() != 0 {
			t.Fatal("invalid request accepted or echoed")
		}
	}
}
