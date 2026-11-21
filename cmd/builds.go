package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/juju/ansiterm"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
	"github.com/valar/cli/util"
	"golang.org/x/term"
)

var (
	buildService  string
	buildPushKind string
)

// validateServiceKind rejects a bad kind before uploading an artifact, since
// the upload is what creates the service.
func validateServiceKind(kind string) error {
	switch kind {
	case "", "function", "batch":
		return nil
	default:
		return newError(kindInvalid, "unknown service kind %q, want \"function\" or \"batch\"", kind)
	}
}

var buildCmd = &cobra.Command{
	Use:     "build [--service service]",
	Short:   "Manage the builds of a service.",
	Aliases: []string{"builds", "b"},
}

var buildListCmd = &cobra.Command{
	Use:   "list [prefix]",
	Short: "List builds of the service.",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &buildService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		if len(args) < 1 {
			args = append(args, "")
		}
		return listBuilds(client, cfg, args[0])
	}),
}

var buildAbortCmd = &cobra.Command{
	Use:   "abort [prefix]",
	Short: "Abort a scheduled or running build.",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &buildService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		if len(args) < 1 {
			args = append(args, "")
		}
		if err := client.AbortBuild(cfg.Project(), cfg.Service(), args[0]); err != nil {
			return err
		}
		return result(struct {
			Build   string `json:"build"`
			Aborted bool   `json:"aborted"`
		}{args[0], true}, func(io.Writer) error { return nil })
	}),
}

var logsFollow = false
var logsRaw = false

func formatLogEntry(logEntry *api.LogEntry, terminalWidth int) string {
	line := &strings.Builder{}

	timestampPrefix := []rune(fmt.Sprintf("│ %s │ ", logEntry.Timestamp.Format(time.RFC3339)))
	colorWrapper := color.WhiteString
	contextPrefix := []rune{}
	switch logEntry.Source {
	case api.LogEntrySourceUnspecified:
	case api.LogEntrySourceWrapper:
		switch logEntry.Stage {
		case api.LogEntryStageUnspecified:
			colorWrapper = color.WhiteString
			contextPrefix = []rune("→ ")
		case api.LogEntryStageSetup:
			colorWrapper = color.GreenString
			contextPrefix = []rune("setup ↗ ")
		case api.LogEntryStageTurndown:
			colorWrapper = color.YellowString
			contextPrefix = []rune("turndown ↘ ")
		}
	case api.LogEntrySourceProcess:
		colorWrapper = color.WhiteString
		contextPrefix = []rune("")
	}

	line.WriteString(color.HiBlackString(string(timestampPrefix)))
	line.WriteString(colorWrapper(string(contextPrefix)))

	// Without a known width (e.g. output is not a terminal) don't wrap at all.
	if terminalWidth <= 0 {
		line.WriteString(colorWrapper(logEntry.Content))
		return line.String()
	}

	contentRunes := []rune(logEntry.Content)
	runeBlockLen := max(1, terminalWidth-len(timestampPrefix)-len(contextPrefix))
	for i := 0; i <= len(contentRunes)/runeBlockLen; i++ {
		if i != 0 {
			line.WriteString(color.HiBlackString(string(timestampPrefix)))
			for j := 0; j < len(contextPrefix); j++ {
				line.WriteRune(' ')
			}
		}
		line.WriteString(colorWrapper(string(contentRunes[i*runeBlockLen : min((i+1)*runeBlockLen, len(contentRunes))])))
	}
	return line.String()
}

var buildLogsCmd = &cobra.Command{
	Use:   "logs [buildid]",
	Short: "Show the build logs of the given task.",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &buildService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		// Get latest matching build
		prefix := ""
		if len(args) > 0 {
			prefix = args[0]
		}
		latestBuildID, err := latestBuild(client, cfg, prefix)
		if err != nil {
			return err
		}
		width, _, _ := term.GetSize(int(os.Stdout.Fd()))
		consumer := func(le api.LogEntry) {
			switch {
			case outputJSON:
				writeJSONLine(stdout, le)
			case logsRaw:
				fmt.Println(le.Content)
			default:
				fmt.Println(formatLogEntry(&le, width))
			}
		}
		if logsFollow {
			return client.StreamBuildLogs(cfg.Project(), cfg.Service(), latestBuildID, consumer)
		}
		return client.ShowBuildLogs(cfg.Project(), cfg.Service(), latestBuildID, consumer)
	}),
}

