package main

import "testing"

func TestShortGPUName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"NVIDIA A100-SXM4-80GB", "A100-80GB"},
		{"NVIDIA H100-SXM5-80GB", "H100-80GB"},
		{"NVIDIA T4", "T4"},
		{"NVIDIA L4", "L4"},
		{"NVIDIA A10G", "A10G"},
		{"A100-PCIE-40GB", "A100-40GB"},
		{"NVIDIA A100-SXM4-40GB", "A100-40GB"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shortGPUName(tt.input)
			if got != tt.want {
				t.Errorf("shortGPUName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0 MiB"},
		{512 * 1024 * 1024, "512 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{40 * 1024 * 1024 * 1024, "40.0 GiB"},
		{80 * 1024 * 1024 * 1024, "80.0 GiB"},
		{1536 * 1024 * 1024, "1.5 GiB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.input)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
