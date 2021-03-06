package lint

import (
	"fmt"

	"github.com/docker-slim/docker-slim/internal/app/master/commands"

	"github.com/urfave/cli"
)

const (
	Name  = "lint"
	Usage = "Analyzes container instructions in Dockerfiles"
	Alias = "l"
)

var CLI = cli.Command{
	Name:    Name,
	Aliases: []string{Alias},
	Usage:   Usage,
	Flags: []cli.Flag{
		cflag(commands.FlagTarget),
		cflag(FlagTargetType),
		cflag(FlagSkipBuildContext),
		cflag(FlagBuildContextDir),
		cflag(FlagSkipDockerignore),
		cflag(FlagIncludeCheckLabel),
		cflag(FlagExcludeCheckLabel),
		cflag(FlagIncludeCheckID),
		cflag(FlagIncludeCheckIDFile),
		cflag(FlagExcludeCheckID),
		cflag(FlagExcludeCheckIDFile),
		cflag(FlagShowNoHits),
		cflag(FlagShowSnippet),
		cflag(FlagListChecks),
	},
	Action: func(ctx *cli.Context) error {
		commands.ShowCommunityInfo()
		doListChecks := ctx.Bool(FlagListChecks)

		targetRef := ctx.String(commands.FlagTarget)
		if !doListChecks {
			if targetRef == "" {
				if len(ctx.Args()) < 1 {
					fmt.Printf("docker-slim[%s]: missing target image/Dockerfile...\n\n", Name)
					cli.ShowCommandHelp(ctx, Name)
					return nil
				} else {
					targetRef = ctx.Args().First()
				}
			}
		}

		gcvalues, err := commands.GlobalCommandFlagValues(ctx)
		if err != nil {
			return err
		}

		targetType := ctx.String(FlagTargetType)
		doSkipBuildContext := ctx.Bool(FlagSkipBuildContext)
		buildContextDir := ctx.String(FlagBuildContextDir)
		doSkipDockerignore := ctx.Bool(FlagSkipDockerignore)

		includeCheckLabels, err := commands.ParseCheckTags(ctx.StringSlice(FlagIncludeCheckLabel))
		if err != nil {
			fmt.Printf("docker-slim[%s]: invalid include check labels: %v\n", Name, err)
			return err
		}

		excludeCheckLabels, err := commands.ParseCheckTags(ctx.StringSlice(FlagExcludeCheckLabel))
		if err != nil {
			fmt.Printf("docker-slim[%s]: invalid exclude check labels: %v\n", Name, err)
			return err
		}

		includeCheckIDs, err := commands.ParseTokenSet(ctx.StringSlice(FlagIncludeCheckID))
		if err != nil {
			fmt.Printf("docker-slim[%s]: invalid include check IDs: %v\n", Name, err)
			return err
		}

		includeCheckIDFile := ctx.String(FlagIncludeCheckIDFile)
		moreIncludeCheckIDs, err := commands.ParseTokenSetFile(includeCheckIDFile)
		if err != nil {
			fmt.Printf("docker-slim[%s]: invalid include check IDs from file(%v): %v\n", Name, includeCheckIDFile, err)
			return err
		}

		for k, v := range moreIncludeCheckIDs {
			includeCheckIDs[k] = v
		}

		excludeCheckIDs, err := commands.ParseTokenSet(ctx.StringSlice(FlagExcludeCheckID))
		if err != nil {
			fmt.Printf("docker-slim[%s]: invalid exclude check IDs: %v\n", Name, err)
			return err
		}

		excludeCheckIDFile := ctx.String(FlagExcludeCheckIDFile)
		moreExcludeCheckIDs, err := commands.ParseTokenSetFile(excludeCheckIDFile)
		if err != nil {
			fmt.Printf("docker-slim[%s]: invalid exclude check IDs from file(%v): %v\n", Name, excludeCheckIDFile, err)
			return err
		}

		for k, v := range moreExcludeCheckIDs {
			excludeCheckIDs[k] = v
		}

		doShowNoHits := ctx.Bool(FlagShowNoHits)
		doShowSnippet := ctx.Bool(FlagShowSnippet)

		xc := commands.NewExecutionContext(Name)

		OnCommand(
			xc,
			gcvalues,
			targetRef,
			targetType,
			doSkipBuildContext,
			buildContextDir,
			doSkipDockerignore,
			includeCheckLabels,
			excludeCheckLabels,
			includeCheckIDs,
			excludeCheckIDs,
			doShowNoHits,
			doShowSnippet,
			doListChecks)
		commands.ShowCommunityInfo()
		return nil
	},
}
