package main

import "testing"

func TestParseHumanSize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"bytes integer", "1024", 1024, false},
		{"kilobytes", "1K", KiB, false},
		{"megabytes", "10M", 10 * MiB, false},
		{"gigabytes", "20G", 20 * GiB, false},
		{"terabytes", "1T", TiB, false},
		{"fractional gigabytes", "1.5G", int64(1.5 * float64(GiB)), false},
		{"lowercase normalized", "20g", 20 * GiB, false},
		{"leading and trailing spaces", " 20G ", 20 * GiB, false},
		{"space between number and unit", "20 G", 20 * GiB, false},
		{"plain integer no unit", "5000", 5000, false},
		{"empty string", "", 0, true},
		{"non-numeric", "abc", 0, true},
		{"negative overflow", "-99999999999999999G", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHumanSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseHumanSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseHumanSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSizeMultiplier(t *testing.T) {
	tests := []struct {
		input      string
		wantMult   int64
		wantSuffix int
	}{
		{"10K", KiB, 1},
		{"10M", MiB, 1},
		{"10G", GiB, 1},
		{"10T", TiB, 1},
		{"1024", 1, 0},
		{"", 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			mult, suffix := parseSizeMultiplier(tt.input)
			if mult != tt.wantMult {
				t.Errorf("multiplier = %d, want %d", mult, tt.wantMult)
			}
			if suffix != tt.wantSuffix {
				t.Errorf("suffixLen = %d, want %d", suffix, tt.wantSuffix)
			}
		})
	}
}
