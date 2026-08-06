// Command whetstone is a reading and argument-building tool that keeps you in
// the loop rather than out of it.
//
//	whetstone -set-key                  # store your OpenAI key, once
//	whetstone -check                    # verify key, endpoint, and model
//	whetstone -web                      # browser UI (paste documents, write)
//	whetstone                           # terminal UI (read, provoke, resolve)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/tanisha327/whetstone/internal/doc"
	"github.com/tanisha327/whetstone/internal/provider"
	"github.com/tanisha327/whetstone/internal/tui"
	"github.com/tanisha327/whetstone/internal/web"
	"github.com/tanisha327/whetstone/internal/workspace"
)

// buildVersion is set via -ldflags at release time; otherwise it comes from the
// module's build info.
var buildVersion = ""

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "whetstone: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	var (
		flagWeb       = flag.Bool("web", false, "serve the browser UI instead of the terminal UI")
		flagPort      = flag.Int("port", 0, "port for -web (default: any free port)")
		flagNoOpen    = flag.Bool("no-open", false, "with -web, print the URL instead of opening a browser")
		flagWorkspace = flag.String("w", "", "workspace file (default: whetstone.json in the working directory)")
		flagQuestion  = flag.String("question", "", "the question you are trying to answer")
		flagKeyFile   = flag.String("key-file", "", "read the API key from this file for this run")
		flagSetKey    = flag.Bool("set-key", false, "prompt for an API key, store it, and exit")
		flagDeleteKey = flag.Bool("delete-key", false, "remove the stored API key and exit")
		flagCheck     = flag.Bool("check", false, "verify the key, endpoint, and model with one request, then exit")
		flagVersion   = flag.Bool("version", false, "print version and exit")
	)
	flag.Usage = usage
	flag.Parse()

	switch {
	case *flagVersion:
		fmt.Println(version())
		return nil
	case *flagSetKey:
		return setKey()
	case *flagDeleteKey:
		return deleteKey()
	}

	prov, err := buildProvider(*flagKeyFile)
	if err != nil {
		return err
	}
	if *flagCheck {
		return check(prov)
	}

	wsPath := *flagWorkspace
	if wsPath == "" {
		wsPath = "whetstone.json"
	}
	abs, err := filepath.Abs(wsPath)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", wsPath, err)
	}

	// A corrupt workspace is fatal on purpose. Starting empty and saving over it
	// would destroy the user's work to save them one error message.
	ws, err := workspace.Load(abs)
	if err != nil {
		return err
	}

	for _, path := range flag.Args() {
		d, err := doc.Load(path)
		if err != nil {
			return err
		}
		if len(d.Sections) == 0 {
			fmt.Fprintf(os.Stderr, "whetstone: %s is empty, skipping\n", path)
			continue
		}
		ws.AddDocument(d)
	}
	if *flagQuestion != "" {
		ws.Question = *flagQuestion
	}

	if *flagWeb {
		return serveWeb(ws, prov, *flagPort, *flagNoOpen)
	}
	return runTUI(ws, prov)
}

// serveWeb runs the browser UI. It blocks until the process is interrupted;
// every mutation has already been persisted by the time a request returns, so
// there is nothing to save on the way out.
func serveWeb(ws *workspace.Workspace, prov provider.Provider, port int, noOpen bool) error {
	s, err := web.NewServer(ws, prov)
	if err != nil {
		return err
	}
	ln, err := s.Listen(port)
	if err != nil {
		return err
	}
	url := s.URL(ln)

	fmt.Println("Whetstone is running at:")
	fmt.Println("    " + url)
	fmt.Println()
	fmt.Println("Workspace: " + ws.Path())
	fmt.Println("Press ctrl+c to stop.")

	if !noOpen {
		if err := web.OpenBrowser(url); err != nil {
			fmt.Fprintln(os.Stderr, "could not open a browser automatically; use the URL above")
		}
	}
	return http.Serve(ln, s)
}

