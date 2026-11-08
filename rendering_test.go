package main

import "testing"

func TestMemoryRendering(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		gos, goarch, mygoGPU string
		want                 bool
	}{
		{"macOS Intel", "darwin", "amd64", "", true},
		{"macOS Intel with the GPU asked for", "darwin", "amd64", "1", false},
		{"macOS Intel with memory already asked for", "darwin", "amd64", "0", false},
		{"macOS Apple silicon", "darwin", "arm64", "", false},
		{"Linux", "linux", "amd64", "", false},
		{"Windows", "windows", "amd64", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := memoryRendering(tt.gos, tt.goarch, tt.mygoGPU); got != tt.want {
				t.Errorf("memoryRendering(%q, %q, %q) = %v, want %v",
					tt.gos, tt.goarch, tt.mygoGPU, got, tt.want)
			}
		})
	}
}
