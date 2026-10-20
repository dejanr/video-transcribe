package subtitle

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func audioFrame(value int16) []byte {
	frame := make([]byte, frameBytes)
	for i := 0; i < len(frame); i += 2 {
		binary.LittleEndian.PutUint16(frame[i:], uint16(value))
	}
	return frame
}

func TestChunkerSkipsSilenceAndShortNoise(t *testing.T) {
	c := chunker{threshold: 0.008, maxFrames: 24}
	silence := audioFrame(0)
	for range 40 {
		if result := c.push(silence); result != nil {
			t.Fatal("silence produced a subtitle chunk")
		}
	}
	c.push(audioFrame(1000))
	for range 10 {
		if result := c.push(silence); result != nil {
			t.Fatal("a single noise frame produced a subtitle chunk")
		}
	}
}

func TestChunkerSplitsOnPauseWithPreRoll(t *testing.T) {
	c := chunker{threshold: 0.008, maxFrames: 24}
	c.push(audioFrame(0))
	for range 4 {
		if result := c.push(audioFrame(1000)); result != nil {
			t.Fatal("speech split before a pause")
		}
	}
	if result := c.push(audioFrame(0)); result != nil {
		t.Fatal("split before half a second of silence")
	}
	result := c.push(audioFrame(0))
	if result == nil || len(result.pcm) != 7*frameBytes || !result.final {
		t.Fatalf("expected a final chunk with pre-roll, four speech frames, and two silence frames; got %+v", result)
	}
	if c.audio != nil {
		t.Fatal("chunk state was not reset")
	}
}

func TestChunkerBoundsContinuousSpeechWithoutLosingFrames(t *testing.T) {
	c := chunker{threshold: 0.008, maxFrames: 8}
	var chunks [][]byte
	for range 24 {
		if result := c.push(audioFrame(-2000)); result != nil {
			chunks = append(chunks, result.pcm)
		}
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 bounded chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if len(chunk) != 8*frameBytes {
			t.Fatalf("chunk has %d bytes", len(chunk))
		}
	}
}

func TestChunkerPreviewsRetainSentenceContext(t *testing.T) {
	c := chunker{threshold: 0.008, maxFrames: 24, stepFrames: 6}
	var updates []*audioChunk
	for range 24 {
		if result := c.push(audioFrame(1000)); result != nil {
			updates = append(updates, result)
		}
	}
	if len(updates) != 4 {
		t.Fatalf("expected three early previews and a final caption, got %d", len(updates))
	}
	for index, update := range updates {
		if len(update.pcm) != (index+1)*6*frameBytes || update.id != 1 || update.final != (index == 3) {
			t.Fatalf("unexpected update %d: %+v", index, update)
		}
	}
	for range 6 {
		if update := c.push(audioFrame(-2000)); update != nil && update.id != 2 {
			t.Fatal("new sentence reused the previous caption ID")
		}
	}
	if !bytes.Equal(updates[0].pcm, bytes.Repeat(audioFrame(1000), 6)) {
		t.Fatal("new audio mutated an in-flight preview")
	}
}

func TestChunkerFinalizesAnEarlyPreviewAtSilence(t *testing.T) {
	c := chunker{threshold: 0.008, maxFrames: 24, stepFrames: 4}
	for range 3 {
		c.push(audioFrame(1000))
	}
	preview := c.push(audioFrame(1000))
	c.push(audioFrame(0))
	final := c.push(audioFrame(0))
	if preview == nil || preview.final || final == nil || !final.final || final.id != preview.id {
		t.Fatalf("pause did not finalize the pending preview: %+v, %+v", preview, final)
	}
	c.reset()
	for range 4 {
		if update := c.push(audioFrame(1000)); update != nil && update.id == final.id {
			t.Fatal("reset reused an invalidated preview ID")
		}
	}
}

func TestChunkerOverlapsHardBoundariesWithoutGrowingWindows(t *testing.T) {
	c := chunker{threshold: 0.008, maxFrames: 8, overlapFrames: 3}
	var first, second *audioChunk
	for i := range 13 {
		if update := c.push(audioFrame(int16(1000 + i*100))); update != nil {
			if first == nil {
				first = update
			} else {
				second = update
			}
		}
	}
	if first == nil || second == nil || !second.overlapped || first.id+1 != second.id {
		t.Fatalf("expected adjacent overlapping windows: %+v, %+v", first, second)
	}
	if len(first.pcm) != 8*frameBytes || len(second.pcm) != 8*frameBytes {
		t.Fatal("overlap increased the maximum window size")
	}
	if !bytes.Equal(first.pcm[5*frameBytes:], second.pcm[:3*frameBytes]) {
		t.Fatal("next window did not retain the previous 0.75 seconds")
	}
	before := bytes.Clone(first.pcm)
	c.push(audioFrame(-2000))
	if !bytes.Equal(first.pcm, before) {
		t.Fatal("carry modified a finalized window")
	}
	c.reset()
	if len(c.carry) != 0 {
		t.Fatal("pause/reset retained old speech")
	}
}

func TestChunkerDoesNotReplayOverlapDuringSilence(t *testing.T) {
	c := chunker{threshold: 0.008, maxFrames: 8, overlapFrames: 3, stepFrames: 4}
	for range 8 {
		c.push(audioFrame(1000))
	}
	for range 40 {
		if update := c.push(audioFrame(0)); update != nil {
			t.Fatal("overlap alone triggered inference")
		}
	}
	for range 4 {
		if update := c.push(audioFrame(1000)); update != nil && update.overlapped {
			t.Fatal("new phrase reused audio from before the pause")
		}
	}
}

func TestWAV(t *testing.T) {
	pcm := audioFrame(-1234)
	body := wav(pcm)
	if string(body[:4]) != "RIFF" || string(body[8:16]) != "WAVEfmt " || string(body[36:40]) != "data" {
		t.Fatal("invalid WAV header")
	}
	if binary.LittleEndian.Uint32(body[24:]) != 16000 || binary.LittleEndian.Uint16(body[22:]) != 1 {
		t.Fatal("expected mono 16 kHz")
	}
	if binary.LittleEndian.Uint32(body[40:]) != uint32(len(pcm)) || !bytes.Equal(body[44:], pcm) {
		t.Fatal("WAV payload was altered")
	}
	if rms(nil) != 0 || rms(audioFrame(0)) != 0 || rms(audioFrame(-32768)) != 1 {
		t.Fatal("incorrect RMS")
	}
}
