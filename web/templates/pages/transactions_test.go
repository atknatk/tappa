package pages

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// TestPlaquesCard_TheShortenedListSaysHowManyItShows (M12-48): when the business has more
// plaques on a wall than The Pass's card lists, the card says how many it lists, and that
// number is the rows it drew -- not a word typed beside a constant that can move without it.
func TestPlaquesCard_TheShortenedListSaysHowManyItShows(t *testing.T) {
	v := PassPlaquesView{State: WidgetRead, More: true}
	for i := 0; i < PassPlaqueRows; i++ {
		v.Rows = append(v.Rows, PassPlaqueRow{Venue: "KF Valletta", Plaque: "PLAQUE 91AC", Seen: "never"})
	}
	var b strings.Builder
	if err := plaquesCard(v).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	body := b.String()
	rows := strings.Count(body, "PLAQUE 91AC")
	if rows != PassPlaqueRows {
		t.Fatalf("the card drew %d rows, want %d", rows, PassPlaqueRows)
	}
	if want := strconv.Itoa(rows) + " plaques, the most recently seen first."; !strings.Contains(body, want) {
		t.Errorf("the shortened card does not say %q", want)
	}

	// CONTROL: a card that lists every plaque does not claim to be shortened.
	v.More = false
	b.Reset()
	if err := plaquesCard(v).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "most recently seen first.") {
		t.Error("a card listing every plaque says it lists only some")
	}
}
