package mode_diff

import (
	"fmt"

	"github.com/TheManticoreProject/Manticore/logger"

	"github.com/TheManticoreProject/manticore-ldapmonitor/config"
	"github.com/TheManticoreProject/manticore-ldapmonitor/directory"
)

// Run compares two readings taken by snapshot mode.
//
// Parameters:
//
//	cfg (config.Config): The configuration of the run.
//
// Returns:
//
//	An error if either file could not be read.
func Run(cfg config.Config) error {
	before, err := directory.ReadSnapshot(cfg.BeforeFile)
	if err != nil {
		return err
	}
	after, err := directory.ReadSnapshot(cfg.AfterFile)
	if err != nil {
		return err
	}

	logger.Print(fmt.Sprintf("[>] Comparing \x1b[94m%s\x1b[0m (\x1b[93m%d\x1b[0m objects, taken %s) with \x1b[94m%s\x1b[0m (\x1b[93m%d\x1b[0m objects, taken %s).",
		directory.FormatText(cfg.BeforeFile), len(before.Objects), before.TakenAt.Format("2006-01-02 15:04:05 UTC"),
		directory.FormatText(cfg.AfterFile), len(after.Objects), after.TakenAt.Format("2006-01-02 15:04:05 UTC")))

	// An object that one reading never looked at is absent from it, and absent is
	// what a deleted object looks like. Saying so beats reporting the whole
	// difference in scope as objects disappearing and leaving the operator to work
	// out why.
	for _, difference := range directory.ScopeMismatch(before, after) {
		logger.Warn(fmt.Sprintf("The two readings do not cover the same ground: %s. Objects that only one of them read will be reported as appearing or disappearing.", difference))
	}

	ignored := directory.IgnoredAttributes(cfg.Reporting.IgnoreUserLogon)
	changes := directory.Diff(before.Snapshot(), after.Snapshot(), ignored)

	logger.Print(fmt.Sprintf("[>] Changes (\x1b[93m%d\x1b[0m):", len(changes)))
	for _, change := range changes {
		directory.Render(change)
	}

	return nil
}
