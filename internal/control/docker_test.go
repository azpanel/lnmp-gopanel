package control

import (
	"io"
	"strings"
	"testing"
)

func TestDockerOutputBoundedEvenWhenCopied(t *testing.T) {
	buffer := &dockerBuffer{limit: 1024}
	n, err := io.Copy(buffer, strings.NewReader(strings.Repeat("x", 64*1024)))
	if err != nil || n != 64*1024 || buffer.data.Len() != 1024 || !buffer.overflow {
		t.Fatal("Docker output limit bypassed")
	}
	if n, err := buffer.Write([]byte("more")); n != 4 || err != nil || buffer.data.Len() != 1024 {
		t.Fatal("overflow write mishandled")
	}
}
