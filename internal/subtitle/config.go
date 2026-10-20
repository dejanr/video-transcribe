package subtitle

import (
	"fmt"
	"strings"
)

type Config struct {
	Model          string
	Source         string
	ChunkSeconds   float64
	StepSeconds    float64
	OverlapSeconds float64
	NoVAD          bool
	VADModelPath   string
	VADThreshold   float64
	FullContext    bool
	Threshold      float64
	Threads        int
	NoGPU          bool
}

func (c Config) Validate() error {
	if c.Model == "" || strings.ContainsAny(c.Model, `/\`) || strings.Contains(c.Model, "..") {
		return fmt.Errorf("model must be a whisper.cpp model name, not a path")
	}
	if strings.Contains(c.Model, ".en") || strings.Contains(c.Model, "turbo") {
		return fmt.Errorf("model %q is unsuitable for German translation; use base, small, medium, or large-v3", c.Model)
	}
	if !within(c.ChunkSeconds, 2, 15) {
		return fmt.Errorf("chunk-seconds must be between 2 and 15")
	}
	if c.StepSeconds != 0 && !within(c.StepSeconds, 0.5, c.ChunkSeconds) {
		return fmt.Errorf("step-seconds must be 0 (no previews), or between 0.5 and chunk-seconds")
	}
	if !within(c.OverlapSeconds, 0, 1) {
		return fmt.Errorf("overlap-seconds must be between 0 and 1, and less than chunk-seconds")
	}
	if !c.NoVAD && !probability(c.VADThreshold) {
		return fmt.Errorf("vad-threshold must be between 0 and 1")
	}
	if !probability(c.Threshold) {
		return fmt.Errorf("silence-threshold must be between 0 and 1")
	}
	if c.Threads < 1 {
		return fmt.Errorf("threads must be positive")
	}
	return nil
}

func within(value, minimum, maximum float64) bool {
	return value >= minimum && value <= maximum
}

func probability(value float64) bool {
	return value > 0 && value < 1
}
