package tenant

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// syntheticCheckWorld is a business of `people` people (every one placed at a venue, every
// other one in that venue's department), `own` of whom carry a cutoff of their own, over
// `venues` venues each with a 10-22 shift and a 04-12 department; every person tapped at
// their own venue and the next one.
func syntheticCheckWorld(people, own, venues int) (cutoffWorld, map[uuid.UUID][]uuid.UUID) {
	w := cutoffWorld{name: "Synthetic", zone: "Europe/Malta", business: hm(5, 0),
		venues: map[uuid.UUID]cutoffPlace{}, departments: map[uuid.UUID]cutoffPlace{}}
	vids := make([]uuid.UUID, venues)
	dids := make([]uuid.UUID, venues)
	for i := range vids {
		vids[i], dids[i] = uuid.New(), uuid.New()
		name := fmt.Sprintf("Venue %04d", i)
		var cut *time.Duration
		if i%2 == 0 {
			cut = dur(1, 0)
		}
		w.venues[vids[i]] = cutoffPlace{id: vids[i], venue: vids[i], name: name, cutoff: cut,
			shift: &CutoffShift{Owner: name, Start: hm(10, 0), End: hm(22, 0)}}
		dname := fmt.Sprintf("Bakery %04d", i)
		w.departments[dids[i]] = cutoffPlace{id: dids[i], venue: vids[i], name: dname,
			shift: &CutoffShift{Owner: dname, Start: hm(4, 0), End: hm(12, 0)}}
	}
	tapped := map[uuid.UUID][]uuid.UUID{}
	for i := 0; i < people; i++ {
		v := i % venues
		p := cutoffPerson{id: uuid.New(), branch: &vids[v]}
		if i%2 == 1 {
			p.department = &dids[v]
		}
		if i < own {
			p.own = dur(i%24, 0)
		}
		w.people = append(w.people, p)
		tapped[p.id] = []uuid.UUID{vids[v], vids[(v+1)%venues]}
	}
	return w, tapped
}

// BenchmarkBusinessDayCheck is the check's CPU half over the security lens's four shapes
// (people / own cutoffs / venues).
func BenchmarkBusinessDayCheck(b *testing.B) {
	for _, c := range [][3]int{{400, 0, 10}, {400, 400, 10}, {2000, 2000, 50}, {5000, 5000, 200}} {
		w, tapped := syntheticCheckWorld(c[0], c[1], c[2])
		b.Run(fmt.Sprintf("%d_%d_%d", c[0], c[1], c[2]), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := w.check(context.Background(), uuid.Nil, tapped); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
