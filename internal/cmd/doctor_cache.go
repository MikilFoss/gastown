package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/style"
)

var (
	cacheClean    bool
	cacheDryRun   bool
	cacheMaxAge   int // days
	cacheMaxSize  int // MB
	cacheFull     bool
	cacheForce    bool
)

var doctorCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Diagnose and clean up ~/.claude-cache bloat",
	Long: `Diagnose and clean up Claude's cache directory (~/.claude-cache).

The cache can grow large over time, causing inotify exhaustion and startup
failures (EMFILE). This command helps identify and clean up stale cache data.

Without flags, shows cache diagnosis:
  - Total size and file count
  - Size breakdown by subdirectory
  - Age distribution of cached items
  - Largest entries

With --clean flag, removes stale cache data:
  - Entries older than specified days (default: 7)
  - Or reduce cache to specified size limit

Safety: Refuses to clean while Claude sessions are running (override with --force).`,
	RunE: runDoctorCache,
}

func init() {
	doctorCacheCmd.Flags().BoolVar(&cacheClean, "clean", false, "Clean stale cache entries")
	doctorCacheCmd.Flags().BoolVar(&cacheDryRun, "dry-run", false, "Show what would be cleaned without removing")
	doctorCacheCmd.Flags().IntVar(&cacheMaxAge, "max-age", 7, "Remove entries older than this many days")
	doctorCacheCmd.Flags().IntVar(&cacheMaxSize, "max-size", 0, "Target cache size in MB (0 = no limit)")
	doctorCacheCmd.Flags().BoolVar(&cacheFull, "full", false, "Remove entire cache directory")
	doctorCacheCmd.Flags().BoolVar(&cacheForce, "force", false, "Force cleanup even with active Claude sessions")

	doctorCmd.AddCommand(doctorCacheCmd)
}

// CacheEntry represents a file or directory in the cache
type CacheEntry struct {
	Path    string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

// CacheStats holds aggregate cache statistics
type CacheStats struct {
	TotalSize   int64
	TotalFiles  int
	TotalDirs   int
	ByDir       map[string]int64          // subdirectory -> size
	ByAge       map[string]int64          // age bucket -> size
	Largest     []CacheEntry              // largest entries
	Stale       []CacheEntry              // entries older than threshold
}

func runDoctorCache(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	cacheDir := filepath.Join(home, ".claude-cache")

	// Check if cache exists
	info, err := os.Stat(cacheDir)
	if os.IsNotExist(err) {
		fmt.Println("Cache directory does not exist (~/.claude-cache)")
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking cache directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("~/.claude-cache is not a directory")
	}

	// Check for active Claude sessions
	activeSessions := getActiveClaudeSessions()
	if len(activeSessions) > 0 && cacheClean && !cacheForce {
		style.PrintWarning("Active Claude sessions detected:")
		for _, s := range activeSessions {
			fmt.Printf("  - %s\n", s)
		}
		fmt.Println()
		fmt.Println("Cleaning cache while Claude is running may cause issues.")
		fmt.Println("Use --force to clean anyway, or stop sessions first.")
		return nil
	}

	// Collect stats
	stats := collectCacheStats(cacheDir, cacheMaxAge)

	if cacheClean || cacheFull {
		return cleanCache(cacheDir, stats, cacheDryRun, cacheFull)
	}

	// Display diagnosis
	displayCacheStats(stats)
	return nil
}

func collectCacheStats(cacheDir string, maxAgeDays int) *CacheStats {
	stats := &CacheStats{
		ByDir: make(map[string]int64),
		ByAge: make(map[string]int64),
	}

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)

	// First pass: collect top-level directory sizes
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return stats
	}

	for _, entry := range entries {
		path := filepath.Join(cacheDir, entry.Name())
		if entry.IsDir() {
			stats.TotalDirs++
			size := getDirSize(path)
			stats.ByDir[entry.Name()] = size
			stats.TotalSize += size

			// Collect stale entries from cleanable directories
			if isCleanableDir(entry.Name()) {
				collectStaleEntries(path, cutoff, stats)
			}
		} else {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			stats.TotalFiles++
			stats.TotalSize += info.Size()

			// Check if stale
			if info.ModTime().Before(cutoff) {
				stats.Stale = append(stats.Stale, CacheEntry{
					Path:    path,
					Size:    info.Size(),
					ModTime: info.ModTime(),
					IsDir:   false,
				})
			}
		}
	}

	// Age distribution (scan all files)
	filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		stats.TotalFiles++
		age := time.Since(info.ModTime())
		bucket := getAgeBucket(age)
		stats.ByAge[bucket] += info.Size()

		// Track largest files
		stats.Largest = append(stats.Largest, CacheEntry{
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	})

	// Sort largest by size descending
	sort.Slice(stats.Largest, func(i, j int) bool {
		return stats.Largest[i].Size > stats.Largest[j].Size
	})

	// Keep top 10 largest
	if len(stats.Largest) > 10 {
		stats.Largest = stats.Largest[:10]
	}

	return stats
}

