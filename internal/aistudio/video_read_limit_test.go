package aistudio

import (
	"io"
	"strings"
	"testing"
)

func TestReadVideoMediaRejectsOversizeBodyWithoutDeclaredLength(t *testing.T) {
	_, err := readVideoMedia(MediaStream{Body: io.NopCloser(strings.NewReader("12345"))}, 4)
	if err == nil {
		t.Fatal("oversized video media body was accepted")
	}
}

func TestReadVideoMediaRejectsOversizedDeclaredLength(t *testing.T) {
	_, err := readVideoMedia(MediaStream{
		Body: io.NopCloser(strings.NewReader("1234")), Size: 5,
	}, 4)
	if err == nil {
		t.Fatal("video media with oversized declared length was accepted")
	}
}

func TestReadVideoMediaAcceptsBodyAtLimit(t *testing.T) {
	blob, err := readVideoMedia(MediaStream{
		Body: io.NopCloser(strings.NewReader("1234")), MIME: "video/mp4", Size: 4,
	}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(blob.Data) != "1234" || blob.MIME != "video/mp4" {
		t.Fatalf("video blob = %#v", blob)
	}
}
