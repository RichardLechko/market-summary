package pipeline

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/RichardLechko/market-summary/internal/digest"
	"github.com/RichardLechko/market-summary/internal/tts"
)

// MetaTTSChars is the month's text-to-speech characters ("tts_chars:" +
// YYYY-MM). Google bills Chirp 3 HD voices past 1 million a month.
const MetaTTSChars = "tts_chars:"

// maxAudioBytes keeps the MP3 attachment well under Gmail's 25 MB limit
// once base64 encoding adds a third.
const maxAudioBytes = 18 << 20

// Speaker turns script lines into one MP3.
type Speaker interface {
	Episode(ctx context.Context, lines []tts.Line, rate float64) ([]byte, int, error)
}

// podcast writes the two-host script and speaks it. It saves the transcript
// and MP3 next to the page and returns the audio, or nil when the podcast is
// off or failed (a failure never stops the email).
func (p *Pipeline) podcast(ctx context.Context, d *digest.Digest, base string, now time.Time) []byte {
	pc := p.Cfg.Settings.Podcast
	if p.Sum == nil || p.TTS == nil || len(pc.Hosts) != 2 {
		return nil
	}
	names := [2]string{pc.Hosts[0].Name, pc.Hosts[1].Name}
	turns, err := p.Sum.Podcast(ctx, d.Brief(), names, pc.Minutes)
	if err != nil {
		p.Log.Error("podcast script failed", "err", err)
		return nil
	}
	voice := map[string]string{}
	for _, h := range pc.Hosts {
		voice[h.Name] = h.Voice
	}
	var (
		lines      []tts.Line
		transcript strings.Builder
		words      int
	)
	for _, t := range turns {
		v, ok := voice[t.Host]
		if !ok {
			v = pc.Hosts[0].Voice
		}
		lines = append(lines, tts.Line{Voice: v, Text: t.Text})
		fmt.Fprintf(&transcript, "%s: %s\n\n", t.Host, t.Text)
		words += len(strings.Fields(t.Text))
	}
	if err := os.WriteFile(base+"-podcast.txt", []byte(transcript.String()), 0o644); err != nil {
		p.Log.Warn("write podcast transcript failed", "err", err)
	}

	start := time.Now()
	audio, chars, err := p.TTS.Episode(ctx, lines, pc.SpeakingRate)
	p.countTTS(now, chars)
	if err != nil {
		p.Log.Error("podcast audio failed", "err", err)
		return nil
	}
	if err := os.WriteFile(base+".mp3", audio, 0o644); err != nil {
		p.Log.Warn("write podcast audio failed", "err", err)
	}
	p.Log.Info("podcast made", "turns", len(turns), "words", words, "characters", chars,
		"mb", fmt.Sprintf("%.1f", float64(len(audio))/(1<<20)), "took", time.Since(start).Round(time.Second))
	if len(audio) > maxAudioBytes {
		p.Log.Warn("podcast too large to email; kept on disk", "path", base+".mp3")
		return nil
	}
	return audio
}

// countTTS adds to the month's text-to-speech character count.
func (p *Pipeline) countTTS(now time.Time, chars int) {
	if chars == 0 {
		return
	}
	key := MetaTTSChars + now.Format("2006-01")
	v, _ := p.Store.Meta(key)
	n, _ := strconv.Atoi(v)
	_ = p.Store.SetMeta(key, strconv.Itoa(n+chars))
}