var buildWatchCmd = &cobra.Command{
	Use:   "watch [prefix]",
	Short: "Watch a build until its completion.",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &buildService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		// Get latest matching build
		prefix := ""
		if len(args) > 0 {
			prefix = args[0]
		}
		latestBuildID, err := latestBuild(client, cfg, prefix)
		if err != nil {
			return err
		}

		if outputJSON {
			return watchBuildJSON(client, cfg, latestBuildID)
		}
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			return watchBuildPlain(client, cfg, latestBuildID)
		}

		terminalWidth, terminalHeight, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			return watchBuildPlain(client, cfg, latestBuildID)
		}
		bar := progressbar.NewOptions(-1, progressbar.OptionEnableColorCodes(true), progressbar.OptionSpinnerType(3), progressbar.OptionSetElapsedTime(false), progressbar.OptionSetMaxDetailRow(max(1, terminalHeight-2)), progressbar.OptionFullWidth())
		build, err := client.InspectBuild(cfg.Project(), cfg.Service(), latestBuildID)
		if err != nil {
			return err
		}

		switch build.Status {
		case "scheduled":
			bar.Describe("Scheduling build onto worker ...")
		}

		if err := client.StreamBuildLogs(cfg.Project(), cfg.Service(), latestBuildID, func(le api.LogEntry) {
			switch le.Stage {
			case api.LogEntryStageUnspecified:
				bar.Describe("Processing ...")
			case api.LogEntryStageSetup:
				bar.Describe("Setting up build environment ...")
			case api.LogEntryStageTurndown:
				bar.Describe("Turning down build environment ...")
			}
			bar.AddDetail(formatLogEntry(&le, terminalWidth))
			fmt.Printf("\n\033[1A\033[K")
			bar.RenderBlank()
		}); err != nil {
			bar.Finish()
			fmt.Println()
			return fmt.Errorf("streaming build logs: %w", err)
		}

		build, err = client.InspectBuild(cfg.Project(), cfg.Service(), latestBuildID)
		if err != nil {
			return err
		}
		switch build.Status {
		case "done":
			bar.Describe(color.GreenString("Build has succeeded."))
		case "failed":
			bar.Describe(color.RedString("Build has failed."))
		}
		bar.Finish()
		fmt.Println()

		return buildOutcome(build)
	}),
}

// latestBuild returns the ID of the newest build matching prefix.
func latestBuild(client *api.Client, cfg config.ServiceConfig, prefix string) (string, error) {
	builds, err := client.ListBuilds(cfg.Project(), cfg.Service(), prefix)
	if err != nil {
		return "", err
	}
	if len(builds) == 0 {
		return "", newError(kindNotFound, "no builds available")
	}
	sort.Slice(builds, func(i, j int) bool { return builds[i].CreatedAt.After(builds[j].CreatedAt) })
	return builds[0].ID, nil
}

// watchBuildJSON streams the build log as JSON lines; the exit code tells the
// outcome.
func watchBuildJSON(client *api.Client, cfg config.ServiceConfig, buildID string) error {
	if err := client.StreamBuildLogs(cfg.Project(), cfg.Service(), buildID, func(le api.LogEntry) {
		writeJSONLine(stdout, le)
	}); err != nil {
		return fmt.Errorf("streaming build logs: %w", err)
	}
	build, err := client.InspectBuild(cfg.Project(), cfg.Service(), buildID)
	if err != nil {
		return err
	}
	return buildOutcome(build)
}

// watchBuildPlain streams the build logs line by line without any cursor
// movement, for when stdout is not a terminal (CI, pipes, redirects).
func watchBuildPlain(client *api.Client, cfg config.ServiceConfig, buildID string) error {
	if err := client.StreamBuildLogs(cfg.Project(), cfg.Service(), buildID, func(le api.LogEntry) {
		fmt.Println(formatLogEntry(&le, 0))
	}); err != nil {
		return fmt.Errorf("streaming build logs: %w", err)
	}
	build, err := client.InspectBuild(cfg.Project(), cfg.Service(), buildID)
	if err != nil {
		return err
	}
	switch build.Status {
	case "done":
		fmt.Println("Build has succeeded.")
	case "failed":
		fmt.Println("Build has failed.")
	default:
		fmt.Printf("Build is %s.\n", build.Status)
	}
	return buildOutcome(build)
}

// buildOutcome turns a failed build into an error, so the exit code tells.
func buildOutcome(build *api.Build) error {
	if build.Status == "failed" {
		return newError(kindFailed, "build %s failed%s", build.ID, detail(build.Err))
	}
	return nil
}

var buildInspectCmd = &cobra.Command{
	Use:   "inspect [prefix]",
	Short: "Inspect the first matched task with the given ID prefix.",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &buildService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return inspectBuild(client, cfg, args[0])
	}),
}

