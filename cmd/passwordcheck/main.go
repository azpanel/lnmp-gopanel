// The scorer is a separate executable so its dictionaries never initialize in
// the long-lived panel or privileged control helper.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/azpanel/lnmp-gopanel/internal/passwordcheck"
	"github.com/trustelem/zxcvbn"
)

func main() {
	if len(os.Args) != 1 || run(os.Stdin, os.Stdout) != nil {
		// Never print input, parser errors, or scorer diagnostics.
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer) error {
	invalid := errors.New("invalid password check request")
	data, err := io.ReadAll(io.LimitReader(input, passwordcheck.MaxRequestBytes+1))
	if err != nil || len(data) > passwordcheck.MaxRequestBytes {
		return invalid
	}
	defer clear(data)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request passwordcheck.Request
	if decoder.Decode(&request) != nil || !passwordcheck.ValidRequest(request) {
		return invalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return invalid
	}
	score := zxcvbn.PasswordStrength(request.Password, []string{request.Username, "lnmp", "gopanel", "lnmp-gopanel"}).Score
	return json.NewEncoder(output).Encode(passwordcheck.Response{Version: 1, Score: &score})
}
