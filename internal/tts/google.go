// Package tts turns a podcast script into one MP3 with Google Cloud
// Text-to-Speech.
package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RichardLechko/market-summary/internal/httpx"
)

const (
	defaultBase = "https://texttospeech.googleapis.com/v1"
	// maxBytes keeps each request under the API's 5,000-byte input limit.
	maxBytes = 4500
	workers  = 4
)

// Google calls the Cloud Text-to-Speech API.
type Google struct {
	HTTP    *httpx.Client
	BaseURL string
}

// NewGoogle returns a client that sends the API key in a header, so it never
// appears in URLs, logs or error messages.
func NewGoogle(apiKey string) *Google {
	c := httpx.New("market-summary/1.0", 0)
	c.Header = http.Header{"X-Goog-Api-Key": {apiKey}}
	return &Google{HTTP: c}
}

func (g *Google) base() string {
	if g.BaseURL != "" {
		return g.BaseURL
	}
	return defaultBase
}

// Line is one turn of the script.
type Line struct {
	Voice string // e.g. en-US-Chirp3-HD-Charon
	Text  string
}

// Synthesize returns MP3 audio for one piece of text.
func (g *Google) Synthesize(ctx context.Context, text, voice string, rate float64) ([]byte, error) {
	req := map[string]any{
		"input":       map[string]string{"text": text},
		"voice":       map[string]string{"languageCode": languageCode(voice), "name": voice},
		"audioConfig": map[string]any{"audioEncoding": "MP3", "speakingRate": rate},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	out, err := g.HTTP.PostJSON(ctx, g.base()+"/text:synthesize", body)
	if err != nil {
		return nil, fmt.Errorf("text-to-speech: %w", err)
	}
	var resp struct {
		AudioContent string `json:"audioContent"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("text-to-speech: %w", err)
	}
	audio, err := base64.StdEncoding.DecodeString(resp.AudioContent)
	if err != nil {
		return nil, fmt.Errorf("text-to-speech audio: %w", err)
	}
	if len(audio) == 0 {
		return nil, errors.New("text-to-speech: empty audio")
	}
	return audio, nil
}

// Episode speaks every line in order and joins the audio into one MP3. It
// returns the audio and the number of characters sent, which is what Google
// bills.
func (g *Google) Episode(ctx context.Context, lines []Line, rate float64) ([]byte, int, error) {
	type job struct {
		i           int
		voice, text string
	}
	var jobs []job
	chars := 0
	for _, l := range lines {
		for _, chunk := range split(l.Text, maxBytes) {
			jobs = append(jobs, job{i: len(jobs), voice: l.Voice, text: chunk})
			chars += len([]rune(chunk))
		}
	}
	if len(jobs) == 0 {
		return nil, 0, errors.New("text-to-speech: empty script")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	parts := make([][]byte, len(jobs))
	ch := make(chan job)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for range min(workers, len(jobs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				a, err := g.Synthesize(ctx, j.text, j.voice, rate)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					mu.Unlock()
					continue
				}
				parts[j.i] = a
			}
		}()
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		ch <- j
	}
	close(ch)
	wg.Wait()
	if firstErr != nil {
		return nil, chars, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, chars, err
	}
	return JoinMP3(parts, 350*time.Millisecond), chars, nil
}

// Check confirms the key works by listing voices, which is free.
func (g *Google) Check(ctx context.Context) error {
	if _, err := g.HTTP.Get(ctx, g.base()+"/voices?languageCode=en-US"); err != nil {
		return fmt.Errorf("text-to-speech: %w", err)
	}
	return nil
}

// languageCode returns "en-US" from a voice name like "en-US-Chirp3-HD-Kore".
func languageCode(voice string) string {
	p := strings.SplitN(voice, "-", 3)
	if len(p) < 2 {
		return "en-US"
	}
	return p[0] + "-" + strings.ToUpper(p[1])
}

// split breaks text into pieces of at most max bytes, at sentence ends where
// possible, then at spaces.
func split(text string, max int) []string {
	text = strings.TrimSpace(text)
	var out []string
	for len(text) > max {
		cut := strings.LastIndexAny(text[:max], ".!?")
		if cut < max/2 {
			cut = strings.LastIndex(text[:max], " ")
		}
		if cut <= 0 {
			cut = max - 1
		}
		out = append(out, strings.TrimSpace(text[:cut+1]))
		text = strings.TrimSpace(text[cut+1:])
	}
	if text != "" {
		out = append(out, text)
	}
	return out
}
