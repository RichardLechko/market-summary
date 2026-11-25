package tts

import (
	"bytes"
	"time"
)

// JoinMP3 joins MP3 clips into one stream with gap of silence between them.
// Each clip arrives as a complete file, so their tags and VBR info frames
// (which state one clip's length and would make players stop or seek wrongly
// in the joined file) are removed. Clips that are not MPEG Layer III are
// joined unchanged.
func JoinMP3(parts [][]byte, gap time.Duration) []byte {
	var out bytes.Buffer
	var silence []byte
	for i, p := range parts {
		p = stripTags(p)
		h, ok := parseFrame(p)
		if ok && isInfoFrame(p[:h.length], h) {
			p = p[h.length:]
			h, ok = parseFrame(p)
		}
		if silence == nil && ok && gap > 0 {
			silence = silentFrames(p, h, gap)
		}
		if i > 0 {
			out.Write(silence)
		}
		out.Write(p)
	}
	return out.Bytes()
}

type frame struct {
	mpeg1   bool
	mono    bool
	length  int
	samples int // per frame
	rate    int // samples per second
}

var (
	bitrates1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	bitrates2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	rates     = map[byte][3]int{3: {44100, 48000, 32000}, 2: {22050, 24000, 16000}, 0: {11025, 12000, 8000}}
)

// parseFrame reads the Layer III frame header at the start of b.
func parseFrame(b []byte) (frame, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return frame{}, false
	}
	version := (b[1] >> 3) & 3
	if version == 1 || (b[1]>>1)&3 != 1 { // reserved version, or not Layer III
		return frame{}, false
	}
	brIdx, srIdx := b[2]>>4, (b[2]>>2)&3
	if srIdx == 3 {
		return frame{}, false
	}
	f := frame{mpeg1: version == 3, mono: b[3]>>6 == 3, rate: rates[version][srIdx]}
	pad := int((b[2] >> 1) & 1)
	if f.mpeg1 {
		f.samples = 1152
		f.length = 144000*bitrates1[brIdx]/f.rate + pad
	} else {
		f.samples = 576
		f.length = 72000*bitrates2[brIdx]/f.rate + pad
	}
	if f.length < 4 || f.length > len(b) {
		return frame{}, false
	}
	return f, true
}

// isInfoFrame reports whether a frame carries a Xing, Info or VBRI header
// rather than audio.
func isInfoFrame(b []byte, f frame) bool {
	side := 17
	switch {
	case f.mpeg1 && !f.mono:
		side = 32
	case !f.mpeg1 && f.mono:
		side = 9
	}
	if off := 4 + side; len(b) >= off+4 {
		if t := string(b[off : off+4]); t == "Xing" || t == "Info" {
			return true
		}
	}
	return len(b) >= 40 && string(b[36:40]) == "VBRI"
}

// stripTags removes a leading ID3v2 tag and a trailing ID3v1 tag.
func stripTags(b []byte) []byte {
	if len(b) >= 10 && string(b[:3]) == "ID3" {
		n := int(b[6]&0x7F)<<21 | int(b[7]&0x7F)<<14 | int(b[8]&0x7F)<<7 | int(b[9]&0x7F)
		n += 10
		if b[5]&0x10 != 0 { // footer present
			n += 10
		}
		if n <= len(b) {
			b = b[n:]
		}
	}
	if len(b) >= 128 && string(b[len(b)-128:len(b)-125]) == "TAG" {
		b = b[:len(b)-128]
	}
	return b
}

// silentFrames returns frames that decode to silence, in the same format as
// the frame at the start of b: a header followed by zeroed side information
// and audio data.
func silentFrames(b []byte, f frame, gap time.Duration) []byte {
	hdr := [4]byte{b[0], b[1] | 1, b[2] &^ 2, b[3]} // no CRC, no padding
	g, ok := parseFrame(append(hdr[:], make([]byte, f.length)...))
	if !ok {
		return nil
	}
	n := int(gap.Seconds()*float64(f.rate)/float64(f.samples) + 0.5)
	out := make([]byte, 0, n*g.length)
	for range n {
		out = append(out, hdr[:]...)
		out = append(out, make([]byte, g.length-4)...)
	}
	return out
}