var buildStatusCmd = &cobra.Command{
	Use:   "status [buildid]",
	Short: "Show the status of the given build.",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &buildService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return showBuildStatusAndExit(client, cfg, args[0])
	}),
}

func listBuilds(client *api.Client, cfg config.ServiceConfig, id string) error {
	builds, err := client.ListBuilds(cfg.Project(), cfg.Service(), id)
	if err != nil {
		return err
	}
	// Sort by date
	sort.Slice(builds, func(i, j int) bool {
		return builds[i].CreatedAt.Before(builds[j].CreatedAt)
	})
	return result(builds, func(w io.Writer) error {
		tw := ansiterm.NewTabWriter(w, 6, 0, 1, ' ', 0)
		fmt.Fprintln(tw, "ID\tSTATUS\tCREATED")
		for _, b := range builds {
			fmt.Fprintln(tw, strings.Join([]string{
				b.ID,
				colorize(b.Status),
				humanize.Time(b.CreatedAt),
			}, "\t"))
		}
		tw.Flush()
		return nil
	})
}

func showBuildStatusAndExit(client *api.Client, cfg config.ServiceConfig, id string) error {
	build, err := client.InspectBuild(cfg.Project(), cfg.Service(), id)
	if err != nil {
		return err
	}
	result(build, func(w io.Writer) error {
		_, err := fmt.Fprintln(w, colorize(build.Status))
		return err
	})
	os.Exit(statusToExitCode(build.Status))
	return nil
}

func inspectBuild(client *api.Client, cfg config.ServiceConfig, id string) error {
	build, err := client.InspectBuild(cfg.Project(), cfg.Service(), id)
	if err != nil {
		return err
	}
	return result(build, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
		fmt.Fprintln(tw, "ID:\t", build.ID)
		fmt.Fprintln(tw, "Constructor:\t", build.Constructor)
		fmt.Fprintln(tw, "CreatedAt:\t", build.CreatedAt)
		fmt.Fprintln(tw, "Status:\t", colorize(build.Status))
		fmt.Fprintln(tw, "Flags:\t", build.Flags)
		fmt.Fprintln(tw, "Owner:\t", build.Owner)
		if build.Err != "" {
			fmt.Fprintln(tw, "Err:\t", build.Err)
		}
		tw.Flush()
		return nil
	})
}

// deployBuild deploys a build, or without one redeploys the active build with
// the current environment.
func deployBuild(client *api.Client, cfg *config.ValidatedServiceConfig, id string, w io.Writer) error {
	hasEnvAPI, err := client.HasEnvAPI(cfg.Project(), cfg.Service())
	if err != nil {
		return err
	}
	deployReq := api.DeployRequest{Build: id}
	switch {
	case hasEnvAPI:
		warnInlineEnvironment(cfg.Unwrap())
	case id == "":
		return fmt.Errorf("deploying without a build needs Valar platform %s or newer with the environment API enabled", api.EnvMinPlatform)
	default:
		deployReq.Environment = inlineList(cfg.Deployment().Environment)
	}
	deployment, err := client.SubmitDeploy(cfg.Project(), cfg.Service(), &deployReq)
	if err != nil {
		return err
	}
	return emit(w, deployment, func(w io.Writer) error {
		_, err := fmt.Fprintln(w, deployment.Version)
		return err
	})
}

// newBuildRequest carries the inline lists of .valar.yml only for servers that
// do not store the environment themselves.
func newBuildRequest(cfg *config.ValidatedServiceConfig, artifact, kind string, skipDeploy, inline bool) *api.BuildRequest {
	var buildReq api.BuildRequest
	buildReq.Artifact = artifact
	buildReq.Kind = kind
	buildReq.Build.Constructor = cfg.Build().Constructor
	buildReq.Deployment.Skip = skipDeploy
	if inline {
		buildReq.Build.Environment = inlineList(cfg.Build().Environment)
		buildReq.Deployment.Environment = inlineList(cfg.Deployment().Environment)
	}
	return &buildReq
}

func inlineList(list []config.EnvironmentConfig) []api.KVPair {
	var kvs []api.KVPair
	for _, kv := range list {
		kvs = append(kvs, api.KVPair(kv))
	}
	return kvs
}

func statusToExitCode(status string) int {
	switch status {
	case "done":
		return 0
	default:
		return 1
	}
}

