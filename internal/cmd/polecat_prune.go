package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/style"
)

var (
	polecatPruneDryRun bool
	polecatPruneRemote bool
)

var polecatPruneCmd = &cobra.Command{
	Use:   "prune <rig>",
	Short: "Remove polecat branches not associated with active polecats",
	Long: `Remove stale polecat branches (local and optionally remote) that are
no longer associated with active polecats.

A branch is pruned if:
  - No polecat exists for it (worktree directory gone), OR
  - The polecat's state is "done" or "nuked" (work completed)

Active polecats (state=working) are never pruned.

By default, only local branches are pruned. Use --remote to also delete
remote branches on origin.

Examples:
  gt polecat prune gastown                # Prune stale local branches
  gt polecat prune gastown --dry-run      # Show what would be pruned
  gt polecat prune gastown --remote       # Also prune remote branches`,
	Args: cobra.ExactArgs(1),
	RunE: runPolecatPrune,
}

func init() {
	polecatPruneCmd.Flags().BoolVar(&polecatPruneDryRun, "dry-run", false, "Show what would be pruned without deleting")
	polecatPruneCmd.Flags().BoolVar(&polecatPruneRemote, "remote", false, "Also prune remote polecat branches on origin")

	polecatCmd.AddCommand(polecatPruneCmd)
}

func runPolecatPrune(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	mgr, r, err := getPolecatManager(rigName)
	if err != nil {
		return err
	}

	// Get the repo git handle (bare repo or mayor/rig)
	repoGit, err := repoBaseForRig(r)
	if err != nil {
		return fmt.Errorf("finding repo base: %w", err)
	}

	// Get active polecats and their branches
	polecats, err := mgr.List()
	if err != nil {
		return fmt.Errorf("listing polecats: %w", err)
	}

	activeBranches := make(map[string]bool)
	for _, p := range polecats {
		if p.State.IsActive() {
			activeBranches[p.Branch] = true
		}
	}

	// Track results
	var localPruned, localKept int
	var remotePruned, remoteKept int

	// --- Local branches ---
	localBranches, err := repoGit.ListBranches("polecat/*")
	if err != nil {
		return fmt.Errorf("listing local branches: %w", err)
	}

	if polecatPruneDryRun {
		fmt.Printf("Scanning %s for stale polecat branches...\n\n", r.Name)
	} else {
		fmt.Printf("Pruning stale polecat branches in %s...\n\n", r.Name)
	}

	if len(localBranches) > 0 {
		fmt.Printf("%s Local branches:\n", style.Bold.Render("Local"))
		for _, branch := range localBranches {
			if activeBranches[branch] {
				localKept++
				if polecatPruneDryRun {
					fmt.Printf("  %s %s %s\n",
						style.Success.Render("keep"),
						branch,
						style.Dim.Render("(active polecat)"))
				}
				continue
			}

			// Branch is not associated with an active polecat — prune it
			if polecatPruneDryRun {
				fmt.Printf("  %s %s\n",
					style.Warning.Render("prune"),
					branch)
				localPruned++
			} else {
				if err := repoGit.DeleteBranch(branch, true); err != nil {
					fmt.Printf("  %s %s: %v\n", style.Error.Render("fail"), branch, err)
				} else {
					fmt.Printf("  %s %s\n", style.Success.Render("✓"), branch)
					localPruned++
				}
			}
		}
		fmt.Println()
	} else {
		fmt.Printf("No local polecat branches found.\n\n")
	}

	// --- Remote branches ---
	if polecatPruneRemote {
		// First fetch --prune to clean up stale remote tracking refs
		if err := repoGit.FetchPrune("origin"); err != nil {
			fmt.Printf("%s git fetch --prune failed: %v (continuing)\n\n",
				style.Warning.Render("⚠"), err)
		}

		remoteBranches, err := repoGit.ListRemoteBranches("origin/polecat/*")
		if err != nil {
			fmt.Printf("%s listing remote branches: %v\n", style.Warning.Render("⚠"), err)
		} else if len(remoteBranches) > 0 {
			fmt.Printf("%s Remote branches:\n", style.Bold.Render("Remote"))
			for _, remoteBranch := range remoteBranches {
				// Strip "origin/" prefix for comparison and deletion
				branch := strings.TrimPrefix(remoteBranch, "origin/")

				if activeBranches[branch] {
					remoteKept++
					if polecatPruneDryRun {
						fmt.Printf("  %s %s %s\n",
							style.Success.Render("keep"),
							remoteBranch,
							style.Dim.Render("(active polecat)"))
					}
					continue
				}

				// Remote branch is not associated with an active polecat — prune it
				if polecatPruneDryRun {
					fmt.Printf("  %s %s\n",
						style.Warning.Render("prune"),
						remoteBranch)
					remotePruned++
				} else {
					if err := repoGit.DeleteRemoteBranch("origin", branch); err != nil {
						fmt.Printf("  %s %s: %v\n", style.Error.Render("fail"), remoteBranch, err)
					} else {
						fmt.Printf("  %s %s\n", style.Success.Render("✓"), remoteBranch)
						remotePruned++
					}
				}
			}
			fmt.Println()
		} else {
			fmt.Printf("No remote polecat branches found.\n\n")
		}
	}

	// --- Summary ---
	totalPruned := localPruned + remotePruned
	totalKept := localKept + remoteKept

	if polecatPruneDryRun {
		fmt.Printf("%s Would prune %d branch(es), keep %d active\n",
			style.Info.Render("ℹ"), totalPruned, totalKept)
	} else if totalPruned == 0 {
		fmt.Printf("%s No stale branches to prune.\n", style.Bold.Render("✓"))
	} else {
		fmt.Printf("%s Pruned %d branch(es) (kept %d active)\n",
			style.Bold.Render("✓"), totalPruned, totalKept)
	}

	return nil
}

// repoBaseForRig returns a git handle for the rig's repo base (bare repo or mayor/rig).
func repoBaseForRig(r *rig.Rig) (*git.Git, error) {
	// First check for shared bare repo (new architecture)
	bareRepoPath := filepath.Join(r.Path, ".repo.git")
	if info, err := os.Stat(bareRepoPath); err == nil && info.IsDir() {
		return git.NewGitWithDir(bareRepoPath, ""), nil
	}

	// Fall back to mayor/rig (legacy architecture)
	mayorPath := filepath.Join(r.Path, "mayor", "rig")
	if _, err := os.Stat(mayorPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no repo base found (neither .repo.git nor mayor/rig exists)")
	}
	return git.NewGit(mayorPath), nil
}
