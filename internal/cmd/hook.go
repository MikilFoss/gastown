package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/events"
	"github.com/steveyegge/gastown/internal/style"
)

var hookCmd = &cobra.Command{
	Use:     "hook [bead-id]",
	GroupID: GroupWork,
	Short:   "Show or attach work on your hook",
	Long: `Show what's on your hook, or attach new work.

With no arguments, shows your current hook status (alias for 'gt mol status').
With a bead ID, attaches that work to your hook.

The hook is the "durability primitive" - work on your hook survives session
restarts, context compaction, and handoffs. When you restart (via gt handoff),
your SessionStart hook finds the attached work and you continue from where
you left off.

Examples:
  gt hook                           # Show what's on my hook
  gt hook status                    # Same as above
  gt hook gt-abc                    # Attach issue gt-abc to your hook
  gt hook gt-abc -s "Fix the bug"   # With subject for handoff mail

Related commands:
  gt sling <bead>    # Hook + start now (keep context)
  gt handoff <bead>  # Hook + restart (fresh context)
  gt unsling         # Remove work from hook`,
	Args: cobra.MaximumNArgs(1),
	RunE: runHookOrStatus,
}

// hookStatusCmd shows hook status (alias for mol status)
var hookStatusCmd = &cobra.Command{
	Use:   "status [target]",
	Short: "Show what's on your hook",
	Long: `Show what's slung on your hook.

This is an alias for 'gt mol status'. Shows what work is currently
attached to your hook, along with progress information.

Examples:
  gt hook status                    # Show my hook
  gt hook status greenplace/nux     # Show nux's hook`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMoleculeStatus,
}

var (
	hookSubject string
	hookMessage string
	hookDryRun  bool
	hookForce   bool
)

func init() {
	// Flags for attaching work (gt hook <bead-id>)
	hookCmd.Flags().StringVarP(&hookSubject, "subject", "s", "", "Subject for handoff mail (optional)")
	hookCmd.Flags().StringVarP(&hookMessage, "message", "m", "", "Message for handoff mail (optional)")
	hookCmd.Flags().BoolVarP(&hookDryRun, "dry-run", "n", false, "Show what would be done")
	hookCmd.Flags().BoolVarP(&hookForce, "force", "f", false, "Replace existing incomplete pinned bead")

	// --json flag for status output (used when no args, i.e., gt hook --json)
	hookCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON (for status)")
	hookStatusCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON")
	hookCmd.AddCommand(hookStatusCmd)

	rootCmd.AddCommand(hookCmd)
}

// runHookOrStatus dispatches to status or hook based on args
func runHookOrStatus(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		// No args - show status
		return runMoleculeStatus(cmd, args)
	}
	// Has arg - attach work
	return runHook(cmd, args)
}

