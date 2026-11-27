package autopopulate

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"last_orders/internal/lastorders/components/winningstats"
)

type Candidate struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Wins    int    `json:"count"`
	LastWon string `json:"last_won_date,omitempty"`
}

var ErrMeetNotFound = errors.New("meet not found")
var ErrMeetClosed = errors.New("meet is closed")

type Pools struct {
	GroupID      string      `json:"group_id"`
	Most         []Candidate `json:"most_visited"`
	Least        []Candidate `json:"least_visited"`
	General      []Candidate `json:"general"`
	EmptyHistory bool        `json:"empty_history,omitempty"`
}
type Service struct {
	client *firestore.Client
	stats  *winningstats.Service
	now    func() time.Time
	london *time.Location
}

func New(client *firestore.Client, stats *winningstats.Service, now func() time.Time) (*Service, error) {
	if client == nil || stats == nil {
		return nil, fmt.Errorf("autopopulate dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		return nil, err
	}
	return &Service{client: client, stats: stats, now: now, london: loc}, nil
}

func (s *Service) Preview(ctx context.Context, meetID string) (string, Pools, error) {
	meet, err := s.client.Collection("polls").Doc(meetID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return "", Pools{}, ErrMeetNotFound
		}
		return "", Pools{}, err
	}
	data := meet.Data()
	group, _ := data["groupId"].(string)
	if group == "" {
		return "", Pools{}, fmt.Errorf("meet has no Group")
	}
	if completed, _ := data["completed"].(bool); completed {
		return "", Pools{}, ErrMeetClosed
	}
	stats, err := s.stats.Read(ctx, group, 1)
	if err != nil {
		return "", Pools{}, err
	}
	if len(stats.Counts) == 0 {
		return group, Pools{GroupID: group, Most: []Candidate{}, Least: []Candidate{}, General: []Candidate{}, EmptyHistory: true}, nil
	}
	cutoff := s.now().In(s.london).AddDate(0, 0, -28).Format(time.DateOnly)
	existing := map[string]bool{}
	if candidates, ok := data["pubs"].(map[string]any); ok {
		for id := range candidates {
			existing[id] = true
		}
	}
	docs, err := s.client.Collection("pubs").Documents(ctx).GetAll()
	if err != nil {
		return "", Pools{}, err
	}
	ranked := make([]Candidate, 0, len(docs)+len(stats.Counts))
	viable := map[string]bool{}
	known := map[string]bool{}
	for _, doc := range docs {
		known[doc.Ref.ID] = true
		v := doc.Data()
		name, _ := v["name"].(string)
		typ, _ := v["venueType"].(string)
		if typ == "" {
			typ = "pub"
		}
		if typ != "pub" || name == "" || v["deleted_on"] != nil {
			continue
		}
		last := stats.Last[doc.Ref.ID]
		row := Candidate{ID: doc.Ref.ID, Name: name, Wins: stats.Counts[doc.Ref.ID], LastWon: last}
		ranked = append(ranked, row)
		viable[doc.Ref.ID] = v["banned"] != true && !existing[doc.Ref.ID] && (last == "" || last < cutoff)
	}
	for id, wins := range stats.Counts {
		if known[id] {
			continue
		}
		last := stats.Last[id]
		ranked = append(ranked, Candidate{ID: id, Name: id, Wins: wins, LastWon: last})
		viable[id] = !existing[id] && (last == "" || last < cutoff)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Wins != ranked[j].Wins {
			return ranked[i].Wins > ranked[j].Wins
		}
		if ranked[i].Name != ranked[j].Name {
			return ranked[i].Name < ranked[j].Name
		}
		return ranked[i].ID < ranked[j].ID
	})
	filterViable := func(candidates []Candidate) []Candidate {
		result := make([]Candidate, 0, len(candidates))
		for _, candidate := range candidates {
			if viable[candidate.ID] {
				result = append(result, candidate)
			}
		}
		return result
	}
	general := filterViable(ranked)
	var most, least []Candidate
	for limit := 5; limit <= len(ranked); limit += 5 {
		top := filterViable(ranked[:limit])
		low := append([]Candidate(nil), ranked...)
		sort.SliceStable(low, func(i, j int) bool {
			if low[i].Wins != low[j].Wins {
				return low[i].Wins < low[j].Wins
			}
			if low[i].Name != low[j].Name {
				return low[i].Name < low[j].Name
			}
			return low[i].ID < low[j].ID
		})
		if limit > len(low) {
			break
		}
		low = low[:limit]
		leastViable := filterViable(low)
		most, least = top, leastViable
		ids := map[string]bool{}
		for _, row := range top {
			ids[row.ID] = true
		}
		for _, row := range leastViable {
			ids[row.ID] = true
		}
		if len(ids) >= 3 {
			break
		}
	}
	if len(most) == 0 {
		most = general
	}
	if len(least) == 0 {
		least = general
	}
	return group, Pools{GroupID: group, Most: most, Least: least, General: general}, nil
}

func (p Pools) SelectionGroups() [][]Candidate {
	most, least, general := shuffled(p.Most), shuffled(p.Least), shuffled(p.General)
	// Duplicate entries retain the established weights in the fallback pool.
	fallback := shuffled(append(append(append([]Candidate(nil), p.General...), p.Most...), p.Least...))
	return [][]Candidate{most, least, general, fallback}
}

func shuffled(rows []Candidate) []Candidate {
	result := append([]Candidate(nil), rows...)
	rand.Shuffle(len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
	return result
}
