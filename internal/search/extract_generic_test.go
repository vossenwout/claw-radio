package search

import "testing"

func TestGenericRejectsScriptAndCSSNoise(t *testing.T) {
	html := `
<html><head><style>.foo{position:absolute;top:1px;}</style></head>
<body>
<script>const x = 1;</script>
<p>.bar{width:100%}</p>
<p>WordPress.com VIP - Powered</p>
<p>Kendrick Lamar - Not Like Us</p>
</body></html>`

	got := Generic(html)
	if len(got) != 1 {
		t.Fatalf("Generic() len=%d, want 1 (%v)", len(got), got)
	}
	if got[0].Artist != "Kendrick Lamar" || got[0].Title != "Not Like Us" {
		t.Fatalf("Generic()[0]=%#v, want Kendrick Lamar - Not Like Us", got[0])
	}
}

func TestKworbExtractsArtistAndTitles(t *testing.T) {
	html := `
<html><head><title>Kendrick Lamar - Spotify Top Songs</title></head>
<body>
<table class="addpos sortable">
<tbody>
<tr><td class="text"><div><a href="https://open.spotify.com/track/123">Not Like Us</a></div></td></tr>
<tr><td class="text"><div><a href="https://open.spotify.com/track/456">HUMBLE.</a></div></td></tr>
</tbody>
</table>
</body></html>`

	got := Kworb(html)
	if len(got) != 2 {
		t.Fatalf("Kworb() len=%d, want 2 (%v)", len(got), got)
	}
	if got[0].Artist != "Kendrick Lamar" || got[0].Title != "Not Like Us" {
		t.Fatalf("Kworb()[0]=%#v, want Kendrick Lamar - Not Like Us", got[0])
	}
}
