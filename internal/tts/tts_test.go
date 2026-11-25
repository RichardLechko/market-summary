package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RichardLechko/market-summary/internal/httpx"
)

// mpeg2Frame returns one MPEG-2 Layer III mono frame at 24 kHz, 32 kbps
// (96 bytes), with fill as its audio data.
func mpeg2Frame(fill byte) []byte {
	f := bytes.Repeat([]byte{fill}, 96)
	copy(f, []byte{0xFF, 0xF3, 0x44, 0xC4})
	return f
}

func infoFrame() []byte {
	f := mpeg2Frame(0)
	copy(f[4+9:], "Info")
	return f
}

func clip(fill byte) []byte {
	id3 := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 5, 1, 2, 3, 4, 5}
	return append(append(append(id3, infoFrame()...), mpeg2Frame(fill)...), mpeg2Frame(fill)...)
}

func TestJoinMP3(t *testing.T) {
	out := JoinMP3([][]byte{clip(0xAA), clip(0xBB)}, 96*time.Millisecond)
	// 96 ms at 24 kHz with 576-sample frames is 4 silent frames.
	if want := 2*96 + 4*96 + 2*96; len(out) != want {
		t.Fatalf("length %d, want %d", len(out), want)
	}
	if bytes.Contains(out, []byte("ID3")) || bytes.Contains(out, []byte("Info")) {
		t.Fatal("tags or info frames left in the joined audio")
	}
	for i := 0; i < len(out); {
		f, ok := parseFrame(out[i:])
		if !ok {
			t.Fatalf("no valid frame at byte %d", i)
		}
		i += f.length
	}
	silent := out[2*96 : 3*96]
	if !bytes.Equal(silent[4:], make([]byte, 92)) {
		t.Fatal("gap frame is not silent")
	}
	if out[2*96+1]&1 != 1 {
		t.Fatal("gap frame should have no CRC")
	}
}

func TestJoinMP3NotLayer3(t *testing.T) {
	a, b := []byte("abc"), []byte("def")
	if got := string(JoinMP3([][]byte{a, b}, time.Second)); got != "abcdef" {
		t.Fatalf("got %q", got)
	}
}

func TestSplit(t *testing.T) {
	text := strings.Repeat("One sentence here. ", 10)
	parts := split(text, 60)
	for _, p := range parts {
		if len(p) > 60 || !strings.HasSuffix(p, ".") {
			t.Fatalf("bad part %q", p)
		}
	}
	if strings.Join(parts, " ") != strings.TrimSpace(text) {
		t.Fatal("text lost in split")
	}
	if got := split("short", 60); len(got) != 1 || got[0] != "short" {
		t.Fatalf("got %v", got)
	}
}

func TestEpisode(t *testing.T) {
	var (
		mu     sync.Mutex
		voices = map[string]string{}
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Goog-Api-Key") != "k" || strings.Contains(r.URL.String(), "k") && r.URL.Query().Get("key") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Input struct{ Text string }
			Voice struct{ LanguageCode, Name string }
		}
		_ = json.Unmarshal(b, &req)
		if req.Voice.LanguageCode != "en-US" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		voices[req.Input.Text] = req.Voice.Name
		mu.Unlock()
		fill := byte(0x11)
		if strings.HasPrefix(req.Input.Text, "B") {
			fill = 0x22
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"audioContent": base64.StdEncoding.EncodeToString(clip(fill))})
	}))
	defer srv.Close()

	g := NewGoogle("k")
	g.BaseURL = srv.URL
	lines := []Line{{"en-US-Chirp3-HD-Charon", "A one."}, {"en-US-Chirp3-HD-Kore", "B two."}, {"en-US-Chirp3-HD-Charon", "A three."}}
	audio, chars, err := g.Episode(context.Background(), lines, 1)
	if err != nil {
		t.Fatal(err)
	}
	if chars != 20 {
		t.Fatalf("chars %d", chars)
	}
	if voices["B two."] != "en-US-Chirp3-HD-Kore" || voices["A three."] != "en-US-Chirp3-HD-Charon" {
		t.Fatalf("voices %v", voices)
	}
	// Order is kept: A, gap, B, gap, A.
	var fills []byte
	for i := 0; i < len(audio); {
		f, ok := parseFrame(audio[i:])
		if !ok {
			t.Fatalf("bad frame at %d", i)
		}
		if v := audio[i+50]; v != 0 && (len(fills) == 0 || fills[len(fills)-1] != v) {
			fills = append(fills, v)
		}
		i += f.length
	}
	if string(fills) != "\x11\x22\x11" {
		t.Fatalf("order %x", fills)
	}
}

func TestEpisodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error": {"message": "API not enabled"}}`))
	}))
	defer srv.Close()
	g := &Google{HTTP: httpx.New("t", 0), BaseURL: srv.URL}
	_, _, err := g.Episode(context.Background(), []Line{{"en-US-Chirp3-HD-Kore", "Hi."}}, 1)
	if err == nil || !strings.Contains(err.Error(), "API not enabled") {
		t.Fatalf("err %v", err)
	}
}

func TestLanguageCode(t *testing.T) {
	if got := languageCode("en-gb-Chirp3-HD-Kore"); got != "en-GB" {
		t.Fatal(got)
	}
}
