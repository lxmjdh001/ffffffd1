package platform

import (
	"bytes"
	"strings"
	"testing"
)

func TestRatingStarsUseFiveBoundedSlots(t *testing.T) {
	for _, tc := range []struct {
		rating string
		want   [5]int
	}{
		{"0", [5]int{}}, {"0.5", [5]int{50}},
		{"4", [5]int{100, 100, 100, 100, 0}},
		{"4.8", [5]int{100, 100, 100, 100, 80}},
		{"5", [5]int{100, 100, 100, 100, 100}},
		{"5.0", [5]int{100, 100, 100, 100, 100}},
		{"", [5]int{}}, {"NaN", [5]int{}}, {"6", [5]int{}},
	} {
		if got := (PageDesign{DisplayRating: tc.rating}).RatingStars(); got != tc.want {
			t.Errorf("rating %q: got %v want %v", tc.rating, got, tc.want)
		}
	}
}

func TestFiveStarRatingRendersInEveryTemplateAndLanguage(t *testing.T) {
	for _, language := range detailPageLanguages {
		for _, layout := range []string{"google_play", "store", "minimal"} {
			d := defaultPageDesign(Application{Name: "Rating test"})
			d.Language, d.Layout, d.DisplayRating = language, layout, "4.8"
			var out bytes.Buffer
			if err := downloadPageTemplate(layout).Execute(&out, map[string]any{"Design": d, "App": Application{}, "Preview": true}); err != nil {
				t.Fatal(err)
			}
			html := out.String()
			if strings.Count(html, `class="d1-rating-star"`) != 5 || !strings.Contains(html, `style="width:80%"`) || strings.Contains(html, `class="star" aria-hidden="true">★`) {
				t.Fatalf("%s/%s: expected five stars with fractional fill", language, layout)
			}
			d.DisplayRating = ""
			out.Reset()
			if err := downloadPageTemplate(layout).Execute(&out, map[string]any{"Design": d, "App": Application{}, "Preview": true}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), `class="d1-rating-stars"`) {
				t.Fatal("empty ratings must remain hidden")
			}
		}
	}
}
