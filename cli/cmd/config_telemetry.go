package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// telemetryStatusStyles mirrors output_text.go's renderer pattern:
// lipgloss.NewRenderer(w) detects TTY support, so the same code
// produces colored output on a terminal and plain ASCII when piped
// or captured into a bytes.Buffer for tests.
type telemetryStatusStyles struct {
	label    lipgloss.Style
	enabled  lipgloss.Style
	disabled lipgloss.Style
	muted    lipgloss.Style
	errText  lipgloss.Style
}

func newTelemetryStatusStyles(w io.Writer) telemetryStatusStyles {
	r := lipgloss.NewRenderer(w)
	return telemetryStatusStyles{
		label:    r.NewStyle().Foreground(lipgloss.Color("6")).Bold(true),
		enabled:  r.NewStyle().Foreground(lipgloss.Color("2")),
		disabled: r.NewStyle().Foreground(lipgloss.Color("3")),
		muted:    r.NewStyle().Faint(true),
		errText:  r.NewStyle().Foreground(lipgloss.Color("9")),
	}
}

func newConfigTelemetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Show or change telemetry settings",
		Long: "Telemetry is anonymous usage data gaffer sends to Kurrent so we can\n" +
			"understand which features people use. It is opt-out: enabled by\n" +
			"default. See TELEMETRY.md (and `gaffer config telemetry status`) for\n" +
			"exactly what is collected and how to turn it off.",
	}
	cmd.AddCommand(newConfigTelemetryStatusCmd())
	cmd.AddCommand(newConfigTelemetryOnCmd())
	cmd.AddCommand(newConfigTelemetryOffCmd())
	return cmd
}

func newConfigTelemetryStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current telemetry configuration",
		Long: "Print the current telemetry state, broken down by source. Use this\n" +
			"to find which layer (user config, environment variable, or workspace\n" +
			"gaffer.toml) is enabling or disabling telemetry for this invocation.\n" +
			"\n" +
			"Always exits 0.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigTelemetryStatus(cmd.OutOrStdout())
		},
	}
}

func newConfigTelemetryOnCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "on",
		Short: "Enable telemetry on this machine",
		Long: "Set the user-level telemetry preference to enabled.\n" +
			"\n" +
			"If telemetry isn't already in active use, this mints a fresh per-\n" +
			"install id and prints a one-time disclosure notice. Existing\n" +
			"environment-variable or workspace opt-outs still take precedence;\n" +
			"the command surfaces them so you know what else to change.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigTelemetryOn(cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

func newConfigTelemetryOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Disable telemetry on this machine",
		Long: "Set the user-level telemetry preference to disabled and clear the\n" +
			"per-install id and salt. Prints the cleared id one last time so you\n" +
			"can capture it for a deletion request (email privacy@kurrent.io).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigTelemetryOff(cmd.OutOrStdout())
		},
	}
}

// runConfigTelemetryStatus renders the per-layer breakdown plus the
// persisted telemetry id. Always exits 0; layer-level errors (parse
// failures) are surfaced inline so the user sees what to fix.
func runConfigTelemetryStatus(out io.Writer) error {
	store, err := loadUserStore()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()

	id, _ := telemetry.ResolveIdentity(store)
	r := telemetry.CheckOptOut(store, cwd, home)
	renderTelemetryStatus(out, id, r)
	return nil
}

func renderTelemetryStatus(out io.Writer, id telemetry.Identity, r telemetry.Resolved) {
	s := newTelemetryStatusStyles(out)

	idLine := s.muted.Render("none")
	if !id.IsZero() {
		idLine = id.TelemetryID
	}
	fmt.Fprintf(out, "%s         %s\n", s.label.Render("id:"), idLine)

	overall := s.enabled.Render("enabled")
	if r.IsDisabled() {
		overall = s.disabled.Render("disabled")
	}
	fmt.Fprintf(out, "%s  %s\n", s.label.Render("telemetry:"), overall)
	fmt.Fprintf(out, "  %s       %s\n", s.label.Render("user:"), renderUserLayer(s, r.User))
	fmt.Fprintf(out, "  %s        %s\n", s.label.Render("env:"), renderEnvLayer(s, r.Env))
	fmt.Fprintf(out, "  %s  %s\n", s.label.Render("workspace:"), renderWorkspaceLayer(s, r.Workspace))
}

func renderUserLayer(s telemetryStatusStyles, l telemetry.Layer) string {
	if l.Err != nil {
		return fmt.Sprintf("%s %s", s.muted.Render("unset"), s.errText.Render(fmt.Sprintf("(error: %v)", l.Err)))
	}
	switch l.State {
	case telemetry.LayerEnabled:
		return s.enabled.Render("enabled")
	case telemetry.LayerDisabled:
		return s.disabled.Render("disabled")
	default:
		return s.muted.Render("unset")
	}
}

func renderEnvLayer(s telemetryStatusStyles, l telemetry.Layer) string {
	if l.State == telemetry.LayerDisabled {
		return fmt.Sprintf("%s %s", s.disabled.Render("disabled"), s.muted.Render("("+l.EnvVar+")"))
	}
	return s.muted.Render("not set")
}

