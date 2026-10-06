package domain

import "testing"

func TestPlayerURL(t *testing.T) {
	if got := PlayerURL(PlatformYouTube, "dQw4w9WgXcQ"); got != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" {
		t.Fatal(got)
	}
	if got := PlayerURL(PlatformTikTok, "7350000000000000001"); got != "https://www.tiktok.com/player/v1/7350000000000000001" {
		t.Fatal(got)
	}
	if got := PlayerURL(PlatformUpload, "x"); got != "" {
		t.Fatal(got)
	}
}
