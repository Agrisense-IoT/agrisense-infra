package runner

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const supabaseRawBase = "https://raw.githubusercontent.com/supabase/supabase/master/docker/volumes/"

// remoteVolumes lists every file that must exist under supabase/volumes/ before
// docker compose starts.  Directories are created automatically as needed.
var remoteVolumes = []string{
	"api/kong.yml",
	"db/_supabase.sql",
	"db/jwt.sql",
	"db/logs.sql",
	"db/pooler.sql",
	"db/realtime.sql",
	"db/roles.sql",
	"db/webhooks.sql",
	"logs/vector.yml",
	"pooler/pooler.exs",
}

// emptyDirs lists directories that only need to exist (bind-mounted as empty).
var emptyDirs = []string{
	"db/data",
	"storage",
	"snippets",
}

// localFiles maps relative paths (under supabase/volumes/) to their content.
// These are created locally rather than downloaded from upstream.
var localFiles = map[string]string{
	"functions/main/index.ts": `import { serve } from "https://deno.land/std@0.177.1/http/server.ts";

const FUNCTION_NAMES = [] as string[];

serve(async (req: Request) => {
  const url = new URL(req.url);
  const fnName = url.pathname.split("/")[1];

  if (!fnName || !FUNCTION_NAMES.includes(fnName)) {
    return new Response(JSON.stringify({ error: "Function not found" }), {
      status: 404,
      headers: { "Content-Type": "application/json" },
    });
  }

  return new Response(JSON.stringify({ error: "Not implemented" }), {
    status: 501,
    headers: { "Content-Type": "application/json" },
  });
});
`,
}

// ensureSupabaseVolumes checks for missing volume files under
// <scriptDir>/supabase/volumes/ and fetches them from the upstream Supabase
// repository.  It returns a channel of status lines so callers can stream
// progress to the UI, and a done channel that closes when the operation is
// complete.  If every file already exists the function returns immediately with
// an empty channel.
func ensureSupabaseVolumes(scriptDir string, progress chan<- string) error {
	volDir := filepath.Join(scriptDir, "supabase", "volumes")

	// Create empty directories unconditionally (no-op if they already exist).
	for _, rel := range emptyDirs {
		dir := filepath.Join(volDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", rel, err)
		}
	}

	// Write local files if missing.
	for rel, content := range localFiles {
		dst := filepath.Join(volDir, filepath.FromSlash(rel))
		if _, err := os.Stat(dst); err == nil {
			continue // already present
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create parent for %s: %w", rel, err)
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}

	// Download any missing files.
	for _, rel := range remoteVolumes {
		dst := filepath.Join(volDir, filepath.FromSlash(rel))
		if _, err := os.Stat(dst); err == nil {
			continue // already present
		}

		send(progress, "Fetching "+rel+" …")

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create parent for %s: %w", rel, err)
		}

		url := supabaseRawBase + rel
		if err := downloadFile(url, dst); err != nil {
			return fmt.Errorf("download %s: %w", rel, err)
		}

		send(progress, "  ✓ "+rel)
	}

	return nil
}

func downloadFile(url, dst string) error {
	resp, err := http.Get(url) //nolint:gosec // URL is a hard-coded constant
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func send(ch chan<- string, msg string) {
	if ch != nil {
		ch <- msg
	}
}