func collectStaleEntries(dir string, cutoff time.Time, stats *CacheStats) {
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			stats.Stale = append(stats.Stale, CacheEntry{
				Path:    path,
				Size:    info.Size(),
				ModTime: info.ModTime(),
				IsDir:   info.IsDir(),
			})
		}
		return nil
	})
}

func displayCacheStats(stats *CacheStats) {
	fmt.Println(style.Bold.Render("Cache Diagnosis: ~/.claude-cache"))
	fmt.Println()

	// Summary
	fmt.Printf("Total size: %s\n", formatSize(stats.TotalSize))
	fmt.Printf("Files: %d, Directories: %d\n", stats.TotalFiles, stats.TotalDirs)
	fmt.Println()

	// Size by directory
	fmt.Println(style.Bold.Render("Size by Directory:"))
	type dirSize struct {
		name string
		size int64
	}
	var dirs []dirSize
	for name, size := range stats.ByDir {
		dirs = append(dirs, dirSize{name, size})
	}
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].size > dirs[j].size
	})
	for _, d := range dirs {
		pct := float64(d.size) / float64(stats.TotalSize) * 100
		fmt.Printf("  %-20s %10s (%5.1f%%)\n", d.name, formatSize(d.size), pct)
	}
	fmt.Println()

	// Age distribution
	fmt.Println(style.Bold.Render("Age Distribution:"))
	ageBuckets := []string{"<1 day", "1-7 days", "7-30 days", ">30 days"}
	for _, bucket := range ageBuckets {
		size := stats.ByAge[bucket]
		if size > 0 {
			pct := float64(size) / float64(stats.TotalSize) * 100
			fmt.Printf("  %-12s %10s (%5.1f%%)\n", bucket, formatSize(size), pct)
		}
	}
	fmt.Println()

	// Largest entries
	if len(stats.Largest) > 0 {
		fmt.Println(style.Bold.Render("Largest Files:"))
		for i, entry := range stats.Largest {
			if i >= 5 {
				break
			}
			relPath, _ := filepath.Rel(os.Getenv("HOME"), entry.Path)
			fmt.Printf("  %10s  ~/%s\n", formatSize(entry.Size), relPath)
		}
		fmt.Println()
	}

	// Stale entries summary
	var staleSize int64
	for _, entry := range stats.Stale {
		if !entry.IsDir {
			staleSize += entry.Size
		}
	}
	if staleSize > 0 {
		fmt.Println(style.Dim.Render(fmt.Sprintf("Stale data (>%d days): %s", cacheMaxAge, formatSize(staleSize))))
		fmt.Println(style.Dim.Render("Run 'gt doctor cache --clean' to remove stale entries"))
	}
}

func cleanCache(cacheDir string, stats *CacheStats, dryRun, full bool) error {
	if full {
		return cleanFullCache(cacheDir, dryRun)
	}
	return cleanStaleEntries(cacheDir, stats, dryRun)
}