func runHook(cmd *cobra.Command, args []string) error {
	beadID := args[0]

	// Polecats cannot hook - they use gt done for lifecycle
	if polecatName := os.Getenv("GT_POLECAT"); polecatName != "" {
		return fmt.Errorf("polecats cannot hook work (use gt done for handoff)")
	}

	// Verify the bead exists
	if err := verifyBeadExists(beadID); err != nil {
		return err
	}

	// Determine agent identity
	agentID, _, _, err := resolveSelfTarget()
	if err != nil {
		return fmt.Errorf("detecting agent identity: %w", err)
	}

	// Find beads directory
	workDir, err := findLocalBeadsDir()
	if err != nil {
		return fmt.Errorf("not in a beads workspace: %w", err)
	}

	b := beads.New(workDir)

	// Check for existing pinned bead for this agent
	existingPinned, err := b.List(beads.ListOptions{
		Status:   beads.StatusPinned,
		Assignee: agentID,
		Priority: -1,
	})
	if err != nil {
		return fmt.Errorf("checking existing pinned beads: %w", err)
	}

	// If there's an existing pinned bead, check if we can auto-replace
	if len(existingPinned) > 0 {
		existing := existingPinned[0]

		// Skip if it's the same bead we're trying to pin
		if existing.ID == beadID {
			fmt.Printf("%s Already hooked: %s\n", style.Bold.Render("✓"), beadID)
			return nil
		}

		// Check if existing bead is complete
		isComplete, hasAttachment := checkPinnedBeadComplete(b, existing)

		if isComplete {
			// Auto-replace completed bead
			fmt.Printf("%s Replacing completed bead %s...\n", style.Dim.Render("ℹ"), existing.ID)
			if !hookDryRun {
				if hasAttachment {
					// Close completed molecule bead (use bd close --force for pinned)
					closeCmd := exec.Command("bd", "close", existing.ID, "--force",
						"--reason=Auto-replaced by gt hook (molecule complete)")
					closeCmd.Stderr = os.Stderr
					if err := closeCmd.Run(); err != nil {
						return fmt.Errorf("closing completed bead %s: %w", existing.ID, err)
					}
				} else {
					// Naked bead - just unpin, don't close (might have value)
					status := "open"
					if err := b.Update(existing.ID, beads.UpdateOptions{Status: &status}); err != nil {
						return fmt.Errorf("unpinning bead %s: %w", existing.ID, err)
					}
				}
			}
		} else if hookForce {
			// Force replace incomplete bead
			fmt.Printf("%s Force-replacing incomplete bead %s...\n", style.Dim.Render("⚠"), existing.ID)
			if !hookDryRun {
				// Unpin by setting status back to open
				status := "open"
				if err := b.Update(existing.ID, beads.UpdateOptions{Status: &status}); err != nil {
					return fmt.Errorf("unpinning bead %s: %w", existing.ID, err)
				}
			}
		} else {
			// Existing incomplete bead blocks new hook
			return fmt.Errorf("existing pinned bead %s is incomplete (%s)\n  Use --force to replace, or complete the existing work first",
				existing.ID, existing.Title)
		}
	}

	fmt.Printf("%s Hooking %s...\n", style.Bold.Render("🪝"), beadID)

	if hookDryRun {
		fmt.Printf("Would run: bd update %s --status=pinned --assignee=%s\n", beadID, agentID)
		if hookSubject != "" {
			fmt.Printf("  subject (for handoff mail): %s\n", hookSubject)
		}
		if hookMessage != "" {
			fmt.Printf("  context (for handoff mail): %s\n", hookMessage)
		}
		return nil
	}

	// Pin the bead using bd update (discovery-based approach)
	pinCmd := exec.Command("bd", "update", beadID, "--status=pinned", "--assignee="+agentID)
	pinCmd.Stderr = os.Stderr
	if err := pinCmd.Run(); err != nil {
		return fmt.Errorf("pinning bead: %w", err)
	}

	fmt.Printf("%s Work attached to hook (pinned bead)\n", style.Bold.Render("✓"))
	fmt.Printf("  Use 'gt handoff' to restart with this work\n")
	fmt.Printf("  Use 'gt hook' to see hook status\n")

	// Log hook event to activity feed
	_ = events.LogFeed(events.TypeHook, agentID, events.HookPayload(beadID))

	return nil
}

// checkPinnedBeadComplete checks if a pinned bead's attached molecule is 100% complete.
// Returns (isComplete, hasAttachment):
// - isComplete=true if no molecule attached OR all molecule steps are closed
// - hasAttachment=true if there's an attached molecule
func checkPinnedBeadComplete(b *beads.Beads, issue *beads.Issue) (isComplete bool, hasAttachment bool) {
	// Check for attached molecule
	attachment := beads.ParseAttachmentFields(issue)
	if attachment == nil || attachment.AttachedMolecule == "" {
		// No molecule attached - consider complete (naked bead)
		return true, false
	}

	// Get progress of attached molecule
	progress, err := getMoleculeProgressInfo(b, attachment.AttachedMolecule)
	if err != nil {
		// Can't determine progress - be conservative, treat as incomplete
		return false, true
	}

	if progress == nil {
		// No steps found - might be a simple issue, treat as complete
		return true, true
	}

	return progress.Complete, true
}


