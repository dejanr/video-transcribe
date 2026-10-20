package subtitle

import (
	"strings"
	"unicode"
)

type captionSequence struct {
	previousID         uint64
	previousGeneration uint64
	previousText       string
}

func (s *captionSequence) event(audio clip, text string) Event {
	rawText := text
	if audio.overlapped && audio.id == s.previousID+1 && audio.generation == s.previousGeneration {
		text = trimOverlap(s.previousText, text)
	}
	kind := "partial"
	if audio.final {
		kind = "subtitle"
		s.previousID, s.previousText, s.previousGeneration = audio.id, rawText, audio.generation
	}
	return Event{Type: kind, Text: text, ID: audio.id}
}

func trimOverlap(previous, current string) string {
	before := strings.Fields(previous)
	after := strings.Fields(current)
	normalize := func(word string) string {
		return strings.TrimFunc(strings.ToLower(word), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
	}
	for size := min(12, len(before), len(after)); size >= 2; size-- {
		match := true
		for i := range size {
			word := normalize(before[len(before)-size+i])
			if word == "" || word != normalize(after[i]) {
				match = false
				break
			}
		}
		if match {
			return strings.Join(after[size:], " ")
		}
	}
	return current
}