func colorize(status string) string {
	switch status {
	case "scheduled", "waiting", "pending":
		return color.HiYellowString("%s", status)
	case "building", "releasing", "binding", "running":
		return color.YellowString("%s", status)
	case "done", "succeeded", "enabled":
		return color.GreenString("%s", status)
	case "failed", "disabled":
		return color.RedString("%s", status)
	default:
		return status
	}
}

var (
	buildPushNoDeploy bool
	buildPushWait     bool
	buildPushTimeout  time.Duration
)

var buildPushCmd = &cobra.Command{
	Use:   "push folder",
	Short: "Push and build a new version.",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		serviceCfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &buildService, globalConfiguration)
		if err != nil {
			return err
		}
		folder, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("locating working directory: %w", err)
		}
		if len(args) != 0 {
			folder = args[0]
		}
		// Upload archive artifact
		archivePath, err := util.CompressDir(folder, serviceCfg.Build().Ignore)
		if err != nil {
			return fmt.Errorf("package compression failed: %w", err)
		}
		defer os.Remove(archivePath)
		targzFile, err := os.Open(archivePath)
		if err != nil {
			return fmt.Errorf("package archive failed: %w", err)
		}
		defer targzFile.Close()
		if err := validateServiceKind(buildPushKind); err != nil {
			return err
		}
		constructor := serviceCfg.Build().Constructor
		if constructor == "" {
			if constructor, err = detectConstructor(folder, os.Stderr); err != nil {
				return err
			}
		}
		deploy := !buildPushNoDeploy && !skipsDeploy(serviceCfg)
		build, err := submitPush(client, serviceCfg, targzFile, buildPushKind, constructor, deploy)
		if err != nil {
			return err
		}
		if err := result(build, func(w io.Writer) error {
			_, err := fmt.Fprintln(w, build.ID)
			return err
		}); err != nil {
			return err
		}
		if !buildPushWait {
			return nil
		}
		return waitForPush(client, serviceCfg.Project(), serviceCfg.Service(), build.ID, deploy, buildPushTimeout, os.Stderr)
	}),
}

func skipsDeploy(cfg *config.ValidatedServiceConfig) bool {
	d := cfg.Unwrap().Deployment
	return d != nil && d.Skip
}

// submitPush uploads the archive and submits its build. A deploying push
// first applies the settings .valar.yml declares, after the upload, which
// creates a new service, and before the build, whose deployment pins them.
func submitPush(client *api.Client, cfg *config.ValidatedServiceConfig, archive io.ReadCloser, kind, constructor string, deploy bool) (*api.Build, error) {
	hasEnvAPI, err := client.HasEnvAPI(cfg.Project(), cfg.Service())
	if err != nil {
		return nil, err
	}
	if hasEnvAPI {
		warnInlineEnvironment(cfg.Unwrap())
	}
	artifact, err := client.SubmitArtifact(cfg.Project(), cfg.Service(), kind, archive)
	if err != nil {
		return nil, err
	}
	if deploy {
		if err := applyDeclaredSettings(client, cfg); err != nil {
			return nil, err
		}
	}
	// The kind is repeated here because the server reads an absent kind as
	// "function" and rejects a mismatch with 409.
	buildReq := newBuildRequest(cfg, artifact.Artifact, kind, !deploy, !hasEnvAPI)
	buildReq.Build.Constructor = constructor
	return client.SubmitBuild(cfg.Project(), cfg.Service(), buildReq)
}

func initBuildsCmd() {
	buildCmd.PersistentFlags().StringVarP(&buildService, "service", "s", "", "The service to act on, instead of the one in .valar.yml")
	buildLogsCmd.PersistentFlags().BoolVarP(&logsFollow, "follow", "f", false, "Follow the logs")
	buildLogsCmd.PersistentFlags().BoolVarP(&logsRaw, "raw", "r", false, "Dump the unformatted log content")
	buildPushCmd.Flags().BoolVar(&buildPushNoDeploy, "no-deploy", false, "Only build, skip deploy action")
	buildPushCmd.Flags().StringVar(&buildPushKind, "kind", "", `Workload type for a new service: "function" (default) or "batch"`)
	buildPushCmd.Flags().BoolVarP(&buildPushWait, "wait", "w", false, "Stream the build log to stderr, wait for the build and deployment, and fail if either fails")
	buildPushCmd.Flags().DurationVar(&buildPushTimeout, "timeout", 30*time.Minute, "How long --wait waits for the build and deployment together")
	buildCmd.AddCommand(buildListCmd, buildInspectCmd, buildLogsCmd, buildAbortCmd, buildStatusCmd, buildWatchCmd, buildPushCmd)
	rootCmd.AddCommand(buildCmd)
}