func cleanFullCache(cacheDir string, dryRun bool) error {
	fmt.Printf("Removing entire cache directory: %s\n", cacheDir)

	if dryRun {
		fmt.Println(style.Dim.Render("(dry-run: no files removed)"))
		return nil
	}

	// Create manifest before deletion
	manifest := createManifest(cacheDir)
	manifestPath := filepath.Join(os.TempDir(), fmt.Sprintf("claude-cache-manifest-%d.json", time.Now().Unix()))
	if data, err := json.MarshalIndent(manifest, "", "  "); err == nil {
		os.WriteFile(manifestPath, data, 0644)
		fmt.Printf("Manifest saved to: %s\n", manifestPath)
	}

	if err := os.RemoveAll(cacheDir); err != nil {
		return fmt.Errorf("removing cache: %w", err)
	}

	fmt.Println(style.Success.Render("Cache cleared successfully"))
	return nil
}

func cleanStaleEntries(cacheDir string, stats *CacheStats, dryRun bool) error {
	if len(stats.Stale) == 0 {
		fmt.Println("No stale entries to clean")
		return nil
	}

	var totalCleaned int64
	var filesCleaned int

	// Group stale entries by parent directory for efficient cleanup
	staleDirs := make(map[string]bool)
	for _, entry := range stats.Stale {
		if entry.IsDir {
			staleDirs[entry.Path] = true
		}
	}

	// Clean stale directories first (removes all contents)
	for dir := range staleDirs {
		// Only clean directories in cleanable subdirectories
		rel, _ := filepath.Rel(cacheDir, dir)
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) > 0 && isCleanableDir(parts[0]) {
			size := getDirSize(dir)
			if dryRun {
				fmt.Printf("Would remove: %s (%s)\n", dir, formatSize(size))
			} else {
				if err := os.RemoveAll(dir); err == nil {
					totalCleaned += size
					filesCleaned++
				}
			}
		}
	}

	// Clean remaining stale files
	for _, entry := range stats.Stale {
		if entry.IsDir {
			continue // Already handled
		}
		// Check if parent was already removed
		parentRemoved := false
		for dir := range staleDirs {
			if strings.HasPrefix(entry.Path, dir) {
				parentRemoved = true
				break
			}
		}
		if parentRemoved {
			continue
		}

		if dryRun {
			fmt.Printf("Would remove: %s (%s)\n", entry.Path, formatSize(entry.Size))
		} else {
			if err := os.Remove(entry.Path); err == nil {
				totalCleaned += entry.Size
				filesCleaned++
			}
		}
	}

	if dryRun {
		fmt.Printf("\n%s\n", style.Dim.Render("(dry-run: no files removed)"))
	} else {
		fmt.Printf("\nCleaned %d entries, freed %s\n", filesCleaned, formatSize(totalCleaned))
	}

	return nil
}

func createManifest(cacheDir string) map[string]interface{} {
	manifest := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"cache_dir": cacheDir,
		"entries":   []string{},
	}

	var entries []string
	filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(cacheDir, path)
		entries = append(entries, rel)
		return nil
	})
	manifest["entries"] = entries
	manifest["entry_count"] = len(entries)

	return manifest
}

func getActiveClaudeSessions() []string {
	// Check for running claude processes
	cmd := exec.Command("pgrep", "-f", "claude")
	output, err := cmd.Output()
	if err != nil {
		return nil // No claude processes
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var sessions []string
	for _, line := range lines {
		if line != "" {
			sessions = append(sessions, fmt.Sprintf("PID %s", line))
		}
	}
	return sessions
}

func getDirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

func getAgeBucket(age time.Duration) string {
	switch {
	case age < 24*time.Hour:
		return "<1 day"
	case age < 7*24*time.Hour:
		return "1-7 days"
	case age < 30*24*time.Hour:
		return "7-30 days"
	default:
		return ">30 days"
	}
}

func isCleanableDir(name string) bool {
	// Directories safe to clean stale entries from
	cleanable := map[string]bool{
		"debug":           true,
		"shell-snapshots": true,
		"session-env":     true,
		"telemetry":       true,
		"file-history":    true,
	}
	return cleanable[name]
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1fGB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1fMB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1fKB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
