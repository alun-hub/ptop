package history

import (
	"strings"
)

// DiffItem represents the comparison of a single metric between two sessions.
type DiffItem struct {
	Kind        string
	Name        string
	BaseDisplay string
	BaseValue   float64
	TargDisplay string
	TargValue   float64
	Delta       Delta
	Verdict     string
}

// DiffResult contains the compared items between two sessions.
type DiffResult struct {
	Base   Session
	Target Session
	Items  []DiffItem
}

// DiffSessions compares base and target sessions, matching metrics across both
// sessions by canonical Key(m.Name) for identical kinds.
func DiffSessions(base, target Session) DiffResult {
	res := DiffResult{
		Base:   base,
		Target: target,
	}

	baseByKind := make(map[string][]Record)
	var kindOrder []string
	seenKinds := make(map[string]bool)

	for _, r := range base.Records {
		k := strings.ToLower(r.Kind)
		if !seenKinds[k] {
			seenKinds[k] = true
			kindOrder = append(kindOrder, k)
		}
		baseByKind[k] = append(baseByKind[k], r)
	}

	targByKind := make(map[string][]Record)
	for _, r := range target.Records {
		k := strings.ToLower(r.Kind)
		if !seenKinds[k] {
			seenKinds[k] = true
			kindOrder = append(kindOrder, k)
		}
		targByKind[k] = append(targByKind[k], r)
	}

	for _, k := range kindOrder {
		baseRecs := baseByKind[k]
		targRecs := targByKind[k]

		var baseMetrics []Metric
		var kindName string
		for _, r := range baseRecs {
			if kindName == "" {
				kindName = r.Kind
			}
			baseMetrics = append(baseMetrics, r.Metrics...)
		}
		for _, r := range targRecs {
			if kindName == "" {
				kindName = r.Kind
			}
		}

		var targMetrics []Metric
		for _, r := range targRecs {
			targMetrics = append(targMetrics, r.Metrics...)
		}

		matchedTargIdx := make(map[int]bool)

		for _, bm := range baseMetrics {
			bk := Key(bm.Name)
			var found bool
			for ti, tm := range targMetrics {
				if matchedTargIdx[ti] {
					continue
				}
				if Key(tm.Name) == bk {
					matchedTargIdx[ti] = true
					found = true

					var d Delta
					if bm.Value != 0 && tm.Value != 0 {
						lowerBetter := bm.LowerBetter || tm.LowerBetter
						var pct float64
						if lowerBetter {
							pct = (bm.Value - tm.Value) / bm.Value * 100
						} else {
							pct = (tm.Value - bm.Value) / bm.Value * 100
						}
						d = Delta{
							Valid:    true,
							Pct:      pct,
							Baseline: bm.Value,
							When:     base.Time,
						}
					}

					verdict := tm.Verdict
					if verdict == "" {
						verdict = bm.Verdict
					}

					res.Items = append(res.Items, DiffItem{
						Kind:        kindName,
						Name:        tm.Name,
						BaseDisplay: bm.Display,
						BaseValue:   bm.Value,
						TargDisplay: tm.Display,
						TargValue:   tm.Value,
						Delta:       d,
						Verdict:     verdict,
					})
					break
				}
			}

			if !found {
				res.Items = append(res.Items, DiffItem{
					Kind:        kindName,
					Name:        bm.Name,
					BaseDisplay: bm.Display,
					BaseValue:   bm.Value,
					TargDisplay: "",
					TargValue:   0,
					Delta:       Delta{},
					Verdict:     bm.Verdict,
				})
			}
		}

		for ti, tm := range targMetrics {
			if matchedTargIdx[ti] {
				continue
			}
			res.Items = append(res.Items, DiffItem{
				Kind:        kindName,
				Name:        tm.Name,
				BaseDisplay: "",
				BaseValue:   0,
				TargDisplay: tm.Display,
				TargValue:   tm.Value,
				Delta:       Delta{},
				Verdict:     tm.Verdict,
			})
		}
	}

	return res
}
