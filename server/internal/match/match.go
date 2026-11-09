// Package match: deterministic clickId lookup, else probabilistic fingerprint scoring against documented Detour weights.
package match

import (
	"errors"
	"fmt"
	"time"

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/store"
)

const (
	MinThreshold = 700
	MaxThreshold = 1200
	MinWindow    = 5
	MaxWindow    = 180
)

var (
	ErrThresholdOutOfRange = errors.New("threshold out of range 700..1200")
	ErrWindowOutOfRange    = errors.New("window out of range 5..180 minutes")
)

type Fingerprint struct {
	Model, Manufacturer, SystemVersion      string
	ScreenWidth, ScreenHeight               int
	Scale                                   float64
	Locale, Timezone, UserAgent, PastedLink string
	CapturedAt                              time.Time
}

type Request struct {
	ClickID     string
	IP          string
	DeviceHash  string
	Fingerprint *Fingerprint
}

type Result struct {
	Matched                         bool
	Click                           store.Click
	Destination                     string
	Method                          string
	Score                           int
	RunnerUp                        int
	Fraud, FraudAction, FraudLinkID string
}

func ValidateSettings(threshold, windowMinutes int) error {
	if windowMinutes < MinWindow || windowMinutes > MaxWindow {
		return fmt.Errorf("%w: got %d", ErrWindowOutOfRange, windowMinutes)
	}
	if threshold < MinThreshold || threshold > MaxThreshold {
		return fmt.Errorf("%w: got %d", ErrThresholdOutOfRange, threshold)
	}
	return nil
}

func Match(st *store.Store, appID string, req Request) (Result, error) {
	if req.DeviceHash != "" {
		if c, err := st.PriorMatch(appID, req.DeviceHash); err == nil {
			return Result{Matched: true, Click: c, Destination: c.Destination, Method: store.MethodPrior, Score: -1, RunnerUp: -1}, nil
		}
	}
	set, _ := st.FraudSettings(appID)
	now := time.Now()
	if req.ClickID != "" {
		c, err := st.ClickByClickID(appID, req.ClickID, req.DeviceHash)
		if errors.Is(err, store.ErrNotFound) {
			return label(noMatch(-1, -1), req), nil
		}
		if err != nil {
			return Result{}, err
		}
		f := fired(c, set, now, true)
		if set.Excluded(f) {
			return label(excludedResult(c), req, f), nil
		}
		return claimLabelled(st, c, req, store.MethodClickID, f)
	}

	if req.Fingerprint != nil {
		if key, tok := store.PasteToken(req.Fingerprint.PastedLink); tok != "" {
			c, err := st.ClickByPasteToken(appID, key, tok, req.DeviceHash)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return Result{}, err
			}
			if err == nil {
				f := fired(c, set, now, true)
				if set.Excluded(f) {
					return label(excludedResult(c), req, f), nil
				}
				r, err := claimLabelled(st, c, req, store.MethodPasteboard, f)
				if err != nil {
					return Result{}, err
				}
				if r.Matched {
					return r, nil
				}
			}
		}
	}

	app, err := st.GetApp(appID)
	if err != nil {
		return Result{}, err
	}
	if err := ValidateSettings(app.MatchThreshold, app.MatchWindowMinutes); err != nil {
		return Result{}, err
	}
	fp := Fingerprint{}
	if req.Fingerprint != nil {
		fp = *req.Fingerprint
	}
	ref := now
	if t := fp.CapturedAt; !t.IsZero() && t.After(now.Add(-MaxWindow*time.Minute)) && t.Before(now.Add(time.Minute)) {
		ref = t
	}
	clicks, err := st.ClicksSince(appID, req.DeviceHash, ref.Add(-time.Duration(app.MatchWindowMinutes)*time.Minute))
	if err != nil {
		return Result{}, err
	}
	top, topClick := -1, store.Click{}
	best, second, bestClick := -1, -1, store.Click{}
	tref := ref
	if tref.After(now) {
		tref = now
	}
	anyActive := set.AnyActive()
	for _, c := range clicks {
		s := Score(c, fp, req.IP)
		if s > top {
			top, topClick = s, c
		}
		if anyActive && set.Excluded(fired(c, set, tref, false)) {
			continue
		}
		if s > best {
			best, second, bestClick = s, best, c
		} else if s > second {
			second = s
		}
	}
	r := noMatch(best, second)
	var f []fraud.Flags
	if top >= app.MatchThreshold {
		if tf := fired(topClick, set, tref, false); set.Excluded(tf) {
			r.FraudAction, r.FraudLinkID, f = store.FraudActionExcluded, topClick.LinkID, append(f, tf)
		}
	}
	if best < app.MatchThreshold {
		return label(r, req, f...), nil
	}
	f = append(f, fired(bestClick, set, tref, false))
	action, linkID := r.FraudAction, r.FraudLinkID
	r, err = claim(st, bestClick, req.DeviceHash, Result{Method: store.MethodProbabilistic, Score: best, RunnerUp: second})
	if err != nil {
		return Result{}, err
	}
	if !r.Matched {
		r.FraudAction, r.FraudLinkID = action, linkID
	} else {
		if action != "" {
			r.FraudAction, r.FraudLinkID = store.FraudActionReattributed, linkID
		}
		if req.DeviceHash != "" {
			n, err := st.CountFingerprintInstalls(appID, req.DeviceHash, r.Click.LinkID, now.AddDate(0, 0, -set.FingerprintWindowDays))
			if err == nil && n+1 >= set.FingerprintMax {
				f = append(f, fraud.Flags{Fingerprint: true})
			}
		}
	}
	return label(r, req, f...), nil
}

func fired(c store.Click, set fraud.Settings, ref time.Time, clickID bool) fraud.Flags {
	return set.Fired(fraud.Click{HitsIP: c.HitsIP, HitsLink: c.HitsLink, FirstSeenAt: c.FirstSeenAt, Open: c.Kind == store.KindOpen,
		UASuspect: c.UASuspect, IPHosting: c.IPHosting}, ref, clickID)
}

func label(r Result, req Request, sets ...fraud.Flags) Result {
	r.Fraud = fraud.Label(req.IP, sets...)
	return r
}

func noMatch(score, runnerUp int) Result {
	return Result{Method: store.MethodOrganic, Score: score, RunnerUp: runnerUp}
}

func excludedResult(c store.Click) Result {
	r := noMatch(-1, -1)
	r.FraudAction, r.FraudLinkID = store.FraudActionExcluded, c.LinkID
	return r
}

func claimLabelled(st *store.Store, c store.Click, req Request, method string, f fraud.Flags) (Result, error) {
	r, err := claim(st, c, req.DeviceHash, Result{Method: method, Score: -1, RunnerUp: -1})
	if err != nil {
		return Result{}, err
	}
	return label(r, req, f), nil
}

func claim(st *store.Store, c store.Click, deviceHash string, r Result) (Result, error) {
	ok, err := st.MarkClickMatched(c.ID, deviceHash)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return noMatch(r.Score, r.RunnerUp), nil
	}
	r.Matched, r.Click, r.Destination = true, c, c.Destination
	return r, nil
}
