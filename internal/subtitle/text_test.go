package subtitle

import "testing"

func TestTrimOverlap(t *testing.T) {
	for _, test := range []struct {
		previous string
		current  string
		want     string
	}{
		{"We are watching a movie.", "A movie today. Can you hear me?", "today. Can you hear me?"},
		{"Good evening, everyone.", "Good evening everyone. Welcome.", "Welcome."},
		{"Good evening.", "Good evening.", ""},
		{"No.", "No. Not really.", "No. Not really."},
		{"I can hear you.", "You are very quiet.", "You are very quiet."},
		{"Earlier words.", "Completely new sentence.", "Completely new sentence."},
		{"", "Hello.", "Hello."},
	} {
		if result := trimOverlap(test.previous, test.current); result != test.want {
			t.Errorf("trimOverlap(%q, %q) = %q; want %q", test.previous, test.current, result, test.want)
		}
	}
}