func runTUI(ws *workspace.Workspace, prov provider.Provider) error {
	if len(ws.Documents) == 0 {
		fmt.Fprintln(os.Stderr,
			"whetstone: no documents in this workspace.\n"+
				"    whetstone report.md      load a file\n"+
				"    whetstone -web           paste one in the browser")
	}
	p := tea.NewProgram(tui.New(ws, prov), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	// The TUI saves on quit, but save again here: losing a session's thinking
	// silently is the worst outcome this program has.
	if err := ws.Save(); err != nil {
		return fmt.Errorf("saving workspace: %w", err)
	}
	fmt.Println("saved " + ws.Path())
	return nil
}

// buildProvider constructs the OpenAI client, turning a missing credential into
// setup instructions rather than an opaque failure on the first request.
func buildProvider(keyFile string) (provider.Provider, error) {
	cfg := provider.OpenAIConfig{}
	if keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("reading -key-file %s: %w", keyFile, err)
		}
		cfg.APIKey = string(data)
	}

	p, err := provider.NewOpenAI(cfg)
	if errors.Is(err, provider.ErrNoCredential) {
		path, pathErr := provider.KeyPath()
		if pathErr != nil {
			path = "<config dir>/whetstone/credentials"
		}
		return nil, fmt.Errorf(
			"no API key found.\n\n"+
				"Set one up once:\n"+
				"    whetstone -set-key\n\n"+
				"Or export it for this shell:\n"+
				"    export %s=sk-...\n\n"+
				"Checked, in order: -key-file, $%s, %s",
			provider.EnvAPIKey, provider.EnvAPIKey, path)
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// setKey prompts for a key and stores it. Input is not echoed when stdin is a
// terminal; when it is a pipe the key is read as a line, so
// `... | whetstone -set-key` works in a provisioning script.
func setKey() error {
	var raw string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Paste your OpenAI API key (input hidden): ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("reading key: %w", err)
		}
		raw = string(b)
	} else if _, err := fmt.Fscanln(os.Stdin, &raw); err != nil {
		return fmt.Errorf("reading key from stdin: %w", err)
	}

	path, err := provider.SaveKey(raw)
	if err != nil {
		return err
	}
	fmt.Printf("Stored %s in %s (mode 0600).\n", provider.Fingerprint(raw), path)
	fmt.Println("Verify it with: whetstone -check")
	return nil
}

func deleteKey() error {
	path, err := provider.DeleteKey()
	if err != nil {
		return err
	}
	fmt.Println("Removed " + path)
	if os.Getenv(provider.EnvAPIKey) != "" {
		fmt.Fprintf(os.Stderr,
			"note: $%s is still set in this shell and will still be used.\n",
			provider.EnvAPIKey)
	}
	return nil
}

// check makes one minimal request so a misconfigured key, endpoint, or model
// fails here with a clear message rather than in the middle of a session.
func check(p provider.Provider) error {
	if openai, ok := p.(*provider.OpenAI); ok {
		fmt.Printf("credential  %s\n", provider.CredentialSource())
		fmt.Printf("endpoint    %s\n", openai.BaseURL())
		fmt.Printf("model       %s\n", openai.Model())
	}
	fmt.Printf("request     sending...\n")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := p.Complete(ctx, provider.Request{
		Purpose:   provider.PurposeCheck,
		System:    "Reply with the single word OK.",
		Messages:  []provider.Message{{Role: provider.RoleUser, Text: "ping"}},
		MaxTokens: 5,
	})
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return fmt.Errorf("check failed after %s: %w", elapsed, err)
	}

	fmt.Printf("            ok in %s\n", elapsed)
	fmt.Printf("served by   %s\n", resp.Model)
	fmt.Printf("tokens      %d in, %d out\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)
	fmt.Printf("reply       %q\n", strings.TrimSpace(resp.Text))
	return nil
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprint(out, `whetstone - a tool that makes you think, not one that thinks for you.

Usage:
    whetstone [flags] [document...]

Documents are markdown or plain text, split into sections you read yourself.
The model provides lenses (where to look) and provocations (objections to
answer), never a finished opinion.

Two front ends over the same workspace file:
    whetstone -web    browser: paste documents, write notes and drafts
    whetstone         terminal: read, apply lenses, provoke, resolve

First run:
    whetstone -set-key      store your OpenAI key
    whetstone -check        verify it works

Flags:
`)
	flag.PrintDefaults()
	fmt.Fprint(out, `
Credential resolution, in order:
    -key-file <path>
    $`+provider.EnvAPIKey+`
    the key file written by -set-key

Environment:
    `+provider.EnvAPIKey+`        API key. Never written to disk by a running session.
    `+provider.EnvBaseURL+`    OpenAI-compatible endpoint (default `+provider.DefaultBaseURL+`).
    `+provider.EnvModel+`       Model name (default `+provider.DefaultModel+`).
    `+provider.EnvKeyFile+`    Override the key file location.

Examples:
    whetstone -check
    whetstone -web
    whetstone -question "Should we adopt the new scheduler?" report.md
`)
}

func version() string {
	if buildVersion != "" {
		return buildVersion
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				rev = s.Value[:7]
			}
		case "vcs.modified":
			if s.Value == "true" {
				modified = "-dirty"
			}
		}
	}
	if rev == "" {
		return "dev"
	}
	return strings.TrimSpace(rev + modified)
}
