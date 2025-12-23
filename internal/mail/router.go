package mail

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/tmux"
)

// Router handles message delivery via beads.
// It routes messages to the correct beads database based on address:
// - Town-level (mayor/, deacon/) -> {townRoot}/.beads
// - Rig-level (rig/polecat) -> {townRoot}/{rig}/.beads
type Router struct {
	workDir  string // fallback directory to run bd commands in
	townRoot string // town root directory (e.g., ~/gt)
	tmux     *tmux.Tmux
}

// NewRouter creates a new mail router.
// workDir should be a directory containing a .beads database.
// The town root is auto-detected from workDir if possible.
func NewRouter(workDir string) *Router {
	// Try to detect town root from workDir
	townRoot := detectTownRoot(workDir)

	return &Router{
		workDir:  workDir,
		townRoot: townRoot,
		tmux:     tmux.NewTmux(),
	}
}

// NewRouterWithTownRoot creates a router with an explicit town root.
func NewRouterWithTownRoot(workDir, townRoot string) *Router {
	return &Router{
		workDir:  workDir,
		townRoot: townRoot,
		tmux:     tmux.NewTmux(),
	}
}

// detectTownRoot finds the town root by looking for mayor/town.json.
func detectTownRoot(startDir string) string {
	dir := startDir
	for {
		// Check for primary marker (mayor/town.json)
		markerPath := filepath.Join(dir, "mayor", "town.json")
		if _, err := os.Stat(markerPath); err == nil {
			return dir
		}

		// Move up
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// resolveBeadsDir returns the correct .beads directory for the given address.
//
// Two-level beads architecture:
// - ALL mail uses town beads ({townRoot}/.beads) regardless of address
// - Rig-level beads ({rig}/.beads) are for project issues only, not mail
//
// This ensures messages are visible to all agents in the town.
func (r *Router) resolveBeadsDir(address string) string {
	// If no town root, fall back to workDir's .beads
	if r.townRoot == "" {
		return filepath.Join(r.workDir, ".beads")
	}

	// All mail uses town-level beads
	return filepath.Join(r.townRoot, ".beads")
}

// isTownLevelAddress returns true if the address is for a town-level agent.
func isTownLevelAddress(address string) bool {
	addr := strings.TrimSuffix(address, "/")
	return addr == "mayor" || addr == "deacon"
}

// Send delivers a message via beads message.
// Routes the message to the correct beads database based on recipient address.
func (r *Router) Send(msg *Message) error {
	// Convert addresses to beads identities
	toIdentity := addressToIdentity(msg.To)

	// Build labels for from/thread/reply-to
	var labels []string
	labels = append(labels, "from:"+msg.From)
	if msg.ThreadID != "" {
		labels = append(labels, "thread:"+msg.ThreadID)
	}
	if msg.ReplyTo != "" {
		labels = append(labels, "reply-to:"+msg.ReplyTo)
	}

	// Build command: bd create <subject> --type=message --assignee=<recipient> -d <body>
	args := []string{"create", msg.Subject,
		"--type", "message",
		"--assignee", toIdentity,
		"-d", msg.Body,
	}

	// Add priority flag
	beadsPriority := PriorityToBeads(msg.Priority)
	args = append(args, "--priority", fmt.Sprintf("%d", beadsPriority))

	// Add labels
	if len(labels) > 0 {
		args = append(args, "--labels", strings.Join(labels, ","))
	}

	// Resolve the correct beads directory for the recipient
	beadsDir := r.resolveBeadsDir(msg.To)

	cmd := exec.Command("bd", args...)
	cmd.Env = append(cmd.Environ(),
		"BEADS_DIR="+beadsDir,
	)
	cmd.Dir = filepath.Dir(beadsDir) // Run in parent of .beads

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return errors.New(errMsg)
		}
		return fmt.Errorf("sending message: %w", err)
	}

	// Notify recipient if they have an active session
	// Skip notification for self-mail (handoffs to future-self don't need present-self notified)
	if !isSelfMail(msg.From, msg.To) {
		_ = r.notifyRecipient(msg)
	}

	return nil
}

// isSelfMail returns true if sender and recipient are the same identity.
// Normalizes addresses by removing trailing slashes for comparison.
func isSelfMail(from, to string) bool {
	fromNorm := strings.TrimSuffix(from, "/")
	toNorm := strings.TrimSuffix(to, "/")
	return fromNorm == toNorm
}

// GetMailbox returns a Mailbox for the given address.
// Routes to the correct beads database based on the address.
func (r *Router) GetMailbox(address string) (*Mailbox, error) {
	beadsDir := r.resolveBeadsDir(address)
	workDir := filepath.Dir(beadsDir) // Parent of .beads
	return NewMailboxFromAddress(address, workDir), nil
}

// notifyRecipient sends a notification to a recipient's tmux session.
// Uses send-keys to echo a visible banner to ensure notification is seen.
// Supports mayor/, rig/polecat, and rig/refinery addresses.
func (r *Router) notifyRecipient(msg *Message) error {
	sessionID := addressToSessionID(msg.To)
	if sessionID == "" {
		return nil // Unable to determine session ID
	}

	// Check if session exists
	hasSession, err := r.tmux.HasSession(sessionID)
	if err != nil || !hasSession {
		return nil // No active session, skip notification
	}

	// Send visible notification banner to the terminal
	return r.tmux.SendNotificationBanner(sessionID, msg.From, msg.Subject)
}

// addressToSessionID converts a mail address to a tmux session ID.
// Returns empty string if address format is not recognized.
func addressToSessionID(address string) string {
	// Mayor address: "mayor/" or "mayor"
	if strings.HasPrefix(address, "mayor") {
		return "gt-mayor"
	}

	// Rig-based address: "rig/target"
	parts := strings.SplitN(address, "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return ""
	}

	rig := parts[0]
	target := parts[1]

	// Polecat: gt-rig-polecat
	// Refinery: gt-rig-refinery (if refinery has its own session)
	return fmt.Sprintf("gt-%s-%s", rig, target)
}