func renderWorkspaceLayer(s telemetryStatusStyles, l telemetry.Layer) string {
	if l.Err != nil {
		return s.errText.Render(fmt.Sprintf("error: %v", l.Err))
	}
	switch l.State {
	case telemetry.LayerEnabled:
		return fmt.Sprintf("%s %s", s.enabled.Render("enabled"), s.muted.Render("(telemetry=true in "+l.Path+")"))
	case telemetry.LayerDisabled:
		return fmt.Sprintf("%s %s", s.disabled.Render("disabled"), s.muted.Render("(telemetry=false in "+l.Path+")"))
	default:
		return s.muted.Render("not set")
	}
}

// runConfigTelemetryOn sets user-level enabled=true. If no other
// layer is opting out, this is also when first-mint fires - the
// notice goes to noticeOut (typically stderr).
//
// A malformed [telemetry] section is not a hard error here: `on`
// is itself the recovery path for a broken config. We surface the
// parse error as a warning and rewrite the section cleanly; any
// unrecoverable prior id is replaced by a fresh mint downstream.
func runConfigTelemetryOn(out, noticeOut io.Writer) error {
	store, err := loadUserStore()
	if err != nil {
		return err
	}
	t, loadErr := telemetry.LoadTelemetry(store)
	if loadErr != nil {
		fmt.Fprintf(noticeOut, "warning: prior telemetry config unreadable: %v\n", loadErr)
		fmt.Fprintln(noticeOut, "rewriting [telemetry] section from scratch")
	}
	on := true
	t.Enabled = &on
	telemetry.WriteTelemetry(store, t)
	if err := store.Save(); err != nil {
		return fmt.Errorf("save user config: %w", err)
	}

	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	r := telemetry.CheckOptOut(store, cwd, home)

	if r.IsDisabled() {
		// User-level preference is now enabled, but another layer
		// vetoes. Tell the user exactly what's still blocking.
		fmt.Fprintln(out, "Telemetry enabled in user config.")
		fmt.Fprintln(out, "However, telemetry is still disabled by:")
		if r.Env.State == telemetry.LayerDisabled {
			fmt.Fprintf(out, "  - environment variable %s\n", r.Env.EnvVar)
		}
		if r.Workspace.State == telemetry.LayerDisabled {
			fmt.Fprintf(out, "  - workspace gaffer.toml: %s\n", r.Workspace.Path)
		}
		return nil
	}

	// Not opted out. EnsureIdentity mints and prints the notice on
	// first run. If we already had an identity, no notice is
	// printed. suppressNotice=false: this is an explicit user opt-in
	// at the terminal, so disclosure-via-stderr is exactly right.
	if _, err := telemetry.EnsureIdentity(store, r, noticeOut, false); err != nil {
		// EnsureIdentity returned a partial-load warning alongside
		// a usable identity (e.g. malformed enabled key in an
		// otherwise-valid section). Preference is saved; surface
		// the warning, don't exit non-zero.
		fmt.Fprintln(out, "Telemetry enabled.")
		fmt.Fprintf(out, "Warning: %v\n", err)
		return nil
	}
	fmt.Fprintln(out, "Telemetry enabled.")
	return nil
}

// runConfigTelemetryOff sets user-level enabled=false and clears the
// persisted id/salt. The cleared id is printed one last time for
// RTBF disclosure: emails to privacy@kurrent.io reference this id.
//
// If the prior config didn't parse, we still proceed (off is the
// recovery path) but tell the user we couldn't recover their id -
// they'll need to remember it themselves for a deletion request.
func runConfigTelemetryOff(out io.Writer) error {
	store, err := loadUserStore()
	if err != nil {
		return err
	}
	prev, _, prevErr := telemetry.IdentityFromConfig(store)

	off := false
	t, _ := telemetry.LoadTelemetry(store)
	t.Enabled = &off
	telemetry.WriteTelemetry(store, t)
	telemetry.ClearIdentity(store)
	if err := store.Save(); err != nil {
		return fmt.Errorf("save user config: %w", err)
	}

	fmt.Fprintln(out, "Telemetry disabled.")
	switch {
	case !prev.IsZero():
		fmt.Fprintf(out, "Your previous telemetry id was: %s\n", prev.TelemetryID)
		fmt.Fprintln(out, "Email privacy@kurrent.io with that id to delete past events.")
	case prevErr != nil:
		fmt.Fprintf(out, "Your previous telemetry id couldn't be recovered (config parse error: %v).\n", prevErr)
		fmt.Fprintln(out, "If you remember it, email privacy@kurrent.io to request deletion.")
	}
	return nil
}

// loadUserStore is the shared "open the user config" helper for
// every config subcommand: DefaultDir + Load, with error wrapping
// that names the path so users know where to look.
func loadUserStore() (*userconfig.Store, error) {
	dir, err := userconfig.DefaultDir()
	if err != nil {
		return nil, fmt.Errorf("locate user config dir: %w", err)
	}
	store, err := userconfig.Load(dir)
	if err != nil {
		return nil, fmt.Errorf("load user config: %w", err)
	}
	return store, nil
}
