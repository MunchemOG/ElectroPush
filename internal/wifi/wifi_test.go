//go:build darwin

package wifi

import "testing"

func TestParseNetworksetupSSID(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "normal association",
			output: "Current Wi-Fi Network: ASUS_5G\n",
			want:   "ASUS_5G",
		},
		{
			name:   "SSID containing a colon keeps everything after the first one",
			output: "Current Wi-Fi Network: Andrei: Robot\n",
			want:   "Andrei: Robot",
		},
		{

			name:   "the not-associated line yields nothing",
			output: "You are not associated with an AirPort network.\n",
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseNetworksetupSSID(tt.output); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFirstNotIn(t *testing.T) {

	atTheLab := []string{"ICHB-Robotics-5G", "14270-RC", "ICHB-GIM", "ASUS_5G"}

	onTheRobot := []string{"14270-RC", "ICHB-Robotics-5G", "ICHB-GIM", "ASUS_5G"}

	tests := []struct {
		name     string
		networks []string
		exclude  []string
		want     string
	}{
		{
			name:     "before switching, the top entry is where we are",
			networks: atTheLab,
			exclude:  []string{"14270-RC", "FTC-9RbP"},
			want:     "ICHB-Robotics-5G",
		},
		{
			name:     "after switching, skipping the robot finds where we came from",
			networks: onTheRobot,
			exclude:  []string{"14270-RC", "FTC-9RbP"},
			want:     "ICHB-Robotics-5G",
		},
		{
			name:     "an empty exclusion just takes the top entry",
			networks: atTheLab,
			exclude:  nil,
			want:     "ICHB-Robotics-5G",
		},
		{
			name:     "blank exclusions are ignored rather than matching",
			networks: atTheLab,
			exclude:  []string{""},
			want:     "ICHB-Robotics-5G",
		},
		{
			name:     "every network excluded yields nothing",
			networks: []string{"14270-RC"},
			exclude:  []string{"14270-RC"},
			want:     "",
		},
		{
			name:     "no saved networks yields nothing",
			networks: nil,
			exclude:  []string{"14270-RC"},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstNotIn(tt.networks, tt.exclude); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsRedacted(t *testing.T) {
	redacted := []string{
		"<redacted>",
		"<unknown>",
		"You are not associated with an AirPort network.",
	}
	for _, ssid := range redacted {
		if !isRedacted(ssid) {
			t.Errorf("%q should count as redacted", ssid)
		}
	}

	real := []string{"ASUS_5G", "14270-RC", "ICHB-Robotics-5G", "FTC-9RbP"}
	for _, ssid := range real {
		if isRedacted(ssid) {
			t.Errorf("%q is a real network name, not a placeholder", ssid)
		}
	}
}
