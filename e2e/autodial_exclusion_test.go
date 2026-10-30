package e2e

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// The journal is a recovery contract even when the server commits after the
// caller times out. Absence alone cannot resolve an unacknowledged add.
func exclusionLifecycle(marker string, before map[string]bool, read func() (map[string]bool, error), write func(string) error, stage func(string) error) (err error) {
	if before[marker] {
		return fmt.Errorf("fixture already excluded")
	}
	if e := stage("add_dispatch"); e != nil {
		return e
	}
	addResolved := false
	defer func() {
		current, e := read()
		if e != nil {
			err = errors.Join(err, e)
			return
		}
		if current[marker] {
			addResolved = true
			if e := stage("remove_dispatch"); e != nil {
				err = errors.Join(err, e)
				return
			}
			err = errors.Join(err, write("include"))
		}
		after, e := read()
		if e != nil {
			err = errors.Join(err, e)
			return
		}
		if !addResolved {
			err = errors.Join(err, stage("add_unresolved"), fmt.Errorf("add may still commit; reconcile journal before reuse"))
			return
		}
		if !reflect.DeepEqual(before, after) {
			err = errors.Join(err, stage("cleanup_unresolved"), fmt.Errorf("original exclusion set not restored"))
			return
		}
		err = errors.Join(err, stage("cleanup_verified"))
	}()
	if e := write("exclude"); e != nil {
		return e
	}
	addResolved = true
	return stage("add_verified")
}

func TestExclusionJournalRetainsPendingAdd(t *testing.T) {
	// Model a provider that responds after the CLI timeout. During both recovery
	// reads the original set is visible; the delayed write commits afterwards.
	set := map[string]bool{}
	stages := []string{}
	writes := []string{}
	err := exclusionLifecycle("fixture", map[string]bool{}, func() (map[string]bool, error) { return set, nil }, func(action string) error { writes = append(writes, action); return fmt.Errorf("outcome_unknown") }, func(s string) error { stages = append(stages, s); return nil })
	set["fixture"] = true // late server commit, after recovery has returned
	if err == nil || !reflect.DeepEqual(writes, []string{"exclude"}) || !reflect.DeepEqual(stages, []string{"add_dispatch", "add_unresolved"}) {
		t.Fatal("pending add falsely certified as cleaned up")
	}
}
func TestExclusionJournalReconcilesObservedAdd(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		set := map[string]bool{}
		stages := []string{}
		writes := []string{}
		err := exclusionLifecycle("fixture", map[string]bool{}, func() (map[string]bool, error) {
			out := map[string]bool{}
			for k, v := range set {
				out[k] = v
			}
			return out, nil
		}, func(action string) error {
			writes = append(writes, action)
			if action == "exclude" {
				set["fixture"] = true
				if unknown {
					return fmt.Errorf("outcome_unknown")
				}
			} else {
				delete(set, "fixture")
			}
			return nil
		}, func(s string) error { stages = append(stages, s); return nil })
		if (err != nil) != unknown || len(set) != 0 || !reflect.DeepEqual(writes, []string{"exclude", "include"}) || stages[len(stages)-1] != "cleanup_verified" {
			t.Fatal("observed add was not safely removed")
		}
	}
}

func TestExclusionCleanupDoesNotDispatchWithoutJournal(t *testing.T) {
	set := map[string]bool{}
	writes := []string{}
	stages := []string{}
	err := exclusionLifecycle("fixture", map[string]bool{}, func() (map[string]bool, error) { return set, nil }, func(action string) error { writes = append(writes, action); set["fixture"] = true; return nil }, func(s string) error {
		stages = append(stages, s)
		if s == "remove_dispatch" {
			return fmt.Errorf("journal sync failed")
		}
		return nil
	})
	if err == nil || !set["fixture"] || !reflect.DeepEqual(writes, []string{"exclude"}) || !reflect.DeepEqual(stages, []string{"add_dispatch", "add_verified", "remove_dispatch"}) {
		t.Fatal("cleanup bypassed durable dispatch recording")
	}
}
