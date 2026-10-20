package subtitle

import (
	"context"
	"fmt"
	"io"
	"time"
)

type session struct {
	config   Config
	server   *inferenceServer
	detector speechDetector
	state    playback
	captions captionSequence
	events   *emitter
}

func (s *session) listen(ctx context.Context, source string, controls io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.state.commands(controls)
	clips := make(chan clip, 1)
	captureDone := make(chan struct{})
	var captureErr error
	go func() {
		captureErr = capture(ctx, s.config, source, &s.state, clips, s.events)
		close(captureDone)
	}()
	defer func() {
		cancel()
		<-captureDone
	}()
	s.events.send(Event{Type: "status", Text: "Listening · " + s.config.Model + " · " + source, Runtime: s.server.backend})
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.server.done:
			return fmt.Errorf("whisper-server exited: %v", s.server.err)
		case <-captureDone:
			return captureErr
		case audio := <-clips:
			if err := s.process(ctx, audio); err != nil {
				return err
			}
		}
	}
}

func (s *session) process(ctx context.Context, audio clip) error {
	if !s.state.accepts(audio) {
		return nil
	}
	processingStarted := time.Now()
	speech := true
	var vadSeconds float64
	if !s.config.NoVAD {
		var err error
		speech, err = s.detector.hasSpeech(ctx, audio.pcm)
		if err != nil {
			return err
		}
		vadSeconds = time.Since(processingStarted).Seconds()
	}
	if !s.state.accepts(audio) {
		return nil
	}
	if !speech {
		if audio.final {
			s.events.send(Event{Type: "subtitle", ID: audio.id})
		}
		return nil
	}

	inferenceStarted := time.Now()
	text, err := s.server.translate(ctx, audio.pcm)
	if err != nil {
		return err
	}
	if !s.state.accepts(audio) || (text == "" && !audio.final) {
		return nil
	}
	caption := s.captions.event(audio, text)
	caption.LatencySeconds = time.Since(audio.started).Seconds()
	caption.AudioSeconds = float64(len(audio.pcm)) / (sampleRate * 2)
	caption.InferenceSeconds = time.Since(inferenceStarted).Seconds()
	caption.QueueSeconds = processingStarted.Sub(audio.queued).Seconds()
	caption.VADSeconds = vadSeconds
	caption.Runtime = s.server.backend
	s.events.send(caption)
	return nil
}
