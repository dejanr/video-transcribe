package subtitle

import (
	"bytes"
	"encoding/binary"
	"math"
)

const sampleRate = 16000
const frameSamples = sampleRate / 4
const frameBytes = frameSamples * 2

type audioChunk struct {
	pcm        []byte
	id         uint64
	final      bool
	overlapped bool
}

type chunker struct {
	threshold     float64
	maxFrames     int
	stepFrames    int
	overlapFrames int
	carry         []byte
	overlapped    bool
	id            uint64
	audio         []byte
	preRoll       []byte
	frames        int
	silent        int
	voiced        int
}

func (c *chunker) push(frame []byte) *audioChunk {
	speech := rms(frame) >= c.threshold
	if len(c.audio) == 0 && !speech {
		c.carry = nil
		c.preRoll = append(c.preRoll[:0], frame...)
		return nil
	}
	if len(c.audio) == 0 {
		c.id++
		c.overlapped = len(c.carry) > 0
		c.frames = len(c.carry) / frameBytes
		c.audio = append(c.audio, c.carry...)
		c.carry = nil
		c.audio = append(c.audio, c.preRoll...)
		c.preRoll = nil
	}
	c.audio = append(c.audio, frame...)
	c.frames++
	if speech {
		c.silent = 0
		c.voiced++
	} else {
		c.silent++
	}
	final := c.frames >= c.maxFrames || (c.silent >= 2 && c.frames >= 6)
	if final {
		result := &audioChunk{pcm: c.audio, id: c.id, final: true, overlapped: c.overlapped}
		voiced := c.voiced
		hardBoundary := c.frames >= c.maxFrames && c.silent < 2
		c.reset()
		if hardBoundary && c.overlapFrames > 0 && voiced >= 2 {
			size := min(c.overlapFrames*frameBytes, len(result.pcm))
			c.carry = bytes.Clone(result.pcm[len(result.pcm)-size:])
		}
		if voiced < 2 {
			return nil
		}
		return result
	}
	if c.stepFrames > 0 && c.frames%c.stepFrames == 0 && c.voiced >= 2 {
		return &audioChunk{pcm: bytes.Clone(c.audio), id: c.id, overlapped: c.overlapped}
	}
	return nil
}

func (c *chunker) reset() {
	c.audio = nil
	c.preRoll = nil
	c.carry = nil
	c.overlapped = false
	c.frames = 0
	c.silent = 0
	c.voiced = 0
}

func rms(pcm []byte) float64 {
	var sum float64
	for i := 0; i+1 < len(pcm); i += 2 {
		value := float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32768
		sum += value * value
	}
	if len(pcm) < 2 {
		return 0
	}
	return math.Sqrt(sum / float64(len(pcm)/2))
}

func wav(pcm []byte) []byte {
	body := make([]byte, 44+len(pcm))
	copy(body, "RIFF")
	binary.LittleEndian.PutUint32(body[4:], uint32(36+len(pcm)))
	copy(body[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(body[16:], 16)
	binary.LittleEndian.PutUint16(body[20:], 1)
	binary.LittleEndian.PutUint16(body[22:], 1)
	binary.LittleEndian.PutUint32(body[24:], sampleRate)
	binary.LittleEndian.PutUint32(body[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(body[32:], 2)
	binary.LittleEndian.PutUint16(body[34:], 16)
	copy(body[36:], "data")
	binary.LittleEndian.PutUint32(body[40:], uint32(len(pcm)))
	copy(body[44:], pcm)
	return body
}
