package tmux

import (
	"testing"
)

func TestOvaltineExample(t *testing.T) {
	before := `● Received "no restore" test. Standing by.

❯ [from gastown/crew/holden] Reliability test 1
  ⎿  Interrupted · What should Claude do instead?

❯ [from gastown/crew/holden] Reliability test 3

● Received reliability tests 1 and 3. Note: test 2 appears to be missing or was not delivered.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
❯ I'm starting to type some instructio
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  ⏵⏵ bypass permissions on (shift+tab to cycle)`

	// More realistic AFTER:
	// - Top line scrolled off
	// - New AI output added ("✻ Ruminating…")
	// - Ovaltine ad appeared
	after := `❯ [from gastown/crew/holden] Reliability test 1
  ⎿  Interrupted · What should Claude do instead?

❯ [from gastown/crew/holden] Reliability test 3

● Received reliability tests 1 and 3. Note: test 2 appears to be missing or was not delivered.

✻ Ruminating…

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
❯ ns for you t[from gastown/crew/holden] Reliability test 7
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  ⏵⏵ bypass permissions on (shift+tab to cycle)                     AD: DRINK OVELTINE!!`

	nudge := "[from gastown/crew/holden] Reliability test 7"

	// Run diff
	diffs := MyersDiff([]byte(before), []byte(after))
	
	t.Logf("Number of diffs: %d", len(diffs))
	for i, d := range diffs {
		opName := map[DiffOp]string{DiffEqual: "=", DiffDelete: "-", DiffInsert: "+"}[d.Op]
		if len(d.Data) > 60 {
			t.Logf("  %d: %s (%d bytes) %q...", i, opName, len(d.Data), string(d.Data[:60]))
		} else {
			t.Logf("  %d: %s %q", i, opName, string(d.Data))
		}
	}

	// Find nudge
	original, gap, afterNudge, found := FindNudgeInDiff([]byte(before), []byte(after), []byte(nudge), diffs)
	
	t.Logf("\nFindNudgeInDiff results:")
	t.Logf("  found: %v", found)
	t.Logf("  original: %q", string(original))
	t.Logf("  gap: %q", string(gap))
	t.Logf("  afterNudge: %q", string(afterNudge))

	// Expected values
	expectedOriginal := "I'm starting to type some instructio"
	expectedGap := "ns for you t"
	expectedAfterNudge := ""

	if !found {
		t.Fatalf("nudge not found")
	}
	if string(original) != expectedOriginal {
		t.Errorf("original: expected %q, got %q", expectedOriginal, string(original))
	}
	if string(gap) != expectedGap {
		t.Errorf("gap: expected %q, got %q", expectedGap, string(gap))
	}
	if string(afterNudge) != expectedAfterNudge {
		t.Errorf("afterNudge: expected %q, got %q", expectedAfterNudge, string(afterNudge))
	}

	// Text to restore
	restore := TextToRestore(original, gap, afterNudge)
	expectedRestore := "I'm starting to type some instructions for you t"
	t.Logf("\nText to restore: %q", string(restore))
	if string(restore) != expectedRestore {
		t.Errorf("restore: expected %q, got %q", expectedRestore, string(restore))
	}

	// Clean delivery check
	isClean := IsCleanDelivery(gap, afterNudge)
	t.Logf("Is clean delivery: %v (expected: false)", isClean)
	if isClean {
		t.Errorf("expected non-clean delivery due to gap typing")
	}
}
