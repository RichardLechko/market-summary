package pipeline

import (
	"context"
	"errors"
	"html"
	"os"
	"strings"
	"testing"

	"github.com/RichardLechko/market-summary/internal/config"
	"github.com/RichardLechko/market-summary/internal/tts"
)

type fakeSpeaker struct {
	lines []tts.Line
	err   error
}

func (f *fakeSpeaker) Episode(_ context.Context, lines []tts.Line, _ float64) ([]byte, int, error) {
	f.lines = lines
	if f.err != nil {
		return nil, 10, f.err
	}
	return []byte("ID3 fake mp3"), 42, nil
}

var hosts = config.Podcast{Minutes: 20, SpeakingRate: 1, Hosts: []config.Host{{Name: "Dan", Voice: "voice-a"}, {Name: "Kate", Voice: "voice-b"}}}

func TestPodcastAttached(t *testing.T) {
	srv := fixtureServer(t)
	sender := &fakeSender{}
	p := newTestPipeline(t, srv, &fakeCaller{}, sender)
	p.Cfg.Settings.Podcast = hosts
	sp := &fakeSpeaker{}
	p.TTS = sp
	res, err := p.Run(context.Background(), Options{Now: now0})
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.lines) != 3 || sp.lines[0].Voice != "voice-a" || sp.lines[1].Voice != "voice-b" || sp.lines[2].Voice != "voice-a" {
		t.Fatalf("lines = %+v", sp.lines)
	}
	atts := sender.atts[0]
	if len(atts) != 1 || atts[0].ContentType != "audio/mpeg" || atts[0].Name != "market-summary-2026-11-24.mp3" {
		t.Fatalf("attachments = %+v", atts)
	}
	if !strings.Contains(html.UnescapeString(sender.sent[0]), "podcast version is attached") {
		t.Fatal("email does not mention the podcast")
	}
	if b, err := os.ReadFile(res.AudioPath); err != nil || string(b) != "ID3 fake mp3" {
		t.Fatalf("audio file: %v", err)
	}
	tr, err := os.ReadFile(strings.TrimSuffix(res.Path, ".html") + "-podcast.txt")
	if err != nil || !strings.Contains(string(tr), "Kate: JPMorgan's CEO resigned.") {
		t.Fatalf("transcript %q, %v", tr, err)
	}
	if n, _ := p.Store.Meta(MetaTTSChars + "2026-11"); n != "42" {
		t.Fatalf("characters counted = %q", n)
	}
}

func TestPodcastFailureStillSends(t *testing.T) {
	srv := fixtureServer(t)
	sender := &fakeSender{}
	p := newTestPipeline(t, srv, &fakeCaller{}, sender)
	p.Cfg.Settings.Podcast = hosts
	p.TTS = &fakeSpeaker{err: errors.New("quota")}
	res, err := p.Run(context.Background(), Options{Now: now0})
	if err != nil || !res.Sent {
		t.Fatalf("sent=%v err=%v", res.Sent, err)
	}
	if len(sender.atts[0]) != 0 || res.AudioPath != "" {
		t.Fatalf("attachments = %+v", sender.atts[0])
	}
	if n, _ := p.Store.Meta(MetaTTSChars + "2026-11"); n != "10" {
		t.Fatalf("characters counted = %q", n)
	}
}
