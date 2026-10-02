package midia

import "testing"

func TestIdentificar(t *testing.T) {
	casos := []struct {
		link, plataforma, id, canonica string
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=10s", PlataformaYouTube, "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"youtube.com/shorts/dQw4w9WgXcQ?feature=share", PlataformaYouTube, "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"https://m.youtube.com/watch?v=dQw4w9WgXcQ", PlataformaYouTube, "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ?si=abc", PlataformaYouTube, "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{" https://www.tiktok.com/@loja.achados/video/7350000000000000001?lang=pt-BR ", PlataformaTikTok, "7350000000000000001", "https://www.tiktok.com/@loja.achados/video/7350000000000000001"},
		{"https://vm.tiktok.com/ZMabc123/", PlataformaTikTok, "", "https://vm.tiktok.com/ZMabc123/"},
		{"https://www.tiktok.com/t/ZTabc/", PlataformaTikTok, "", "https://www.tiktok.com/t/ZTabc/"},
	}
	for _, c := range casos {
		p, id, can, err := identificar(c.link)
		if err != nil || p != c.plataforma || id != c.id || can != c.canonica {
			t.Errorf("identificar(%q) = %q %q %q %v", c.link, p, id, can, err)
		}
	}
	for _, link := range []string{
		"", "https://www.youtube.com/watch?v=curto", "https://www.youtube.com/@canal", "https://vimeo.com/123",
		"https://shopee.com.br/video/123", "https://www.tiktok.com/@loja/photo/7350000000000000001",
		"ftp://youtube.com/watch?v=dQw4w9WgXcQ", "https://youtube.com.golpe.com/watch?v=dQw4w9WgXcQ",
	} {
		if _, _, _, err := identificar(link); err == nil {
			t.Errorf("identificar(%q) aceitou", link)
		}
	}
}

func TestPlayerURL(t *testing.T) {
	if got := PlayerURL(PlataformaYouTube, "dQw4w9WgXcQ"); got != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" {
		t.Fatal(got)
	}
	if got := PlayerURL(PlataformaTikTok, "7350000000000000001"); got != "https://www.tiktok.com/player/v1/7350000000000000001" {
		t.Fatal(got)
	}
}
